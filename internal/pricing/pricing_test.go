package pricing

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func strp(s string) *string { return &s }

func sampleRaw(overrides ...func(*RawEvent)) RawEvent {
	r := RawEvent{
		Source:                "codex",
		Provider:              "openai",
		Agent:                 "Codex",
		Model:                 "gpt-6-astra",
		ResponseID:            "r1",
		OccurredAt:            "2026-10-29T06:00:00.000Z",
		InputTokens:           "100",
		OutputTokens:          "20",
		CachedInputTokens:     strp("40"),
		CacheWriteInputTokens: strp("10"),
		ReasoningOutputTokens: strp("5"),
	}
	for _, fn := range overrides {
		fn(&r)
	}
	return r
}

func TestExactDecimalAndHugeIntegers(t *testing.T) {
	n, err := Normalize(sampleRaw())
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if n.TotalTokens != "120" {
		t.Fatalf("TotalTokens = %s, want 120", n.TotalTokens)
	}
	est := Estimate(n, nil)
	if est.CostUSD == nil || *est.CostUSD != "0.001665" {
		t.Fatalf("CostUSD = %v, want 0.001665", est.CostUSD)
	}
	if est.CostInputUSD == nil || *est.CostInputUSD != "0.0005" {
		t.Fatalf("CostInputUSD = %v, want 0.0005", est.CostInputUSD)
	}

	// Huge integer exceeding int64 and JS MAX_SAFE_INTEGER
	bigRaw := sampleRaw(func(r *RawEvent) {
		r.InputTokens = "9007199254740993123456789"
		r.CachedInputTokens = strp("0")
		r.CacheWriteInputTokens = strp("0")
	})
	bigNorm, err := Normalize(bigRaw)
	if err != nil {
		t.Fatalf("Normalize big: %v", err)
	}
	if bigNorm.TotalTokens != "9007199254740993123456809" {
		t.Fatalf("big TotalTokens = %s", bigNorm.TotalTokens)
	}
	bigEst := Estimate(bigNorm, nil)
	if bigEst.CostInputUSD == nil || *bigEst.CostInputUSD != "180143985094819862469.13578" {
		t.Fatalf("big CostInputUSD = %v, want 180143985094819862469.13578", bigEst.CostInputUSD)
	}

	// 12h cache write on gpt-6-astra
	n12h, err := Normalize(sampleRaw(func(r *RawEvent) {
		r.CacheWriteDuration = strp("12h")
	}))
	if err != nil {
		t.Fatalf("Normalize 12h: %v", err)
	}
	est12h := Estimate(n12h, nil)
	if est12h.CostUSD == nil || *est12h.CostUSD != "0.00179" {
		t.Fatalf("12h CostUSD = %v, want 0.00179", est12h.CostUSD)
	}

	// Long context tier (>272000)
	lcNorm, err := Normalize(sampleRaw(func(r *RawEvent) {
		r.InputTokens = "272001"
		r.CachedInputTokens = strp("0")
		r.CacheWriteInputTokens = strp("0")
		r.OutputTokens = "2"
		r.ReasoningOutputTokens = strp("0")
	}))
	if err != nil {
		t.Fatalf("Normalize lc: %v", err)
	}
	lcEst := Estimate(lcNorm, nil)
	if lcEst.CostUSD == nil || *lcEst.CostUSD != "5.44017" {
		t.Fatalf("longContext CostUSD = %v, want 5.44017", lcEst.CostUSD)
	}
}

func TestLookupRatePrefersDynamicThenCatalog(t *testing.T) {
	dyn := map[string]ModelRate{
		"gpt-6-astra": {Provider: "openai", Input: "99", Output: "99"},
	}
	rate, ok := LookupRate("gpt-6-astra", dyn)
	if !ok || rate.Input != "99" {
		t.Fatalf("dynamic rate must win: got %+v ok=%v", rate, ok)
	}

	rate, ok = LookupRate("gemini-4-argon", nil)
	if !ok || rate.Input != "2" {
		t.Fatalf("catalog fallback: got %+v ok=%v", rate, ok)
	}

	if rate, ok = LookupRate("never-registered", map[string]ModelRate{}); ok {
		t.Fatalf("unknown model must not resolve, got %+v", rate)
	}
}

func TestNormalizeModelAndProvidersCompatible(t *testing.T) {
	cases := []struct {
		raw          string
		wantCanon    string
		wantProvider string
		wantFree     bool
	}{
		{"openrouter/anthropic/claude-sonnet-4.6", "claude-sonnet-4-6", "anthropic", false},
		{"us.anthropic.claude-opus-4-6-20260205-v1:0", "claude-opus-4-6", "anthropic", false},
		{"openai/gpt-6.2-astra-2026-10-15", "gpt-6.2-astra", "openai", false},
		{"google/gemini-4-carbon-latest", "gemini-4-carbon-latest", "google", false},
		{"openrouter/z-ai/glm-4.7-flash:free", "glm-4.7-flash:free", "zai", true},
		{"vertex_ai/google/gemini-3.8-flash-preview", "gemini-3.8-flash-preview", "google", false},
		{"mistralai/mistral-large-3-2512", "mistral-large-3-2512", "mistral", false},
	}
	for _, tc := range cases {
		nm := NormalizeModel(tc.raw)
		if nm.Canonical != tc.wantCanon {
			t.Errorf("NormalizeModel(%q).Canonical = %q, want %q", tc.raw, nm.Canonical, tc.wantCanon)
		}
		if nm.ProviderHint != tc.wantProvider {
			t.Errorf("NormalizeModel(%q).ProviderHint = %q, want %q", tc.raw, nm.ProviderHint, tc.wantProvider)
		}
		if nm.IsFreeVariant != tc.wantFree {
			t.Errorf("NormalizeModel(%q).IsFreeVariant = %v, want %v", tc.raw, nm.IsFreeVariant, tc.wantFree)
		}
	}

	// Trailing aliases (-latest, -preview, 4-digit YYMM) resolve via Candidates without clobbering step-5-preview
	if rate, ok := LookupRate("google/gemini-4-carbon-latest", nil); !ok || rate.Input != "2" {
		t.Fatalf("latest alias lookup failed: ok=%v rate=%+v", ok, rate)
	}
	if rate, ok := LookupRate("vertex_ai/google/gemini-3.8-flash-preview", nil); !ok || rate.Input != "0.75" {
		t.Fatalf("preview alias lookup failed: ok=%v rate=%+v", ok, rate)
	}
	if rate, ok := LookupRate("mistralai/mistral-large-3-2512", nil); !ok || rate.Input != "0.50" {
		t.Fatalf("4-digit date suffix lookup failed: ok=%v rate=%+v", ok, rate)
	}
	if rate, ok := LookupRate("step-5-preview", nil); !ok || rate.Input != "1" {
		t.Fatalf("step-5-preview exact lookup failed: ok=%v rate=%+v", ok, rate)
	}

	// Aggregator routing with matching provider prefix succeeds; mismatched fails closed
	nRoute, err := Normalize(sampleRaw(func(r *RawEvent) {
		r.Provider = "openrouter"
		r.Model = "openrouter/anthropic/claude-sonnet-4.6"
		r.CacheWriteInputTokens = strp("0")
		r.CachedInputTokens = strp("0")
	}))
	if err != nil {
		t.Fatalf("Normalize route: %v", err)
	}
	estRoute := Estimate(nRoute, nil)
	if estRoute.CostUSD == nil || *estRoute.CostUSD != "0.0006" {
		t.Fatalf("openrouter anthropic claude-sonnet-4.6 cost = %v (%s), want 0.0006", estRoute.CostUSD, estRoute.CostNote)
	}

	// Bedrock route should resolve claude-opus-4-6
	if rate, ok := LookupRate("us.anthropic.claude-opus-4-6-20260205-v1:0", nil); !ok || rate.Input != "5" {
		t.Fatalf("Bedrock lookup failed: ok=%v rate=%+v", ok, rate)
	}

	if ProvidersCompatible("openai", "anthropic", "") {
		t.Fatal("openai and anthropic must not be compatible")
	}
	if !ProvidersCompatible("zai", "z-ai", "") {
		t.Fatal("zai and z-ai alias must be compatible")
	}
	if ProvidersCompatible("openai", "openrouter", "anthropic") {
		t.Fatal("aggregator with anthropic model prefix must not match openai rate")
	}
}

func TestNov2026RatesLongContextAndSchedule(t *testing.T) {
	// claude-haiku-5-5 > 100k tokens -> x5 input & output ($0.50 / $2.50)
	nHaiku, err := Normalize(sampleRaw(func(r *RawEvent) {
		r.Provider = "anthropic"
		r.Model = "claude-haiku-5-5"
		r.InputTokens = "200000"
		r.OutputTokens = "100000"
		r.CachedInputTokens = strp("0")
		r.CacheWriteInputTokens = strp("0")
		r.ReasoningOutputTokens = strp("0")
	}))
	if err != nil {
		t.Fatalf("Normalize haiku: %v", err)
	}
	estHaiku := Estimate(nHaiku, nil)
	// 200k * 0.10 * 5 / 1e6 = 0.10; 100k * 0.50 * 5 / 1e6 = 0.25 -> 0.35
	if estHaiku.CostUSD == nil || *estHaiku.CostUSD != "0.35" {
		t.Fatalf("claude-haiku-5-5 >100k cost = %v (%s), want 0.35", estHaiku.CostUSD, estHaiku.CostNote)
	}

	// grok-4.7 > 200k tokens -> x2 input & output
	nGrok, err := Normalize(sampleRaw(func(r *RawEvent) {
		r.Provider = "x-ai"
		r.Model = "grok-4.7"
		r.InputTokens = "250000"
		r.OutputTokens = "50000"
		r.CachedInputTokens = strp("0")
		r.CacheWriteInputTokens = strp("0")
		r.ReasoningOutputTokens = strp("0")
	}))
	if err != nil {
		t.Fatalf("Normalize grok: %v", err)
	}
	estGrok := Estimate(nGrok, nil)
	// 250k * 2 * 2 / 1e6 = 1.0; 50k * 6 * 2 / 1e6 = 0.6 -> 1.6
	if estGrok.CostUSD == nil || *estGrok.CostUSD != "1.6" {
		t.Fatalf("grok-4.7 >200k cost = %v (%s), want 1.6", estGrok.CostUSD, estGrok.CostNote)
	}

	// minimax-m3 > 512k tokens -> x2
	nMini, err := Normalize(sampleRaw(func(r *RawEvent) {
		r.Provider = "minimax"
		r.Model = "minimax-m3"
		r.InputTokens = "600000"
		r.OutputTokens = "100000"
		r.CachedInputTokens = strp("0")
		r.CacheWriteInputTokens = strp("0")
		r.ReasoningOutputTokens = strp("0")
	}))
	if err != nil {
		t.Fatalf("Normalize minimax: %v", err)
	}
	estMini := Estimate(nMini, nil)
	// 600k * 0.30 * 2 / 1e6 = 0.36; 100k * 1.20 * 2 / 1e6 = 0.24 -> 0.6
	if estMini.CostUSD == nil || *estMini.CostUSD != "0.6" {
		t.Fatalf("minimax-m3 >512k cost = %v (%s), want 0.6", estMini.CostUSD, estMini.CostNote)
	}

	// gemini-4-carbon scheduled transition on 2026-12-01T00:00:00Z ($2/$10 -> $4/$20)
	nGemIntro, _ := Normalize(sampleRaw(func(r *RawEvent) {
		r.Provider = "google"
		r.Model = "gemini-4-carbon"
		r.OccurredAt = "2026-11-19T12:00:00.000Z"
		r.InputTokens = "1000000"
		r.OutputTokens = "0"
		r.CachedInputTokens = strp("0")
		r.CacheWriteInputTokens = strp("0")
		r.ReasoningOutputTokens = strp("0")
	}))
	if est := Estimate(nGemIntro, nil); est.CostUSD == nil || *est.CostUSD != "2" {
		t.Fatalf("gemini-4-carbon intro cost = %v, want 2", est.CostUSD)
	}

	nGemStd, _ := Normalize(sampleRaw(func(r *RawEvent) {
		r.Provider = "google"
		r.Model = "gemini-4-carbon"
		r.OccurredAt = "2026-12-02T00:00:00.000Z"
		r.InputTokens = "1000000"
		r.OutputTokens = "0"
		r.CachedInputTokens = strp("0")
		r.CacheWriteInputTokens = strp("0")
		r.ReasoningOutputTokens = strp("0")
	}))
	if est := Estimate(nGemStd, nil); est.CostUSD == nil || *est.CostUSD != "4" {
		t.Fatalf("gemini-4-carbon post-Dec-1 cost = %v, want 4", est.CostUSD)
	}
}

func TestFailClosedPricingRules(t *testing.T) {
	// 1. Unknown model
	nUnknown, _ := Normalize(sampleRaw(func(r *RawEvent) { r.Model = "not-verified" }))
	if est := Estimate(nUnknown, nil); est.CostUSD != nil || est.CostNote != NoteUnknownRate {
		t.Fatalf("expected fail-closed unknownRate, got %+v", est)
	}

	// 2. Provider mismatch
	nMismatch, _ := Normalize(sampleRaw(func(r *RawEvent) { r.Provider = "anthropic" }))
	if est := Estimate(nMismatch, nil); est.CostUSD != nil || est.CostNote != NoteUnknownRate {
		t.Fatalf("expected fail-closed provider mismatch, got %+v", est)
	}

	// 3. Incomplete cache breakdown
	nInc, _ := Normalize(sampleRaw(func(r *RawEvent) { r.CachedInputTokens = nil }))
	if est := Estimate(nInc, nil); est.CostUSD != nil || est.CostNote != NoteIncompleteCache {
		t.Fatalf("expected fail-closed incompleteCache, got %+v", est)
	}

	// 4. Modality unverified (active gemini-2.5-flash-lite requires modality="text")
	nMod, _ := Normalize(sampleRaw(func(r *RawEvent) {
		r.Provider = "google"
		r.Model = "gemini-2.5-flash-lite"
		r.CachedInputTokens = strp("0")
		r.CacheWriteInputTokens = strp("0")
	}))
	if est := Estimate(nMod, nil); est.CostUSD != nil || est.CostNote != NoteModalityUnverified {
		t.Fatalf("expected fail-closed modalityUnverified, got %+v", est)
	}

	// 5. Missing cacheWrite12h must NEVER fall back to 30m rate
	n12hNoTier, _ := Normalize(sampleRaw(func(r *RawEvent) {
		r.Provider = "anthropic"
		r.Model = "claude-sonnet-4-6"
		r.CacheWriteDuration = strp("12h")
	}))
	if est := Estimate(n12hNoTier, nil); est.CostUSD != nil || est.CostNote != NoteCacheWriteUnavailable {
		t.Fatalf("expected fail-closed 12h cacheWriteUnavailable, got %+v", est)
	}

	// 6. Missing cacheWrite with >0 cache write tokens (active gemini-4-argon)
	nNoCW, _ := Normalize(sampleRaw(func(r *RawEvent) {
		r.Provider = "google"
		r.Model = "gemini-4-argon"
	}))
	if est := Estimate(nNoCW, nil); est.CostUSD != nil || est.CostNote != NoteCacheWriteUnavailable {
		t.Fatalf("expected fail-closed missing cacheWrite, got %+v", est)
	}

	// 7. Deprecated / retired models must fail closed unless overridden by dynamic agent rate
	for _, dep := range []struct {
		provider string
		model    string
	}{
		{"openai", "gpt-5"},
		{"openai", "o1"},
		{"openai", "o3-mini"},
		{"openai", "o4-mini"},
		{"google", "gemini-2.5-pro"},
		{"google", "gemini-2.5-flash"},
	} {
		nDep, _ := Normalize(sampleRaw(func(r *RawEvent) {
			r.Provider = dep.provider
			r.Model = dep.model
			r.CachedInputTokens = strp("0")
			r.CacheWriteInputTokens = strp("0")
		}))
		est := Estimate(nDep, nil)
		if est.CostUSD != nil || est.CostNote != NoteDeprecatedRate {
			t.Fatalf("deprecated model %s/%s must fail closed with NoteDeprecatedRate, got %+v", dep.provider, dep.model, est)
		}
		// Custom agent rate can explicitly price a deprecated model if declared
		dyn := map[string]ModelRate{
			dep.model: {Provider: dep.provider, Input: "1", Output: "2", Source: "agent"},
		}
		if estDyn := Estimate(nDep, dyn); estDyn.CostUSD == nil {
			t.Fatalf("agent-declared override for %s should compute cost, got %+v", dep.model, estDyn)
		}
	}
}

func TestNormalizeBreakdownBounds(t *testing.T) {
	if _, err := Normalize(sampleRaw(func(r *RawEvent) { r.ReasoningOutputTokens = strp("21") })); err != ErrInvalidBreakdown {
		t.Fatalf("expected ErrInvalidBreakdown for reasoning > output, got %v", err)
	}
	if _, err := Normalize(sampleRaw(func(r *RawEvent) { r.CachedInputTokens = strp("101") })); err != ErrInvalidBreakdown {
		t.Fatalf("expected ErrInvalidBreakdown for cached > input, got %v", err)
	}
}

func TestExternalCatalogResolverParsingAndDiskCache(t *testing.T) {
	modelsDevJSON := `{
		"openai": {
			"models": {
				"gpt-7-preview": {
					"cost": {"input": 3.25, "output": 12.5, "cache_read": 0.325, "cache_write": 4.0625}
				},
				"bad-negative": {
					"cost": {"input": -1, "output": 2}
				}
			}
		}
	}`
	litellmJSON := `{
		"deepseek/deepseek-v5-alpha": {
			"litellm_provider": "deepseek",
			"input_cost_per_token": 0.0000005,
			"output_cost_per_token": 0.000002,
			"cache_read_input_token_cost": 0.00000005
		}
	}`
	openrouterJSON := `{
		"data": [
			{
				"id": "mistralai/mistral-next-1",
				"pricing": {
					"prompt": "0.00000075",
					"completion": "0.00000225",
					"input_cache_read": "0.000000075"
				}
			}
		]
	}`

	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/models.dev":
			_, _ = w.Write([]byte(modelsDevJSON))
		case "/litellm":
			_, _ = w.Write([]byte(litellmJSON))
		case "/openrouter":
			_, _ = w.Write([]byte(openrouterJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cachePath := filepath.Join(t.TempDir(), "pricing-cache.json")
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

	res := NewExternalCatalogResolver(cachePath, true, []string{"models.dev", "litellm", "openrouter"})
	res.AllowInsecureForTest = true
	res.NowFn = func() time.Time { return now }
	res.HTTPClient = ts.Client()
	res.URLs = map[string]string{
		"models.dev": ts.URL + "/models.dev",
		"litellm":    ts.URL + "/litellm",
		"openrouter": ts.URL + "/openrouter",
	}

	// 1. models.dev exact decimal conversion
	r1, ok := res.Resolve("openai", "gpt-7-preview")
	if !ok || r1.Input != "3.25" || r1.Output != "12.5" || r1.Cached != "0.325" || r1.CacheWrite != "4.0625" || r1.Source != "models.dev" {
		t.Fatalf("models.dev resolve mismatch: ok=%v rate=%+v", ok, r1)
	}
	// Negative prices must be rejected
	if _, ok := res.Resolve("openai", "bad-negative"); ok {
		t.Fatal("negative price entry must not resolve")
	}

	// 2. LiteLLM per-token -> per-1M exact decimal conversion
	r2, ok := res.Resolve("deepseek", "deepseek-v5-alpha")
	if !ok || r2.Input != "0.5" || r2.Output != "2" || r2.Cached != "0.05" || r2.Source != "litellm" {
		t.Fatalf("litellm resolve mismatch: ok=%v rate=%+v", ok, r2)
	}

	// 3. OpenRouter string per-token -> per-1M exact decimal conversion
	r3, ok := res.Resolve("mistral", "mistralai/mistral-next-1")
	if !ok || r3.Input != "0.75" || r3.Output != "2.25" || r3.Cached != "0.075" || r3.Source != "openrouter" {
		t.Fatalf("openrouter resolve mismatch: ok=%v rate=%+v", ok, r3)
	}

	// Verify 0600 cache file was written
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("expected cache file written: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("cache file perm = %o, want 0600", perm)
	}

	// 4. Offline resolver (SyncEnabled=false) reuses disk cache with zero network requests
	offline := NewExternalCatalogResolver(cachePath, false, []string{"models.dev", "litellm", "openrouter"})
	offline.NowFn = func() time.Time { return now.Add(time.Hour) }
	rOff, ok := offline.Resolve("openai", "gpt-7-preview")
	if !ok || rOff.Input != "3.25" {
		t.Fatalf("offline cache resolve failed: ok=%v rate=%+v", ok, rOff)
	}
	if hits != 3 {
		t.Fatalf("expected exactly 3 initial HTTP requests (no offline calls), got %d", hits)
	}
}
