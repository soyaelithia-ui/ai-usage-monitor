package api

import (
	"net/http"
	"os"
	"strconv"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/hooks"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/ingest"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/pricing"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/security"
)

func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	now := s.Store.NowFn()
	f, err := db.ParseFilters(r.URL.Query(), now)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Filtros inválidos")
		return
	}
	res, err := s.Store.QueryAnalytics(f)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Error de consulta")
		return
	}
	errs, errCount, _ := s.Store.RecentErrors()

	var collectorStatus any = map[string]string{"state": "not_configured"}
	if s.Ingest != nil {
		collectorStatus = s.Ingest.StatusMap()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"from":        res.From,
		"to":          res.To,
		"window":      res.Window,
		"intervalMs":  res.IntervalMs,
		"totals":      res.Totals,
		"buckets":     res.Buckets,
		"agents":      res.Agents,
		"models":      res.Models,
		"agentPanels": res.AgentPanels,
		"options":     res.Options,
		"pricingNote": res.PricingNote,
		"updatedAt":   now.UTC().Format("2006-01-02T15:04:05.000Z"),
		"activation":  s.Store.ActivationTime().Format("2006-01-02T15:04:05.000Z"),
		"collector":   collectorStatus,
		"errors":      errs,
		"errorCount":  errCount,
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f, err := db.ParseFilters(q, s.Store.NowFn())
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Consulta inválida")
		return
	}
	limit := 50
	if rawLimit := q.Get("limit"); rawLimit != "" {
		v, err := strconv.Atoi(rawLimit)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "Consulta inválida")
			return
		}
		limit = v
	}
	offset := 0
	if rawOffset := q.Get("offset"); rawOffset != "" {
		v, err := strconv.Atoi(rawOffset)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "Consulta inválida")
			return
		}
		offset = v
	}
	page, err := s.Store.QueryEvents(f, limit, offset)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Consulta inválida")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

type sourceAgentDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Tone    string `json:"tone"`
	Symbol  string `json:"symbol"`
	Reader  string `json:"reader"`
	IconURL string `json:"iconUrl,omitempty"`
}

func (s *Server) handleSources(w http.ResponseWriter, _ *http.Request) {
	dynAgents, _ := s.Store.GetDynamicAgents()
	dynMap := make(map[string]db.DynamicAgent, len(dynAgents))
	for _, da := range dynAgents {
		dynMap[da.ID] = da
	}

	merged := make([]sourceAgentDTO, 0)
	seen := make(map[string]bool)

	for _, ba := range pricing.BuiltinAgents {
		seen[ba.ID] = true
		dto := sourceAgentDTO{
			ID:     ba.ID,
			Name:   ba.Name,
			Tone:   ba.Tone,
			Symbol: ba.Symbol,
			Reader: ba.Reader,
		}
		if da, ok := dynMap[ba.ID]; ok {
			if da.Name != "" {
				dto.Name = da.Name
			}
			if da.Tone != "" {
				dto.Tone = da.Tone
			}
			if da.Symbol != "" {
				dto.Symbol = da.Symbol
			}
			if u, ok := da.Config["iconUrl"].(string); ok {
				if clean, valid := security.SanitizeIconURL(u); valid {
					dto.IconURL = clean
				}
			}
		}
		merged = append(merged, dto)
	}

	for _, da := range dynAgents {
		if seen[da.ID] {
			continue
		}
		seen[da.ID] = true
		tone := da.Tone
		if tone == "" {
			tone = "mint"
		}
		sym := da.Symbol
		if sym == "" && da.Name != "" {
			sym = da.Name[:1]
		}
		var icon string
		if u, ok := da.Config["iconUrl"].(string); ok {
			if clean, valid := security.SanitizeIconURL(u); valid {
				icon = clean
			}
		}
		merged = append(merged, sourceAgentDTO{
			ID:      da.ID,
			Name:    da.Name,
			Tone:    tone,
			Symbol:  sym,
			Reader:  da.Reader,
			IconURL: icon,
		})
	}

	for _, d := range ingest.DiscoverLocalTechnologies(s.Cfg) {
		if d.Detected && !seen[d.ID] {
			seen[d.ID] = true
			merged = append(merged, sourceAgentDTO{
				ID:     d.ID,
				Name:   d.Name,
				Tone:   d.Tone,
				Symbol: d.Symbol,
				Reader: d.Reader,
			})
		}
	}

	errs, errCount, _ := s.Store.RecentErrors()

	resp := make(map[string]any)
	if s.Ingest != nil {
		for k, v := range s.Ingest.StatusMap() {
			resp[k] = v
		}
	}
	cwd, _ := os.Getwd()
	resp["agents"] = merged
	resp["hooks"] = hooks.InspectIntegrations(s.Cfg, s.Store, cwd)
	resp["errors"] = errs
	resp["errorCount"] = errCount
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	techs := ingest.DiscoverLocalTechnologies(s.Cfg)
	knownIdx := make(map[string]int, len(techs))
	for i, t := range techs {
		knownIdx[t.ID] = i
	}

	dynAgents, _ := s.Store.GetDynamicAgents()
	for _, da := range dynAgents {
		if idx, exists := knownIdx[da.ID]; exists {
			techs[idx].Detected = true
			if techs[idx].Carrier == "" {
				techs[idx].Carrier = "auto-registro del propio agente"
			}
			continue
		}
		tone := da.Tone
		if tone == "" {
			tone = "mint"
		}
		sym := da.Symbol
		if sym == "" && da.Name != "" {
			sym = da.Name[:1]
		}
		techs = append(techs, ingest.DiscoveredTech{
			ID:         da.ID,
			Name:       da.Name,
			Reader:     da.Reader,
			Tone:       tone,
			Symbol:     sym,
			Detected:   true,
			Configured: da.Config["path"] != nil || da.Config["root"] != nil,
			Carrier:    "auto-registro del propio agente",
		})
	}

	models, err := s.Store.DistinctPricedModels()
	if err != nil {
		models = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"technologies": techs,
		"models":       models,
	})
}

func (s *Server) handleReconciliation(w http.ResponseWriter, r *http.Request) {
	f, err := db.ParseFilters(r.URL.Query(), s.Store.NowFn())
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Filtros inválidos")
		return
	}
	res, err := s.Store.QueryReconciliation(f)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Error de consulta")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handlePricing publishes rates only for models actually observed in events.
// The built-in catalog is a private internal calculation source, never a public model list.
func (s *Server) handlePricing(w http.ResponseWriter, _ *http.Request) {
	dyn, err := s.Store.GetDynamicPricing()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Error de consulta")
		return
	}
	observed, err := s.Store.DistinctPricedModels()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Error de consulta")
		return
	}

	models := make([]map[string]any, 0, len(observed))
	longContext := make(map[string]pricing.LongContextTier)
	schedule := make(map[string][]pricing.ScheduledRateChange)
	for _, row := range observed {
		model, _ := row["model"].(string)
		if model == "" {
			continue
		}
		entry := map[string]any{"model": model}
		rate, priced := pricing.LookupRate(model, dyn)
		entry["priced"] = priced
		if priced {
			entry["rate"] = rate
		}
		if tier, ok := pricing.LookupLongContext(model); ok {
			longContext[model] = tier
		}
		if changes, ok := pricing.LookupSchedule(model); ok {
			schedule[model] = changes
		}
		models = append(models, entry)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"models":      models,
		"longContext": longContext,
		"schedule":    schedule,
		"pricingNote": pricing.PricingNote,
	})
}
