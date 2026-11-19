package security

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	botUARegex = regexp.MustCompile(`(?i)sqlmap|nikto|nmap|masscan|zgrab|hydra|dirbuster|gobuster|ffuf|nuclei|nessus|openvas|acunetix|netsparker|wpscan|libwww|python-requests|go-http-client|scrapy|phantomjs|selenium|puppeteer|playwright|headlesschrome|bot|crawler|spider|slurp|archiver|java/|httpclient`)

	scannerPathRegex = regexp.MustCompile(`(?i)^/(?:\.env(?:[./].*)?|\.git(?:/.*)?|\.svn(?:/.*)?|\.hg(?:/.*)?|\.ds_store(?:/.*)?|\.aws(?:/.*)?|\.ssh(?:/.*)?|\.docker(?:/.*)?|wp-(?:admin|login|content|includes)(?:\.php(?:/.*)?|/.*)?|xmlrpc\.php(?:/.*)?|phpmyadmin(?:/.*)?|server-status(?:/.*)?|server-info(?:/.*)?|actuator(?:/.*)?|debug(?:/.*)?|console(?:/.*)?|cgi-bin(?:/.*)?|vendor/phpunit(?:/.*)?|\.well-known/security\.txt(?:/.*)?)$`)

	traversalRegex = regexp.MustCompile(`(?i)(?:\.\.|%2e%2e|\.%2e|%2e\.|\x00|%00|\\|%5c|%252e)`)
)

// RateLimiter implements a fixed-window global rate limiter.
type RateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	count   int
	resetAt time.Time
	nowFn   func() time.Time
}

// NewRateLimiter creates a global rate limiter with the given per-minute limit.
func NewRateLimiter(limit int, nowFn func() time.Time) *RateLimiter {
	if limit <= 0 {
		limit = 600
	}
	if nowFn == nil {
		nowFn = time.Now
	}
	return &RateLimiter{
		limit:  limit,
		window: time.Minute,
		nowFn:  nowFn,
	}
}

// Allow checks whether the next request is within the global quota.
func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.nowFn()
	if rl.resetAt.IsZero() || !now.Before(rl.resetAt) {
		rl.resetAt = now.Add(rl.window)
		rl.count = 0
	}
	if rl.count >= rl.limit {
		return false
	}
	rl.count++
	return true
}

func hasControlChar(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

func collapseLeadingSlashes(s string) string {
	for strings.HasPrefix(s, "//") {
		s = s[1:]
	}
	return s
}

// GuardRequest performs perimeter checks on raw URI, traversal, scanner probes,
// proxy spoofing headers, and automated bot User-Agents.
// Returns 0 if allowed, or an HTTP status code to reject with.
func GuardRequest(r *http.Request, localIngest bool) int {
	rawURI := r.RequestURI
	if rawURI == "" {
		rawURI = r.URL.RequestURI()
	}
	if len(rawURI) > 2048 || hasControlChar(rawURI) {
		return http.StatusBadRequest
	}
	if traversalRegex.MatchString(rawURI) {
		return http.StatusNotFound
	}

	// Collapse consecutive leading slashes so protocol-relative paths cannot bypass routing.
	if strings.HasPrefix(r.URL.Path, "//") {
		r.URL.Path = collapseLeadingSlashes(r.URL.Path)
	}
	if strings.HasPrefix(r.URL.RawPath, "//") {
		r.URL.RawPath = collapseLeadingSlashes(r.URL.RawPath)
	}

	decodedPath := r.URL.Path
	if rawPath := r.URL.RawPath; rawPath != "" {
		if unesc, err := url.PathUnescape(rawPath); err == nil {
			decodedPath = unesc
		}
	} else if idx := strings.IndexByte(rawURI, '?'); idx >= 0 {
		if unesc, err := url.PathUnescape(collapseLeadingSlashes(rawURI[:idx])); err == nil {
			decodedPath = unesc
		}
	} else if unesc, err := url.PathUnescape(collapseLeadingSlashes(rawURI)); err == nil {
		decodedPath = unesc
	}

	if strings.Contains(decodedPath, "..") || strings.ContainsRune(decodedPath, '\x00') || strings.ContainsRune(decodedPath, '\\') {
		return http.StatusNotFound
	}
	if scannerPathRegex.MatchString(decodedPath) || scannerPathRegex.MatchString(r.URL.Path) {
		return http.StatusNotFound
	}

	if HasProxySpoofingHeaders(r) {
		return http.StatusForbidden
	}

	if !localIngest {
		for _, ua := range r.Header.Values("User-Agent") {
			if botUARegex.MatchString(ua) {
				return http.StatusForbidden
			}
		}
	}

	return 0
}
