package security

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	rasterDataURIRegex = regexp.MustCompile(`(?i)^data:image/(?:png|jpeg|webp|gif);base64,[A-Za-z0-9+/=]+$`)
	rateStringRegex    = regexp.MustCompile(`^(?:0|[1-9]\d{0,9})(?:\.\d{1,12})?$`)
)

// SanitizeIconURL allows only safe https:// URLs (no credentials), local /icons/... paths
// (without traversal or backslashes), or safe raster data URIs (never SVG data URIs).
func SanitizeIconURL(raw string) (string, bool) {
	u := strings.TrimSpace(raw)
	if u == "" || len(u) > 2048 || strings.ContainsRune(u, '\\') || strings.Contains(strings.ToLower(u), "%5c") {
		return "", false
	}
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c <= 0x20 || c == 0x7f || c == '<' || c == '>' || c == '"' {
			return "", false
		}
	}
	if strings.HasPrefix(u, "https://") {
		parsed, err := url.Parse(u)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return "", false
		}
		return u, true
	}
	if strings.HasPrefix(u, "/icons/") && !strings.HasPrefix(u, "//") && !strings.Contains(u, "..") {
		decoded, err := url.PathUnescape(u)
		if err != nil {
			return "", false
		}
		if !strings.Contains(decoded, "..") && !strings.ContainsRune(decoded, '\\') && !strings.HasPrefix(decoded, "//") {
			return u, true
		}
		return "", false
	}
	if rasterDataURIRegex.MatchString(u) {
		return u, true
	}
	return "", false
}

// ValidRate validates a positive finite decimal rate string.
func ValidRate(raw string) bool {
	return rateStringRegex.MatchString(raw)
}
