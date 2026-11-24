package api

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/config"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/security"
)

type contextKey string

const (
	sessionContextKey   contextKey = "session"
	LocalLANSessionHash            = "__local_lan_session__"
)

func SessionFromContext(ctx context.Context) *db.SessionInfo {
	v, _ := ctx.Value(sessionContextKey).(*db.SessionInfo)
	return v
}

func isLANEnabled(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	if cfg.AllowLAN || cfg.AuthMode == "local" {
		return true
	}
	if cfg.BindAddr != "" {
		if addr, err := netip.ParseAddr(cfg.BindAddr); err == nil && !addr.IsLoopback() {
			return true
		}
	}
	return false
}

func (s *Server) isLocalAuthMode() bool {
	return s.Cfg != nil && s.Cfg.AuthMode == "local"
}

func (s *Server) effectiveAuthMode() string {
	if s.isLocalAuthMode() {
		return "local"
	}
	return "token"
}

func (s *Server) isRequestHTTPS(r *http.Request) bool {
	if r.TLS != nil || strings.EqualFold(r.URL.Scheme, "https") {
		return true
	}
	if r.URL.Scheme == "" && s.Cfg != nil && strings.HasPrefix(strings.ToLower(s.Cfg.Origin), "https://") {
		if u, err := url.Parse(s.Cfg.Origin); err == nil && u.Host != "" && strings.EqualFold(u.Host, r.Host) {
			return true
		}
	}
	return false
}

func (s *Server) isRequestLoopbackHost(r *http.Request) bool {
	if r.Host != "" {
		return security.IsLoopbackHost(r.Host)
	}
	return security.IsLoopbackHost(r.RemoteAddr)
}

func (s *Server) sessionCookieSpec(r *http.Request) (string, bool) {
	if isLANEnabled(s.Cfg) && !s.isRequestHTTPS(r) && !s.isRequestLoopbackHost(r) {
		return LANSessionCookieName, false
	}
	return SessionCookieName, true
}

func (s *Server) authenticate(r *http.Request) (*db.SessionInfo, bool) {
	if s.isLocalAuthMode() {
		if !security.IsLocalNetworkAddr(r.RemoteAddr) {
			return nil, false
		}
		return &db.SessionInfo{SessionHash: LocalLANSessionHash}, true
	}
	cookieName, _ := s.sessionCookieSpec(r)
	var rawTokens []string
	for _, c := range r.Cookies() {
		if c.Name == cookieName {
			rawTokens = append(rawTokens, c.Value)
		}
	}
	if len(rawTokens) != 1 {
		return nil, false
	}
	token := strings.TrimSpace(rawTokens[0])
	if !security.SecretRegexp.MatchString(token) {
		return nil, false
	}
	sess, err := s.Store.LookupSession(token)
	if err != nil || sess == nil {
		return nil, false
	}
	return sess, true
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, rawSession string, expiresMs int64) {
	name, secure := s.sessionCookieSpec(r)
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    rawSession,
		Path:     "/",
		Expires:  time.UnixMilli(expiresMs).UTC(),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	name, secure := s.sessionCookieSpec(r)
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) requireSessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.isLocalAuthMode() && !security.IsLocalNetworkAddr(r.RemoteAddr) {
			writeJSONError(w, http.StatusForbidden, "Acceso permitido solo desde red local")
			return
		}
		sess, ok := s.authenticate(r)
		if !ok {
			if r.URL.Path == "/" && r.Method == http.MethodGet {
				http.Redirect(w, r, "/access", http.StatusSeeOther)
				return
			}
			s.clearSessionCookie(w, r)
			writeJSONError(w, http.StatusUnauthorized, "Acceso caducado o no iniciado")
			return
		}
		ctx := context.WithValue(r.Context(), sessionContextKey, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireCSRF(r *http.Request, sess *db.SessionInfo) bool {
	if sess == nil {
		return false
	}
	provided := r.Header.Get("X-CSRF-Token")
	expected := s.Store.CSRFFromSession(sess.RawSession)
	return security.EqualConst(provided, expected)
}

func (s *Server) handleAuthExchange(w http.ResponseWriter, r *http.Request) {
	allowed, err := s.Store.CheckAndRecordExchangeAttempt()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Error de autenticación")
		return
	}
	if !allowed {
		w.Header().Set("Retry-After", "60")
		writeJSONError(w, http.StatusTooManyRequests, "Demasiados intentos. Espera un minuto.")
		return
	}

	var body struct {
		Token string `json:"token"`
	}
	if status := decodeJSONBody(w, r, &body); status != 0 {
		writeJSONError(w, status, "Solicitud inválida")
		return
	}
	if !security.SecretRegexp.MatchString(body.Token) {
		writeJSONError(w, http.StatusUnauthorized, "Token inválido, utilizado o caducado")
		return
	}

	sess, err := s.Store.ExchangeWithDefaultTTL(body.Token, s.Cfg.SessionTTL)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Error de autenticación")
		return
	}
	if sess == nil && s.Cfg.AccessToken != "" && security.EqualConst(body.Token, s.Cfg.AccessToken) {
		ttl := s.Cfg.SessionTTL
		if ttl <= 0 {
			ttl = db.PermanentSessionTTL
		}
		sess, err = s.Store.CreateSession(ttl)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "Error de autenticación")
			return
		}
		sess.Permanent = true
	}
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "Token inválido, utilizado o caducado")
		return
	}

	s.setSessionCookie(w, r, sess.RawSession, sess.ExpiresMs)
	authMode := s.effectiveAuthMode()
	writeJSON(w, http.StatusOK, map[string]any{
		"authMode":      authMode,
		"tokenRequired": authMode != "local",
		"permanent":     sess.Permanent,
		"csrf":          sess.CSRF,
		"expiresAt":     sess.ExpiresAt,
	})
}

func (s *Server) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	if s.isLocalAuthMode() {
		if !security.IsLocalNetworkAddr(r.RemoteAddr) {
			writeJSONError(w, http.StatusForbidden, "Acceso permitido solo desde red local")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"authMode":      "local",
			"tokenRequired": false,
			"csrf":          "",
			"expiresAt":     "",
		})
		return
	}
	sess, ok := s.authenticate(r)
	if !ok {
		s.clearSessionCookie(w, r)
		writeJSONError(w, http.StatusUnauthorized, "Acceso caducado o no iniciado")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authMode":      "token",
		"tokenRequired": true,
		"csrf":          s.Store.CSRFFromSession(sess.RawSession),
		"expiresAt":     sess.ExpiresAt,
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if s.isLocalAuthMode() {
		if !security.IsLocalNetworkAddr(r.RemoteAddr) {
			writeJSONError(w, http.StatusForbidden, "Acceso permitido solo desde red local")
			return
		}
		s.clearSessionCookie(w, r)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	sess := SessionFromContext(r.Context())
	if !s.requireCSRF(r, sess) {
		writeJSONError(w, http.StatusForbidden, "CSRF inválido")
		return
	}
	sessionHash, err := s.Store.DeleteSession(sess.RawSession)
	if err == nil && sessionHash != "" {
		s.Broker.RevokeSession(sessionHash)
	}
	s.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
