package api

import (
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/hooks"
)

func (s *Server) handleHooksOverview(w http.ResponseWriter, _ *http.Request) {
	cwd, _ := os.Getwd()
	overview := hooks.InspectIntegrations(s.Cfg, s.Store, cwd)
	writeJSON(w, http.StatusOK, overview)
}

// handleAgentHookHTTP exposes native non-interrupting agent hook responses over local loopback HTTP
// (for agents supporting HTTP hooks, protected by Authorization: Bearer <INGEST_KEY>).
func (s *Server) handleAgentHookHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeIngest(r) {
		writeJSONError(w, http.StatusForbidden, "Hooks locales protegidos")
		return
	}

	agentParam := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "agent")))
	if agentParam == "" {
		agentParam = "auto"
	}
	if agentParam != "auto" && !agentIDRegex.MatchString(agentParam) {
		writeJSONError(w, http.StatusBadRequest, "Identificador de agente inválido")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if isMaxBytesErr(err, &maxErr) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "Solicitud demasiado grande")
			return
		}
		writeJSONError(w, http.StatusBadRequest, "Cuerpo de hook inválido")
		return
	}

	in := hooks.ParseInput(rawBody)
	resolvedAgent := agentParam
	if resolvedAgent == "auto" {
		resolvedAgent = in.DetectedAgent
	}

	q := r.URL.Query()
	evName := strings.TrimSpace(q.Get("event"))
	if evName == "" {
		evName = in.EventName
	}

	snap := hooks.QuerySnapshot(s.Store, s.Cfg, hooks.QueryOptions{
		AgentID:         resolvedAgent,
		SessionID:       in.SessionID,
		ModelHint:       in.Model,
		WindowOverride:  strings.TrimSpace(q.Get("window")),
		QuotaUSDOver:    strings.TrimSpace(q.Get("quotaUsd")),
		QuotaTokensOver: strings.TrimSpace(q.Get("quotaTokens")),
		ContextUsedPct:  in.ContextUsedPct,
		SessionCostHint: in.SessionCostHint,
		Now:             s.Store.NowFn(),
	})

	includeGlobal := q.Get("global") != "0" && q.Get("global") != "false"
	payload, isJSON := hooks.BuildResponse(resolvedAgent, evName, snap, q.Get("format"), includeGlobal)
	if isJSON {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}
