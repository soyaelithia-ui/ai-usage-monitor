package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/config"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/ingest"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/sse"
)

func newIngestTestServer(t *testing.T) (*db.Store, *Server, *time.Time, string, func()) {
	t.Helper()
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "api_test.db")
	store, err := db.Open(dbPath, "", func() time.Time { return now })
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	ingestKey := "1234567890123456789012345678901234567890123"
	cfg := &config.Config{
		Port:         8080,
		BindAddr:     "127.0.0.1",
		DBPath:       dbPath,
		Origin:       "http://127.0.0.1:8080",
		IngestKey:    ingestKey,
		RateLimitMax: 600,
	}
	broker := sse.NewBroker()
	mgr := ingest.NewManager(store, cfg, broker.BroadcastChange)
	srv, err := NewServer(cfg, store, broker, mgr, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return store, srv, &now, ingestKey, func() {
		srv.Close()
		_ = store.Close()
	}
}

func TestDynamicRegistrationIngestAndFiltersAPI(t *testing.T) {
	store, srv, now, ingestKey, cleanup := newIngestTestServer(t)
	defer cleanup()

	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	// 1. Reject invalid rates (cacheWrite12h invalid)
	badReg := `{"id":"myagent","name":"My Agent","provider":"custom","iconUrl":"data:image/svg+xml;base64,PHN2Zz4=","rates":{"m1":{"input":"1","output":"2","cacheWrite12h":"-5"}}}`
	reqBad, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/agents/register", strings.NewReader(badReg))
	reqBad.Header.Set("Authorization", "Bearer "+ingestKey)
	reqBad.Header.Set("Content-Type", "application/json")
	respBad, _ := http.DefaultClient.Do(reqBad)
	respBad.Body.Close()
	if respBad.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid cacheWrite12h rate, got %d", respBad.StatusCode)
	}

	// 2. Reject shadowing built-in agent
	shadowReg := `{"id":"codex","name":"Fake Codex"}`
	reqShad, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/agents/register", strings.NewReader(shadowReg))
	reqShad.Header.Set("Authorization", "Bearer "+ingestKey)
	reqShad.Header.Set("Content-Type", "application/json")
	respShad, _ := http.DefaultClient.Do(reqShad)
	respShad.Body.Close()
	if respShad.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 shadowing builtin agent, got %d", respShad.StatusCode)
	}

	// 3. Authenticate browser session and open SSE stream
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

	sseReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/stream", nil)
	sseReq.Header.Set("User-Agent", "Mozilla/5.0")
	sseReq.AddCookie(sessCookie)
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer sseResp.Body.Close()

	// 4. Self-register + ingest via usage-monitor.usage.v1 with valid cacheWrite & cacheWrite12h and sanitized iconUrl
	ingestPayload := map[string]any{
		"schema":                "usage-monitor.usage.v1",
		"semantics":             "input_includes_cache;output_includes_reasoning",
		"source":                "hermes",
		"agent":                 "Hermes",
		"provider":              "nous",
		"model":                 "hermes-4-pro",
		"responseId":            "h-1",
		"sessionId":             "sess-A",
		"occurredAt":            now.Format(time.RFC3339),
		"inputTokens":           "1000",
		"outputTokens":          "200",
		"cachedInputTokens":     "100",
		"cacheWriteInputTokens": "50",
		"cacheWriteDuration":    "12h",
		"iconUrl":               "data:image/svg+xml;base64,PHN2Zz4=", // unsafe SVG must be dropped
		"rates": map[string]any{
			"hermes-4-pro": map[string]any{
				"input":         "2",
				"cached":        "0.2",
				"cacheWrite":    "2.5",
				"cacheWrite12h": "5",
				"output":        "8",
			},
		},
	}
	ingBytes, _ := json.Marshal(ingestPayload)
	ingReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/ingest", bytes.NewReader(ingBytes))
	ingReq.Header.Set("Authorization", "Bearer "+ingestKey)
	ingReq.Header.Set("Content-Type", "application/json")
	ingResp, err := http.DefaultClient.Do(ingReq)
	if err != nil {
		t.Fatalf("POST /api/ingest: %v", err)
	}
	ingResp.Body.Close()
	if ingResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on ingest, got %d", ingResp.StatusCode)
	}

	// Verify SSE receives change notification
	sseGotChange := make(chan bool, 1)
	go func() {
		r := bufio.NewReader(sseResp.Body)
		for {
			l, err := r.ReadString('\n')
			if strings.TrimSpace(l) == "event: change" {
				sseGotChange <- true
				return
			}
			if err != nil {
				sseGotChange <- false
				return
			}
		}
	}()
	select {
	case ok := <-sseGotChange:
		if !ok {
			t.Fatal("SSE closed before change event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE change event after /api/ingest")
	}

	// Ingest a second event on a built-in agent (antigravity) declaring an inline "rate" for a brand-new model
	ingestPayload2 := map[string]any{
		"schema":                "usage-monitor.usage.v1",
		"semantics":             "input_includes_cache;output_includes_reasoning",
		"source":                "antigravity",
		"provider":              "google",
		"model":                 "gemini-5-alpha",
		"responseId":            "ag-1",
		"sessionId":             "sess-B",
		"occurredAt":            now.Add(-2 * time.Minute).Format(time.RFC3339),
		"inputTokens":           "1000000",
		"outputTokens":          "0",
		"cachedInputTokens":     "0",
		"cacheWriteInputTokens": "0",
		"rate": map[string]any{
			"input":  "3.5",
			"output": "14",
		},
	}
	ingBytes2, _ := json.Marshal(ingestPayload2)
	ingReq2, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/ingest", bytes.NewReader(ingBytes2))
	ingReq2.Header.Set("Authorization", "Bearer "+ingestKey)
	ingReq2.Header.Set("Content-Type", "application/json")
	ingResp2, _ := http.DefaultClient.Do(ingReq2)
	if ingResp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on builtin agent rate declaration, got %d", ingResp2.StatusCode)
	}
	ingResp2.Body.Close()

	// Verify /api/sources stripped unsafe SVG iconUrl, has zero "tokenizer" key, and includes errors/errorCount
	srcReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/sources", nil)
	srcReq.Header.Set("User-Agent", "Mozilla/5.0")
	srcReq.AddCookie(sessCookie)
	srcResp, _ := http.DefaultClient.Do(srcReq)
	var srcOut map[string]any
	_ = json.NewDecoder(srcResp.Body).Decode(&srcOut)
	srcResp.Body.Close()
	if _, leaked := srcOut["tokenizer"]; leaked {
		t.Fatalf("/api/sources must never expose tokenizer state: %+v", srcOut["tokenizer"])
	}
	if _, hasErrors := srcOut["errors"]; !hasErrors {
		t.Fatalf("/api/sources must include errors slice")
	}
	if _, hasErrCount := srcOut["errorCount"]; !hasErrCount {
		t.Fatalf("/api/sources must include errorCount")
	}
	for _, rawAg := range srcOut["agents"].([]any) {
		ag := rawAg.(map[string]any)
		if ag["id"] == "hermes" && ag["iconUrl"] != nil && ag["iconUrl"] != "" {
			t.Fatalf("unsafe SVG iconUrl was not stripped: %v", ag["iconUrl"])
		}
	}

	// Verify unified agent filtering works for both canonical ID ("hermes") and display label ("Hermes")
	for _, agentQuery := range []string{"hermes", "Hermes"} {
		anaReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/analytics?window=1h&agent="+agentQuery+"&sessionId=sess-A", nil)
		anaReq.Header.Set("User-Agent", "Mozilla/5.0")
		anaReq.AddCookie(sessCookie)
		anaResp, _ := http.DefaultClient.Do(anaReq)
		var anaOut map[string]any
		_ = json.NewDecoder(anaResp.Body).Decode(&anaOut)
		anaResp.Body.Close()
		totals := anaOut["totals"].(map[string]any)
		if int(totals["count"].(float64)) != 1 || totals["totalTokens"] != "1200" {
			t.Fatalf("unexpected filtered analytics totals for agent=%q: %+v", agentQuery, totals)
		}
	}

	// Verify built-in agent inline rate was applied to gemini-5-alpha ($3.5 for 1M input)
	agReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/analytics?window=1h&agent=antigravity", nil)
	agReq.Header.Set("User-Agent", "Mozilla/5.0")
	agReq.AddCookie(sessCookie)
	agResp, _ := http.DefaultClient.Do(agReq)
	var agOut map[string]any
	_ = json.NewDecoder(agResp.Body).Decode(&agOut)
	agResp.Body.Close()
	if agOut["totals"].(map[string]any)["costUsd"] != "3.5" {
		t.Fatalf("expected antigravity inline rate costUsd=3.5, got %+v", agOut["totals"])
	}

	// Verify /api/events pagination & options
	evReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/events?window=1h&limit=1&offset=0", nil)
	evReq.Header.Set("User-Agent", "Mozilla/5.0")
	evReq.AddCookie(sessCookie)
	evResp, _ := http.DefaultClient.Do(evReq)
	var evOut map[string]any
	_ = json.NewDecoder(evResp.Body).Decode(&evOut)
	evResp.Body.Close()
	if int(evOut["total"].(float64)) != 2 || len(evOut["events"].([]any)) != 1 {
		t.Fatalf("unexpected paginated events response: %+v", evOut)
	}
	if _, hasOpts := evOut["options"]; !hasOpts {
		t.Fatalf("/api/events must include options for standalone history view")
	}

	// Verify /api/reconciliation exposes zero tokenizer/engine keys
	recReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/reconciliation?window=1h", nil)
	recReq.Header.Set("User-Agent", "Mozilla/5.0")
	recReq.AddCookie(sessCookie)
	recResp, _ := http.DefaultClient.Do(recReq)
	var recRaw bytes.Buffer
	_, _ = recRaw.ReadFrom(recResp.Body)
	recResp.Body.Close()
	if strings.Contains(strings.ToLower(recRaw.String()), "tokenizer") {
		t.Fatalf("/api/reconciliation must never expose tokenizer keys: %s", recRaw.String())
	}
}
