package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/security"
)

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	if s.isLocalAuthMode() && !security.IsLocalNetworkAddr(r.RemoteAddr) {
		writeJSONError(w, http.StatusForbidden, "Acceso permitido solo desde red local")
		return
	}
	sess := SessionFromContext(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "Acceso caducado o no iniciado")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "Streaming no soportado")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store, no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	_, _ = fmt.Fprintf(w, "event: ready\ndata: %d\n\n", s.Broker.Revision())
	flusher.Flush()

	ch, unsub := s.Broker.Subscribe(sess.SessionHash)
	defer unsub()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, err := fmt.Fprint(w, ": heartbeat\n\n")
			if err != nil {
				return
			}
			flusher.Flush()
		case ev, open := <-ch:
			if !open {
				return
			}
			_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, ev.Data)
			if err != nil {
				return
			}
			flusher.Flush()
			if ev.Name == "expired" {
				return
			}
		}
	}
}
