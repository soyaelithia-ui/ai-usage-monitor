package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/pricing"
)

// WindowDurations maps supported window strings to milliseconds.
var WindowDurations = map[string]int64{
	"15m": 900_000,
	"1h":  3_600_000,
	"24h": 86_400_000,
	"7d":  604_800_000,
}

// Filters represents shared query parameters across analytics, events, and reconciliation.
type Filters struct {
	Where    string
	Params   []any
	From     int64
	To       int64
	Window   string
	Selected map[string]string
}

// ParseFilters validates and builds SQL filter clauses from URL query parameters.
func ParseFilters(q url.Values, now time.Time) (*Filters, error) {
	window := q.Get("window")
	if window == "" {
		window = "1h"
	}
	winMs, ok := WindowDurations[window]
	if !ok {
		return nil, errors.New("invalid_window")
	}

	nowMs := now.UnixMilli()
	to := nowMs
	if rawTo := q.Get("to"); rawTo != "" {
		parsed, err := strconv.ParseInt(rawTo, 10, 64)
		if err != nil || parsed < 0 || parsed > nowMs+60_000 {
			return nil, errors.New("invalid_to")
		}
		to = parsed
	}

	from := to - winMs
	where := "occurred_at>=? AND occurred_at<=?"
	params := []any{from, to}
	selected := make(map[string]string, 5)

	for _, key := range []string{"agent", "model", "provider", "sessionId", "method"} {
		val := q.Get(key)
		selected[key] = val
		if val == "" {
			continue
		}
		if len(val) > 160 {
			return nil, errors.New("invalid_filter")
		}
		switch key {
		case "agent":
			// Match either canonical agent/source ID or legacy display name case-insensitively.
			where += " AND (lower(json_extract(data,'$.source'))=lower(?) OR lower(agent)=lower(?))"
			params = append(params, val, val)
		case "model":
			where += " AND model=?"
			params = append(params, val)
		default:
			where += " AND json_extract(data,'$." + key + "')=?"
			params = append(params, val)
		}
	}

	return &Filters{
		Where:    where,
		Params:   params,
		From:     from,
		To:       to,
		Window:   window,
		Selected: selected,
	}, nil
}

type accum struct {
	count                 int
	inputTokens           *big.Int
	outputTokens          *big.Int
	cachedInputTokens     *big.Int
	cacheWriteInputTokens *big.Int
	reasoningOutputTokens *big.Int
	normalInputTokens     *big.Int
	visibleOutputTokens   *big.Int
	costCacheReadUsd      decimal.Decimal
	costCacheWriteUsd     decimal.Decimal
	costVisibleOutputUsd  decimal.Decimal
	costReasoningUsd      decimal.Decimal
	costInputUsd          decimal.Decimal
	costCacheUsd          decimal.Decimal
	costOutputUsd         decimal.Decimal
	costUsd               decimal.Decimal
	missingCache          int
	missingReasoning      int
	unpriced              int
}

func newAccum() *accum {
	return &accum{
		inputTokens:           new(big.Int),
		outputTokens:          new(big.Int),
		cachedInputTokens:     new(big.Int),
		cacheWriteInputTokens: new(big.Int),
		reasoningOutputTokens: new(big.Int),
		normalInputTokens:     new(big.Int),
		visibleOutputTokens:   new(big.Int),
	}
}

func addBigStr(dst *big.Int, s *string) *big.Int {
	if s == nil || *s == "" {
		return nil
	}
	v, ok := new(big.Int).SetString(*s, 10)
	if !ok {
		return nil
	}
	dst.Add(dst, v)
	return v
}

func addDecStr(dst *decimal.Decimal, s *string) {
	if s == nil || *s == "" {
		return
	}
	if d, err := decimal.NewFromString(*s); err == nil {
		*dst = dst.Add(d)
	}
}

func (a *accum) add(e *storedEvent) {
	a.count++
	inV, _ := new(big.Int).SetString(e.InputTokens, 10)
	outV, _ := new(big.Int).SetString(e.OutputTokens, 10)
	if inV != nil {
		a.inputTokens.Add(a.inputTokens, inV)
	}
	if outV != nil {
		a.outputTokens.Add(a.outputTokens, outV)
	}

	cRead := addBigStr(a.cachedInputTokens, e.CachedInputTokens)
	cWrite := addBigStr(a.cacheWriteInputTokens, e.CacheWriteInputTokens)
	reason := addBigStr(a.reasoningOutputTokens, e.ReasoningOutputTokens)

	if cRead != nil && cWrite != nil && inV != nil {
		norm := new(big.Int).Sub(inV, cRead)
		norm.Sub(norm, cWrite)
		a.normalInputTokens.Add(a.normalInputTokens, norm)
	} else {
		a.missingCache++
	}

	if reason != nil && outV != nil {
		vis := new(big.Int).Sub(outV, reason)
		a.visibleOutputTokens.Add(a.visibleOutputTokens, vis)
	} else {
		a.missingReasoning++
	}

	addDecStr(&a.costInputUsd, e.CostInputUSD)
	addDecStr(&a.costCacheUsd, e.CostCacheUSD)
	addDecStr(&a.costOutputUsd, e.CostOutputUSD)
	addDecStr(&a.costCacheReadUsd, e.CostCacheReadUSD)
	addDecStr(&a.costCacheWriteUsd, e.CostCacheWriteUSD)
	addDecStr(&a.costVisibleOutputUsd, e.CostVisibleOutputUSD)
	addDecStr(&a.costReasoningUsd, e.CostReasoningUSD)

	if e.CostUSD == nil {
		a.unpriced++
	} else {
		addDecStr(&a.costUsd, e.CostUSD)
	}
}

// SummaryMetrics is the JSON representation of aggregated token & cost totals.
type SummaryMetrics struct {
	ID                    string `json:"id,omitempty"`
	Name                  string `json:"name,omitempty"`
	At                    string `json:"at,omitempty"`
	Count                 int    `json:"count"`
	InputTokens           string `json:"inputTokens"`
	OutputTokens          string `json:"outputTokens"`
	TotalTokens           string `json:"totalTokens"`
	CachedInputTokens     string `json:"cachedInputTokens"`
	CacheWriteInputTokens string `json:"cacheWriteInputTokens"`
	ReasoningOutputTokens string `json:"reasoningOutputTokens"`
	NormalInputTokens     string `json:"normalInputTokens"`
	VisibleOutputTokens   string `json:"visibleOutputTokens"`
	CostCacheReadUSD      string `json:"costCacheReadUsd"`
	CostCacheWriteUSD     string `json:"costCacheWriteUsd"`
	CostVisibleOutputUSD  string `json:"costVisibleOutputUsd"`
	CostReasoningUSD      string `json:"costReasoningUsd"`
	CostInputUSD          string `json:"costInputUsd"`
	CostCacheUSD          string `json:"costCacheUsd"`
	CostOutputUSD         string `json:"costOutputUsd"`
	CostUSD               string `json:"costUsd"`
	CumulativeCostUSD     string `json:"cumulativeCostUsd,omitempty"`
	MissingCache          int    `json:"missingCache"`
	MissingReasoning      int    `json:"missingReasoning"`
	Unpriced              int    `json:"unpriced"`
}

func (a *accum) toSummary(id, name, at string) SummaryMetrics {
	totalTok := new(big.Int).Add(a.inputTokens, a.outputTokens)
	return SummaryMetrics{
		ID:                    id,
		Name:                  name,
		At:                    at,
		Count:                 a.count,
		InputTokens:           a.inputTokens.String(),
		OutputTokens:          a.outputTokens.String(),
		TotalTokens:           totalTok.String(),
		CachedInputTokens:     a.cachedInputTokens.String(),
		CacheWriteInputTokens: a.cacheWriteInputTokens.String(),
		ReasoningOutputTokens: a.reasoningOutputTokens.String(),
		NormalInputTokens:     a.normalInputTokens.String(),
		VisibleOutputTokens:   a.visibleOutputTokens.String(),
		CostCacheReadUSD:      a.costCacheReadUsd.String(),
		CostCacheWriteUSD:     a.costCacheWriteUsd.String(),
		CostVisibleOutputUSD:  a.costVisibleOutputUsd.String(),
		CostReasoningUSD:      a.costReasoningUsd.String(),
		CostInputUSD:          a.costInputUsd.String(),
		CostCacheUSD:          a.costCacheUsd.String(),
		CostOutputUSD:         a.costOutputUsd.String(),
		CostUSD:               a.costUsd.String(),
		MissingCache:          a.missingCache,
		MissingReasoning:      a.missingReasoning,
		Unpriced:              a.unpriced,
	}
}

// AgentBucket holds per-bucket telemetry for an agent sparkline.
type AgentBucket struct {
	At           string `json:"at"`
	Count        int    `json:"count"`
	InputTokens  string `json:"inputTokens"`
	OutputTokens string `json:"outputTokens"`
	CostUSD      string `json:"costUsd"`
}

// AgentPanel summarizes an agent's totals and 60-bucket sparkline.
type AgentPanel struct {
	SummaryMetrics
	LastEventAt string        `json:"lastEventAt"`
	Buckets     []AgentBucket `json:"buckets"`
}

// AnalyticsResult is the response payload for /api/analytics and /api/usage.
type AnalyticsResult struct {
	From        int64               `json:"from"`
	To          int64               `json:"to"`
	Window      string              `json:"window"`
	IntervalMs  float64             `json:"intervalMs"`
	Totals      SummaryMetrics      `json:"totals"`
	Buckets     []SummaryMetrics    `json:"buckets"`
	Agents      []SummaryMetrics    `json:"agents"`
	Models      []SummaryMetrics    `json:"models"`
	AgentPanels []AgentPanel        `json:"agentPanels"`
	Options     map[string][]string `json:"options"`
	PricingNote string              `json:"pricingNote"`
}

func canonicalAgentKey(e *storedEvent) string {
	if s := strings.ToLower(strings.TrimSpace(e.Source)); s != "" && s != "local" {
		return s
	}
	return strings.ToLower(strings.TrimSpace(e.Agent))
}

// QueryAnalytics computes bucketed telemetry, agent/model summaries, and filter options.
func (s *Store) QueryAnalytics(f *Filters) (*AnalyticsResult, error) {
	stepMs := float64(WindowDurations[f.Window]) / 60.0
	totals := newAccum()
	bucketAccums := make([]*accum, 60)
	bucketAts := make([]string, 60)
	for i := 0; i < 60; i++ {
		bucketAccums[i] = newAccum()
		t := time.UnixMilli(f.From + int64(float64(i)*stepMs)).UTC()
		bucketAts[i] = t.Format("2006-01-02T15:04:05.000Z")
	}

	agentAccums := make(map[string]*accum)
	agentNames := make(map[string]string)
	modelAccums := make(map[string]*accum)
	agentOrder := make([]string, 0)
	modelOrder := make([]string, 0)

	type agentBucketRaw struct {
		count int
		in    *big.Int
		out   *big.Int
		cost  decimal.Decimal
	}
	type agentSeries struct {
		lastEventAt string
		buckets     []*agentBucketRaw
	}
	seriesMap := make(map[string]*agentSeries)

	rows, err := s.SQL.Query(`SELECT data, occurred_at FROM events WHERE `+f.Where+` ORDER BY occurred_at, id`, f.Params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var rawData string
		var occurredMs int64
		if err := rows.Scan(&rawData, &occurredMs); err != nil {
			return nil, err
		}
		var e storedEvent
		if err := json.Unmarshal([]byte(rawData), &e); err != nil {
			continue
		}
		idx := int(float64(occurredMs-f.From) / stepMs)
		if idx < 0 {
			idx = 0
		} else if idx > 59 {
			idx = 59
		}

		totals.add(&e)
		bucketAccums[idx].add(&e)

		agKey := canonicalAgentKey(&e)
		if _, ok := agentAccums[agKey]; !ok {
			agentAccums[agKey] = newAccum()
			agentNames[agKey] = e.Agent
			agentOrder = append(agentOrder, agKey)
		} else if e.Agent != "" {
			agentNames[agKey] = e.Agent
		}
		agentAccums[agKey].add(&e)

		if _, ok := modelAccums[e.Model]; !ok {
			modelAccums[e.Model] = newAccum()
			modelOrder = append(modelOrder, e.Model)
		}
		modelAccums[e.Model].add(&e)

		ser, ok := seriesMap[agKey]
		if !ok {
			bs := make([]*agentBucketRaw, 60)
			for i := 0; i < 60; i++ {
				bs[i] = &agentBucketRaw{in: new(big.Int), out: new(big.Int)}
			}
			ser = &agentSeries{buckets: bs}
			seriesMap[agKey] = ser
		}
		ser.lastEventAt = e.OccurredAt
		b := ser.buckets[idx]
		b.count++
		if v, ok := new(big.Int).SetString(e.InputTokens, 10); ok {
			b.in.Add(b.in, v)
		}
		if v, ok := new(big.Int).SetString(e.OutputTokens, 10); ok {
			b.out.Add(b.out, v)
		}
		addDecStr(&b.cost, e.CostUSD)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	accumulated := decimal.Zero
	bucketsOut := make([]SummaryMetrics, 60)
	for i := 0; i < 60; i++ {
		accumulated = accumulated.Add(bucketAccums[i].costUsd)
		sm := bucketAccums[i].toSummary("", "", bucketAts[i])
		sm.CumulativeCostUSD = accumulated.String()
		bucketsOut[i] = sm
	}

	agentsOut := make([]SummaryMetrics, 0, len(agentOrder))
	panelsOut := make([]AgentPanel, 0, len(agentOrder))
	for _, agKey := range agentOrder {
		name := agentNames[agKey]
		if name == "" {
			name = agKey
		}
		sm := agentAccums[agKey].toSummary(agKey, name, "")
		agentsOut = append(agentsOut, sm)

		ser := seriesMap[agKey]
		ab := make([]AgentBucket, 60)
		for i := 0; i < 60; i++ {
			ab[i] = AgentBucket{
				At:           bucketAts[i],
				Count:        ser.buckets[i].count,
				InputTokens:  ser.buckets[i].in.String(),
				OutputTokens: ser.buckets[i].out.String(),
				CostUSD:      ser.buckets[i].cost.String(),
			}
		}
		panelsOut = append(panelsOut, AgentPanel{
			SummaryMetrics: sm,
			LastEventAt:    ser.lastEventAt,
			Buckets:        ab,
		})
	}

	modelsOut := make([]SummaryMetrics, 0, len(modelOrder))
	for _, name := range modelOrder {
		modelsOut = append(modelsOut, modelAccums[name].toSummary("", name, ""))
	}

	options, err := s.queryFilterOptions(f.From, f.To)
	if err != nil {
		return nil, err
	}

	return &AnalyticsResult{
		From:        f.From,
		To:          f.To,
		Window:      f.Window,
		IntervalMs:  stepMs,
		Totals:      totals.toSummary("", "", ""),
		Buckets:     bucketsOut,
		Agents:      agentsOut,
		Models:      modelsOut,
		AgentPanels: panelsOut,
		Options:     options,
		PricingNote: pricing.PricingNote,
	}, nil
}

func (s *Store) queryFilterOptions(from, to int64) (map[string][]string, error) {
	options := make(map[string][]string, 5)
	for _, spec := range [][2]string{
		{"agents", "agent"},
		{"models", "model"},
		{"providers", "provider"},
		{"sessions", "sessionId"},
		{"methods", "method"},
	} {
		vals, err := s.distinctOption(spec[1], from, to)
		if err != nil {
			return nil, err
		}
		options[spec[0]] = vals
	}
	return options, nil
}

func (s *Store) distinctOption(jsonKey string, from, to int64) ([]string, error) {
	var q string
	if jsonKey == "agent" {
		// Return canonical agent IDs (source when non-empty and not "local", else lower(agent)) from native events only.
		q = `SELECT DISTINCT CASE
			WHEN json_extract(data,'$.source') IS NOT NULL AND json_extract(data,'$.source') != '' AND lower(json_extract(data,'$.source')) != 'local'
				THEN lower(json_extract(data,'$.source'))
			ELSE lower(agent)
		END AS name FROM events WHERE occurred_at>=? AND occurred_at<=? AND name IS NOT NULL AND name != '' ORDER BY name`
	} else {
		q = `SELECT DISTINCT json_extract(data,'$.` + jsonKey + `') AS name FROM events WHERE occurred_at>=? AND occurred_at<=? AND name IS NOT NULL AND name != '' ORDER BY name`
	}
	rows, err := s.SQL.Query(q, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil && name != "" {
			out = append(out, name)
		}
	}
	return out, rows.Err()
}

// EventsPage represents paginated telemetry events with window filter options.
type EventsPage struct {
	Events  []map[string]any    `json:"events"`
	Total   int                 `json:"total"`
	Offset  int                 `json:"offset"`
	Limit   int                 `json:"limit"`
	To      int64               `json:"to"`
	Options map[string][]string `json:"options,omitempty"`
}

// QueryEvents returns paginated events matching shared filters along with filter dropdown options.
func (s *Store) QueryEvents(f *Filters, limit, offset int) (*EventsPage, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, errors.New("invalid_pagination")
	}
	var total int
	if err := s.SQL.QueryRow(`SELECT COUNT(*) FROM events WHERE `+f.Where, f.Params...).Scan(&total); err != nil {
		return nil, err
	}

	args := append(append([]any{}, f.Params...), limit, offset)
	rows, err := s.SQL.Query(
		`SELECT id, data FROM events WHERE `+f.Where+` ORDER BY occurred_at DESC, id DESC LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var item map[string]any
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			continue
		}
		item["id"] = id
		events = append(events, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	options, err := s.queryFilterOptions(f.From, f.To)
	if err != nil {
		return nil, err
	}

	return &EventsPage{
		Events:  events,
		Total:   total,
		Offset:  offset,
		Limit:   limit,
		To:      f.To,
		Options: options,
	}, nil
}

// ReconciliationSession summarizes native reconciliation per session.
type ReconciliationSession struct {
	SessionID         string `json:"sessionId"`
	Count             int    `json:"count"`
	InputTokens       string `json:"inputTokens"`
	OutputTokens      string `json:"outputTokens"`
	CachedInputTokens string `json:"cachedInputTokens"`
}

// ReconciliationResult summarizes response verification against native observations.
type ReconciliationResult struct {
	Responses        int                     `json:"responses"`
	Matched          int                     `json:"matched"`
	Conflicts        int                     `json:"conflicts"`
	Unverified       int                     `json:"unverified"`
	LastObserved     *string                 `json:"lastObserved"`
	Sessions         []ReconciliationSession `json:"sessions"`
	TextMeasurements []map[string]any        `json:"textMeasurements"`
	Note             string                  `json:"note"`
}

func matchesSelectedFilters(m map[string]any, selected map[string]string) bool {
	for k, want := range selected {
		if want == "" {
			continue
		}
		if k == "agent" {
			src, _ := m["source"].(string)
			ag, _ := m["agent"].(string)
			if !strings.EqualFold(src, want) && !strings.EqualFold(ag, want) {
				return false
			}
			continue
		}
		got, _ := m[k].(string)
		if got != want {
			return false
		}
	}
	return true
}

func sanitizeMeasurementForPublic(m map[string]any) map[string]any {
	// Ensure no internal tokenizer identity, engine, or vocabulary state is ever exposed on public API.
	delete(m, "tokenizer")
	delete(m, "vocabulary")
	delete(m, "engine")
	delete(m, "family")
	delete(m, "tokenizerState")
	delete(m, "version")
	return m
}

// QueryReconciliation compares persisted events against native observations and returns
// separate fallback visible-text measurements (never added to native totals).
func (s *Store) QueryReconciliation(f *Filters) (*ReconciliationResult, error) {
	rows, err := s.SQL.Query(
		`SELECT e.identity, e.fingerprint, e.data, o.fingerprint, o.observed_at
		 FROM (SELECT * FROM events WHERE `+f.Where+`) e
		 LEFT JOIN native_observations o ON o.identity = e.identity
		 ORDER BY e.occurred_at DESC, e.id DESC`,
		f.Params...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := &ReconciliationResult{
		Sessions:         make([]ReconciliationSession, 0),
		TextMeasurements: make([]map[string]any, 0),
		Note:             "Conciliación por respuesta contra el contador nativo leído. El contexto repetido pertenece a cada llamada. La estimación local cubre solo texto visible y no sustituye ni se suma al consumo nativo.",
	}

	type sessAcc struct {
		count  int
		in     *big.Int
		out    *big.Int
		cached *big.Int
	}
	sessMap := make(map[string]*sessAcc)
	var lastObs string

	for rows.Next() {
		var ident, fp, rawData string
		var obsFP, obsAt sql.NullString
		if err := rows.Scan(&ident, &fp, &rawData, &obsFP, &obsAt); err != nil {
			return nil, err
		}
		var e storedEvent
		if err := json.Unmarshal([]byte(rawData), &e); err != nil {
			continue
		}
		res.Responses++
		if !obsFP.Valid {
			res.Unverified++
		} else if obsFP.String == fp {
			res.Matched++
		} else {
			res.Conflicts++
		}
		if obsAt.Valid && (lastObs == "" || obsAt.String > lastObs) {
			lastObs = obsAt.String
		}

		sid := e.SessionID
		if sid == "" {
			sid = "unknown"
		}
		sa, ok := sessMap[sid]
		if !ok {
			sa = &sessAcc{in: new(big.Int), out: new(big.Int), cached: new(big.Int)}
			sessMap[sid] = sa
		}
		sa.count++
		if v, ok := new(big.Int).SetString(e.InputTokens, 10); ok {
			sa.in.Add(sa.in, v)
		}
		if v, ok := new(big.Int).SetString(e.OutputTokens, 10); ok {
			sa.out.Add(sa.out, v)
		}
		if e.CachedInputTokens != nil {
			if v, ok := new(big.Int).SetString(*e.CachedInputTokens, 10); ok {
				sa.cached.Add(sa.cached, v)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if lastObs != "" {
		res.LastObserved = &lastObs
	}

	keys := make([]string, 0, len(sessMap))
	for k := range sessMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sa := sessMap[k]
		res.Sessions = append(res.Sessions, ReconciliationSession{
			SessionID:         k,
			Count:             sa.count,
			InputTokens:       sa.in.String(),
			OutputTokens:      sa.out.String(),
			CachedInputTokens: sa.cached.String(),
		})
	}

	tmRows, err := s.SQL.Query(`SELECT data FROM text_measurements WHERE occurred_at>=? AND occurred_at<=? ORDER BY occurred_at DESC`, f.From, f.To)
	if err == nil {
		defer tmRows.Close()
		for tmRows.Next() {
			if len(res.TextMeasurements) >= 100 {
				break
			}
			var raw string
			if tmRows.Scan(&raw) == nil {
				var m map[string]any
				if json.Unmarshal([]byte(raw), &m) == nil && matchesSelectedFilters(m, f.Selected) {
					res.TextMeasurements = append(res.TextMeasurements, sanitizeMeasurementForPublic(m))
				}
			}
		}
	}

	return res, nil
}

// RecentErrors returns the 20 latest error entries and total error count.
func (s *Store) RecentErrors() ([]map[string]string, int, error) {
	var count int
	if err := s.SQL.QueryRow(`SELECT COUNT(*) FROM errors`).Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := s.SQL.Query(`SELECT kind, at FROM errors ORDER BY at DESC LIMIT 20`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]map[string]string, 0)
	for rows.Next() {
		var kind, at string
		if rows.Scan(&kind, &at) == nil {
			out = append(out, map[string]string{"kind": kind, "at": at})
		}
	}
	return out, count, rows.Err()
}

// DistinctPricedModels returns only models observed in telemetry events with consistent
// canonical rate resolution matching pricing.Estimate.
func (s *Store) DistinctPricedModels() ([]map[string]any, error) {
	dyn, err := s.GetDynamicPricing()
	if err != nil {
		return nil, err
	}
	rows, err := s.SQL.Query(`SELECT DISTINCT model FROM events ORDER BY model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var m string
		if rows.Scan(&m) == nil {
			_, priced := pricing.LookupRate(m, dyn)
			out = append(out, map[string]any{
				"model":  m,
				"priced": priced,
			})
		}
	}
	return out, rows.Err()
}
