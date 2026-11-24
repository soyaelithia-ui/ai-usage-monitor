package api

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/config"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/ingest"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/security"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/sse"
)

const (
	SessionCookieName    = "__Host-usage-monitor"
	LANSessionCookieName = "usage-monitor"
	MaxJSONBodyBytes     = 64 * 1024
)

// Server holds dependencies for HTTP API and SSE handlers.
type Server struct {
	Cfg          *config.Config
	Store        *db.Store
	Broker       *sse.Broker
	Ingest       *ingest.Manager
	OriginPolicy *security.OriginPolicy
	RateLimiter  *security.RateLimiter
	StaticFS     fs.FS
	Handler      http.Handler
	stopSweep    chan struct{}
}

// NewServer constructs the HTTP router with full security perimeter middleware.
func NewServer(cfg *config.Config, store *db.Store, broker *sse.Broker, ingestMgr *ingest.Manager, staticFS fs.FS) (*Server, error) {
	originPolicy, err := security.BuildOriginPolicyWithOptions(cfg.Origin, cfg.AllowedOrigins, isLANEnabled(cfg))
	if err != nil {
		return nil, err
	}
	if broker == nil {
		broker = sse.NewBroker()
	}

	s := &Server{
		Cfg:          cfg,
		Store:        store,
		Broker:       broker,
		Ingest:       ingestMgr,
		OriginPolicy: originPolicy,
		RateLimiter:  security.NewRateLimiter(cfg.RateLimitMax, store.NowFn),
		StaticFS:     staticFS,
		stopSweep:    make(chan struct{}),
	}

	r := chi.NewRouter()

	r.Get("/health", s.handleHealth)

	// Public authentication & entry endpoints
	r.Post("/auth/exchange", s.handleAuthExchange)
	r.Get("/auth/session", s.handleAuthSession)
	r.Get("/access", s.handleAccessPage)

	// Public static bundles (compiled Svelte/CSS/fonts/icons; contain zero telemetry data)
	r.Get("/assets/*", s.handleStaticAsset)
	r.Get("/fonts/*", s.handleStaticAsset)
	r.Get("/icons/*", s.handleStaticAsset)
	r.Get("/favicon.svg", s.handleStaticAsset)
	r.Get("/theme-init.js", s.handleStaticAsset)

	// Local bearer-protected ingestion & HTTP hook routes (exempt from browser session cookie, origin & UA check)
	r.Post("/api/ingest", s.handleIngest)
	r.Post("/api/agents/register", s.handleAgentRegister)
	r.Post("/api/hooks/{agent}", s.handleAgentHookHTTP)

	// Protected routes (fail-closed session cookie required)
	r.Group(func(pr chi.Router) {
		pr.Use(s.requireSessionMiddleware)
		pr.Get("/", s.handleIndexPage)
		pr.Post("/auth/logout", s.handleAuthLogout)
		pr.Get("/api/analytics", s.handleAnalytics)
		pr.Get("/api/usage", s.handleAnalytics)
		pr.Get("/api/events", s.handleEvents)
		pr.Get("/api/sources", s.handleSources)
		pr.Get("/api/hooks", s.handleHooksOverview)
		pr.Get("/api/discovery", s.handleDiscovery)
		pr.Get("/api/reconciliation", s.handleReconciliation)
		pr.Get("/api/pricing", s.handlePricing)
		pr.Get("/api/stream", s.handleSSE)
		pr.Get("/api/sse", s.handleSSE)
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if s.isLocalAuthMode() && !security.IsLocalNetworkAddr(r.RemoteAddr) {
			writeJSONError(w, http.StatusForbidden, "Acceso permitido solo desde red local")
			return
		}
		// Fail closed on any unknown path: require session if not public
		if _, ok := s.authenticate(r); !ok {
			s.clearSessionCookie(w, r)
			writeJSONError(w, http.StatusUnauthorized, "Acceso caducado o no iniciado")
			return
		}
		writeJSONError(w, http.StatusNotFound, "No encontrado")
	})

	s.Handler = s.perimeterMiddleware(r)
	go s.runSessionSweep()
	return s, nil
}

// Close stops background SSE sweep and closes active streams.
func (s *Server) Close() {
	select {
	case <-s.stopSweep:
	default:
		close(s.stopSweep)
	}
	if s.Broker != nil {
		s.Broker.Close()
	}
}

func (s *Server) runSessionSweep() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopSweep:
			return
		case <-ticker.C:
			for _, hash := range s.Broker.ActiveSessionHashes() {
				if hash == LocalLANSessionHash {
					continue
				}
				if !s.Store.IsSessionHashActive(hash) {
					s.Broker.RevokeSession(hash)
				}
			}
		}
	}
}

func (s *Server) perimeterMiddleware(next http.Handler) http.Handler {
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.RateLimiter.Allow() {
			w.Header().Set("Retry-After", "60")
			writeJSONError(w, http.StatusTooManyRequests, "Demasiadas solicitudes")
			return
		}

		isLocalHook := strings.HasPrefix(r.URL.Path, "/api/hooks/") &&
			!strings.Contains(strings.TrimPrefix(r.URL.Path, "/api/hooks/"), "/")
		localIngest := r.Method == http.MethodPost &&
			(r.URL.Path == "/api/ingest" || r.URL.Path == "/api/agents/register" || isLocalHook)

		if status := security.GuardRequest(r, localIngest); status != 0 {
			writeJSONError(w, status, "Solicitud rechazada")
			return
		}

		if s.isLocalAuthMode() && !security.IsLocalNetworkAddr(r.RemoteAddr) {
			writeJSONError(w, http.StatusForbidden, "Acceso permitido solo desde red local")
			return
		}

		if !localIngest {
			if s.Cfg.GatewayKey != "" && !security.EqualConst(r.Header.Get("X-Usage-Monitor-Gateway"), s.Cfg.GatewayKey) {
				writeJSONError(w, http.StatusForbidden, "Gateway requerido")
				return
			}
			if status := s.OriginPolicy.CheckOriginAndFraming(r); status != 0 {
				writeJSONError(w, status, "Origen o formato no permitido")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
	return security.HeadersMiddleware(base)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAccessPage(w http.ResponseWriter, r *http.Request) {
	s.serveHTMLAsset(w, r, "index.html")
}

func (s *Server) handleIndexPage(w http.ResponseWriter, r *http.Request) {
	s.serveHTMLAsset(w, r, "index.html")
}

func (s *Server) serveHTMLAsset(w http.ResponseWriter, r *http.Request, name string) {
	if s.StaticFS == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><body>AI Usage Monitor</body></html>"))
		return
	}
	f, err := s.StaticFS.Open(name)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><body>AI Usage Monitor</body></html>"))
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Error de lectura")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) handleStaticAsset(w http.ResponseWriter, r *http.Request) {
	if s.StaticFS == nil {
		writeJSONError(w, http.StatusNotFound, "No encontrado")
		return
	}
	clean := strings.TrimPrefix(r.URL.Path, "/")
	if clean == "" || strings.Contains(clean, "..") || strings.HasPrefix(clean, ".") {
		writeJSONError(w, http.StatusNotFound, "No encontrado")
		return
	}
	info, err := fs.Stat(s.StaticFS, clean)
	if err != nil || info.IsDir() {
		writeJSONError(w, http.StatusNotFound, "No encontrado")
		return
	}
	http.FileServerFS(s.StaticFS).ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) int {
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if err.Error() == "http: request body too large" || isMaxBytesErr(err, &maxErr) {
			return http.StatusRequestEntityTooLarge
		}
		return http.StatusBadRequest
	}
	return 0
}

func isMaxBytesErr(err error, target **http.MaxBytesError) bool {
	if mbe, ok := err.(*http.MaxBytesError); ok {
		*target = mbe
		return true
	}
	return false
}
