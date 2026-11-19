package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/config"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/sse"
)

func newTestEnv(t *testing.T) (*db.Store, *Server, *time.Time, func()) {
	t.Helper()
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := db.Open(dbPath, "", func() time.Time { return now })
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	var mode string
	if err := store.SQL.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("expected WAL journal_mode, got %q (err=%v)", mode, err)
	}

	cfg := &config.Config{
		Port:         8080,
		BindAddr:     "127.0.0.1",
		DBPath:       dbPath,
		Origin:       "http://127.0.0.1:8080",
		RateLimitMax: 600,
	}
	broker := sse.NewBroker()
	srv, err := NewServer(cfg, store, broker, nil, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	cleanup := func() {
		srv.Close()
		_ = store.Close()
	}
	return store, srv, &now, cleanup
}

func TestSecurityHeadersAndFailClosedRoutes(t *testing.T) {
	_, srv, _, cleanup := newTestEnv(t)
	defer cleanup()

	// Unauthenticated GET / redirects 303 to /access
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/access" {
		t.Fatalf("expected 303 -> /access, got %d (%s)", rec.Code, rec.Header().Get("Location"))
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("missing X-Frame-Options: DENY")
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("missing CSP frame-ancestors 'none'")
	}

	// Unauthenticated API endpoints fail closed with 401
	for _, path := range []string{"/api/stream", "/api/usage", "/api/events", "/auth/session"} {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080"+path, nil)
		w := httptest.NewRecorder()
		srv.Handler.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for unauthenticated %s, got %d", path, w.Code)
		}
	}

	// Health returns 200
	rHealth := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/health", nil)
	wHealth := httptest.NewRecorder()
	srv.Handler.ServeHTTP(wHealth, rHealth)
	if wHealth.Code != http.StatusOK {
		t.Fatalf("expected 200 for /health, got %d", wHealth.Code)
	}
}

func TestAtomicTokenExchangeCSRFAndSSERevocation(t *testing.T) {
	store, srv, now, cleanup := newTestEnv(t)
	defer cleanup()

	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	issued, err := store.Issue()
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Concurrent atomic exchange: exactly 1 succeeds, rest get 401
	var wg sync.WaitGroup
	codes := make([]int, 5)
	var sessionCookie *http.Cookie
	var csrf string
	var mu sync.Mutex

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]string{"token": issued.Token})
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/auth/exchange", bytes.NewReader(body))
			req.Header.Set("User-Agent", "Mozilla/5.0")
			req.Header.Set("Origin", "http://127.0.0.1:8080")
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("exchange request error: %v", err)
				return
			}
			defer resp.Body.Close()
			codes[idx] = resp.StatusCode
			if resp.StatusCode == http.StatusOK {
				var out map[string]string
				_ = json.NewDecoder(resp.Body).Decode(&out)
				mu.Lock()
				csrf = out["csrf"]
				for _, c := range resp.Cookies() {
					if c.Name == SessionCookieName {
						sessionCookie = c
					}
				}
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	okCount := 0
	for _, c := range codes {
		if c == http.StatusOK {
			okCount++
		}
	}
	if okCount != 1 || sessionCookie == nil || csrf == "" {
		t.Fatalf("expected exactly 1 atomic exchange success, got codes=%v cookie=%v", codes, sessionCookie)
	}
	if !sessionCookie.HttpOnly || !sessionCookie.Secure || sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected cookie attributes: %+v", sessionCookie)
	}

	// Connect SSE stream with authenticated cookie
	sseReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/stream", nil)
	sseReq.Header.Set("User-Agent", "Mozilla/5.0")
	sseReq.AddCookie(sessionCookie)
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer sseResp.Body.Close()
	if sseResp.StatusCode != http.StatusOK || !strings.HasPrefix(sseResp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("unexpected SSE response: %d %s", sseResp.StatusCode, sseResp.Header.Get("Content-Type"))
	}

	reader := bufio.NewReader(sseResp.Body)
	line1, _ := reader.ReadString('\n')
	if strings.TrimSpace(line1) != "event: ready" {
		t.Fatalf("expected event: ready, got %q", line1)
	}

	// Logout with wrong CSRF -> 403
	badLogoutReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/auth/logout", strings.NewReader("{}"))
	badLogoutReq.Header.Set("User-Agent", "Mozilla/5.0")
	badLogoutReq.AddCookie(sessionCookie)
	badLogoutReq.Header.Set("Origin", "http://127.0.0.1:8080")
	badLogoutReq.Header.Set("Content-Type", "application/json")
	badLogoutReq.Header.Set("X-CSRF-Token", "wrong-csrf")
	badResp, _ := http.DefaultClient.Do(badLogoutReq)
	badResp.Body.Close()
	if badResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for invalid CSRF, got %d", badResp.StatusCode)
	}

	// Logout with valid CSRF -> 200 AND active SSE stream receives event: expired and closes immediately
	logoutReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/auth/logout", strings.NewReader("{}"))
	logoutReq.Header.Set("User-Agent", "Mozilla/5.0")
	logoutReq.AddCookie(sessionCookie)
	logoutReq.Header.Set("Origin", "http://127.0.0.1:8080")
	logoutReq.Header.Set("Content-Type", "application/json")
	logoutReq.Header.Set("X-CSRF-Token", csrf)
	logoutResp, err := http.DefaultClient.Do(logoutReq)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	logoutResp.Body.Close()
	if logoutResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on logout, got %d", logoutResp.StatusCode)
	}

	// Read remaining SSE frames until EOF; must see event: expired quickly
	gotExpired := false
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			l, err := reader.ReadString('\n')
			if strings.TrimSpace(l) == "event: expired" {
				gotExpired = true
			}
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE stream did not close immediately after logout")
	}
	if !gotExpired {
		t.Fatal("expected event: expired on SSE stream after logout")
	}

	// Test expiration sweep closes SSE stream after 8h
	issued2, _ := store.Issue()
	body2, _ := json.Marshal(map[string]string{"token": issued2.Token})
	exReq2, _ := http.NewRequest(http.MethodPost, ts.URL+"/auth/exchange", bytes.NewReader(body2))
	exReq2.Header.Set("User-Agent", "Mozilla/5.0")
	exReq2.Header.Set("Origin", "http://127.0.0.1:8080")
	exReq2.Header.Set("Content-Type", "application/json")
	exResp2, _ := http.DefaultClient.Do(exReq2)
	var cookie2 *http.Cookie
	for _, c := range exResp2.Cookies() {
		if c.Name == SessionCookieName {
			cookie2 = c
		}
	}
	exResp2.Body.Close()

	sseReq2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/stream", nil)
	sseReq2.Header.Set("User-Agent", "Mozilla/5.0")
	sseReq2.AddCookie(cookie2)
	sseResp2, err := http.DefaultClient.Do(sseReq2)
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer sseResp2.Body.Close()

	// Advance clock past 8 hours
	*now = now.Add(8*time.Hour + time.Minute)
	done2 := make(chan struct{})
	gotExpired2 := false
	go func() {
		defer close(done2)
		r2 := bufio.NewReader(sseResp2.Body)
		for {
			l, err := r2.ReadString('\n')
			if strings.TrimSpace(l) == "event: expired" {
				gotExpired2 = true
			}
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE stream did not close after session expiration")
	}
	if !gotExpired2 {
		t.Fatal("expected event: expired after 8h session expiration")
	}
}

func TestExchangeRateLimitingAndMachineKeyRevocation(t *testing.T) {
	store, srv, _, cleanup := newTestEnv(t)
	defer cleanup()

	// 20 attempts allowed, 21st returns 429
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/auth/exchange", strings.NewReader(`{"token":"bad"}`))
		req.Header.Set("Origin", "http://127.0.0.1:8080")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, rec.Code)
		}
	}
	req21 := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/auth/exchange", strings.NewReader(`{"token":"bad"}`))
	req21.Header.Set("Origin", "http://127.0.0.1:8080")
	req21.Header.Set("Content-Type", "application/json")
	rec21 := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec21, req21)
	if rec21.Code != http.StatusTooManyRequests || rec21.Header().Get("Retry-After") != "60" {
		t.Fatalf("attempt 21: expected 429 Retry-After: 60, got %d (%s)", rec21.Code, rec21.Header().Get("Retry-After"))
	}

	// Verify deleting machine.key + restart revokes sessions
	issued, _ := store.Issue()
	sess, _ := store.Exchange(issued.Token)
	if sess == nil {
		t.Fatal("expected valid session")
	}
	keyPath := filepath.Join(filepath.Dir(store.Path), "machine.key")
	if err := os.Remove(keyPath); err != nil {
		t.Fatalf("remove machine.key: %v", err)
	}
	_ = store.Close()

	storeReopened, err := db.Open(store.Path, "", nil)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer storeReopened.Close()
	if got, _ := storeReopened.LookupSession(sess.RawSession); got != nil {
		t.Fatal("expected session invalid after deleting machine.key")
	}
}
