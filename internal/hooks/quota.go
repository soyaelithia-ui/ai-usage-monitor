package hooks

import (
	"math/big"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/config"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/pricing"
)

var (
	decZero     = decimal.Zero
	decHundred  = decimal.NewFromInt(100)
	decWarnPct  = decimal.NewFromInt(80)
	decThousand = decimal.NewFromInt(1000)
	decMillion  = decimal.NewFromInt(1_000_000)
	decBillion  = decimal.NewFromInt(1_000_000_000)
)

// WindowMetrics holds exact token and decimal USD totals for a time or session scope.
type WindowMetrics struct {
	Count                 int    `json:"count"`
	InputTokens           string `json:"inputTokens"`
	OutputTokens          string `json:"outputTokens"`
	CachedInputTokens     string `json:"cachedInputTokens"`
	CacheWriteInputTokens string `json:"cacheWriteInputTokens"`
	ReasoningOutputTokens string `json:"reasoningOutputTokens"`
	TotalTokens           string `json:"totalTokens"`
	CostUSD               string `json:"costUsd"`
	Unpriced              int    `json:"unpriced"`
}

// QuotaStatus summarizes exact-decimal spend and integer token quota progress.
// Status is strictly observational ("unconfigured", "ok", "warning", "exceeded") and never blocks agents.
type QuotaStatus struct {
	Window          string `json:"window"`
	State           string `json:"state"`
	LimitUSD        string `json:"limitUsd,omitempty"`
	UsedUSD         string `json:"usedUsd"`
	RemainingUSD    string `json:"remainingUsd,omitempty"`
	PercentUSD      string `json:"percentUsd,omitempty"`
	LimitTokens     string `json:"limitTokens,omitempty"`
	UsedTokens      string `json:"usedTokens"`
	RemainingTokens string `json:"remainingTokens,omitempty"`
	PercentTokens   string `json:"percentTokens,omitempty"`
}

// UsageSnapshot is the real-time telemetry and quota snapshot rendered natively inside agent hooks.
type UsageSnapshot struct {
	AgentID         string         `json:"agentId"`
	AgentName       string         `json:"agentName"`
	SessionID       string         `json:"sessionId,omitempty"`
	Model           string         `json:"model,omitempty"`
	Window          string         `json:"window"`
	Session         *WindowMetrics `json:"session,omitempty"`
	Agent           WindowMetrics  `json:"agent"`
	Global          WindowMetrics  `json:"global"`
	AgentQuota      QuotaStatus    `json:"agentQuota"`
	GlobalQuota     QuotaStatus    `json:"globalQuota"`
	ContextUsedPct  string         `json:"contextUsedPct,omitempty"`
	SessionCostHint string         `json:"sessionCostHint,omitempty"`
	UpdatedAt       string         `json:"updatedAt"`
}

// QueryOptions parameterizes a real-time hook snapshot query.
type QueryOptions struct {
	AgentID         string
	SessionID       string
	ModelHint       string
	WindowOverride  string
	QuotaUSDOver    string
	QuotaTokensOver string
	ContextUsedPct  string
	SessionCostHint string
	Now             time.Time
}

func summaryToWindowMetrics(sm db.SummaryMetrics) WindowMetrics {
	return WindowMetrics{
		Count:                 sm.Count,
		InputTokens:           defaultZeroStr(sm.InputTokens),
		OutputTokens:          defaultZeroStr(sm.OutputTokens),
		CachedInputTokens:     defaultZeroStr(sm.CachedInputTokens),
		CacheWriteInputTokens: defaultZeroStr(sm.CacheWriteInputTokens),
		ReasoningOutputTokens: defaultZeroStr(sm.ReasoningOutputTokens),
		TotalTokens:           defaultZeroStr(sm.TotalTokens),
		CostUSD:               defaultZeroStr(sm.CostUSD),
		Unpriced:              sm.Unpriced,
	}
}

func defaultZeroStr(s string) string {
	if strings.TrimSpace(s) == "" {
		return "0"
	}
	return s
}

func parseDecSafe(s string) decimal.Decimal {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil {
		return decZero
	}
	return d
}

func parseBigSafe(s string) *big.Int {
	v, ok := new(big.Int).SetString(strings.TrimSpace(s), 10)
	if !ok || v.Sign() < 0 {
		return new(big.Int)
	}
	return v
}

// EvaluateQuota computes exact decimal USD and arbitrary-precision token quota status without IEEE 754 floats.
func EvaluateQuota(usedCostUSD, usedTokens string, limit config.QuotaLimit, fallbackWindow string) QuotaStatus {
	win := limit.Window
	if win == "" {
		win = fallbackWindow
	}
	if win == "" {
		win = "24h"
	}

	usedCost := parseDecSafe(usedCostUSD)
	usedTok := parseBigSafe(usedTokens)

	status := QuotaStatus{
		Window:     win,
		State:      "unconfigured",
		UsedUSD:    usedCost.String(),
		UsedTokens: usedTok.String(),
	}

	hasLimit := false
	worstState := "ok"
	escalate := func(pct decimal.Decimal) {
		if pct.GreaterThanOrEqual(decHundred) {
			worstState = "exceeded"
		} else if pct.GreaterThanOrEqual(decWarnPct) && worstState != "exceeded" {
			worstState = "warning"
		}
	}

	if limStr := strings.TrimSpace(limit.USD); limStr != "" {
		if limDec, err := decimal.NewFromString(limStr); err == nil && limDec.Sign() >= 0 {
			hasLimit = true
			status.LimitUSD = limDec.String()
			rem := limDec.Sub(usedCost)
			if rem.Sign() < 0 {
				rem = decZero
			}
			status.RemainingUSD = rem.String()
			var pct decimal.Decimal
			if limDec.IsZero() {
				if usedCost.Sign() > 0 {
					pct = decHundred
				}
			} else {
				pct = usedCost.Mul(decHundred).DivRound(limDec, 1)
			}
			status.PercentUSD = pct.StringFixed(1)
			escalate(pct)
		}
	}

	if limTokStr := strings.TrimSpace(limit.Tokens); limTokStr != "" {
		if limTok, ok := new(big.Int).SetString(limTokStr, 10); ok && limTok.Sign() >= 0 {
			hasLimit = true
			status.LimitTokens = limTok.String()
			rem := new(big.Int).Sub(limTok, usedTok)
			if rem.Sign() < 0 {
				rem = new(big.Int)
			}
			status.RemainingTokens = rem.String()
			var pct decimal.Decimal
			if limTok.Sign() == 0 {
				if usedTok.Sign() > 0 {
					pct = decHundred
				}
			} else {
				usedD := decimal.NewFromBigInt(usedTok, 0)
				limD := decimal.NewFromBigInt(limTok, 0)
				pct = usedD.Mul(decHundred).DivRound(limD, 1)
			}
			status.PercentTokens = pct.StringFixed(1)
			escalate(pct)
		}
	}

	if hasLimit {
		status.State = worstState
	}
	return status
}

// CanonicalAgentID maps hook agent identifiers (like claude-statusline) to telemetry agent IDs.
func CanonicalAgentID(agent string) string {
	id := strings.ToLower(strings.TrimSpace(agent))
	switch id {
	case "claude-statusline", "claudecode", "claude-code":
		return "claude"
	case "github-copilot", "copilot-cli":
		return "copilot"
	case "gemini-cli":
		return "gemini"
	case "hermes-agent":
		return "hermes"
	case "generic", "statusline", "auto":
		return ""
	default:
		return id
	}
}

// DisplayAgentName returns the user-facing display name for a canonical agent ID.
func DisplayAgentName(agentID string) string {
	switch strings.ToLower(strings.TrimSpace(agentID)) {
	case "antigravity":
		return "Antigravity"
	case "codex":
		return "Codex"
	case "claude", "claude-statusline":
		return "Claude Code"
	case "gemini":
		return "Gemini CLI"
	case "cursor":
		return "Cursor"
	case "copilot":
		return "GitHub Copilot"
	case "hermes":
		return "Hermes Agent"
	case "opencode":
		return "OpenCode"
	case "", "generic", "statusline":
		return "Global"
	default:
		for _, ba := range pricing.BuiltinAgents {
			if ba.ID == agentID {
				return ba.Name
			}
		}
		return strings.ToUpper(agentID[:1]) + agentID[1:]
	}
}

// QuerySnapshot computes the real-time spend, token, and quota snapshot from SQLite.
// If store is nil (e.g. database not initialized yet), it safely returns a zero-spend snapshot.
func QuerySnapshot(store *db.Store, cfg *config.Config, opts QueryOptions) UsageSnapshot {
	now := opts.Now
	if now.IsZero() {
		if store != nil && store.NowFn != nil {
			now = store.NowFn()
		} else {
			now = time.Now()
		}
	}
	now = now.UTC()

	canonID := CanonicalAgentID(opts.AgentID)
	agentLim := cfg.ResolveQuota(canonID)
	globalLim := cfg.ResolveQuota("")

	if opts.WindowOverride != "" {
		if _, ok := db.WindowDurations[opts.WindowOverride]; ok {
			agentLim.Window = opts.WindowOverride
			globalLim.Window = opts.WindowOverride
		}
	}
	if strings.TrimSpace(opts.QuotaUSDOver) != "" {
		agentLim.USD = strings.TrimSpace(opts.QuotaUSDOver)
	}
	if strings.TrimSpace(opts.QuotaTokensOver) != "" {
		agentLim.Tokens = strings.TrimSpace(opts.QuotaTokensOver)
	}

	win := agentLim.Window
	if _, ok := db.WindowDurations[win]; !ok {
		win = "24h"
		agentLim.Window = win
	}
	toMs := now.UnixMilli()
	fromMs := toMs - db.WindowDurations[win]

	var agentMetrics WindowMetrics
	var globalMetrics WindowMetrics
	var sessionMetrics *WindowMetrics
	model := strings.TrimSpace(opts.ModelHint)
	agentName := DisplayAgentName(canonID)

	if store != nil && store.SQL != nil {
		if gSum, gModel, err := store.QueryUsageSummary(fromMs, toMs, "", ""); err == nil {
			globalMetrics = summaryToWindowMetrics(gSum)
			if model == "" && gModel != "" && canonID == "" {
				model = gModel
			}
		} else {
			globalMetrics = summaryToWindowMetrics(db.SummaryMetrics{})
		}

		if canonID != "" {
			if aSum, aModel, err := store.QueryUsageSummary(fromMs, toMs, canonID, ""); err == nil {
				agentMetrics = summaryToWindowMetrics(aSum)
				if aSum.Name != "" {
					agentName = aSum.Name
				}
				if aModel != "" {
					model = aModel
				}
			} else {
				agentMetrics = summaryToWindowMetrics(db.SummaryMetrics{})
			}
		} else {
			agentMetrics = globalMetrics
		}

		if sid := strings.TrimSpace(opts.SessionID); sid != "" {
			// Session totals look across up to 7d so long sessions stay accurate.
			sessFrom := toMs - db.WindowDurations["7d"]
			if sSum, sModel, err := store.QueryUsageSummary(sessFrom, toMs, canonID, sid); err == nil {
				wm := summaryToWindowMetrics(sSum)
				sessionMetrics = &wm
				if sModel != "" {
					model = sModel
				}
			}
		}
	} else {
		globalMetrics = summaryToWindowMetrics(db.SummaryMetrics{})
		agentMetrics = globalMetrics
		if strings.TrimSpace(opts.SessionID) != "" {
			wm := summaryToWindowMetrics(db.SummaryMetrics{})
			sessionMetrics = &wm
		}
	}

	agentQuota := EvaluateQuota(agentMetrics.CostUSD, agentMetrics.TotalTokens, agentLim, win)
	globalQuota := EvaluateQuota(globalMetrics.CostUSD, globalMetrics.TotalTokens, globalLim, win)

	return UsageSnapshot{
		AgentID:         canonID,
		AgentName:       agentName,
		SessionID:       strings.TrimSpace(opts.SessionID),
		Model:           model,
		Window:          win,
		Session:         sessionMetrics,
		Agent:           agentMetrics,
		Global:          globalMetrics,
		AgentQuota:      agentQuota,
		GlobalQuota:     globalQuota,
		ContextUsedPct:  strings.TrimSpace(opts.ContextUsedPct),
		SessionCostHint: strings.TrimSpace(opts.SessionCostHint),
		UpdatedAt:       now.Format("2006-01-02T15:04:05.000Z"),
	}
}

// FormatUSD formats an exact decimal USD string cleanly for compact terminal/UI badges.
func FormatUSD(raw string) string {
	d := parseDecSafe(raw)
	if d.IsZero() {
		return "$0.00"
	}
	abs := d.Abs()
	if abs.LessThan(decimal.NewFromFloat(0.01)) {
		return "$" + d.StringFixed(4)
	}
	if abs.LessThan(decimal.NewFromInt(1)) {
		return "$" + d.StringFixed(3)
	}
	return "$" + d.StringFixed(2)
}

// FormatTokensCompact formats an arbitrary-precision integer token count into compact k/M/B units.
func FormatTokensCompact(raw string) string {
	v := parseBigSafe(raw)
	d := decimal.NewFromBigInt(v, 0)
	switch {
	case d.GreaterThanOrEqual(decBillion):
		return d.DivRound(decBillion, 2).String() + "B"
	case d.GreaterThanOrEqual(decMillion):
		return d.DivRound(decMillion, 2).String() + "M"
	case d.GreaterThanOrEqual(decThousand):
		return d.DivRound(decThousand, 1).String() + "k"
	default:
		return v.String()
	}
}
