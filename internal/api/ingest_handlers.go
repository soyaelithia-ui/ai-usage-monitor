package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/ingest"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/pricing"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/security"
)

var (
	agentIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	localPathKey = regexp.MustCompile(`(?i)(path|root|dir|file|socket|db|database|sqlite)$`)
	validTones   = map[string]bool{
		"mint": true, "violet": true, "blue": true,
		"amber": true, "rose": true, "cyan": true,
	}
)

func (s *Server) authorizeIngest(r *http.Request) bool {
	if s.Cfg.IngestKey == "" {
		return false
	}
	if r.Header.Get("X-Usage-Monitor-Gateway") != "" || r.Header.Get("Origin") != "" {
		return false
	}
	return security.EqualConst(r.Header.Get("Authorization"), "Bearer "+s.Cfg.IngestKey)
}

func numOrStr(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", false
	case string:
		return x, true
	case json.Number:
		return x.String(), true
	default:
		return "", false
	}
}

func validateAndSaveSingleRate(store *db.Store, model string, rawEntry any, defaultProvider string) error {
	if model == "" || len(model) > 128 || model == "__proto__" || model == "constructor" || model == "prototype" {
		return errors.New("invalid model key")
	}
	entry, ok := rawEntry.(map[string]any)
	if !ok {
		return errors.New("invalid rate entry")
	}
	inStr, okIn := numOrStr(entry["input"])
	outStr, okOut := numOrStr(entry["output"])
	if !okIn || !okOut || !security.ValidRate(inStr) || !security.ValidRate(outStr) {
		return errors.New("invalid input/output rate")
	}

	cachedStr := ""
	if entry["cached"] != nil {
		var okC bool
		cachedStr, okC = numOrStr(entry["cached"])
		if !okC || !security.ValidRate(cachedStr) {
			return errors.New("invalid cached rate")
		}
	}

	cwStr := ""
	if v := firstNonNil(entry["cacheWrite"], entry["cache_write"]); v != nil {
		var okCW bool
		cwStr, okCW = numOrStr(v)
		if !okCW || !security.ValidRate(cwStr) {
			return errors.New("invalid cacheWrite rate")
		}
	}

	cw12Str := ""
	if v := firstNonNil(entry["cacheWrite12h"], entry["cache_write_12h"]); v != nil {
		var okCW12 bool
		cw12Str, okCW12 = numOrStr(v)
		if !okCW12 || !security.ValidRate(cw12Str) {
			return errors.New("invalid cacheWrite12h rate")
		}
	}

	prov := defaultProvider
	if pStr, ok := entry["provider"].(string); ok && strings.TrimSpace(pStr) != "" {
		prov = strings.TrimSpace(pStr)
	}
	return store.UpsertDynamicPricing(model, pricing.ModelRate{
		Provider:      prov,
		Input:         inStr,
		Cached:        cachedStr,
		Output:        outStr,
		CacheWrite:    cwStr,
		CacheWrite12h: cw12Str,
		Source:        "agent",
	})
}

func validateAndSaveRates(store *db.Store, rawRates any, defaultProvider string) error {
	if rawRates == nil {
		return nil
	}
	ratesMap, ok := rawRates.(map[string]any)
	if !ok {
		return errors.New("invalid rates object")
	}
	count := 0
	for model, rawEntry := range ratesMap {
		if count >= 32 {
			break
		}
		count++
		if err := validateAndSaveSingleRate(store, model, rawEntry, defaultProvider); err != nil {
			return err
		}
	}
	return nil
}

func firstNonNil(a, b any) any {
	if a != nil {
		return a
	}
	return b
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeIngest(r) {
		writeJSONError(w, http.StatusForbidden, "Ingesta local protegida")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	var body map[string]any
	if err := dec.Decode(&body); err != nil || body == nil {
		var maxErr *http.MaxBytesError
		if err != nil && isMaxBytesErr(err, &maxErr) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "Solicitud demasiado grande")
			return
		}
		writeJSONError(w, http.StatusUnprocessableEntity, "Métricas o semántica no verificables")
		return
	}

	schema, _ := body["schema"].(string)
	sourceRaw, _ := body["source"].(string)
	var rawEv pricing.RawEvent

	if schema == "usage-monitor.usage.v1" || sourceRaw == "antigravity" {
		if schema != "usage-monitor.usage.v1" || body["semantics"] != "input_includes_cache;output_includes_reasoning" {
			writeJSONError(w, http.StatusUnprocessableEntity, "Métricas o semántica no verificables")
			return
		}
		model, _ := body["model"].(string)
		provider, _ := body["provider"].(string)
		if strings.TrimSpace(model) == "" || strings.TrimSpace(provider) == "" {
			writeJSONError(w, http.StatusUnprocessableEntity, "Métricas o semántica no verificables")
			return
		}

		sourceID := "custom"
		if sourceRaw == "antigravity" {
			sourceID = "antigravity"
		} else if agentIDRegex.MatchString(sourceRaw) {
			sourceID = sourceRaw
		}

		agentName := strings.ToUpper(sourceID[:1]) + sourceID[1:]
		agentProvider := provider
		if dynList, err := s.Store.GetDynamicAgents(); err == nil {
			for _, da := range dynList {
				if da.ID == sourceID {
					agentName = da.Name
					if da.Provider != "" {
						agentProvider = da.Provider
					}
					break
				}
			}
		}
		for _, ba := range pricing.BuiltinAgents {
			if ba.ID == sourceID {
				agentName = ba.Name
				if ba.Provider != "" {
					agentProvider = ba.Provider
				}
				break
			}
		}

		if !ingest.BuiltinAgentIDs(sourceID) {
			if n, ok := body["agent"].(string); ok && strings.TrimSpace(n) != "" {
				agentName = strings.TrimSpace(n)
				if len(agentName) > 100 {
					agentName = agentName[:100]
				}
			}
			if len(agentProvider) > 64 {
				agentProvider = agentProvider[:64]
			}
			tone, _ := body["tone"].(string)
			if !validTones[tone] {
				tone = ""
			}
			symbol, _ := body["symbol"].(string)
			symbol = strings.TrimSpace(symbol)
			if len(symbol) > 4 {
				symbol = symbol[:4]
			}
			var cfg map[string]any
			if iconRaw, ok := body["iconUrl"].(string); ok && iconRaw != "" {
				if cleanIcon, valid := security.SanitizeIconURL(iconRaw); valid {
					cfg = map[string]any{"iconUrl": cleanIcon}
				}
			}
			if err := s.Store.UpsertDynamicAgent(db.DynamicAgent{
				ID:       sourceID,
				Name:     agentName,
				Reader:   "contract",
				Provider: agentProvider,
				Tone:     tone,
				Symbol:   symbol,
				Config:   cfg,
			}); err != nil {
				writeJSONError(w, http.StatusUnprocessableEntity, "Métricas o semántica no verificables")
				return
			}
		}

		// Allow any authenticated agent (built-in or dynamic) to declare verified rates for new models.
		if err := validateAndSaveRates(s.Store, body["rates"], provider); err != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, "Tarifas declaradas inválidas")
			return
		}
		if body["rate"] != nil {
			if err := validateAndSaveSingleRate(s.Store, model, body["rate"], provider); err != nil {
				writeJSONError(w, http.StatusUnprocessableEntity, "Tarifas declaradas inválidas")
				return
			}
		}

		ev, err := rawEventFromMap(body, sourceID, agentName, provider)
		if err != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, "Métricas o semántica no verificables")
			return
		}
		rawEv = ev
	} else {
		model, _ := body["model"].(string)
		provider, _ := body["provider"].(string)
		if err := validateAndSaveRates(s.Store, body["rates"], provider); err != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, "Tarifas declaradas inválidas")
			return
		}
		if body["rate"] != nil && strings.TrimSpace(model) != "" {
			if err := validateAndSaveSingleRate(s.Store, model, body["rate"], provider); err != nil {
				writeJSONError(w, http.StatusUnprocessableEntity, "Tarifas declaradas inválidas")
				return
			}
		}
		ev, err := rawEventFromMap(body, sourceRaw, "", "")
		if err != nil {
			writeJSONError(w, http.StatusUnprocessableEntity, "Métricas o semántica no verificables")
			return
		}
		rawEv = ev
	}

	result, err := s.Store.InsertEvent(rawEv)
	if err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, "Métricas o semántica no verificables")
		return
	}
	if result == "inserted" {
		s.Broker.BroadcastChange()
	}
	if s.Ingest != nil {
		s.Ingest.Scan()
	}
	status := http.StatusOK
	if result == "conflict" {
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]string{"result": result})
}

func rawEventFromMap(m map[string]any, sourceOverride, agentOverride, providerOverride string) (pricing.RawEvent, error) {
	str := func(k string) string {
		s, _ := numOrStr(m[k])
		return s
	}
	optCounter := func(k string) *string {
		v, exists := m[k]
		if !exists || v == nil {
			return nil
		}
		s, ok := numOrStr(v)
		if !ok {
			return nil
		}
		return &s
	}

	src := str("source")
	if sourceOverride != "" {
		src = sourceOverride
	}
	ag := str("agent")
	if agentOverride != "" {
		ag = agentOverride
	}
	prov := str("provider")
	if providerOverride != "" {
		prov = providerOverride
	}
	occ := str("occurredAt")
	if occ == "" {
		occ = str("timestamp")
	}
	respID := str("responseId")
	if respID == "" {
		respID = str("id")
	}
	var cwDur *string
	if d, ok := m["cacheWriteDuration"].(string); ok && d != "" {
		cwDur = &d
	}
	method := str("method")
	if method == "" {
		method = "reported"
	}

	return pricing.RawEvent{
		Source:                src,
		Provider:              prov,
		Agent:                 ag,
		Model:                 str("model"),
		ResponseID:            respID,
		SessionID:             str("sessionId"),
		TurnID:                str("turnId"),
		Method:                method,
		SourceVersion:         str("sourceVersion"),
		Coverage:              str("coverage"),
		Modality:              str("modality"),
		OccurredAt:            occ,
		InputTokens:           str("inputTokens"),
		OutputTokens:          str("outputTokens"),
		CachedInputTokens:     optCounter("cachedInputTokens"),
		CacheWriteInputTokens: optCounter("cacheWriteInputTokens"),
		ReasoningOutputTokens: optCounter("reasoningOutputTokens"),
		CacheWriteDuration:    cwDur,
	}, nil
}

func (s *Server) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeIngest(r) {
		writeJSONError(w, http.StatusForbidden, "Autorización requerida")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	var body map[string]any
	if err := dec.Decode(&body); err != nil || body == nil {
		writeJSONError(w, http.StatusBadRequest, "Datos de registro inválidos")
		return
	}

	id, _ := body["id"].(string)
	if !agentIDRegex.MatchString(id) {
		writeJSONError(w, http.StatusBadRequest, "Identificador de agente inválido")
		return
	}
	if ingest.BuiltinAgentIDs(id) {
		writeJSONError(w, http.StatusConflict, "Los agentes del catálogo base son inmutables")
		return
	}

	reader, _ := body["reader"].(string)
	if reader != "contract" && reader != "sqlite" && reader != "jsonl" {
		reader = "contract"
	}
	name, _ := body["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		name = id
	} else if len(name) > 100 {
		name = name[:100]
	}

	provider, _ := body["provider"].(string)
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = "custom"
	} else if len(provider) > 64 {
		provider = provider[:64]
	}

	tone, _ := body["tone"].(string)
	if !validTones[tone] {
		tone = ""
	}
	symbol, _ := body["symbol"].(string)
	symbol = strings.TrimSpace(symbol)
	if len(symbol) > 4 {
		symbol = symbol[:4]
	}

	safeConfig := make(map[string]any)
	if rawCfg, ok := body["config"].(map[string]any); ok {
		count := 0
		for k, v := range rawCfg {
			if count >= 32 {
				break
			}
			count++
			if localPathKey.MatchString(k) || k == "__proto__" || k == "constructor" || k == "prototype" || k == "iconUrl" {
				continue
			}
			switch val := v.(type) {
			case string:
				if len(val) <= 2048 {
					safeConfig[k] = val
				}
			case bool, json.Number:
				safeConfig[k] = val
			}
		}
	}

	var cleanIcon string
	if iconStr, ok := body["iconUrl"].(string); ok {
		if sanitized, valid := security.SanitizeIconURL(iconStr); valid {
			cleanIcon = sanitized
		}
	}
	if cleanIcon == "" {
		if rawCfg, ok := body["config"].(map[string]any); ok {
			if iconStr, ok := rawCfg["iconUrl"].(string); ok {
				if sanitized, valid := security.SanitizeIconURL(iconStr); valid {
					cleanIcon = sanitized
				}
			}
		}
	}
	if cleanIcon != "" {
		safeConfig["iconUrl"] = cleanIcon
	}

	if err := validateAndSaveRates(s.Store, body["rates"], provider); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Tarifas inválidas")
		return
	}

	if err := s.Store.UpsertDynamicAgent(db.DynamicAgent{
		ID:       id,
		Name:     name,
		Reader:   reader,
		Provider: provider,
		Tone:     tone,
		Symbol:   symbol,
		Config:   safeConfig,
	}); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Datos de registro inválidos")
		return
	}

	s.Broker.BroadcastChange()
	if s.Ingest != nil {
		s.Ingest.Scan()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"agent": map[string]string{
			"id":     id,
			"name":   name,
			"reader": reader,
		},
	})
}
