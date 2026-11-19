package pricing

import (
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

func init() {
	// Ensure exact precision for division by 1,000,000 across 12-decimal rates.
	decimal.DivisionPrecision = 32
}

var (
	intStringRegex = regexp.MustCompile(`^(0|[1-9][0-9]{0,39})$`)
	million        = decimal.NewFromInt(1_000_000)

	ErrMissingMetric        = errors.New("missing_metric")
	ErrInvalidInteger       = errors.New("invalid_integer")
	ErrInvalidLabel         = errors.New("invalid_label")
	ErrInvalidTimestamp     = errors.New("invalid_timestamp")
	ErrInvalidCacheDuration = errors.New("invalid_cache_duration")
	ErrInvalidBreakdown     = errors.New("invalid_breakdown")
)

// RawEvent represents unnormalized event input before validation.
type RawEvent struct {
	Source                string
	Provider              string
	Agent                 string
	Model                 string
	ResponseID            string
	SessionID             string
	TurnID                string
	Method                string
	SourceVersion         string
	Coverage              string
	Modality              string
	OccurredAt            string
	InputTokens           string
	OutputTokens          string
	CachedInputTokens     *string
	CacheWriteInputTokens *string
	ReasoningOutputTokens *string
	CacheWriteDuration    *string
}

// NormalizedEvent is the validated canonical telemetry event.
type NormalizedEvent struct {
	Source                string  `json:"source"`
	Provider              string  `json:"provider"`
	Agent                 string  `json:"agent"`
	Model                 string  `json:"model"`
	ResponseID            string  `json:"responseId"`
	OccurredAt            string  `json:"occurredAt"`
	InputTokens           string  `json:"inputTokens"`
	OutputTokens          string  `json:"outputTokens"`
	CachedInputTokens     *string `json:"cachedInputTokens"`
	CacheWriteInputTokens *string `json:"cacheWriteInputTokens"`
	ReasoningOutputTokens *string `json:"reasoningOutputTokens"`
	CacheWriteDuration    *string `json:"cacheWriteDuration"`
	TotalTokens           string  `json:"totalTokens"`
	Modality              string  `json:"modality,omitempty"`
}

// CostEstimate holds the exact decimal cost breakdown (or nil fields when fail-closed).
type CostEstimate struct {
	CostCacheReadUSD     *string `json:"costCacheReadUsd,omitempty"`
	CostCacheWriteUSD    *string `json:"costCacheWriteUsd,omitempty"`
	CostReasoningUSD     *string `json:"costReasoningUsd,omitempty"`
	CostVisibleOutputUSD *string `json:"costVisibleOutputUsd,omitempty"`
	CostUSD              *string `json:"costUsd"`
	CostInputUSD         *string `json:"costInputUsd"`
	CostCacheUSD         *string `json:"costCacheUsd"`
	CostOutputUSD        *string `json:"costOutputUsd"`
	CostNote             string  `json:"costNote"`
}

func validateLabel(v, fallback string, hasFallback bool) (string, error) {
	if v == "" && hasFallback {
		return fallback, nil
	}
	if v == "" || len(v) > 160 {
		return "", ErrInvalidLabel
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] == 0x7f {
			return "", ErrInvalidLabel
		}
	}
	return v, nil
}

// ParseTokenString validates a canonical non-negative integer token string.
func ParseTokenString(s string) (*big.Int, error) {
	if !intStringRegex.MatchString(s) {
		return nil, ErrInvalidInteger
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, ErrInvalidInteger
	}
	return n, nil
}

// ParseTimestamp parses ISO-8601/RFC3339 timestamps into UTC time.Time.
func ParseTimestamp(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, ErrInvalidTimestamp
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999Z", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, ErrInvalidTimestamp
}

// Normalize validates and canonicalizes a raw telemetry event.
func Normalize(raw RawEvent) (*NormalizedEvent, error) {
	ts, err := ParseTimestamp(raw.OccurredAt)
	if err != nil {
		return nil, err
	}
	source, err := validateLabel(raw.Source, "local", true)
	if err != nil {
		return nil, err
	}
	provider, err := validateLabel(raw.Provider, "unknown", true)
	if err != nil {
		return nil, err
	}
	agent, err := validateLabel(raw.Agent, "unknown", true)
	if err != nil {
		return nil, err
	}
	model, err := validateLabel(raw.Model, "unknown", true)
	if err != nil {
		return nil, err
	}
	respID, err := validateLabel(raw.ResponseID, "", false)
	if err != nil {
		return nil, err
	}

	if raw.InputTokens == "" || raw.OutputTokens == "" {
		return nil, ErrMissingMetric
	}
	inBig, err := ParseTokenString(raw.InputTokens)
	if err != nil {
		return nil, err
	}
	outBig, err := ParseTokenString(raw.OutputTokens)
	if err != nil {
		return nil, err
	}

	var cachedBig, cwriteBig, reasonBig *big.Int
	var cachedStr, cwriteStr, reasonStr *string

	if raw.CachedInputTokens != nil {
		s := strings.TrimSpace(*raw.CachedInputTokens)
		v, err := ParseTokenString(s)
		if err != nil {
			return nil, err
		}
		cachedBig = v
		cachedStr = &s
	}
	if raw.CacheWriteInputTokens != nil {
		s := strings.TrimSpace(*raw.CacheWriteInputTokens)
		v, err := ParseTokenString(s)
		if err != nil {
			return nil, err
		}
		cwriteBig = v
		cwriteStr = &s
	}
	if raw.ReasoningOutputTokens != nil {
		s := strings.TrimSpace(*raw.ReasoningOutputTokens)
		v, err := ParseTokenString(s)
		if err != nil {
			return nil, err
		}
		reasonBig = v
		reasonStr = &s
	}

	var cwDur *string
	if raw.CacheWriteDuration != nil && *raw.CacheWriteDuration != "" {
		d := *raw.CacheWriteDuration
		if d != "30m" && d != "12h" {
			return nil, ErrInvalidCacheDuration
		}
		cwDur = &d
	}

	// Enforce subset bounds: cached + cacheWrite <= input, reasoning <= output.
	cacheSum := new(big.Int)
	if cachedBig != nil {
		cacheSum.Add(cacheSum, cachedBig)
	}
	if cwriteBig != nil {
		cacheSum.Add(cacheSum, cwriteBig)
	}
	if cacheSum.Cmp(inBig) > 0 {
		return nil, ErrInvalidBreakdown
	}
	if reasonBig != nil && reasonBig.Cmp(outBig) > 0 {
		return nil, ErrInvalidBreakdown
	}

	totalBig := new(big.Int).Add(inBig, outBig)
	return &NormalizedEvent{
		Source:                source,
		Provider:              provider,
		Agent:                 agent,
		Model:                 model,
		ResponseID:            respID,
		OccurredAt:            ts.Format("2006-01-02T15:04:05.000Z"),
		InputTokens:           inBig.String(),
		OutputTokens:          outBig.String(),
		CachedInputTokens:     cachedStr,
		CacheWriteInputTokens: cwriteStr,
		ReasoningOutputTokens: reasonStr,
		CacheWriteDuration:    cwDur,
		TotalTokens:           totalBig.String(),
		Modality:              raw.Modality,
	}, nil
}

func unestimated(note string) CostEstimate {
	return CostEstimate{CostNote: note}
}

func strPtr(s string) *string { return &s }

// LookupRate resolves the effective rate for a model using canonical normalization.
// Rates registered through ingestion (agent-declared or cached external) take precedence
// over the private built-in table.
func LookupRate(model string, custom map[string]ModelRate) (ModelRate, bool) {
	nm := NormalizeModel(model)
	if len(nm.Candidates) == 0 {
		return ModelRate{}, false
	}

	if len(custom) > 0 {
		for _, cand := range nm.Candidates {
			if rate, ok := custom[cand]; ok {
				return rate, true
			}
		}
		for k, rate := range custom {
			kn := NormalizeModel(k)
			if kn.Canonical != "" && kn.Canonical == nm.Canonical {
				return rate, true
			}
		}
	}

	for _, cand := range nm.Candidates {
		if rate, ok := Catalog[cand]; ok {
			return rate, true
		}
	}
	return ModelRate{}, false
}

// LookupLongContext resolves long-context multipliers using canonical model normalization.
func LookupLongContext(model string) (LongContextTier, bool) {
	nm := NormalizeModel(model)
	for _, cand := range nm.Candidates {
		if tier, ok := LongContextTiers[cand]; ok {
			return tier, true
		}
	}
	return LongContextTier{}, false
}

// LookupSchedule resolves scheduled rate changes using canonical model normalization.
func LookupSchedule(model string) ([]ScheduledRateChange, bool) {
	nm := NormalizeModel(model)
	for _, cand := range nm.Candidates {
		if changes, ok := PricingSchedule[cand]; ok && len(changes) > 0 {
			return changes, true
		}
	}
	return nil, false
}

// Estimate computes exact arbitrary-precision USD costs or fails closed with CostNote.
func Estimate(e *NormalizedEvent, customPricing map[string]ModelRate) CostEstimate {
	nm := NormalizeModel(e.Model)
	rate, found := LookupRate(e.Model, customPricing)
	if !found {
		if _, deprecated := IsDeprecated(e.Model); deprecated {
			return unestimated(NoteDeprecatedRate)
		}
		return unestimated(NoteUnknownRate)
	}
	if !ProvidersCompatible(rate.Provider, e.Provider, nm.ProviderHint) {
		return unestimated(NoteUnknownRate)
	}

	if evTime, err := ParseTimestamp(e.OccurredAt); err == nil {
		if changes, ok := LookupSchedule(e.Model); ok {
			for _, ch := range changes {
				if fromTime, err := ParseTimestamp(ch.From); err == nil && !evTime.Before(fromTime) {
					if ch.Input != "" {
						rate.Input = ch.Input
					}
					if ch.Cached != "" {
						rate.Cached = ch.Cached
					}
					if ch.Output != "" {
						rate.Output = ch.Output
					}
				}
			}
		}
	}

	if e.CachedInputTokens == nil || e.CacheWriteInputTokens == nil {
		return unestimated(NoteIncompleteCache)
	}
	if rate.Modality != "" && e.Modality != rate.Modality {
		return unestimated(NoteModalityUnverified)
	}

	cwriteBig, _ := ParseTokenString(*e.CacheWriteInputTokens)
	if rate.CacheWrite == "" && cwriteBig.Sign() > 0 {
		return unestimated(NoteCacheWriteUnavailable)
	}
	if e.CacheWriteDuration != nil && *e.CacheWriteDuration == "12h" && rate.CacheWrite12h == "" && cwriteBig.Sign() > 0 {
		return unestimated(NoteCacheWriteUnavailable)
	}

	inBig, _ := ParseTokenString(e.InputTokens)
	cachedBig, _ := ParseTokenString(*e.CachedInputTokens)
	outBig, _ := ParseTokenString(e.OutputTokens)

	scaleIn := decimal.NewFromInt(1)
	scaleOut := decimal.NewFromInt(1)
	if lc, hasLC := LookupLongContext(e.Model); hasLC {
		if inBig.Cmp(big.NewInt(lc.Threshold)) > 0 {
			if sIn, err := decimal.NewFromString(lc.Input); err == nil {
				scaleIn = sIn
			}
			if sOut, err := decimal.NewFromString(lc.Output); err == nil {
				scaleOut = sOut
			}
		}
	}

	rIn, err := decimal.NewFromString(rate.Input)
	if err != nil {
		return unestimated(NoteUnknownRate)
	}
	rOut, err := decimal.NewFromString(rate.Output)
	if err != nil {
		return unestimated(NoteUnknownRate)
	}
	rCached := rIn
	if rate.Cached != "" {
		if rc, err := decimal.NewFromString(rate.Cached); err == nil {
			rCached = rc
		}
	}
	rCWrite := decimal.Zero
	if e.CacheWriteDuration != nil && *e.CacheWriteDuration == "12h" {
		if rate.CacheWrite12h != "" {
			rCWrite, _ = decimal.NewFromString(rate.CacheWrite12h)
		} else if rate.CacheWrite != "" {
			rCWrite, _ = decimal.NewFromString(rate.CacheWrite)
		}
	} else if rate.CacheWrite != "" {
		rCWrite, _ = decimal.NewFromString(rate.CacheWrite)
	}

	dIn := decimal.NewFromBigInt(inBig, 0)
	dCached := decimal.NewFromBigInt(cachedBig, 0)
	dCWrite := decimal.NewFromBigInt(cwriteBig, 0)
	dOut := decimal.NewFromBigInt(outBig, 0)

	cacheReadCost := dCached.Mul(rCached).Mul(scaleIn).Div(million)
	cacheWriteCost := dCWrite.Mul(rCWrite).Mul(scaleIn).Div(million)
	cacheCost := cacheReadCost.Add(cacheWriteCost)
	normalTokens := dIn.Sub(dCached).Sub(dCWrite)
	normalCost := normalTokens.Mul(rIn).Mul(scaleIn).Div(million)
	outputCost := dOut.Mul(rOut).Mul(scaleOut).Div(million)
	totalCost := normalCost.Add(cacheCost).Add(outputCost)

	var reasoningCostStr, visibleCostStr *string
	if e.ReasoningOutputTokens != nil {
		rBig, _ := ParseTokenString(*e.ReasoningOutputTokens)
		dReason := decimal.NewFromBigInt(rBig, 0)
		reasonCost := dReason.Mul(rOut).Mul(scaleOut).Div(million)
		visibleCost := dOut.Sub(dReason).Mul(rOut).Mul(scaleOut).Div(million)
		reasoningCostStr = strPtr(reasonCost.String())
		visibleCostStr = strPtr(visibleCost.String())
	}

	note := NoteAssumedStandard
	normProvider := NormalizeProvider(rate.Provider)
	if rate.CacheWrite == "" {
		if pNote, ok := ProviderNotes[normProvider]; ok {
			note = pNote
		}
	} else if cwriteBig.Sign() > 0 && (e.CacheWriteDuration == nil || *e.CacheWriteDuration == "") {
		note = NoteCacheWriteAssumed
	}

	return CostEstimate{
		CostCacheReadUSD:     strPtr(cacheReadCost.String()),
		CostCacheWriteUSD:    strPtr(cacheWriteCost.String()),
		CostReasoningUSD:     reasoningCostStr,
		CostVisibleOutputUSD: visibleCostStr,
		CostUSD:              strPtr(totalCost.String()),
		CostInputUSD:         strPtr(normalCost.String()),
		CostCacheUSD:         strPtr(cacheCost.String()),
		CostOutputUSD:        strPtr(outputCost.String()),
		CostNote:             note,
	}
}
