package security

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type wildcardRule struct {
	scheme string
	port   string
	suffix string
}

// OriginPolicy validates Origin headers against the configured origin,
// loopback aliases, and validated single-label wildcard rules.
type OriginPolicy struct {
	exact     map[string]struct{}
	wildcards []wildcardRule
}

// NormalizeAllowedOrigin validates a single allowedOrigin entry.
func NormalizeAllowedOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty allowed origin")
	}
	const prefix = "https://*."
	if strings.HasPrefix(raw, prefix) {
		rest := raw[len(prefix):]
		u, err := url.Parse("https://" + rest)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return "", errors.New("invalid wildcard origin")
		}
		host := u.Hostname()
		if host == "" || strings.Contains(host, "*") || !strings.Contains(host, ".") {
			return "", errors.New("invalid wildcard domain")
		}
		port := u.Port()
		if port != "" {
			return prefix + host + ":" + port, nil
		}
		return prefix + host, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("invalid allowed origin")
	}
	host := u.Hostname()
	isLoopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback) {
		return "", errors.New("allowed origin must be https or loopback http")
	}
	return u.Scheme + "://" + u.Host, nil
}

// BuildOriginPolicy constructs an OriginPolicy from primary origin and allowedOrigins.
func BuildOriginPolicy(primary string, allowedOrigins []string) (*OriginPolicy, error) {
	p := &OriginPolicy{
		exact: make(map[string]struct{}),
	}
	u, err := url.Parse(primary)
	if err != nil || u.Host == "" {
		return nil, errors.New("invalid primary origin")
	}
	p.exact[u.Scheme+"://"+u.Host] = struct{}{}

	host := u.Hostname()
	if u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1") {
		port := u.Port()
		portSuffix := ""
		if port != "" {
			portSuffix = ":" + port
		}
		p.exact["http://localhost"+portSuffix] = struct{}{}
		p.exact["http://127.0.0.1"+portSuffix] = struct{}{}
		p.exact["http://[::1]"+portSuffix] = struct{}{}
	}

	for _, entry := range allowedOrigins {
		norm, err := NormalizeAllowedOrigin(entry)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(norm, "https://*.") {
			rest := norm[len("https://*."):]
			wu, _ := url.Parse("https://" + rest)
			p.wildcards = append(p.wildcards, wildcardRule{
				scheme: "https",
				port:   wu.Port(),
				suffix: "." + wu.Hostname(),
			})
		} else {
			p.exact[norm] = struct{}{}
		}
	}
	return p, nil
}

// IsOriginAllowed checks whether candidate matches the policy.
func (p *OriginPolicy) IsOriginAllowed(candidate string) bool {
	if candidate == "" {
		return false
	}
	if _, ok := p.exact[candidate]; ok {
		return true
	}
	u, err := url.Parse(candidate)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	norm := u.Scheme + "://" + u.Host
	if _, ok := p.exact[norm]; ok {
		return true
	}
	hostname := u.Hostname()
	port := u.Port()
	for _, w := range p.wildcards {
		if u.Scheme != w.scheme || port != w.port {
			continue
		}
		if !strings.HasSuffix(hostname, w.suffix) {
			continue
		}
		label := strings.TrimSuffix(hostname, w.suffix)
		if label != "" && !strings.Contains(label, ".") {
			return true
		}
	}
	return false
}

// CheckOriginAndFraming enforces Origin allowlist, Content-Type on mutations,
// framing denial, and cross-site Fetch Metadata rules.
// Returns HTTP status code (0 means allowed).
func (p *OriginPolicy) CheckOriginAndFraming(r *http.Request) int {
	dest := strings.ToLower(r.Header.Get("Sec-Fetch-Dest"))
	if dest == "iframe" || dest == "frame" || dest == "embed" || dest == "object" {
		return http.StatusForbidden
	}

	origin := r.Header.Get("Origin")
	if origin != "" && !p.IsOriginAllowed(origin) {
		return http.StatusForbidden
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if !p.IsOriginAllowed(origin) {
			return http.StatusForbidden
		}
		ct := strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])
		if !strings.EqualFold(ct, "application/json") {
			return http.StatusForbidden
		}
	}

	site := strings.ToLower(r.Header.Get("Sec-Fetch-Site"))
	mode := strings.ToLower(r.Header.Get("Sec-Fetch-Mode"))
	entryNav := r.Method == http.MethodGet &&
		(r.URL.Path == "/" || r.URL.Path == "/access") &&
		mode == "navigate" &&
		dest == "document"
	if site == "cross-site" && !entryNav {
		return http.StatusForbidden
	}

	return 0
}
