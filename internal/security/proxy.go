package security

import (
	"net/http"
	"strings"
)

// HasProxySpoofingHeaders enforces backend loopback isolation by rejecting
// any request bearing forwarding or proxy-chaining headers.
func HasProxySpoofingHeaders(r *http.Request) bool {
	for rawName, values := range r.Header {
		name := strings.ToLower(rawName)
		if name == "x-real-ip" ||
			name == "via" ||
			name == "forwarded" ||
			strings.HasPrefix(name, "forwarded-") ||
			strings.HasPrefix(name, "x-forwarded-") {
			return true
		}
		// Reject duplicate conflicting security-sensitive single-value headers.
		if (name == "host" || name == "origin" || name == "content-type") && len(values) > 1 {
			return true
		}
	}
	return false
}
