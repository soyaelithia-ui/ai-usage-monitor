package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/config"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/pricing"
)

func TestEvaluateQuotaExactDecimalAndBigInt(t *testing.T) {
	// 1. Unconfigured
	q0 := EvaluateQuota("1.25", "5000", config.QuotaLimit{}, "24h")
	if q0.State != "unconfigured" || q0.Window != "24h" || q0.UsedUSD != "1.25" || q0.UsedTokens != "5000" {
		t.Fatalf("unexpected unconfigured quota: %+v", q0)
	}

	// 2. OK (exact decimal 0.1 + 0.2 = 0.3 out of 1.0 -> 30.0%, remaining 0.7)
	q1 := EvaluateQuota("0.3", "250000", config.QuotaLimit{USD: "1.0", Tokens: "1000000", Window: "24h"}, "24h")
	if q1.State != "ok" || q1.PercentUSD != "30.0" || q1.RemainingUSD != "0.7" || q1.PercentTokens != "25.0" || q1.RemainingTokens != "750000" {
		t.Fatalf("unexpected ok quota: %+v", q1)
	}

	// 3. Warning at >= 80%
	q2 := EvaluateQuota("8.00", "0", config.QuotaLimit{USD: "10.00", Window: "7d"}, "24h")
	if q2.State != "warning" || q2.PercentUSD != "80.0" || q2.RemainingUSD != "2" || q2.Window != "7d" {
		t.Fatalf("unexpected warning quota: %+v", q2)
	}

	// 4. Exceeded >= 100% (remaining clamped to 0, huge big.Int tokens beyond JS MAX_SAFE_INTEGER)
	q3 := EvaluateQuota("12.50", "90071992547409950", config.QuotaLimit{USD: "10.00", Tokens: "90071992547409930"}, "24h")
	if q3.State != "exceeded" || q3.RemainingUSD != "0" || q3.PercentUSD != "125.0" || q3.RemainingTokens != "0" {
		t.Fatalf("unexpected exceeded quota: %+v", q3)
	}
}

func TestNativeAdaptersAreStrictlyNonInterrupting(t *testing.T) {
	now := time.Date(2026, 11, 22, 3, 0, 0, 0, time.UTC)
	store, err := db.Open(":memory:", "", func() time.Time { return now })
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer store.Close()

	// Register explicit exact rate so cost is deterministic
	if err := store.UpsertDynamicPricing("gemini-5-pro", pricing.ModelRate{
		Provider: "google",
		Input:    "2",
		Cached:   "0.2",
		Output:   "10",
	}); err != nil {
		t.Fatalf("UpsertDynamicPricing: %v", err)
	}
	if _, err := store.InsertEvent(pricing.RawEvent{
		Source:                "antigravity",
		Agent:                 "Antigravity",
		Provider:              "google",
		Model:                 "gemini-5-pro",
		ResponseID:            "resp-ag-1",
		SessionID:             "conv-123",
		OccurredAt:            now.Add(-2 * time.Minute).Format(time.RFC3339),
		InputTokens:           "500000",
		OutputTokens:          "50000",
		CachedInputTokens:     strPtr("0"),
		CacheWriteInputTokens: strPtr("0"),
	}); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	cfg := &config.Config{
		Quotas: config.QuotasConfig{
			Window:  "24h",
			Default: config.QuotaLimit{USD: "5.00"},
			Agents: map[string]config.QuotaLimit{
				"antigravity": {USD: "2.00", Window: "24h"},
			},
		},
	}

	snap := QuerySnapshot(store, cfg, QueryOptions{
		AgentID:   "antigravity",
		SessionID: "conv-123",
		Now:       now,
	})
	// Cost = 500k*2/1M ($1.00) + 50k*10/1M ($0.50) = $1.50 out of $2.00 -> 75.0%, remaining $0.50
	if snap.Agent.CostUSD != "1.5" || snap.AgentQuota.PercentUSD != "75.0" || snap.AgentQuota.RemainingUSD != "0.5" {
		t.Fatalf("unexpected snapshot math: cost=%s pct=%s rem=%s", snap.Agent.CostUSD, snap.AgentQuota.PercentUSD, snap.AgentQuota.RemainingUSD)
	}

	// 1. Antigravity PostInvocation -> injectSteps with metadata.user_markdown & empty systemMessage (0 tokens to model)
	agBytes, isJSON := BuildResponse("antigravity", "PostInvocation", snap, "native", true)
	if !isJSON {
		t.Fatal("expected JSON for antigravity")
	}
	var agOut map[string]any
	if err := json.Unmarshal(agBytes, &agOut); err != nil {
		t.Fatalf("unmarshal antigravity: %v", err)
	}
	if _, hasTerm := agOut["terminationBehavior"]; hasTerm {
		t.Fatalf("antigravity must never set terminationBehavior: %s", string(agBytes))
	}
	steps := agOut["injectSteps"].([]any)
	sm := steps[0].(map[string]any)["systemMessage"].(map[string]any)
	if sm["systemMessage"] != "" {
		t.Fatalf("antigravity systemMessage sent to model must be empty string, got %q", sm["systemMessage"])
	}
	md := sm["metadata"].(map[string]any)["user_markdown"].(string)
	if !strings.Contains(md, "$1.50") || !strings.Contains(md, "$2.00") || !strings.Contains(md, "75.0%") {
		t.Fatalf("unexpected antigravity markdown: %q", md)
	}

	// Antigravity PostToolUse -> {} and Stop -> {"decision":"allow"} (never "continue")
	if b, _ := BuildResponse("antigravity", "PostToolUse", snap, "native", true); string(b) != "{}" {
		t.Fatalf("expected {} for antigravity PostToolUse, got %s", string(b))
	}
	stopB, _ := BuildResponse("antigravity", "Stop", snap, "native", true)
	if strings.Contains(string(stopB), `"continue"`) {
		t.Fatalf("antigravity Stop hook must never force continue: %s", string(stopB))
	}

	// 2. Verify all JSON agent contracts never block, deny, terminate, or set continue:false
	cases := []struct {
		agent string
		event string
		key   string
	}{
		{"codex", "Stop", "systemMessage"},
		{"codex", "SessionStart", "systemMessage"},
		{"claude", "Stop", "systemMessage"},
		{"gemini", "AfterAgent", "systemMessage"},
		{"cursor", "afterAgentResponse", "user_message"},
		{"cursor", "stop", "user_message"},
		{"copilot", "sessionEnd", "systemMessage"},
		{"generic", "any", "systemMessage"},
	}
	for _, tc := range cases {
		raw, isJSON := BuildResponse(tc.agent, tc.event, snap, "native", true)
		if !isJSON {
			t.Fatalf("%s/%s: expected JSON", tc.agent, tc.event)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("%s/%s invalid JSON: %v", tc.agent, tc.event, err)
		}
		msg, _ := out[tc.key].(string)
		if msg == "" {
			t.Fatalf("%s/%s missing expected display key %q in %s", tc.agent, tc.event, tc.key, string(raw))
		}
		if dec, _ := out["decision"].(string); dec == "block" || dec == "deny" {
			t.Fatalf("%s/%s must never block/deny: %s", tc.agent, tc.event, string(raw))
		}
		if cont, exists := out["continue"]; exists && cont == false {
			t.Fatalf("%s/%s must never set continue:false: %s", tc.agent, tc.event, string(raw))
		}
		if _, hasAgentMsg := out["agent_message"]; hasAgentMsg {
			t.Fatalf("%s/%s must not inject agent_message into model context", tc.agent, tc.event)
		}
		if _, hasFollow := out["followup_message"]; hasFollow {
			t.Fatalf("%s/%s must not inject followup_message", tc.agent, tc.event)
		}
	}

	// 3. Claude StatusLine -> compact single-line plain text (no JSON, no newlines)
	snap.ContextUsedPct = "42"
	slBytes, isJSON := BuildResponse("claude-statusline", "", snap, "native", true)
	if isJSON {
		t.Fatal("expected plain text for claude-statusline")
	}
	sl := string(slBytes)
	if strings.Contains(sl, "\n") || !strings.Contains(sl, "ctx 42%") || !strings.Contains(sl, "$1.50") {
		t.Fatalf("unexpected claude-statusline output: %q", sl)
	}
}

func TestParseInputAutoDetectAndFailOpen(t *testing.T) {
	// UTF-8 BOM + Antigravity payload
	agRaw := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"conversationId":"c-1","hookEventName":"PostInvocation","modelName":"gemini-3-pro"}`)...)
	in := ParseInput(agRaw)
	if in.DetectedAgent != "antigravity" || in.SessionID != "c-1" || in.EventName != "PostInvocation" || in.Model != "gemini-3-pro" {
		t.Fatalf("unexpected antigravity parse: %+v", in)
	}

	// Codex payload
	codexIn := ParseInput([]byte(`{"session_id":"s-9","turn_id":"t-1","hook_event_name":"Stop","model":"gpt-5.4","transcript_path":"/home/u/.codex/sessions/a.jsonl"}`))
	if codexIn.DetectedAgent != "codex" || codexIn.SessionID != "s-9" || codexIn.EventName != "Stop" {
		t.Fatalf("unexpected codex parse: %+v", codexIn)
	}

	// Claude statusLine payload
	slIn := ParseInput([]byte(`{"session_id":"cl-1","model":{"display_name":"Claude Sonnet"},"context_window":{"used_percentage":37},"cost":{"total_cost_usd":0.12}}`))
	if slIn.DetectedAgent != "claude-statusline" || slIn.ContextUsedPct != "37" || slIn.SessionCostHint != "0.12" {
		t.Fatalf("unexpected claude-statusline parse: %+v", slIn)
	}

	// Malformed stdin must fail open gracefully
	badIn := ParseInput([]byte(`{not valid json`))
	if badIn.DetectedAgent != "generic" {
		t.Fatalf("expected fail-open generic on malformed stdin, got %+v", badIn)
	}
}

func TestInstallPreservesExistingHooksAndUninstallsCleanly(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	bin := "/usr/local/bin/ai-usage-monitor"

	// Seed existing Antigravity hook to ensure non-destructive merge
	agPath := filepath.Join(cwd, ".agents", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(agPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agPath, []byte(`{"my-custom-lint":{"Stop":[{"type":"command","command":"echo ok"}]}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	agents := []string{"antigravity", "codex", "claude", "claude-statusline", "gemini", "cursor", "copilot"}
	for _, ag := range agents {
		res, err := Install(ag, ScopeWorkspace, cwd, home, bin, false)
		if err != nil {
			t.Fatalf("Install(%s): %v", ag, err)
		}
		if !res.Changed {
			t.Fatalf("Install(%s): expected changed=true on first install", ag)
		}
		if !isInstalledAtPath(ag, res.Path) {
			t.Fatalf("Install(%s): expected isInstalledAtPath=true", ag)
		}
		// Second install must be idempotent
		res2, err := Install(ag, ScopeWorkspace, cwd, home, bin, false)
		if err != nil {
			t.Fatalf("re-Install(%s): %v", ag, err)
		}
		if res2.Changed {
			t.Fatalf("re-Install(%s): expected idempotent changed=false", ag)
		}
	}

	// Verify existing user hook in Antigravity was preserved
	rawAg, err := os.ReadFile(agPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawAg), `"my-custom-lint"`) || !strings.Contains(string(rawAg), `"ai-usage-monitor"`) {
		t.Fatalf("existing user hook lost during merge: %s", string(rawAg))
	}

	// Uninstall all and verify clean removal while preserving user hook
	for _, ag := range agents {
		res, err := Uninstall(ag, ScopeWorkspace, cwd, home, false)
		if err != nil {
			t.Fatalf("Uninstall(%s): %v", ag, err)
		}
		if !res.Changed {
			t.Fatalf("Uninstall(%s): expected changed=true", ag)
		}
		if isInstalledAtPath(ag, res.Path) {
			t.Fatalf("Uninstall(%s): expected installed=false after uninstall", ag)
		}
	}

	rawAgAfter, err := os.ReadFile(agPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawAgAfter), `"my-custom-lint"`) || strings.Contains(string(rawAgAfter), `"ai-usage-monitor"`) {
		t.Fatalf("unexpected antigravity hooks.json after uninstall: %s", string(rawAgAfter))
	}

	// Refuse symlinks on install
	symTarget := filepath.Join(cwd, "real.json")
	_ = os.WriteFile(symTarget, []byte("{}"), 0o600)
	codexDir := filepath.Join(home, ".codex")
	_ = os.MkdirAll(codexDir, 0o700)
	_ = os.Symlink(symTarget, filepath.Join(codexDir, "hooks.json"))
	if _, err := Install("codex", ScopeGlobal, cwd, home, bin, false); err == nil {
		t.Fatal("expected symlink refusal error on Install")
	}
}

func strPtr(s string) *string { return &s }
