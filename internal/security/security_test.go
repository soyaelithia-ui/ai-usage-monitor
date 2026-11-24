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

func TestLocalNetworkAddrAndLANOriginPolicy(t *testing.T) {
	localAddrs := []string{
		"127.0.0.1:54321",
		"[::1]:54321",
		"::1",
		"10.0.0.5:1234",
		"172.16.0.1:1234",
		"172.31.255.254:1234",
		"192.168.1.42:8080",
		"169.254.10.5:80",
		"[fe80::1]:8080",
		"[fe80::1%eth0]:8080",
		"[fd12:3456:789a::1]:8080",
		"[::ffff:192.168.1.42]:8080",
	}
	for _, addr := range localAddrs {
		if !IsLocalNetworkAddr(addr) {
			t.Fatalf("expected local-network addr %q to be allowed", addr)
		}
	}

	nonLocalAddrs := []string{
		"203.0.113.10:12345",
		"8.8.8.8:53",
		"172.32.0.1:8080",
		"192.169.1.1:8080",
		"[2001:db8::1]:8080",
		"[::ffff:203.0.113.10]:8080",
		"0.0.0.0:8080",
		"",
		"not-an-ip",
	}
	for _, addr := range nonLocalAddrs {
		if IsLocalNetworkAddr(addr) {
			t.Fatalf("expected non-local addr %q to be rejected", addr)
		}
	}

	// Strict policy keeps LAN HTTP origins disallowed
	strictPolicy, err := BuildOriginPolicy("http://127.0.0.1:8080", nil)
	if err != nil {
		t.Fatalf("BuildOriginPolicy: %v", err)
	}
	if strictPolicy.IsOriginAllowed("http://192.168.1.50:8080") || strictPolicy.IsOriginAllowed("http://nas.local:8080") {
		t.Fatal("strict policy must reject non-loopback LAN HTTP origins")
	}

	// LAN policy allows RFC1918, ULA, link-local, loopback, and *.local origins
	lanPolicy, err := BuildOriginPolicyWithOptions("http://127.0.0.1:8080", nil, true)
	if err != nil {
		t.Fatalf("BuildOriginPolicyWithOptions: %v", err)
	}
	lanAllowed := []string{
		"http://192.168.1.50:8080",
		"https://192.168.1.50:8443",
		"http://10.0.0.5:8080",
		"http://172.16.10.2:8080",
		"http://169.254.1.5:8080",
		"http://[fd00::1]:8080",
		"http://macbook.local:8080",
		"https://studio.local",
		"http://localhost:8080",
	}
	for _, o := range lanAllowed {
		if !lanPolicy.IsOriginAllowed(o) {
			t.Fatalf("expected LAN origin %q to be allowed", o)
		}
	}
	lanDenied := []string{
		"https://evil.com",
		"http://evil.com",
		"http://203.0.113.10:8080",
		"http://local.evil.com",
		"http://.local",
		"ftp://192.168.1.50",
		"http://user:pass@192.168.1.50:8080",
		"http://192.168.1.50:8080/path",
	}
	for _, o := range lanDenied {
		if lanPolicy.IsOriginAllowed(o) {
			t.Fatalf("expected external/invalid origin %q to be denied in LAN mode", o)
		}
	}

	// LAN policy still enforces Sec-Fetch-Site: cross-site and blocks external Origin mutations
	rLANPost := httptest.NewRequest(http.MethodPost, "http://192.168.1.50:8080/auth/exchange", nil)
	rLANPost.Header.Set("Origin", "http://192.168.1.50:8080")
	rLANPost.Header.Set("Content-Type", "application/json")
	rLANPost.Header.Set("Sec-Fetch-Site", "same-origin")
	if got := lanPolicy.CheckOriginAndFraming(rLANPost); got != 0 {
		t.Fatalf("expected 0 for same-origin LAN POST, got %d", got)
	}

	rEvilPost := httptest.NewRequest(http.MethodPost, "http://192.168.1.50:8080/auth/exchange", nil)
	rEvilPost.Header.Set("Origin", "https://evil.com")
	rEvilPost.Header.Set("Content-Type", "application/json")
	if got := lanPolicy.CheckOriginAndFraming(rEvilPost); got != http.StatusForbidden {
		t.Fatalf("expected 403 for evil.com POST in LAN mode, got %d", got)
	}

	rCrossAPI := httptest.NewRequest(http.MethodGet, "http://192.168.1.50:8080/api/usage", nil)
	rCrossAPI.Header.Set("Sec-Fetch-Site", "cross-site")
	rCrossAPI.Header.Set("Sec-Fetch-Mode", "cors")
	if got := lanPolicy.CheckOriginAndFraming(rCrossAPI); got != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-site API request in LAN mode, got %d", got)
	}
}

func TestOriginPolicyIsHostAllowed(t *testing.T) {
	var nilPolicy *OriginPolicy
	if nilPolicy.IsHostAllowed("127.0.0.1:8080") {
		t.Fatal("nil OriginPolicy must reject Host")
	}

	policy, err := BuildOriginPolicyWithOptions(
		"https://monitor.example.com",
		[]string{
			"https://pinned.example.org:8443",
			"https://*.tailnet.dev",
			"https://*.port.dev:9443",
		},
		true,
	)
	if err != nil {
		t.Fatalf("BuildOriginPolicyWithOptions: %v", err)
	}

	allowed := []string{
		"127.0.0.1:8080",
		"127.0.0.1",
		"localhost:8080",
		"LOCALHOST",
		"192.168.1.71:8080",
		"10.0.0.5:8080",
		"172.16.0.1:8080",
		"169.254.1.5:8080",
		"[::1]:8080",
		"[::1]",
		"::1",
		"[fd12:3456:789a::1]:8080",
		"[fe80::1%eth0]:8080",
		"mybox.local:8080",
		"MyBox.LOCAL",
		"monitor.example.com",
		"monitor.example.com:8080",
		"Monitor.Example.COM:8080",
		"pinned.example.org:8443",
		"node1.tailnet.dev",
		"node1.tailnet.dev:8080",
		"box.port.dev:9443",
	}
	for _, host := range allowed {
		if !policy.IsHostAllowed(host) {
			t.Fatalf("expected Host %q to be allowed", host)
		}
	}

	denied := []string{
		"",
		"   ",
		" 127.0.0.1:8080",
		"127.0.0.1:8080 ",
		"127.0.0.1\n:8080",
		"127.0.0.1\x00",
		"evil-rebind.com:8080",
		"evil-rebind.com",
		"203.0.113.10:8080",
		"0.0.0.0:8080",
		"user@127.0.0.1:8080",
		"127.0.0.1:8080/evil",
		"127.0.0.1:badport",
		"127.0.0.1:",
		"[mybox.local]:8080",
		"foo[bar].local",
		"localhost.evil.com:8080",
		".local:8080",
		"pinned.example.org",
		"pinned.example.org:9090",
		"a.b.tailnet.dev",
		"box.port.dev",
		"box.port.dev:8080",
	}
	for _, host := range denied {
		if policy.IsHostAllowed(host) {
			t.Fatalf("expected Host %q to be denied", host)
		}
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
