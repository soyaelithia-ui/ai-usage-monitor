package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/pricing"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/security"
)

// DynamicAgent represents a dynamically registered or auto-discovered agent.
type DynamicAgent struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Reader    string         `json:"reader"`
	Provider  string         `json:"provider,omitempty"`
	Tone      string         `json:"tone,omitempty"`
	Symbol    string         `json:"symbol,omitempty"`
	Config    map[string]any `json:"config,omitempty"`
	CreatedAt int64          `json:"createdAt"`
}

// TextMeasurement represents a silent fallback visible-text count stored strictly
// apart from native consumption for reconciliation only.
type TextMeasurement struct {
	Source     string
	Provider   string
	Agent      string
	Model      string
	SessionID  string
	ResponseID string
	OccurredAt string
	Status     string
	Tokens     *int
}

type storedEvent struct {
	*pricing.NormalizedEvent
	pricing.CostEstimate
	Method         string `json:"method"`
	SessionID      string `json:"sessionId,omitempty"`
	TurnID         string `json:"turnId,omitempty"`
	SourceVersion  string `json:"sourceVersion,omitempty"`
	Coverage       string `json:"coverage"`
	Modality       string `json:"modality"`
	PricingVersion string `json:"pricingVersion"`
	ServiceTier    string `json:"serviceTier"`
}

func sanitizeShort(s string) string {
	if s == "" || len(s) > 160 {
		return ""
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return ""
		}
	}
	return s
}

func buildStoredEvent(raw pricing.RawEvent, norm *pricing.NormalizedEvent, est pricing.CostEstimate) storedEvent {
	method := "reported"
	if raw.Method == "native" {
		method = "native"
	}
	coverage := sanitizeShort(raw.Coverage)
	if coverage == "" {
		coverage = "reported_usage"
	}
	modality := sanitizeShort(raw.Modality)
	if modality == "" {
		modality = "unknown"
	}
	return storedEvent{
		NormalizedEvent: norm,
		CostEstimate:    est,
		Method:          method,
		SessionID:       sanitizeShort(raw.SessionID),
		TurnID:          sanitizeShort(raw.TurnID),
		SourceVersion:   sanitizeShort(raw.SourceVersion),
		Coverage:        coverage,
		Modality:        modality,
		PricingVersion:  pricing.PricingVersion,
		ServiceTier:     "standard_assumed",
	}
}

// InsertEvent normalizes, prices (auto-resolving missing models via RateResolver when configured),
// deduplicates, and persists an event. Returns "inserted", "duplicate", or "conflict".
func (s *Store) InsertEvent(raw pricing.RawEvent) (string, error) {
	norm, err := pricing.Normalize(raw)
	if err != nil {
		return "", err
	}
	canonBytes, err := json.Marshal(norm)
	if err != nil {
		return "", err
	}
	fingerprint := security.LegacyHash(string(canonBytes))
	identity := security.LegacyHash(norm.Provider + ":" + norm.Source + ":" + norm.ResponseID)

	dynPricing, err := s.GetDynamicPricing()
	if err != nil {
		return "", err
	}

	// Priority: 1. Agent-declared dynamic_pricing -> 2. Built-in Catalog -> 3. Optional cached public resolver.
	nm := pricing.NormalizeModel(norm.Model)
	if _, found := pricing.LookupRate(norm.Model, dynPricing); !found && s.RateResolver != nil {
		if _, deprecated := pricing.IsDeprecated(norm.Model); !deprecated {
			if resolved, ok := s.RateResolver.Resolve(norm.Provider, norm.Model); ok {
				key := nm.Canonical
				if key == "" {
					key = norm.Model
				}
				if err := s.UpsertDynamicPricingIfAbsent(key, resolved); err == nil {
					if dynPricing == nil {
						dynPricing = make(map[string]pricing.ModelRate)
					}
					if _, exists := dynPricing[key]; !exists {
						dynPricing[key] = resolved
					}
				}
			}
		}
	}

	est := pricing.Estimate(norm, dynPricing)
	stored := buildStoredEvent(raw, norm, est)

	tx, err := s.SQL.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	if raw.Method == "native" {
		s.observeTx(tx, identity, fingerprint, &stored)
		textKey := security.LegacyHash(norm.Source + ":" + norm.ResponseID)
		_, _ = tx.Exec(`DELETE FROM text_measurements WHERE identity=?`, textKey)
	}

	var priorFingerprint string
	err = tx.QueryRow(`SELECT fingerprint FROM events WHERE identity=?`, identity).Scan(&priorFingerprint)
	if err == nil {
		if priorFingerprint == fingerprint {
			_ = tx.Commit()
			return "duplicate", nil
		}
		errID := security.LegacyHash("conflict" + identity)
		at := s.NowFn().UTC().Format("2006-01-02T15:04:05.000Z")
		_, _ = tx.Exec(`INSERT OR IGNORE INTO errors(id, kind, at) VALUES (?, 'conflict', ?)`, errID, at)
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return "conflict", nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	ts, _ := pricing.ParseTimestamp(norm.OccurredAt)
	dataBytes, err := json.Marshal(stored)
	if err != nil {
		return "", err
	}

	if _, err := tx.Exec(
		`INSERT INTO events(identity, fingerprint, occurred_at, agent, model, data) VALUES (?, ?, ?, ?, ?, ?)`,
		identity, fingerprint, ts.UnixMilli(), norm.Agent, norm.Model, string(dataBytes),
	); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return "inserted", nil
}

// SaveTextMeasurement persists a fallback local visible-text measurement exclusively into
// text_measurements for reconciliation. It never writes to events, skips if native usage
// already exists for the response, and omits internal tokenizer metadata from public records.
func (s *Store) SaveTextMeasurement(m TextMeasurement) error {
	ts, err := pricing.ParseTimestamp(m.OccurredAt)
	if err != nil {
		return err
	}
	source := sanitizeShort(m.Source)
	respID := sanitizeShort(m.ResponseID)
	if source == "" || respID == "" {
		return pricing.ErrInvalidLabel
	}
	provider := sanitizeShort(m.Provider)
	if provider == "" {
		provider = "unknown"
	}
	agent := sanitizeShort(m.Agent)
	if agent == "" {
		agent = source
	}
	model := sanitizeShort(m.Model)
	if model == "" {
		model = "unknown"
	}

	// Never overwrite or shadow an existing native event for the same response.
	nativeID := security.LegacyHash(provider + ":" + source + ":" + respID)
	var exists int
	if err := s.SQL.QueryRow(`SELECT 1 FROM events WHERE identity=?`, nativeID).Scan(&exists); err == nil {
		return nil
	}

	status := sanitizeShort(m.Status)
	if status == "" {
		status = "unsupported"
	}

	var tokensVal any
	if m.Tokens != nil && *m.Tokens >= 0 {
		tokensVal = strconv.Itoa(*m.Tokens)
	}

	payload := map[string]any{
		"source":     source,
		"provider":   provider,
		"agent":      agent,
		"model":      model,
		"sessionId":  sanitizeShort(m.SessionID),
		"responseId": respID,
		"occurredAt": ts.Format("2006-01-02T15:04:05.000Z"),
		"method":     "local_text",
		"coverage":   "visible_output_text_only",
		"status":     status,
		"tokens":     tokensVal,
		"comparison": "not_comparable_to_full_native_usage",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	key := security.LegacyHash(source + ":" + respID)
	_, err = s.SQL.Exec(
		`INSERT INTO text_measurements(identity, occurred_at, data) VALUES (?, ?, ?)
		 ON CONFLICT(identity) DO UPDATE SET occurred_at=excluded.occurred_at, data=excluded.data`,
		key, ts.UnixMilli(), string(raw),
	)
	return err
}

func (s *Store) observeTx(tx *sql.Tx, identity, fingerprint string, ev *storedEvent) {
	var prevObs, accepted string
	errObs := tx.QueryRow(`SELECT fingerprint FROM native_observations WHERE identity=?`, identity).Scan(&prevObs)
	errAcc := tx.QueryRow(`SELECT fingerprint FROM events WHERE identity=?`, identity).Scan(&accepted)
	if errObs == nil && errAcc == nil && prevObs != accepted {
		return // Keep the first conflict visible until reviewed.
	}

	payload, err := json.Marshal(map[string]any{
		"method":                ev.Method,
		"sessionId":             ev.SessionID,
		"turnId":                ev.TurnID,
		"sourceVersion":         ev.SourceVersion,
		"coverage":              ev.Coverage,
		"modality":              ev.Modality,
		"pricingVersion":        ev.PricingVersion,
		"serviceTier":           ev.ServiceTier,
		"inputTokens":           ev.InputTokens,
		"outputTokens":          ev.OutputTokens,
		"cachedInputTokens":     ev.CachedInputTokens,
		"cacheWriteInputTokens": ev.CacheWriteInputTokens,
		"reasoningOutputTokens": ev.ReasoningOutputTokens,
		"totalTokens":           ev.TotalTokens,
	})
	if err != nil {
		return
	}
	nowISO := s.NowFn().UTC().Format("2006-01-02T15:04:05.000Z")
	_, _ = tx.Exec(
		`INSERT INTO native_observations(identity, fingerprint, observed_at, data) VALUES (?, ?, ?, ?)
		 ON CONFLICT(identity) DO UPDATE SET fingerprint=excluded.fingerprint, observed_at=excluded.observed_at, data=excluded.data`,
		identity, fingerprint, nowISO, string(payload),
	)
}

// UpsertDynamicAgent inserts or updates a dynamic agent registration.
func (s *Store) UpsertDynamicAgent(a DynamicAgent) error {
	var cfgJSON *string
	if len(a.Config) > 0 {
		b, err := json.Marshal(a.Config)
		if err != nil {
			return err
		}
		str := string(b)
		cfgJSON = &str
	}
	createdAt := a.CreatedAt
	if createdAt == 0 {
		createdAt = s.NowFn().UnixMilli()
	}
	_, err := s.SQL.Exec(
		`INSERT INTO dynamic_agents(id, name, reader, provider, tone, symbol, config_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name,
		   reader=excluded.reader,
		   provider=excluded.provider,
		   tone=coalesce(excluded.tone, dynamic_agents.tone),
		   symbol=coalesce(excluded.symbol, dynamic_agents.symbol),
		   config_json=coalesce(excluded.config_json, dynamic_agents.config_json)`,
		a.ID, a.Name, a.Reader, nullIfEmpty(a.Provider), nullIfEmpty(a.Tone), nullIfEmpty(a.Symbol), cfgJSON, createdAt,
	)
	return err
}

// GetDynamicAgents returns all dynamically registered agents ordered by creation time.
func (s *Store) GetDynamicAgents() ([]DynamicAgent, error) {
	rows, err := s.SQL.Query(`SELECT id, name, reader, provider, tone, symbol, config_json, created_at FROM dynamic_agents ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DynamicAgent
	for rows.Next() {
		var a DynamicAgent
		var provider, tone, symbol, cfgJSON sql.NullString
		if err := rows.Scan(&a.ID, &a.Name, &a.Reader, &provider, &tone, &symbol, &cfgJSON, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Provider = provider.String
		a.Tone = tone.String
		a.Symbol = symbol.String
		if cfgJSON.Valid && cfgJSON.String != "" {
			var m map[string]any
			if json.Unmarshal([]byte(cfgJSON.String), &m) == nil {
				a.Config = m
			}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func validateDynamicRate(model string, rate pricing.ModelRate) (string, string, string, error) {
	model = strings.TrimSpace(model)
	if model == "" || len(model) > 128 {
		return "", "", "", errors.New("invalid model name")
	}
	if !security.ValidRate(rate.Input) || !security.ValidRate(rate.Output) {
		return "", "", "", errors.New("invalid input or output rate")
	}
	cached := rate.Cached
	if cached == "" {
		cached = rate.Input
	}
	if !security.ValidRate(cached) {
		return "", "", "", errors.New("invalid cached rate")
	}
	if rate.CacheWrite != "" && !security.ValidRate(rate.CacheWrite) {
		return "", "", "", errors.New("invalid cacheWrite rate")
	}
	if rate.CacheWrite12h != "" && !security.ValidRate(rate.CacheWrite12h) {
		return "", "", "", errors.New("invalid cacheWrite12h rate")
	}
	src := strings.TrimSpace(rate.Source)
	if src == "" {
		src = "agent"
	}
	return model, cached, src, nil
}

// UpsertDynamicPricing validates and stores custom decimal model pricing (agent-declared takes highest priority).
func (s *Store) UpsertDynamicPricing(model string, rate pricing.ModelRate) error {
	cleanModel, cached, src, err := validateDynamicRate(model, rate)
	if err != nil {
		return err
	}
	_, err = s.SQL.Exec(
		`INSERT INTO dynamic_pricing(model, provider, input, cached, output, cache_write, cache_write_12h, source)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(model) DO UPDATE SET
		   provider=coalesce(excluded.provider, dynamic_pricing.provider),
		   input=excluded.input,
		   cached=excluded.cached,
		   output=excluded.output,
		   cache_write=coalesce(excluded.cache_write, dynamic_pricing.cache_write),
		   cache_write_12h=coalesce(excluded.cache_write_12h, dynamic_pricing.cache_write_12h),
		   source=excluded.source`,
		cleanModel, nullIfEmpty(rate.Provider), rate.Input, cached, rate.Output, nullIfEmpty(rate.CacheWrite), nullIfEmpty(rate.CacheWrite12h), src,
	)
	return err
}

// UpsertDynamicPricingIfAbsent stores a resolved external rate without overwriting an existing entry.
func (s *Store) UpsertDynamicPricingIfAbsent(model string, rate pricing.ModelRate) error {
	cleanModel, cached, src, err := validateDynamicRate(model, rate)
	if err != nil {
		return err
	}
	_, err = s.SQL.Exec(
		`INSERT OR IGNORE INTO dynamic_pricing(model, provider, input, cached, output, cache_write, cache_write_12h, source)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		cleanModel, nullIfEmpty(rate.Provider), rate.Input, cached, rate.Output, nullIfEmpty(rate.CacheWrite), nullIfEmpty(rate.CacheWrite12h), src,
	)
	return err
}

// GetDynamicPricing loads all custom/auto-registered pricing definitions.
func (s *Store) GetDynamicPricing() (map[string]pricing.ModelRate, error) {
	rows, err := s.SQL.Query(`SELECT model, provider, input, cached, output, cache_write, cache_write_12h, source FROM dynamic_pricing`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]pricing.ModelRate)
	for rows.Next() {
		var model, input, cached, output string
		var provider, cw, cw12, src sql.NullString
		if err := rows.Scan(&model, &provider, &input, &cached, &output, &cw, &cw12, &src); err != nil {
			return nil, err
		}
		out[model] = pricing.ModelRate{
			Provider:      provider.String,
			Input:         input,
			Cached:        cached,
			Output:        output,
			CacheWrite:    cw.String,
			CacheWrite12h: cw12.String,
			Source:        src.String,
		}
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
