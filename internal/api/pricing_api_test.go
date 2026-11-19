package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/pricing"
)

func decodePricing(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.handlePricing(rec, httptest.NewRequest(http.MethodGet, "/api/pricing", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

func tokenPtr(s string) *string { return &s }

// TestPricingEndpointExposesOnlyObservedModels enforces that no fixed model name
// reaches the web API: the pricing surface lists only models seen in events.
func TestPricingEndpointExposesOnlyObservedModels(t *testing.T) {
	store, srv, now, _, cleanup := newIngestTestServer(t)
	defer cleanup()

	body := decodePricing(t, srv)
	if _, ok := body["pricing"]; ok {
		t.Fatalf("response must not expose the fixed catalog under %q", "pricing")
	}
	if models, _ := body["models"].([]any); len(models) != 0 {
		t.Fatalf("expected no models without observed events, got %v", models)
	}

	insert := func(model, provider, responseID string) {
		t.Helper()
		if _, err := store.InsertEvent(pricing.RawEvent{
			Source:                "antigravity",
			Provider:              provider,
			Agent:                 "Antigravity",
			Model:                 model,
			ResponseID:            responseID,
			OccurredAt:            now.Format("2006-01-02T15:04:05.000Z"),
			InputTokens:           "100",
			OutputTokens:          "20",
			CachedInputTokens:     tokenPtr("10"),
			CacheWriteInputTokens: tokenPtr("0"),
		}); err != nil {
			t.Fatalf("InsertEvent(%s): %v", model, err)
		}
	}
	insert("gpt-6-astra", "openai", "r-catalog")
	insert("otherwise-unseen-model", "custom", "r-unknown")

	body = decodePricing(t, srv)
	models, _ := body["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("expected exactly the 2 observed models, got %v", models)
	}

	byModel := make(map[string]map[string]any, len(models))
	for _, raw := range models {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("model entry is not an object: %v", raw)
		}
		byModel[entry["model"].(string)] = entry
	}

	known, ok := byModel["gpt-6-astra"]
	if !ok {
		t.Fatalf("observed catalog model missing from pricing payload: %v", byModel)
	}
	if known["priced"] != true {
		t.Fatalf("gpt-6-astra priced = %v, want true", known["priced"])
	}
	rate, _ := known["rate"].(map[string]any)
	if rate["input"] != "10" {
		t.Fatalf("gpt-6-astra rate.input = %v, want 10", rate["input"])
	}

	unknown, ok := byModel["otherwise-unseen-model"]
	if !ok {
		t.Fatalf("observed unknown model missing from pricing payload: %v", byModel)
	}
	if unknown["priced"] != false {
		t.Fatalf("unknown model priced = %v, want false", unknown["priced"])
	}
	if _, hasRate := unknown["rate"]; hasRate {
		t.Fatalf("unknown model must not carry a rate: %v", unknown)
	}

	for _, hidden := range []string{"gemini-4-argon", "claude-opus-4-6"} {
		if _, found := byModel[hidden]; found {
			t.Fatalf("unobserved catalog model %q leaked into the web payload", hidden)
		}
	}
}

// TestPricingEndpointFiltersFixedCatalogSurfaces pins the exposure invariant:
// dynamic rates win, duplicates collapse, and fixed catalog surfaces stay filtered to observed models.
func TestPricingEndpointFiltersFixedCatalogSurfaces(t *testing.T) {
	store, srv, now, _, cleanup := newIngestTestServer(t)
	defer cleanup()

	if err := store.UpsertDynamicPricing("dyn-unobserved", pricing.ModelRate{Provider: "custom", Input: "3", Output: "6"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := store.UpsertDynamicPricing("gpt-6-astra", pricing.ModelRate{Provider: "openai", Input: "99", Output: "99"}); err != nil {
		t.Fatalf("upsert override: %v", err)
	}

	insert := func(model, provider, responseID string) {
		t.Helper()
		if _, err := store.InsertEvent(pricing.RawEvent{
			Source:                "antigravity",
			Provider:              provider,
			Agent:                 "Antigravity",
			Model:                 model,
			ResponseID:            responseID,
			OccurredAt:            now.Format("2006-01-02T15:04:05.000Z"),
			InputTokens:           "100",
			OutputTokens:          "20",
			CachedInputTokens:     tokenPtr("10"),
			CacheWriteInputTokens: tokenPtr("0"),
		}); err != nil {
			t.Fatalf("InsertEvent(%s): %v", model, err)
		}
	}
	insert("gpt-6-astra", "openai", "f1")
	insert("gpt-6-astra", "openai", "f2") // duplicate model must collapse
	insert("gemini-4-argon", "google", "f3")

	body := decodePricing(t, srv)
	models, _ := body["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("want 2 distinct observed models, got %v", models)
	}
	byModel := make(map[string]map[string]any, len(models))
	for _, raw := range models {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("model entry is not an object: %v", raw)
		}
		byModel[entry["model"].(string)] = entry
	}
	if got := byModel["gpt-6-astra"]["rate"].(map[string]any)["input"]; got != "99" {
		t.Fatalf("dynamic rate must win, got %v", got)
	}
	if lc, _ := body["longContext"].(map[string]any); len(lc) != 1 || lc["gpt-6-astra"] == nil {
		t.Fatalf("longContext must contain only observed models: %v", lc)
	}
	if sc, _ := body["schedule"].(map[string]any); len(sc) != 1 || sc["gemini-4-argon"] == nil {
		t.Fatalf("schedule must contain only observed models: %v", sc)
	}
	if _, ok := body["dynamicPricing"]; ok {
		t.Fatalf("dynamicPricing must not be published")
	}

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	for _, hidden := range []string{"gemini-4-carbon", "gemini-3.8-flash", "claude-opus-4-6", "dyn-unobserved"} {
		if strings.Contains(string(raw), hidden) {
			t.Fatalf("unobserved model %q leaked: %s", hidden, raw)
		}
	}
}

// TestExternalResolverAutoRegistersObservedOnly verifies that ExternalCatalogResolver
// auto-registers rates for newly observed models while keeping unobserved cached models hidden.
func TestExternalResolverAutoRegistersObservedOnly(t *testing.T) {
	store, srv, now, _, cleanup := newIngestTestServer(t)
	defer cleanup()

	extServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"openai": {
				"models": {
					"gpt-7-observed": {"cost": {"input": 4, "output": 16}},
					"gpt-7-secret-unseen": {"cost": {"input": 9, "output": 36}}
				}
			}
		}`))
	}))
	defer extServer.Close()

	resolver := pricing.NewExternalCatalogResolver(filepath.Join(t.TempDir(), "cache.json"), true, []string{"models.dev"})
	resolver.AllowInsecureForTest = true
	resolver.HTTPClient = extServer.Client()
	resolver.NowFn = func() time.Time { return *now }
	resolver.URLs = map[string]string{"models.dev": extServer.URL}
	store.SetRateResolver(resolver)

	id, err := store.InsertEvent(pricing.RawEvent{
		Source:                "codex",
		Provider:              "openai",
		Agent:                 "Codex",
		Model:                 "gpt-7-observed",
		ResponseID:            "ext-1",
		OccurredAt:            now.Format("2006-01-02T15:04:05.000Z"),
		InputTokens:           "1000000",
		OutputTokens:          "0",
		CachedInputTokens:     tokenPtr("0"),
		CacheWriteInputTokens: tokenPtr("0"),
	})
	if err != nil || id == "" {
		t.Fatalf("InsertEvent: id=%q err=%v", id, err)
	}

	body := decodePricing(t, srv)
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "gpt-7-secret-unseen") {
		t.Fatalf("unobserved external model leaked into /api/pricing: %s", raw)
	}
	models := body["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("expected 1 observed model, got %v", models)
	}
	entry := models[0].(map[string]any)
	if entry["model"] != "gpt-7-observed" || entry["priced"] != true {
		t.Fatalf("expected gpt-7-observed auto-priced, got %+v", entry)
	}
	if entry["rate"].(map[string]any)["input"] != "4" {
		t.Fatalf("expected auto-registered input rate 4, got %+v", entry["rate"])
	}
}

// TestPricingRouteRequiresSessionAndExposesObservedModels exercises the real
// router and middleware stack, not just the handler.
func TestPricingRouteRequiresSessionAndExposesObservedModels(t *testing.T) {
	store, srv, now, _, cleanup := newIngestTestServer(t)
	defer cleanup()
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	if _, err := store.InsertEvent(pricing.RawEvent{
		Source:                "antigravity",
		Provider:              "openai",
		Agent:                 "Antigravity",
		Model:                 "gpt-6-astra",
		ResponseID:            "route-1",
		OccurredAt:            now.Format("2006-01-02T15:04:05.000Z"),
		InputTokens:           "100",
		OutputTokens:          "20",
		CachedInputTokens:     tokenPtr("10"),
		CacheWriteInputTokens: tokenPtr("0"),
	}); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	anonReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/pricing", nil)
	anonReq.Header.Set("User-Agent", "Mozilla/5.0")
	anon, err := http.DefaultClient.Do(anonReq)
	if err != nil {
		t.Fatalf("anonymous GET: %v", err)
	}
	anon.Body.Close()
	if anon.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", anon.StatusCode)
	}

	issued, err := store.Issue()
	if err != nil {
		t.Fatalf("store.Issue: %v", err)
	}
	exBody, _ := json.Marshal(map[string]string{"token": issued.Token})
	exReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/auth/exchange", bytes.NewReader(exBody))
	exReq.Header.Set("User-Agent", "Mozilla/5.0")
	exReq.Header.Set("Origin", "http://127.0.0.1:8080")
	exReq.Header.Set("Content-Type", "application/json")
	exResp, err := http.DefaultClient.Do(exReq)
	if err != nil {
		t.Fatalf("auth exchange: %v", err)
	}
	var cookie *http.Cookie
	for _, c := range exResp.Cookies() {
		if c.Name == SessionCookieName {
			cookie = c
		}
	}
	exResp.Body.Close()
	if cookie == nil {
		t.Fatal("auth exchange did not set a session cookie")
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/pricing", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated status = %d, want 200", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	models, _ := payload["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("want the single observed model, got %v", models)
	}
	entry, _ := models[0].(map[string]any)
	if entry["model"] != "gpt-6-astra" || entry["priced"] != true {
		t.Fatalf("unexpected entry: %v", entry)
	}
	if _, ok := payload["pricing"]; ok {
		t.Fatalf("fixed catalog must not be published")
	}
}
