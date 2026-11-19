package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/security"
)

type contextKey string

const sessionContextKey contextKey = "session"

func SessionFromContext(ctx context.Context) *db.SessionInfo {
	v, _ := ctx.Value(sessionContextKey).(*db.SessionInfo)
	return v
}

func (s *Server) authenticate(r *http.Request) (*db.SessionInfo, bool) {
	var rawTokens []string
	for _, c := range r.Cookies() {
		if c.Name == SessionCookieName {
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

func (s *Server) setSessionCookie(w http.ResponseWriter, rawSession string, expiresMs int64) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    rawSession,
		Path:     "/",
		Expires:  time.UnixMilli(expiresMs).UTC(),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) requireSessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.authenticate(r)
		if !ok {
			if r.URL.Path == "/" && r.Method == http.MethodGet {
				http.Redirect(w, r, "/access", http.StatusSeeOther)
				return
			}
			s.clearSessionCookie(w)
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

	sess, err := s.Store.Exchange(body.Token)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Error de autenticación")
		return
	}
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "Token inválido, utilizado o caducado")
		return
	}

	s.setSessionCookie(w, sess.RawSession, sess.ExpiresMs)
	writeJSON(w, http.StatusOK, map[string]string{
		"csrf":      sess.CSRF,
		"expiresAt": sess.ExpiresAt,
	})
}

func (s *Server) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.authenticate(r)
	if !ok {
		s.clearSessionCookie(w)
		writeJSONError(w, http.StatusUnauthorized, "Acceso caducado o no iniciado")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"csrf":      s.Store.CSRFFromSession(sess.RawSession),
		"expiresAt": sess.ExpiresAt,
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	sess := SessionFromContext(r.Context())
	if !s.requireCSRF(r, sess) {
		writeJSONError(w, http.StatusForbidden, "CSRF inválido")
		return
	}
	sessionHash, err := s.Store.DeleteSession(sess.RawSession)
	if err == nil && sessionHash != "" {
		s.Broker.RevokeSession(sessionHash)
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
