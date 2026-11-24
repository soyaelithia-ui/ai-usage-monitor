package api

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/config"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/sse"
	_ "modernc.org/sqlite"
)

type testClock struct {
	mu  sync.RWMutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func newTestEnv(t *testing.T) (*db.Store, *Server, *testClock, func()) {
	t.Helper()
	clock := &testClock{now: time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := db.Open(dbPath, "", clock.Now)
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
	return store, srv, clock, cleanup
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
				var out struct {
					CSRF          string `json:"csrf"`
					AuthMode      string `json:"authMode"`
					TokenRequired bool   `json:"tokenRequired"`
					Permanent     bool   `json:"permanent"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
					t.Errorf("decode exchange response: %v", err)
					return
				}
				if out.AuthMode != "token" || !out.TokenRequired || out.Permanent {
					t.Errorf("unexpected exchange metadata: %+v", out)
				}
				mu.Lock()
				csrf = out.CSRF
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
	now.Advance(8*time.Hour + time.Minute)
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

func TestPublicFaviconRoute(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := db.Open(dbPath, "", func() time.Time { return now })
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer store.Close()

	cfg := &config.Config{
		Port:         8080,
		BindAddr:     "127.0.0.1",
		AuthMode:     "token",
		DBPath:       dbPath,
		Origin:       "http://127.0.0.1:8080",
		RateLimitMax: 600,
	}
	staticFS := fstest.MapFS{
		"index.html":  {Data: []byte("<html></html>")},
		"favicon.svg": {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"></svg>`)},
	}
	srv, err := NewServer(cfg, store, sse.NewBroker(), nil, staticFS)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/favicon.svg", nil)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for public /favicon.svg, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Fatalf("expected svg body, got %q", rec.Body.String())
	}
}

func TestPermanentReusableTTLKeepAndStaticAccessToken(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := db.Open(dbPath, "", clock.Now)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer store.Close()

	staticTok := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	cfg := &config.Config{
		Port:         8080,
		BindAddr:     "127.0.0.1",
		AuthMode:     "token",
		AccessToken:  staticTok,
		SessionTTL:   30 * 24 * time.Hour,
		DBPath:       dbPath,
		Origin:       "http://127.0.0.1:8080",
		RateLimitMax: 600,
	}
	srv, err := NewServer(cfg, store, sse.NewBroker(), nil, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	// 1. Permanent reusable token stays valid across multiple exchanges & gets 365d session
	perm, err := store.IssueWithOptions(db.IssueOptions{Permanent: true})
	if err != nil {
		t.Fatalf("IssueWithOptions permanent: %v", err)
	}
	if perm.ExpiresAt != "never" || !perm.Permanent || !perm.Reusable {
		t.Fatalf("unexpected permanent token metadata: %+v", perm)
	}

	var cookies []*http.Cookie
	for i := 0; i < 2; i++ {
		body, _ := json.Marshal(map[string]string{"token": perm.Token})
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/auth/exchange", bytes.NewReader(body))
		req.Header.Set("Origin", "http://127.0.0.1:8080")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("permanent exchange #%d failed: %d (%s)", i+1, rec.Code, rec.Body.String())
		}
		var out struct {
			AuthMode      string `json:"authMode"`
			TokenRequired bool   `json:"tokenRequired"`
			Permanent     bool   `json:"permanent"`
			CSRF          string `json:"csrf"`
			ExpiresAt     string `json:"expiresAt"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
			t.Fatalf("decode permanent exchange: %v", err)
		}
		if out.AuthMode != "token" || !out.TokenRequired || !out.Permanent || out.CSRF == "" {
			t.Fatalf("unexpected permanent exchange payload: %+v", out)
		}
		wantExp := clock.Now().Add(db.PermanentSessionTTL).UTC().Format("2006-01-02T15:04:05.000Z")
		if out.ExpiresAt != wantExp {
			t.Fatalf("permanent session expiresAt = %q, want %q", out.ExpiresAt, wantExp)
		}
		for _, c := range rec.Result().Cookies() {
			if c.Name == SessionCookieName {
				cookies = append(cookies, c)
			}
		}
	}
	if len(cookies) != 2 || cookies[0].Value == cookies[1].Value {
		t.Fatalf("expected 2 distinct permanent sessions, got %+v", cookies)
	}

	// 2. --keep preserves existing sessions & accesses while issuing a 2h token
	ttlTok, err := store.IssueWithOptions(db.IssueOptions{
		TTL:          2 * time.Hour,
		KeepExisting: true,
	})
	if err != nil {
		t.Fatalf("IssueWithOptions keep+ttl: %v", err)
	}
	// Existing permanent session still works!
	sessReq := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/auth/session", nil)
	sessReq.AddCookie(cookies[0])
	sessRec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(sessRec, sessReq)
	if sessRec.Code != http.StatusOK {
		t.Fatalf("expected kept permanent session to remain valid, got %d", sessRec.Code)
	}
	var sessOut struct {
		AuthMode      string `json:"authMode"`
		TokenRequired bool   `json:"tokenRequired"`
		CSRF          string `json:"csrf"`
		ExpiresAt     string `json:"expiresAt"`
	}
	_ = json.NewDecoder(sessRec.Body).Decode(&sessOut)
	if sessOut.AuthMode != "token" || !sessOut.TokenRequired || sessOut.CSRF == "" {
		t.Fatalf("unexpected /auth/session response: %+v", sessOut)
	}

	// 3. Static AccessToken works repeatedly and uses cfg.SessionTTL (30d)
	staticBody, _ := json.Marshal(map[string]string{"token": staticTok})
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/auth/exchange", bytes.NewReader(staticBody))
		req.Header.Set("Origin", "http://127.0.0.1:8080")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("static accessToken exchange #%d failed: %d", i+1, rec.Code)
		}
		var out struct {
			Permanent bool   `json:"permanent"`
			ExpiresAt string `json:"expiresAt"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&out)
		want30d := clock.Now().Add(30 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
		if !out.Permanent || out.ExpiresAt != want30d {
			t.Fatalf("static accessToken response = %+v, want permanent=true expiresAt=%s", out, want30d)
		}
	}

	// 4. Non-reusable 2h token expires after 2h
	clock.Advance(2*time.Hour + time.Second)
	expiredBody, _ := json.Marshal(map[string]string{"token": ttlTok.Token})
	expReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/auth/exchange", bytes.NewReader(expiredBody))
	expReq.Header.Set("Origin", "http://127.0.0.1:8080")
	expReq.Header.Set("Content-Type", "application/json")
	expRec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(expRec, expReq)
	if expRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired 2h token, got %d", expRec.Code)
	}

	// Permanent token still works after 2h and after 300d!
	clock.Advance(300 * 24 * time.Hour)
	permStillReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/auth/exchange", bytes.NewReader([]byte(`{"token":"`+perm.Token+`"}`)))
	permStillReq.Header.Set("Origin", "http://127.0.0.1:8080")
	permStillReq.Header.Set("Content-Type", "application/json")
	permStillRec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(permStillRec, permStillReq)
	if permStillRec.Code != http.StatusOK {
		t.Fatalf("expected permanent token still valid after 300d, got %d", permStillRec.Code)
	}
}

func TestAccessesSchemaMigrationIdempotency(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	_, err = rawDB.Exec(`
		CREATE TABLE accesses(hash TEXT PRIMARY KEY, expires INTEGER NOT NULL, used INTEGER NOT NULL DEFAULT 0);
		CREATE TABLE dynamic_pricing(model TEXT PRIMARY KEY, provider TEXT, input TEXT NOT NULL, cached TEXT NOT NULL, output TEXT NOT NULL, cache_write TEXT, cache_write_12h TEXT);
	`)
	if err != nil {
		t.Fatalf("create legacy tables: %v", err)
	}
	_ = rawDB.Close()

	store, err := db.Open(dbPath, "", nil)
	if err != nil {
		t.Fatalf("db.Open migration: %v", err)
	}
	defer store.Close()

	tok, err := store.IssueWithOptions(db.IssueOptions{Permanent: true})
	if err != nil {
		t.Fatalf("IssueWithOptions after migration: %v", err)
	}
	if sess, err := store.Exchange(tok.Token); err != nil || sess == nil || !sess.Permanent {
		t.Fatalf("Exchange after migration failed: sess=%+v err=%v", sess, err)
	}
}

func TestLocalTokenlessModeAndRemoteAddrEnforcement(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "local.db")
	store, err := db.Open(dbPath, "", func() time.Time { return now })
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer store.Close()

	cfg := &config.Config{
		Port:         8080,
		BindAddr:     "0.0.0.0",
		AuthMode:     "local",
		AllowLAN:     true,
		DBPath:       dbPath,
		Origin:       "http://127.0.0.1:8080",
		RateLimitMax: 600,
	}
	srv, err := NewServer(cfg, store, sse.NewBroker(), nil, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	// Local RFC1918 and loopback IPs are allowed without any token
	for _, remoteAddr := range []string{"127.0.0.1:50000", "192.168.1.42:51234", "10.0.0.7:4444", "[::1]:60000"} {
		sessReq := httptest.NewRequest(http.MethodGet, "http://192.168.1.50:8080/auth/session", nil)
		sessReq.RemoteAddr = remoteAddr
		sessRec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(sessRec, sessReq)
		if sessRec.Code != http.StatusOK {
			t.Fatalf("remoteAddr=%s /auth/session = %d, want 200", remoteAddr, sessRec.Code)
		}
		var out struct {
			AuthMode      string `json:"authMode"`
			TokenRequired bool   `json:"tokenRequired"`
			CSRF          string `json:"csrf"`
			ExpiresAt     string `json:"expiresAt"`
		}
		if err := json.NewDecoder(sessRec.Body).Decode(&out); err != nil {
			t.Fatalf("decode /auth/session: %v", err)
		}
		if out.AuthMode != "local" || out.TokenRequired || out.CSRF != "" || out.ExpiresAt != "" {
			t.Fatalf("unexpected local /auth/session payload: %+v", out)
		}

		rootReq := httptest.NewRequest(http.MethodGet, "http://192.168.1.50:8080/", nil)
		rootReq.RemoteAddr = remoteAddr
		rootRec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rootRec, rootReq)
		if rootRec.Code != http.StatusOK {
			t.Fatalf("remoteAddr=%s GET / = %d, want 200 (no /access redirect)", remoteAddr, rootRec.Code)
		}

		usageReq := httptest.NewRequest(http.MethodGet, "http://192.168.1.50:8080/api/usage", nil)
		usageReq.RemoteAddr = remoteAddr
		usageRec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(usageRec, usageReq)
		if usageRec.Code != http.StatusOK {
			t.Fatalf("remoteAddr=%s GET /api/usage = %d, want 200", remoteAddr, usageRec.Code)
		}

		notFoundReq := httptest.NewRequest(http.MethodGet, "http://192.168.1.50:8080/api/missing", nil)
		notFoundReq.RemoteAddr = remoteAddr
		notFoundRec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(notFoundRec, notFoundReq)
		if notFoundRec.Code != http.StatusNotFound {
			t.Fatalf("remoteAddr=%s GET /api/missing = %d, want 404", remoteAddr, notFoundRec.Code)
		}
	}

	// Logout in local mode succeeds without CSRF
	logoutReq := httptest.NewRequest(http.MethodPost, "http://192.168.1.50:8080/auth/logout", strings.NewReader(`{}`))
	logoutReq.RemoteAddr = "192.168.1.42:51234"
	logoutReq.Header.Set("Origin", "http://192.168.1.50:8080")
	logoutReq.Header.Set("Content-Type", "application/json")
	logoutRec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("local mode POST /auth/logout = %d, want 200", logoutRec.Code)
	}

	// Non-local public IPs must fail closed with 403 across all routes
	for _, publicIP := range []string{"203.0.113.10:12345", "8.8.8.8:5555", "[2001:db8::1]:9999"} {
		for _, path := range []string{"/auth/session", "/", "/api/usage", "/api/stream", "/health"} {
			r := httptest.NewRequest(http.MethodGet, "http://192.168.1.50:8080"+path, nil)
			r.RemoteAddr = publicIP
			w := httptest.NewRecorder()
			srv.Handler.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("publicIP=%s path=%s got %d, want 403", publicIP, path, w.Code)
			}
		}
	}

	// External Origin and cross-site API requests still fail closed with 403 even from local IP
	evilReq := httptest.NewRequest(http.MethodGet, "http://192.168.1.50:8080/api/usage", nil)
	evilReq.RemoteAddr = "192.168.1.42:51234"
	evilReq.Header.Set("Origin", "https://evil.com")
	evilRec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(evilRec, evilReq)
	if evilRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for evil.com Origin in local mode, got %d", evilRec.Code)
	}

	// SSE stream in local mode works and is NOT killed by runSessionSweep
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()
	sseReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/stream", nil)
	sseReq.Header.Set("User-Agent", "Mozilla/5.0")
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("local SSE connect: %v", err)
	}
	defer sseResp.Body.Close()
	if sseResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for local SSE, got %d", sseResp.StatusCode)
	}
	reader := bufio.NewReader(sseResp.Body)
	line1, _ := reader.ReadString('\n')
	if strings.TrimSpace(line1) != "event: ready" {
		t.Fatalf("expected event: ready, got %q", line1)
	}
	// Wait past the 500ms sweep interval and broadcast a change; stream must stay alive
	time.Sleep(700 * time.Millisecond)
	srv.Broker.BroadcastChange()
	gotChange := make(chan bool, 1)
	go func() {
		for {
			l, err := reader.ReadString('\n')
			if strings.TrimSpace(l) == "event: change" {
				gotChange <- true
				return
			}
			if err != nil {
				gotChange <- false
				return
			}
		}
	}()
	select {
	case ok := <-gotChange:
		if !ok {
			t.Fatal("local SSE stream closed unexpectedly during sweep")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for change event on local SSE stream")
	}
}

func TestLANHTTPCookieFallbackVsHTTPSAndLoopback(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lan_cookies.db")
	store, err := db.Open(dbPath, "", func() time.Time { return now })
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer store.Close()

	cfg := &config.Config{
		Port:         8080,
		BindAddr:     "0.0.0.0",
		AuthMode:     "token",
		AllowLAN:     true,
		DBPath:       dbPath,
		Origin:       "http://127.0.0.1:8080",
		RateLimitMax: 600,
	}
	srv, err := NewServer(cfg, store, sse.NewBroker(), nil, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	tok, err := store.IssueWithOptions(db.IssueOptions{Reusable: true})
	if err != nil {
		t.Fatalf("IssueWithOptions: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"token": tok.Token})

	// 1. Plain HTTP on non-loopback LAN host -> cookie "usage-monitor", Secure: false
	lanReq := httptest.NewRequest(http.MethodPost, "http://192.168.1.50:8080/auth/exchange", bytes.NewReader(body))
	lanReq.RemoteAddr = "192.168.1.42:12345"
	lanReq.Header.Set("Origin", "http://192.168.1.50:8080")
	lanReq.Header.Set("Content-Type", "application/json")
	lanRec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(lanRec, lanReq)
	if lanRec.Code != http.StatusOK {
		t.Fatalf("LAN HTTP exchange failed: %d (%s)", lanRec.Code, lanRec.Body.String())
	}
	var lanCookie *http.Cookie
	for _, c := range lanRec.Result().Cookies() {
		if c.Name == LANSessionCookieName {
			lanCookie = c
		}
	}
	if lanCookie == nil || lanCookie.Secure || !lanCookie.HttpOnly || lanCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("expected non-Secure %s cookie for LAN HTTP, got %+v", LANSessionCookieName, lanRec.Result().Cookies())
	}

	// Authenticating with the LAN cookie on LAN HTTP succeeds
	lanUsage := httptest.NewRequest(http.MethodGet, "http://192.168.1.50:8080/api/usage", nil)
	lanUsage.RemoteAddr = "192.168.1.42:12345"
	lanUsage.AddCookie(lanCookie)
	lanUsageRec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(lanUsageRec, lanUsage)
	if lanUsageRec.Code != http.StatusOK {
		t.Fatalf("LAN HTTP /api/usage with %s cookie = %d, want 200", LANSessionCookieName, lanUsageRec.Code)
	}

	// 2. HTTPS on LAN host -> cookie "__Host-usage-monitor", Secure: true
	tlsReq := httptest.NewRequest(http.MethodPost, "https://192.168.1.50:8443/auth/exchange", bytes.NewReader(body))
	tlsReq.TLS = &tls.ConnectionState{}
	tlsReq.RemoteAddr = "192.168.1.42:12345"
	tlsReq.Header.Set("Origin", "https://192.168.1.50:8443")
	tlsReq.Header.Set("Content-Type", "application/json")
	tlsRec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(tlsRec, tlsReq)
	if tlsRec.Code != http.StatusOK {
		t.Fatalf("LAN HTTPS exchange failed: %d", tlsRec.Code)
	}
	var tlsCookie *http.Cookie
	for _, c := range tlsRec.Result().Cookies() {
		if c.Name == SessionCookieName {
			tlsCookie = c
		}
	}
	if tlsCookie == nil || !tlsCookie.Secure || !tlsCookie.HttpOnly {
		t.Fatalf("expected Secure %s cookie for HTTPS, got %+v", SessionCookieName, tlsRec.Result().Cookies())
	}

	// 3. Loopback HTTP even when AllowLAN=true -> cookie "__Host-usage-monitor", Secure: true
	loopReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/auth/exchange", bytes.NewReader(body))
	loopReq.RemoteAddr = "127.0.0.1:12345"
	loopReq.Header.Set("Origin", "http://127.0.0.1:8080")
	loopReq.Header.Set("Content-Type", "application/json")
	loopRec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(loopRec, loopReq)
	if loopRec.Code != http.StatusOK {
		t.Fatalf("Loopback HTTP exchange failed: %d", loopRec.Code)
	}
	var loopCookie *http.Cookie
	for _, c := range loopRec.Result().Cookies() {
		if c.Name == SessionCookieName {
			loopCookie = c
		}
	}
	if loopCookie == nil || !loopCookie.Secure {
		t.Fatalf("expected Secure %s cookie on loopback HTTP, got %+v", SessionCookieName, loopRec.Result().Cookies())
	}
}

func TestConfigLoadingDurationsAuthModeAndLAN(t *testing.T) {
	if d, err := config.ParseDuration("30d"); err != nil || d != 30*24*time.Hour {
		t.Fatalf("ParseDuration(30d) = %v, %v", d, err)
	}
	if d, err := config.ParseDuration("365d"); err != nil || d != 365*24*time.Hour {
		t.Fatalf("ParseDuration(365d) = %v, %v", d, err)
	}
	if d, err := config.ParseDuration("2w"); err != nil || d != 14*24*time.Hour {
		t.Fatalf("ParseDuration(2w) = %v, %v", d, err)
	}
	if d, err := config.ParseDuration("1d12h"); err != nil || d != 36*time.Hour {
		t.Fatalf("ParseDuration(1d12h) = %v, %v", d, err)
	}
	for _, bad := range []string{"", "0", "-5d", "abc"} {
		if _, err := config.ParseDuration(bad); err == nil {
			t.Fatalf("expected ParseDuration(%q) to fail", bad)
		}
	}

	// Verify config/backend.example.json loads cleanly
	examplePath := filepath.Join("..", "..", "config", "backend.example.json")
	t.Setenv("USAGE_MONITOR_CONFIG", examplePath)
	cfgEx, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load(backend.example.json): %v", err)
	}
	if cfgEx.BindAddr != "127.0.0.1" || cfgEx.AuthMode != "token" || cfgEx.AllowLAN {
		t.Fatalf("unexpected defaults from backend.example.json: %+v", cfgEx)
	}

	// AuthMode normalization + BindAddr non-loopback enables AllowLAN and LAN origins
	t.Setenv("USAGE_MONITOR_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("AUTH_MODE", "no-token")
	t.Setenv("BIND_ADDR", "0.0.0.0")
	t.Setenv("ORIGIN", "http://192.168.1.50:8080")
	t.Setenv("SESSION_TTL", "365d")
	t.Setenv("ACCESS_TOKEN", "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC")
	cfgLAN, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load LAN: %v", err)
	}
	if cfgLAN.AuthMode != "local" || !cfgLAN.AllowLAN || cfgLAN.SessionTTL != 365*24*time.Hour {
		t.Fatalf("unexpected LAN config: %+v", cfgLAN)
	}

	// Invalid authMode rejected
	t.Setenv("AUTH_MODE", "bogus")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected invalid AUTH_MODE to fail")
	}
	t.Setenv("AUTH_MODE", "token")

	// Strict mode rejects non-loopback HTTP origin
	t.Setenv("BIND_ADDR", "127.0.0.1")
	t.Setenv("ALLOW_LAN", "false")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected strict mode to reject http://192.168.1.50:8080")
	}

	// AccessToken must differ from IngestKey and match 43-char format
	t.Setenv("ORIGIN", "http://127.0.0.1:8080")
	t.Setenv("INGEST_KEY", "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected matching ACCESS_TOKEN and INGEST_KEY to fail")
	}
}

func TestLocalModeDNSRebindingHostRejection(t *testing.T) {
	now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "rebinding.db")
	store, err := db.Open(dbPath, "", func() time.Time { return now })
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer store.Close()

	cfg := &config.Config{
		Port:           8080,
		BindAddr:       "0.0.0.0",
		AuthMode:       "local",
		AllowLAN:       true,
		DBPath:         dbPath,
		Origin:         "http://127.0.0.1:8080",
		AllowedOrigins: []string{"https://dashboard.example.com"},
		RateLimitMax:   600,
	}
	srv, err := NewServer(cfg, store, sse.NewBroker(), nil, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	// DNS rebinding attempts from local client IPs with evil Host must fail closed with 403.
	for _, remoteAddr := range []string{"127.0.0.1:50000", "192.168.1.50:51234"} {
		for _, path := range []string{"/", "/auth/session", "/api/usage", "/health", "/api/missing"} {
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080"+path, nil)
			req.RemoteAddr = remoteAddr
			req.Host = "evil-rebind.com:8080"
			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("remoteAddr=%s host=evil-rebind.com:8080 path=%s got %d, want 403", remoteAddr, path, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "Host no permitido") {
				t.Fatalf("expected Host no permitido error, got %q", rec.Body.String())
			}
		}
	}

	// Legitimate local network, loopback, IPv6, mDNS, and configured origin Hosts succeed.
	for _, host := range []string{
		"127.0.0.1:8080",
		"192.168.1.71:8080",
		"[::1]:8080",
		"mybox.local:8080",
		"dashboard.example.com:8080",
	} {
		for _, remoteAddr := range []string{"127.0.0.1:50000", "192.168.1.50:51234"} {
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/usage", nil)
			req.RemoteAddr = remoteAddr
			req.Host = host
			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("remoteAddr=%s host=%s /api/usage got %d, want 200 (%s)", remoteAddr, host, rec.Code, rec.Body.String())
			}
		}
	}
}

func TestDefaultSingleUseTokenPreservesOriginalExpiry(t *testing.T) {
	store, srv, clock, cleanup := newTestEnv(t)
	defer cleanup()

	t0 := clock.Now()
	wantExp := t0.Add(db.SessionTTL).UTC().Format("2006-01-02T15:04:05.000Z")

	// Mint two default 8h single-use tokens at t0.
	issuedHTTP, err := store.Issue()
	if err != nil {
		t.Fatalf("Issue HTTP token: %v", err)
	}
	issuedDirect, err := store.IssueWithOptions(db.IssueOptions{KeepExisting: true})
	if err != nil {
		t.Fatalf("Issue direct token: %v", err)
	}

	// Advance clock by 4 hours before exchanging.
	clock.Advance(4 * time.Hour)

	// 1. Direct Store.Exchange at t0 + 4h must expire at t0 + 8h (not t0 + 12h).
	directSess, err := store.Exchange(issuedDirect.Token)
	if err != nil || directSess == nil {
		t.Fatalf("Store.Exchange failed: sess=%+v err=%v", directSess, err)
	}
	if directSess.ExpiresMs != t0.Add(db.SessionTTL).UnixMilli() || directSess.ExpiresAt != wantExp {
		t.Fatalf("direct exchange expires = %d (%s), want %d (%s)", directSess.ExpiresMs, directSess.ExpiresAt, t0.Add(db.SessionTTL).UnixMilli(), wantExp)
	}

	// 2. HTTP POST /auth/exchange at t0 + 4h must expire at t0 + 8h (not t0 + 12h).
	body, _ := json.Marshal(map[string]string{"token": issuedHTTP.Token})
	exReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/auth/exchange", bytes.NewReader(body))
	exReq.Header.Set("Origin", "http://127.0.0.1:8080")
	exReq.Header.Set("Content-Type", "application/json")
	exRec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(exRec, exReq)
	if exRec.Code != http.StatusOK {
		t.Fatalf("POST /auth/exchange failed: %d (%s)", exRec.Code, exRec.Body.String())
	}
	var out struct {
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.NewDecoder(exRec.Body).Decode(&out); err != nil {
		t.Fatalf("decode exchange response: %v", err)
	}
	if out.ExpiresAt != wantExp {
		t.Fatalf("HTTP exchange expiresAt = %q, want %q (t0 + 8h, not t0 + 12h)", out.ExpiresAt, wantExp)
	}
	var sessCookie *http.Cookie
	for _, c := range exRec.Result().Cookies() {
		if c.Name == SessionCookieName {
			sessCookie = c
		}
	}
	if sessCookie == nil {
		t.Fatal("missing session cookie")
	}

	// Still valid at t0 + 7h59m.
	clock.Advance(3*time.Hour + 59*time.Minute)
	validReq := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/auth/session", nil)
	validReq.AddCookie(sessCookie)
	validRec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(validRec, validReq)
	if validRec.Code != http.StatusOK {
		t.Fatalf("expected session valid at t0+7h59m, got %d", validRec.Code)
	}

	// Expired at t0 + 8h01m.
	clock.Advance(2 * time.Minute)
	expiredReq := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/auth/session", nil)
	expiredReq.AddCookie(sessCookie)
	expiredRec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(expiredRec, expiredReq)
	if expiredRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected session expired at t0+8h01m (401), got %d", expiredRec.Code)
	}
}

func TestSubMillisecondTTLRejected(t *testing.T) {
	for _, subMs := range []string{"500us", "999us", "1ns", "0.5ms", "0.000000001d"} {
		if _, err := config.ParseDuration(subMs); err == nil {
			t.Fatalf("expected ParseDuration(%q) to reject sub-millisecond duration", subMs)
		}
	}
	if d, err := config.ParseDuration("1ms"); err != nil || d != time.Millisecond {
		t.Fatalf("ParseDuration(1ms) = %v, %v", d, err)
	}

	store, _, clock, cleanup := newTestEnv(t)
	defer cleanup()

	for _, badTTL := range []time.Duration{500 * time.Microsecond, -time.Millisecond} {
		if _, err := store.IssueWithOptions(db.IssueOptions{TTL: badTTL}); err == nil {
			t.Fatalf("expected IssueWithOptions(TTL=%v) to fail", badTTL)
		}
		if _, err := store.CreateSession(badTTL); err == nil {
			t.Fatalf("expected CreateSession(ttl=%v) to fail", badTTL)
		}
	}

	if _, err := store.IssueWithOptions(db.IssueOptions{TTL: time.Millisecond}); err != nil {
		t.Fatalf("expected IssueWithOptions(TTL=1ms) to succeed, got %v", err)
	}
	sess1ms, err := store.CreateSession(time.Millisecond)
	if err != nil || sess1ms.ExpiresMs != clock.Now().Add(time.Millisecond).UnixMilli() {
		t.Fatalf("CreateSession(1ms) = %+v, %v", sess1ms, err)
	}
	sessDefault, err := store.CreateSession(0)
	if err != nil || sessDefault.ExpiresMs != clock.Now().Add(db.SessionTTL).UnixMilli() {
		t.Fatalf("CreateSession(0) = %+v, %v", sessDefault, err)
	}
}
