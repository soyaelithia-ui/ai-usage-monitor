package security

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

type wildcardRule struct {
	scheme string
	port   string
	suffix string
}

// OriginPolicy validates Origin headers against the configured origin,
// loopback aliases, validated single-label wildcard rules, and optional LAN origins.
type OriginPolicy struct {
	exact     map[string]struct{}
	wildcards []wildcardRule
	allowLAN  bool
}

// IsLoopbackHost reports whether a host or host:port refers to a loopback address or localhost.
func IsLoopbackHost(hostPort string) bool {
	host := extractHost(hostPort)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return addr.Unmap().IsLoopback()
}

// IsLocalNetworkAddr reports whether remoteAddr (host:port or raw IP) is loopback,
// RFC1918 private, IPv4/IPv6 link-local unicast, or IPv6 ULA.
func IsLocalNetworkAddr(remoteAddr string) bool {
	host := extractHost(remoteAddr)
	if host == "" {
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}

// IsLocalNetworkHost reports whether a hostname or IP belongs to the local machine or LAN
// (loopback, localhost, RFC1918 private, link-local unicast, IPv6 ULA, or *.local mDNS).
func IsLocalNetworkHost(host string) bool {
	h := strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	if h == "" {
		return false
	}
	if h == "localhost" {
		return true
	}
	if strings.HasSuffix(h, ".local") {
		return len(h) > len(".local") && !strings.HasPrefix(h, ".") && !strings.ContainsAny(h, "* /\\@:")
	}
	addr, err := netip.ParseAddr(h)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}

// IsLocalNetworkOrigin reports whether candidate is a clean HTTP or HTTPS origin on a local-network host.
func IsLocalNetworkOrigin(candidate string) bool {
	u, err := url.Parse(candidate)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Host == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return IsLocalNetworkHost(u.Hostname())
}

func extractHost(hostPort string) string {
	s := strings.TrimSpace(hostPort)
	if s == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	return strings.Trim(s, "[]")
}

// NormalizeAllowedOrigin validates a single allowedOrigin entry under strict HTTPS/loopback rules.
func NormalizeAllowedOrigin(raw string) (string, error) {
	return normalizeAllowedOrigin(raw, false)
}

func normalizeAllowedOrigin(raw string, allowLAN bool) (string, error) {
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
	isLAN := allowLAN && u.Scheme == "http" && IsLocalNetworkHost(host)
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback) && !isLAN {
		return "", errors.New("allowed origin must be https or loopback http")
	}
	return u.Scheme + "://" + u.Host, nil
}

// BuildOriginPolicy constructs a strict OriginPolicy from primary origin and allowedOrigins.
func BuildOriginPolicy(primary string, allowedOrigins []string) (*OriginPolicy, error) {
	return BuildOriginPolicyWithOptions(primary, allowedOrigins, false)
}

// BuildOriginPolicyWithOptions constructs an OriginPolicy with optional LAN origin allowance.
func BuildOriginPolicyWithOptions(primary string, allowedOrigins []string, allowLAN bool) (*OriginPolicy, error) {
	p := &OriginPolicy{
		exact:    make(map[string]struct{}),
		allowLAN: allowLAN,
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
		norm, err := normalizeAllowedOrigin(entry, allowLAN)
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
	if p.allowLAN && IsLocalNetworkOrigin(candidate) {
		return true
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
