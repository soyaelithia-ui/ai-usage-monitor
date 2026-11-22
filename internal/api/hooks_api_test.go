package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/config"
)

func TestHooksHTTPAndOverviewAPI(t *testing.T) {
	store, srv, now, ingestKey, cleanup := newIngestTestServer(t)
	defer cleanup()

	// Configure real-time quotas on the server
	srv.Cfg.Quotas = config.QuotasConfig{
		Window:  "24h",
		Default: config.QuotaLimit{USD: "10.00"},
		Agents: map[string]config.QuotaLimit{
			"antigravity": {USD: "5.00", Window: "24h"},
			"codex":       {USD: "4.00", Window: "24h"},
		},
	}

	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	// 1. Unauthenticated GET /api/hooks (with browser UA) must fail closed with 401
	unauthReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/hooks", nil)
	unauthReq.Header.Set("User-Agent", "Mozilla/5.0")
	unauthResp, err := http.DefaultClient.Do(unauthReq)
	if err != nil {
		t.Fatalf("GET /api/hooks: %v", err)
	}
	unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated GET /api/hooks, got %d", unauthResp.StatusCode)
	}

	// 2. Unauthorized POST /api/hooks/antigravity must return 403
	unauthHook, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/hooks/antigravity", strings.NewReader(`{}`))
	unauthHook.Header.Set("Content-Type", "application/json")
	unauthHookResp, _ := http.DefaultClient.Do(unauthHook)
	unauthHookResp.Body.Close()
	if unauthHookResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for unauthorized POST /api/hooks/antigravity, got %d", unauthHookResp.StatusCode)
	}

	// 3. Ingest an Antigravity event with inline rate ($2.50 total)
	ingestBody, _ := json.Marshal(map[string]any{
		"schema":                "usage-monitor.usage.v1",
		"semantics":             "input_includes_cache;output_includes_reasoning",
		"source":                "antigravity",
		"provider":              "google",
		"model":                 "gemini-5-hook-test",
		"responseId":            "ag-hook-1",
		"sessionId":             "conv-hook-1",
		"occurredAt":            now.Format(time.RFC3339),
		"inputTokens":           "1000000",
		"outputTokens":          "0",
		"cachedInputTokens":     "0",
		"cacheWriteInputTokens": "0",
		"rate": map[string]any{
			"input":  "2.5",
			"output": "10",
		},
	})
	ingReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/ingest", bytes.NewReader(ingestBody))
	ingReq.Header.Set("Authorization", "Bearer "+ingestKey)
	ingReq.Header.Set("Content-Type", "application/json")
	ingResp, _ := http.DefaultClient.Do(ingReq)
	ingResp.Body.Close()
	if ingResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on ingest, got %d", ingResp.StatusCode)
	}

	// 4. Call POST /api/hooks/antigravity with Bearer INGEST_KEY and verify native user_markdown response
	agHookReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/hooks/antigravity", strings.NewReader(`{"conversationId":"conv-hook-1","hookEventName":"PostInvocation"}`))
	agHookReq.Header.Set("Authorization", "Bearer "+ingestKey)
	agHookReq.Header.Set("Content-Type", "application/json")
	agHookResp, err := http.DefaultClient.Do(agHookReq)
	if err != nil {
		t.Fatalf("POST /api/hooks/antigravity: %v", err)
	}
	var agOut map[string]any
	_ = json.NewDecoder(agHookResp.Body).Decode(&agOut)
	agHookResp.Body.Close()
	if agHookResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for antigravity hook, got %d", agHookResp.StatusCode)
	}
	steps := agOut["injectSteps"].([]any)
	sm := steps[0].(map[string]any)["systemMessage"].(map[string]any)
	if sm["systemMessage"] != "" {
		t.Fatalf("expected empty model systemMessage, got %v", sm["systemMessage"])
	}
	md := sm["metadata"].(map[string]any)["user_markdown"].(string)
	if !strings.Contains(md, "$2.50") || !strings.Contains(md, "$5.00") || !strings.Contains(md, "50.0%") {
		t.Fatalf("unexpected antigravity user_markdown: %q", md)
	}

	// 5. Call POST /api/hooks/codex and verify native non-blocking systemMessage
	codexReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/hooks/codex", strings.NewReader(`{"session_id":"cx-1","hook_event_name":"Stop"}`))
	codexReq.Header.Set("Authorization", "Bearer "+ingestKey)
	codexReq.Header.Set("Content-Type", "application/json")
	codexResp, _ := http.DefaultClient.Do(codexReq)
	var codexOut map[string]any
	_ = json.NewDecoder(codexResp.Body).Decode(&codexOut)
	codexResp.Body.Close()
	if codexResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for codex hook, got %d", codexResp.StatusCode)
	}
	if msg, _ := codexOut["systemMessage"].(string); !strings.Contains(msg, "$4.00") {
		t.Fatalf("expected codex quota in systemMessage, got %+v", codexOut)
	}
	if _, hasDecision := codexOut["decision"]; hasDecision {
		t.Fatalf("codex Stop hook must not set blocking decision: %+v", codexOut)
	}

	// 6. Authenticated browser session GET /api/hooks returns integrations + quotas without leaking home path
	issued, _ := store.Issue()
	exBody, _ := json.Marshal(map[string]string{"token": issued.Token})
	exReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/auth/exchange", bytes.NewReader(exBody))
	exReq.Header.Set("User-Agent", "Mozilla/5.0")
	exReq.Header.Set("Origin", "http://127.0.0.1:8080")
	exReq.Header.Set("Content-Type", "application/json")
	exResp, _ := http.DefaultClient.Do(exReq)
	var sessCookie *http.Cookie
	for _, c := range exResp.Cookies() {
		if c.Name == SessionCookieName {
			sessCookie = c
		}
	}
	exResp.Body.Close()

	hooksReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/hooks", nil)
	hooksReq.Header.Set("User-Agent", "Mozilla/5.0")
	hooksReq.AddCookie(sessCookie)
	hooksResp, _ := http.DefaultClient.Do(hooksReq)
	var rawBuf bytes.Buffer
	_, _ = rawBuf.ReadFrom(hooksResp.Body)
	hooksResp.Body.Close()
	if hooksResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on authenticated GET /api/hooks, got %d", hooksResp.StatusCode)
	}
	if home, _ := os.UserHomeDir(); home != "" && strings.Contains(rawBuf.String(), home) {
		t.Fatalf("GET /api/hooks leaked local home directory path %q", home)
	}
	var overview map[string]any
	if err := json.Unmarshal(rawBuf.Bytes(), &overview); err != nil {
		t.Fatalf("invalid JSON from /api/hooks: %v", err)
	}
	if len(overview["integrations"].([]any)) < 7 {
		t.Fatalf("expected at least 7 native hook integrations, got %d", len(overview["integrations"].([]any)))
	}
}
