package security

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardRequestTraversalAndScanners(t *testing.T) {
	cases := []struct {
		name   string
		uri    string
		want   int
		ingest bool
	}{
		{"normal", "/api/analytics?window=1h", 0, false},
		{"too long", "/" + strings.Repeat("a", 2050), http.StatusBadRequest, false},
		{"control char", "/api/usage\x07", http.StatusBadRequest, false},
		{"dotdot", "/../../etc/passwd", http.StatusNotFound, false},
		{"encoded dotdot", "/%2e%2e/secret", http.StatusNotFound, false},
		{"double encoded", "/%252e%252e/secret", http.StatusNotFound, false},
		{"backslash", "/icons\\secret.png", http.StatusNotFound, false},
		{"encoded backslash", "/icons/%5csecret", http.StatusNotFound, false},
		{"null byte", "/%00/admin", http.StatusNotFound, false},
		{"scanner env", "/.env", http.StatusNotFound, false},
		{"scanner git", "/.git/config", http.StatusNotFound, false},
		{"scanner wp", "/wp-login.php", http.StatusNotFound, false},
		{"scanner phpmyadmin", "/phpmyadmin/index.php", http.StatusNotFound, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/", nil)
			req.RequestURI = tc.uri
			got := GuardRequest(req, tc.ingest)
			if got != tc.want {
				t.Fatalf("GuardRequest(%q) = %d, want %d", tc.uri, got, tc.want)
			}
		})
	}
}

func TestGuardRequestLeadingSlashCollapse(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/access", nil)
	req.RequestURI = "///access"
	req.URL.Path = "///access"
	if got := GuardRequest(req, false); got != 0 {
		t.Fatalf("unexpected status %d", got)
	}
	if req.URL.Path != "/access" {
		t.Fatalf("expected collapsed path /access, got %q", req.URL.Path)
	}
}

func TestProxySpoofingAndBotUA(t *testing.T) {
	for _, hdr := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Real-IP", "Via", "Forwarded", "Forwarded-For"} {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/health", nil)
		req.Header.Set(hdr, "1.2.3.4")
		if got := GuardRequest(req, false); got != http.StatusForbidden {
			t.Fatalf("expected 403 for %s, got %d", hdr, got)
		}
	}

	// Duplicate smuggling UA
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/health", nil)
	req.Header.Add("User-Agent", "Mozilla/5.0")
	req.Header.Add("User-Agent", "sqlmap/1.7")
	if got := GuardRequest(req, false); got != http.StatusForbidden {
		t.Fatalf("expected 403 for smuggled bot UA, got %d", got)
	}

	// Local ingest exempts UA check only, never proxy headers
	reqIngest := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/ingest", nil)
	reqIngest.Header.Set("User-Agent", "Go-http-client/2.0")
	if got := GuardRequest(reqIngest, true); got != 0 {
		t.Fatalf("expected 0 for localIngest UA, got %d", got)
	}
	reqIngest.Header.Set("X-Real-IP", "1.2.3.4")
	if got := GuardRequest(reqIngest, true); got != http.StatusForbidden {
		t.Fatalf("expected 403 for localIngest with proxy header, got %d", got)
	}
}

func TestOriginPolicyAndFraming(t *testing.T) {
	policy, err := BuildOriginPolicy("http://localhost:8080", []string{"https://*.example.dev"})
	if err != nil {
		t.Fatalf("BuildOriginPolicy error: %v", err)
	}
	allowed := []string{
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"http://[::1]:8080",
		"https://sub.example.dev",
	}
	for _, o := range allowed {
		if !policy.IsOriginAllowed(o) {
			t.Fatalf("expected allowed origin %q", o)
		}
	}
	denied := []string{
		"http://localhost:9090",
		"https://a.b.example.dev",
		"https://example.dev",
		"https://evil.com",
	}
	for _, o := range denied {
		if policy.IsOriginAllowed(o) {
			t.Fatalf("expected denied origin %q", o)
		}
	}

	// Framing blocked
	rFrame := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/", nil)
	rFrame.Header.Set("Sec-Fetch-Dest", "iframe")
	if got := policy.CheckOriginAndFraming(rFrame); got != http.StatusForbidden {
		t.Fatalf("expected 403 for iframe, got %d", got)
	}

	// Cross-site exception only on GET / or /access document navigate
	rCrossAPI := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/usage", nil)
	rCrossAPI.Header.Set("Sec-Fetch-Site", "cross-site")
	rCrossAPI.Header.Set("Sec-Fetch-Mode", "cors")
	rCrossAPI.Header.Set("Sec-Fetch-Dest", "empty")
	if got := policy.CheckOriginAndFraming(rCrossAPI); got != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-site API, got %d", got)
	}

	rCrossEntry := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/access", nil)
	rCrossEntry.Header.Set("Sec-Fetch-Site", "cross-site")
	rCrossEntry.Header.Set("Sec-Fetch-Mode", "navigate")
	rCrossEntry.Header.Set("Sec-Fetch-Dest", "document")
	if got := policy.CheckOriginAndFraming(rCrossEntry); got != 0 {
		t.Fatalf("expected 0 for cross-site entry navigation, got %d", got)
	}

	// Mutation requires Origin + application/json
	rPostNoCT := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/auth/logout", nil)
	rPostNoCT.Header.Set("Origin", "http://127.0.0.1:8080")
	rPostNoCT.Header.Set("Content-Type", "text/plain")
	if got := policy.CheckOriginAndFraming(rPostNoCT); got != http.StatusForbidden {
		t.Fatalf("expected 403 for text/plain mutation, got %d", got)
	}
}

func TestSanitizeIconURLAndRate(t *testing.T) {
	if u, ok := SanitizeIconURL("/icons/codex.svg"); !ok || u != "/icons/codex.svg" {
		t.Fatalf("expected valid local icon, got %q %v", u, ok)
	}
	if _, ok := SanitizeIconURL("/icons/../secret.png"); ok {
		t.Fatal("expected traversal icon rejected")
	}
	if _, ok := SanitizeIconURL("/icons/%2e%2e/secret.png"); ok {
		t.Fatal("expected encoded traversal icon rejected")
	}
	if _, ok := SanitizeIconURL("data:image/svg+xml;base64,PHN2Zz4="); ok {
		t.Fatal("expected svg data URI rejected")
	}
	if _, ok := SanitizeIconURL("data:image/png;base64,iVBORw0KGgo="); !ok {
		t.Fatal("expected png data URI accepted")
	}
	if _, ok := SanitizeIconURL("https://user:pass@example.com/a.png"); ok {
		t.Fatal("expected credentialed https URL rejected")
	}
	if _, ok := SanitizeIconURL("https://cdn.example.com/a.png"); !ok {
		t.Fatal("expected clean https URL accepted")
	}

	if !ValidRate("12.5") || !ValidRate("0") || !ValidRate("0.123456789012") {
		t.Fatal("expected valid rates")
	}
	if ValidRate("-1") || ValidRate("1e5") || ValidRate("NaN") || ValidRate("") {
		t.Fatal("expected invalid rates rejected")
	}
}

func TestSealerMachineKeyLifecycle(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s1, err := NewSealer(dbPath, "")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	keyPath := filepath.Join(dir, "machine.key")
	st, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("machine.key stat: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 perms, got %o", st.Mode().Perm())
	}
	sig1 := s1.Seal("secret-token")

	// Reload same key -> identical seal
	s2, err := NewSealer(dbPath, "")
	if err != nil {
		t.Fatalf("reload NewSealer: %v", err)
	}
	if sig2 := s2.Seal("secret-token"); sig1 != sig2 {
		t.Fatal("expected deterministic seal with persisted machine.key")
	}

	// Delete machine.key -> new key invalidates old seals
	if err := os.Remove(keyPath); err != nil {
		t.Fatalf("remove machine.key: %v", err)
	}
	s3, err := NewSealer(dbPath, "")
	if err != nil {
		t.Fatalf("recreate NewSealer: %v", err)
	}
	if s3.SealedEqual("secret-token", sig1) {
		t.Fatal("deleting machine.key must revoke old seals")
	}
}
