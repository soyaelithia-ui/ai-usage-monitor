package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// AgentHookSpec describes a natively supported agent hook integration.
type AgentHookSpec struct {
	ID              string   `json:"id"`
	AgentID         string   `json:"agentId"`
	Name            string   `json:"name"`
	WorkspaceConfig string   `json:"workspaceConfig"`
	GlobalConfig    string   `json:"globalConfig"`
	Events          []string `json:"events"`
	Channel         string   `json:"channel"`
	NonInterrupting bool     `json:"nonInterrupting"`
	SupportsInstall bool     `json:"supportsInstall"`
	Description     string   `json:"description"`
}

// SupportedSpecs returns the canonical catalog of native agent hook integrations.
func SupportedSpecs() []AgentHookSpec {
	return []AgentHookSpec{
		{
			ID:              "antigravity",
			AgentID:         "antigravity",
			Name:            "Antigravity",
			WorkspaceConfig: ".agents/hooks.json",
			GlobalConfig:    "~/.gemini/config/hooks.json",
			Events:          []string{"SessionStart", "PostInvocation"},
			Channel:         "metadata.user_markdown (0 tokens al modelo)",
			NonInterrupting: true,
			SupportsInstall: true,
			Description:     "Renderiza cuota y gasto inline en la UI sin enviar texto al modelo ni detener la ejecución.",
		},
		{
			ID:              "codex",
			AgentID:         "codex",
			Name:            "Codex CLI",
			WorkspaceConfig: ".codex/hooks.json",
			GlobalConfig:    "~/.codex/hooks.json",
			Events:          []string{"SessionStart", "Stop"},
			Channel:         "systemMessage",
			NonInterrupting: true,
			SupportsInstall: true,
			Description:     "Muestra cuota y gasto en la terminal de Codex al iniciar sesión y finalizar cada turno.",
		},
		{
			ID:              "claude",
			AgentID:         "claude",
			Name:            "Claude Code",
			WorkspaceConfig: ".claude/settings.json",
			GlobalConfig:    "~/.claude/settings.json",
			Events:          []string{"SessionStart", "Stop"},
			Channel:         "systemMessage (continue: true)",
			NonInterrupting: true,
			SupportsInstall: true,
			Description:     "Notificación nativa en terminal de Claude Code sin inyectar contexto ni bloquear turnos.",
		},
		{
			ID:              "claude-statusline",
			AgentID:         "claude",
			Name:            "Claude Code StatusLine",
			WorkspaceConfig: ".claude/settings.json",
			GlobalConfig:    "~/.claude/settings.json",
			Events:          []string{"statusLine"},
			Channel:         "statusLine (barra persistente)",
			NonInterrupting: true,
			SupportsInstall: true,
			Description:     "Barra de estado en vivo en la parte inferior de Claude Code con gasto, cuota y ventana de contexto.",
		},
		{
			ID:              "gemini",
			AgentID:         "gemini",
			Name:            "Gemini CLI",
			WorkspaceConfig: ".gemini/settings.json",
			GlobalConfig:    "~/.gemini/settings.json",
			Events:          []string{"SessionStart", "AfterAgent"},
			Channel:         "systemMessage",
			NonInterrupting: true,
			SupportsInstall: true,
			Description:     "Muestra gasto y cuota en tiempo real en Gemini CLI sin bloquear prompts ni inyectar contexto.",
		},
		{
			ID:              "cursor",
			AgentID:         "cursor",
			Name:            "Cursor",
			WorkspaceConfig: ".cursor/hooks.json",
			GlobalConfig:    "~/.cursor/hooks.json",
			Events:          []string{"afterAgentResponse", "stop"},
			Channel:         "user_message",
			NonInterrupting: true,
			SupportsInstall: true,
			Description:     "Muestra cuota y gasto directamente al usuario en Cursor mediante user_message sin alterar al agente.",
		},
		{
			ID:              "copilot",
			AgentID:         "copilot",
			Name:            "GitHub Copilot CLI",
			WorkspaceConfig: ".github/hooks/ai-usage-monitor.json",
			GlobalConfig:    "~/.copilot/hooks/ai-usage-monitor.json",
			Events:          []string{"postToolUse", "sessionEnd"},
			Channel:         "systemMessage (continue: true)",
			NonInterrupting: true,
			SupportsInstall: true,
			Description:     "Integra cuota y gasto en vivo en GitHub Copilot CLI con contrato nativo no bloqueante.",
		},
		{
			ID:              "generic",
			AgentID:         "",
			Name:            "Genérico / OpenCode / Hermes",
			WorkspaceConfig: "(stdout JSON / statusline)",
			GlobalConfig:    "(stdout JSON / statusline)",
			Events:          []string{"any"},
			Channel:         "JSON + statusline",
			NonInterrupting: true,
			SupportsInstall: false,
			Description:     "Modo universal para OpenCode, Hermes Agent, scripts propios, tmux o prompts de shell.",
		},
	}
}

// LookupSpec finds a hook specification by ID.
func LookupSpec(id string) (AgentHookSpec, bool) {
	norm := strings.ToLower(strings.TrimSpace(id))
	for _, sp := range SupportedSpecs() {
		if sp.ID == norm {
			return sp, true
		}
	}
	return AgentHookSpec{}, false
}

// HookInput normalizes stdin payloads emitted by any supported agent runtime.
type HookInput struct {
	DetectedAgent   string
	EventName       string
	SessionID       string
	Model           string
	TranscriptPath  string
	ContextUsedPct  string
	SessionCostHint string
	Raw             map[string]any
}

// ParseInput tolerantly parses JSON hook stdin (stripping UTF-8 BOM and never failing on empty/malformed input).
func ParseInput(raw []byte) HookInput {
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}))
	if len(trimmed) == 0 {
		return HookInput{DetectedAgent: "generic"}
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil || m == nil {
		return HookInput{DetectedAgent: "generic"}
	}

	in := HookInput{
		DetectedAgent:  DetectAgent(m),
		EventName:      firstString(m, "hookEventName", "hook_event_name", "event", "eventName"),
		SessionID:      firstString(m, "conversationId", "session_id", "sessionId", "conversation_id"),
		TranscriptPath: firstString(m, "transcriptPath", "transcript_path"),
		Raw:            m,
	}

	in.Model = extractModelHint(m)
	in.ContextUsedPct = extractNestedNumStr(m, "context_window", "used_percentage")
	if in.ContextUsedPct == "" {
		in.ContextUsedPct = extractNestedNumStr(m, "contextWindow", "usedPercentage")
	}
	in.SessionCostHint = extractNestedNumStr(m, "cost", "total_cost_usd")
	if in.SessionCostHint == "" {
		in.SessionCostHint = extractNestedNumStr(m, "cost", "totalCostUsd")
	}
	return in
}

// DetectAgent identifies the calling agent runtime from native hook payload fields.
func DetectAgent(m map[string]any) string {
	if m == nil {
		return "generic"
	}
	if _, ok := m["context_window"]; ok {
		return "claude-statusline"
	}
	if _, ok := m["conversationId"]; ok {
		return "antigravity"
	}
	if _, ok := m["artifactDirectoryPath"]; ok {
		return "antigravity"
	}
	if _, ok := m["invocationNum"]; ok {
		return "antigravity"
	}
	evCamel, _ := m["hookEventName"].(string)
	if evCamel == "PreInvocation" || evCamel == "PostInvocation" {
		return "antigravity"
	}

	evSnake, _ := m["hook_event_name"].(string)
	if _, ok := m["conversation_id"]; ok || evSnake == "afterAgentResponse" || evSnake == "stop" {
		return "cursor"
	}
	if _, ok := m["generation_id"]; ok {
		return "cursor"
	}
	if evSnake == "BeforeAgent" || evSnake == "AfterAgent" {
		return "gemini"
	}
	if _, ok := m["prompt_response"]; ok {
		return "gemini"
	}
	if _, ok := m["toolName"]; ok || evCamel == "postToolUse" || evCamel == "sessionEnd" || evSnake == "postToolUse" || evSnake == "sessionEnd" {
		return "copilot"
	}

	tp := strings.ToLower(firstString(m, "transcriptPath", "transcript_path"))
	switch {
	case strings.Contains(tp, "antigravity"):
		return "antigravity"
	case strings.Contains(tp, ".codex") || strings.Contains(tp, "/codex/"):
		return "codex"
	case strings.Contains(tp, ".claude"):
		return "claude"
	case strings.Contains(tp, ".gemini"):
		return "gemini"
	case strings.Contains(tp, ".cursor"):
		return "cursor"
	case strings.Contains(tp, ".copilot"):
		return "copilot"
	}

	if _, ok := m["turn_id"]; ok {
		return "codex"
	}
	if _, ok := m["session_id"]; ok && evSnake != "" {
		return "claude"
	}
	if _, ok := m["workspacePaths"]; ok {
		return "antigravity"
	}
	return "generic"
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func extractModelHint(m map[string]any) string {
	if s := firstString(m, "modelName", "model_name"); s != "" && !strings.EqualFold(s, "auto") {
		return s
	}
	if v, ok := m["model"]; ok {
		switch x := v.(type) {
		case string:
			if strings.TrimSpace(x) != "" && !strings.EqualFold(strings.TrimSpace(x), "auto") {
				return strings.TrimSpace(x)
			}
		case map[string]any:
			if s := firstString(x, "id", "display_name", "name"); s != "" {
				return s
			}
		}
	}
	return ""
}

func extractNestedNumStr(m map[string]any, parent, child string) string {
	sub, ok := m[parent].(map[string]any)
	if !ok || sub == nil {
		return ""
	}
	v, ok := sub[child]
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case json.Number:
		return x.String()
	case string:
		return strings.TrimSpace(x)
	case float64:
		return fmt.Sprintf("%.1f", x)
	default:
		return ""
	}
}

// FormatMessage renders a compact, scannable, non-intrusive spend & quota summary.
func FormatMessage(snap UsageSnapshot, markdown, compact, includeGlobal bool) string {
	name := snap.AgentName
	if name == "" {
		name = "AI Usage"
	}

	parts := make([]string, 0, 6)
	if markdown {
		parts = append(parts, fmt.Sprintf("`AI Usage · %s`", name))
	} else if compact {
		parts = append(parts, name)
	} else {
		parts = append(parts, fmt.Sprintf("AI Usage · %s", name))
	}

	// Session spend/tokens if available
	if snap.Session != nil && (snap.Session.Count > 0 || snap.SessionCostHint != "") {
		sessCost := snap.Session.CostUSD
		if snap.Session.Count == 0 && snap.SessionCostHint != "" {
			sessCost = snap.SessionCostHint
		}
		tokStr := ""
		if snap.Session.Count > 0 {
			tokStr = fmt.Sprintf(" (%s tok)", FormatTokensCompact(snap.Session.TotalTokens))
		}
		if markdown {
			parts = append(parts, fmt.Sprintf("Sesión: **%s**%s", FormatUSD(sessCost), tokStr))
		} else if compact {
			parts = append(parts, fmt.Sprintf("sess %s%s", FormatUSD(sessCost), tokStr))
		} else {
			parts = append(parts, fmt.Sprintf("Sesión: %s%s", FormatUSD(sessCost), tokStr))
		}
	}

	// Window spend + quota
	aq := snap.AgentQuota
	winPart := formatQuotaSegment(snap.Window, snap.Agent, aq, markdown, compact)
	parts = append(parts, winPart)

	// Optional global total if different from agent and requested
	if includeGlobal && snap.AgentID != "" && snap.Global.Count > snap.Agent.Count {
		gq := snap.GlobalQuota
		if gq.LimitUSD != "" {
			if markdown {
				parts = append(parts, fmt.Sprintf("Total %s: **%s / %s** (%s%%)", snap.Window, FormatUSD(snap.Global.CostUSD), FormatUSD(gq.LimitUSD), gq.PercentUSD))
			} else {
				parts = append(parts, fmt.Sprintf("Total %s: %s/%s (%s%%)", snap.Window, FormatUSD(snap.Global.CostUSD), FormatUSD(gq.LimitUSD), gq.PercentUSD))
			}
		} else if markdown {
			parts = append(parts, fmt.Sprintf("Total %s: **%s**", snap.Window, FormatUSD(snap.Global.CostUSD)))
		} else if !compact {
			parts = append(parts, fmt.Sprintf("Total %s: %s", snap.Window, FormatUSD(snap.Global.CostUSD)))
		}
	}

	// Context window hint if reported by the host agent (e.g. Claude Code statusLine)
	if snap.ContextUsedPct != "" {
		parts = append(parts, fmt.Sprintf("ctx %s%%", snap.ContextUsedPct))
	}

	// Unpriced transparency (fail-closed honesty)
	if snap.Agent.Unpriced > 0 {
		parts = append(parts, fmt.Sprintf("+%d sin tarifa", snap.Agent.Unpriced))
	}

	// Advisory quota badge (strictly observational, never blocks)
	switch aq.State {
	case "exceeded":
		if markdown {
			parts = append(parts, "**[CUOTA SUPERADA]**")
		} else {
			parts = append(parts, "[CUOTA SUPERADA]")
		}
	case "warning":
		if markdown {
			parts = append(parts, "**[CUOTA ALTA]**")
		} else {
			parts = append(parts, "[CUOTA ALTA]")
		}
	}

	sep := " | "
	if markdown {
		sep = " · "
	}
	return strings.Join(parts, sep)
}

func formatQuotaSegment(win string, metrics WindowMetrics, q QuotaStatus, markdown, compact bool) string {
	costStr := FormatUSD(metrics.CostUSD)
	tokStr := FormatTokensCompact(metrics.TotalTokens)

	if q.LimitUSD != "" {
		limStr := FormatUSD(q.LimitUSD)
		remStr := FormatUSD(q.RemainingUSD)
		if markdown {
			if q.State == "exceeded" {
				return fmt.Sprintf("%s: **%s / %s** (%s%% · %s tok)", win, costStr, limStr, q.PercentUSD, tokStr)
			}
			return fmt.Sprintf("%s: **%s / %s** (%s%% · quedan %s · %s tok)", win, costStr, limStr, q.PercentUSD, remStr, tokStr)
		}
		if compact {
			return fmt.Sprintf("%s %s/%s (%s%% · %s tok)", win, costStr, limStr, q.PercentUSD, tokStr)
		}
		if q.State == "exceeded" {
			return fmt.Sprintf("%s: %s/%s (%s%% · %s tok)", win, costStr, limStr, q.PercentUSD, tokStr)
		}
		return fmt.Sprintf("%s: %s/%s (%s%% · quedan %s · %s tok)", win, costStr, limStr, q.PercentUSD, remStr, tokStr)
	}

	if q.LimitTokens != "" {
		limTok := FormatTokensCompact(q.LimitTokens)
		remTok := FormatTokensCompact(q.RemainingTokens)
		if markdown {
			return fmt.Sprintf("%s: **%s** · Cuota: **%s / %s tok** (%s%% · quedan %s)", win, costStr, tokStr, limTok, q.PercentTokens, remTok)
		}
		if compact {
			return fmt.Sprintf("%s %s (%s/%s tok · %s%%)", win, costStr, tokStr, limTok, q.PercentTokens)
		}
		return fmt.Sprintf("%s: %s · Cuota: %s/%s tok (%s%% · quedan %s)", win, costStr, tokStr, limTok, q.PercentTokens, remTok)
	}

	if markdown {
		return fmt.Sprintf("%s: **%s** (%s tok)", win, costStr, tokStr)
	}
	if compact {
		return fmt.Sprintf("%s %s (%s tok)", win, costStr, tokStr)
	}
	return fmt.Sprintf("%s: %s (%s tok)", win, costStr, tokStr)
}

// BuildResponse produces the exact, strictly non-interrupting native stdout payload for the target agent & event.
// Returns (payloadBytes, isJSON).
func BuildResponse(agent, event string, snap UsageSnapshot, formatOverride string, includeGlobal bool) ([]byte, bool) {
	fmtMode := strings.ToLower(strings.TrimSpace(formatOverride))
	ag := strings.ToLower(strings.TrimSpace(agent))
	ev := strings.TrimSpace(event)

	switch fmtMode {
	case "line":
		return []byte(FormatMessage(snap, false, true, includeGlobal)), false
	case "markdown":
		return []byte(FormatMessage(snap, true, false, includeGlobal)), false
	case "json":
		return marshalSafe(map[string]any{
			"continue":      true,
			"systemMessage": FormatMessage(snap, false, false, includeGlobal),
			"telemetry":     snap,
		}), true
	}

	switch ag {
	case "antigravity":
		switch ev {
		case "PostToolUse", "PreToolUse":
			// Antigravity expects {} for PostToolUse; never interfere with PreToolUse permissions.
			return []byte("{}"), true
		case "Stop":
			// Non-"continue" decision allows the agent loop to stop normally without looping.
			return marshalSafe(map[string]any{
				"decision": "allow",
			}), true
		default:
			// SessionStart, PreInvocation, PostInvocation:
			// Render inline user_markdown while sending empty systemMessage (0 tokens to model, zero interruption).
			md := FormatMessage(snap, true, false, includeGlobal)
			return marshalSafe(map[string]any{
				"injectSteps": []map[string]any{
					{
						"systemMessage": map[string]any{
							"systemMessage": "",
							"metadata": map[string]any{
								"user_markdown": md,
							},
						},
					},
				},
			}), true
		}

	case "codex":
		if ev == "PreToolUse" {
			return []byte("{}"), true
		}
		return marshalSafe(map[string]any{
			"systemMessage": FormatMessage(snap, false, false, includeGlobal),
		}), true

	case "claude":
		if ev == "PreToolUse" {
			return marshalSafe(map[string]any{
				"continue": true,
			}), true
		}
		return marshalSafe(map[string]any{
			"continue":      true,
			"systemMessage": FormatMessage(snap, false, false, includeGlobal),
		}), true

	case "claude-statusline", "statusline":
		return []byte(FormatMessage(snap, false, true, includeGlobal)), false

	case "gemini":
		return marshalSafe(map[string]any{
			"systemMessage": FormatMessage(snap, false, false, includeGlobal),
		}), true

	case "cursor":
		return marshalSafe(map[string]any{
			"user_message": FormatMessage(snap, false, false, includeGlobal),
		}), true

	case "copilot":
		return marshalSafe(map[string]any{
			"continue":      true,
			"systemMessage": FormatMessage(snap, false, false, includeGlobal),
		}), true

	default:
		// generic, hermes, opencode, or unknown custom agent
		return marshalSafe(map[string]any{
			"continue":      true,
			"systemMessage": FormatMessage(snap, false, false, includeGlobal),
			"telemetry":     snap,
		}), true
	}
}

func marshalSafe(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}
