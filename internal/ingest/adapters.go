package ingest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/pricing"
)

// AgentSpec describes a declarative JSONL or SQLite reader configuration.
type AgentSpec struct {
	ID                   string
	Name                 string
	Reader               string
	Provider             string
	Tone                 string
	Symbol               string
	ConfigKey            string
	FilePattern          string
	Markers              []string
	RecordTypes          []string
	ContextRecordType    string
	ContextKeyPath       string
	ContextModelPath     string
	ExpectedVersion      string
	Method               string
	SourceVersion        string
	Coverage             string
	QuerySQL             string
	Columns              map[string]string
	AcceptRules          []AcceptRule
	RequireFields        []string
	UsagePaths           map[string][]string
	OccurredAtFormat     string
	FallbackContentKey   string
	FallbackContentPaths []string
}

// AcceptRule filters records by type and field presence/equality.
type AcceptRule struct {
	Type   string
	Path   string
	Equals string
	HasEq  bool
}

// FallbackRecord holds metadata and visible text extracted when a local record lacks native counters.
type FallbackRecord struct {
	Source     string
	Provider   string
	Agent      string
	Model      string
	SessionID  string
	ResponseID string
	OccurredAt time.Time
	Text       string
}

// ParseJSONPreservingNumbers decodes a JSON object preserving large integer numbers as json.Number.
func ParseJSONPreservingNumbers(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.New("empty json object")
	}
	return m, nil
}

// CanonicalID converts arbitrary numeric/string IDs into canonical decimal strings without float truncation.
func CanonicalID(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case int64:
		return strconv.FormatInt(x, 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case int:
		return strconv.Itoa(x)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// Pluck navigates a dotted path inside a parsed JSON map (supporting integer slice indices).
func Pluck(record map[string]any, dotted string) (any, bool) {
	if dotted == "" || record == nil {
		return nil, false
	}
	var cur any = record
	for _, part := range strings.Split(dotted, ".") {
		switch node := cur.(type) {
		case map[string]any:
			if node == nil {
				return nil, false
			}
			v, exists := node[part]
			if !exists || v == nil {
				return nil, false
			}
			cur = v
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(node) || node[idx] == nil {
				return nil, false
			}
			cur = node[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

func resolvePathExpr(record map[string]any, ctx map[string]string, expr string) (any, bool) {
	for _, candidate := range strings.Split(expr, "|") {
		p := strings.TrimSpace(candidate)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "context.") {
			if val, ok := ctx[strings.TrimPrefix(p, "context.")]; ok && val != "" {
				return val, true
			}
			continue
		}
		if v, ok := Pluck(record, p); ok {
			return v, true
		}
	}
	return nil, false
}

// ResolveOccurredAt parses timestamps according to format ("epoch-ms" or ISO-8601)
// BEFORE validation/boundary comparison.
func ResolveOccurredAt(format string, val any) (time.Time, error) {
	if val == nil {
		return time.Time{}, pricing.ErrInvalidTimestamp
	}
	if format == "epoch-ms" {
		var ms int64
		switch x := val.(type) {
		case json.Number:
			if i, err := x.Int64(); err == nil {
				ms = i
			} else if f, err := x.Float64(); err == nil {
				ms = int64(f)
			} else {
				return time.Time{}, pricing.ErrInvalidTimestamp
			}
		case int64:
			ms = x
		case float64:
			ms = int64(x)
		case string:
			parsed, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
			if err != nil {
				return time.Time{}, pricing.ErrInvalidTimestamp
			}
			ms = parsed
		default:
			return time.Time{}, pricing.ErrInvalidTimestamp
		}
		if ms < 0 {
			return time.Time{}, pricing.ErrInvalidTimestamp
		}
		return time.UnixMilli(ms).UTC(), nil
	}

	s := CanonicalID(val)
	if t, err := pricing.ParseTimestamp(s); err == nil {
		return t, nil
	}
	// Fallback: accept numeric epoch-ms if an agent mixes ISO and epoch-ms timestamps.
	if ms, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil && ms > 1_000_000_000_000 {
		return time.UnixMilli(ms).UTC(), nil
	}
	return time.Time{}, pricing.ErrInvalidTimestamp
}

func versionMatches(expected, actual string) bool {
	if expected == "" || actual == "" {
		return true
	}
	if actual == expected {
		return true
	}
	if strings.HasPrefix(expected, "2.") && strings.HasPrefix(actual, "2.") {
		return true
	}
	return false
}

func matchesAcceptRules(spec AgentSpec, record map[string]any) bool {
	if len(spec.AcceptRules) == 0 {
		return true
	}
	recType := CanonicalID(record["type"])
	for _, rule := range spec.AcceptRules {
		if rule.Type != "" && recType != rule.Type {
			continue
		}
		if rule.Path != "" {
			v, ok := Pluck(record, rule.Path)
			if !ok {
				continue
			}
			if rule.HasEq && CanonicalID(v) != rule.Equals {
				continue
			}
		}
		return true
	}
	return false
}

// ExtractVisibleText extracts purely textual content from a local record for silent
// fallback tokenization. Returns ("", false) when non-text media/tool parts exist or no text is found.
func ExtractVisibleText(spec AgentSpec, rec map[string]any) (string, bool) {
	paths := make([]string, 0, len(spec.FallbackContentPaths)+1)
	if spec.FallbackContentKey != "" {
		paths = append(paths, spec.FallbackContentKey)
	}
	paths = append(paths, spec.FallbackContentPaths...)

	for _, p := range paths {
		v, ok := Pluck(rec, p)
		if !ok {
			continue
		}
		if txt, valid := visibleTextValue(v); valid && txt != "" {
			return txt, true
		}
	}
	return "", false
}

func visibleTextValue(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, x != ""
	case []any:
		if len(x) == 0 {
			return "", false
		}
		var b strings.Builder
		for _, item := range x {
			switch p := item.(type) {
			case string:
				b.WriteString(p)
			case map[string]any:
				if t, ok := p["type"].(string); ok && t != "" && t != "text" && t != "output_text" {
					return "", false
				}
				txt, ok := p["text"].(string)
				if !ok {
					return "", false
				}
				b.WriteString(txt)
			default:
				return "", false
			}
		}
		out := b.String()
		return out, out != ""
	default:
		return "", false
	}
}

// ExtractFallbackMeta extracts visible-text fallback metadata when a local record has no native counters.
func ExtractFallbackMeta(spec AgentSpec, record map[string]any, ctx map[string]string) (FallbackRecord, bool) {
	if spec.ExpectedVersion != "" && ctx["version"] != "" && !versionMatches(spec.ExpectedVersion, ctx["version"]) {
		return FallbackRecord{}, false
	}
	if !matchesAcceptRules(spec, record) {
		return FallbackRecord{}, false
	}

	txt, ok := ExtractVisibleText(spec, record)
	if !ok {
		return FallbackRecord{}, false
	}

	firstStr := func(field string) string {
		for _, p := range spec.UsagePaths[field] {
			if v, ok := resolvePathExpr(record, ctx, p); ok {
				if s := CanonicalID(v); s != "" {
					return s
				}
			}
		}
		return ""
	}

	respID := firstStr("responseId")
	model := firstStr("model")
	if respID == "" || model == "" || model == "unknown" {
		return FallbackRecord{}, false
	}

	var occVal any
	for _, p := range spec.UsagePaths["occurredAt"] {
		if v, ok := resolvePathExpr(record, ctx, p); ok && CanonicalID(v) != "" {
			occVal = v
			break
		}
	}
	if occVal == nil {
		for _, p := range spec.UsagePaths["occurredAtFallback"] {
			if v, ok := resolvePathExpr(record, ctx, p); ok && CanonicalID(v) != "" {
				occVal = v
				break
			}
		}
	}
	if occVal == nil {
		return FallbackRecord{}, false
	}
	occTime, err := ResolveOccurredAt(spec.OccurredAtFormat, occVal)
	if err != nil {
		return FallbackRecord{}, false
	}

	provider := firstStr("provider")
	if provider == "" {
		provider = spec.Provider
	}

	return FallbackRecord{
		Source:     spec.ID,
		Provider:   provider,
		Agent:      spec.Name,
		Model:      model,
		SessionID:  firstStr("sessionId"),
		ResponseID: respID,
		OccurredAt: occTime,
		Text:       txt,
	}, true
}

var (
	counterFields = map[string]bool{
		"inputTokens":           true,
		"outputTokens":          true,
		"cachedInputTokens":     true,
		"cacheWriteInputTokens": true,
		"reasoningOutputTokens": true,
	}
	requiredCounters = map[string]bool{
		"inputTokens":  true,
		"outputTokens": true,
	}
)

// CatalogMessage builds a pricing.RawEvent from a record according to AgentSpec.
// Returns (nil, nil) if the record carries no native usage counters or fails accept filter.
func CatalogMessage(spec AgentSpec, record map[string]any, ctx map[string]string) (*pricing.RawEvent, error) {
	if spec.ExpectedVersion != "" && ctx["version"] != "" && !versionMatches(spec.ExpectedVersion, ctx["version"]) {
		return nil, errors.New(spec.ID + "_unsupported_version")
	}

	if !matchesAcceptRules(spec, record) {
		return nil, nil
	}

	resolve := func(path string) (any, bool) {
		return resolvePathExpr(record, ctx, path)
	}

	for _, reqField := range spec.RequireFields {
		paths := spec.UsagePaths[reqField]
		found := false
		for _, p := range paths {
			if _, ok := resolve(p); ok {
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New(spec.ID + "_missing_" + reqField)
		}
	}

	hasAnyCounter := false
	for field, paths := range spec.UsagePaths {
		if !counterFields[field] {
			continue
		}
		for _, p := range paths {
			if _, ok := resolve(p); ok {
				hasAnyCounter = true
				break
			}
		}
		if hasAnyCounter {
			break
		}
	}
	if !hasAnyCounter {
		return nil, nil
	}

	extractCounter := func(field string) (*string, error) {
		paths := spec.UsagePaths[field]
		if len(paths) == 0 {
			return nil, nil
		}
		firstVal, firstOK := resolve(paths[0])
		if !firstOK {
			if requiredCounters[field] {
				return nil, errors.New(spec.ID + "_missing_metric")
			}
			return nil, nil
		}
		sum := new(big.Int)
		addVal := func(v any) error {
			s := CanonicalID(v)
			n, err := pricing.ParseTokenString(s)
			if err != nil {
				return err
			}
			sum.Add(sum, n)
			return nil
		}
		if err := addVal(firstVal); err != nil {
			return nil, err
		}
		for _, p := range paths[1:] {
			if v, ok := resolve(p); ok {
				if err := addVal(v); err != nil {
					return nil, err
				}
			}
		}
		out := sum.String()
		return &out, nil
	}

	extractFirstStr := func(field string) string {
		for _, p := range spec.UsagePaths[field] {
			if v, ok := resolve(p); ok {
				s := CanonicalID(v)
				if s != "" {
					return s
				}
			}
		}
		return ""
	}

	inTok, err := extractCounter("inputTokens")
	if err != nil {
		return nil, err
	}
	outTok, err := extractCounter("outputTokens")
	if err != nil {
		return nil, err
	}
	if inTok == nil && outTok == nil {
		return nil, nil
	}
	if inTok == nil || outTok == nil {
		return nil, errors.New(spec.ID + "_missing_metric")
	}

	cachedTok, err := extractCounter("cachedInputTokens")
	if err != nil {
		return nil, err
	}
	cwriteTok, err := extractCounter("cacheWriteInputTokens")
	if err != nil {
		return nil, err
	}
	reasonTok, err := extractCounter("reasoningOutputTokens")
	if err != nil {
		return nil, err
	}

	var occVal any
	for _, p := range spec.UsagePaths["occurredAt"] {
		if v, ok := resolve(p); ok && CanonicalID(v) != "" {
			occVal = v
			break
		}
	}
	if occVal == nil {
		for _, p := range spec.UsagePaths["occurredAtFallback"] {
			if v, ok := resolve(p); ok && CanonicalID(v) != "" {
				occVal = v
				break
			}
		}
	}
	if occVal == nil {
		return nil, nil
	}
	occTime, err := ResolveOccurredAt(spec.OccurredAtFormat, occVal)
	if err != nil {
		return nil, err
	}

	provider := extractFirstStr("provider")
	if provider == "" {
		provider = spec.Provider
	}
	model := extractFirstStr("model")
	if model == "" {
		model = "unknown"
	}

	method := spec.Method
	if method == "" {
		method = "native"
	}

	return &pricing.RawEvent{
		Source:                spec.ID,
		Agent:                 spec.Name,
		Method:                method,
		SourceVersion:         spec.SourceVersion,
		Coverage:              spec.Coverage,
		Provider:              provider,
		Model:                 model,
		ResponseID:            extractFirstStr("responseId"),
		SessionID:             extractFirstStr("sessionId"),
		TurnID:                extractFirstStr("turnId"),
		OccurredAt:            occTime.Format("2006-01-02T15:04:05.000Z"),
		InputTokens:           *inTok,
		OutputTokens:          *outTok,
		CachedInputTokens:     cachedTok,
		CacheWriteInputTokens: cwriteTok,
		ReasoningOutputTokens: reasonTok,
	}, nil
}

// CodexSpec returns the built-in declarative spec for Codex JSONL logs.
func CodexSpec() AgentSpec {
	return AgentSpec{
		ID:                   "codex",
		Name:                 "Codex",
		Reader:               "jsonl",
		Provider:             "openai",
		Tone:                 "mint",
		Symbol:               "C",
		ConfigKey:            "codexRoot",
		FilePattern:          `\.jsonl(?:\.[\w-]+)?$`,
		Markers:              []string{`"turn_context"`, `"token_usage_record"`},
		RecordTypes:          []string{"token_usage_record"},
		ContextRecordType:    "turn_context",
		ContextKeyPath:       "payload.turn_id",
		ContextModelPath:     "payload.model",
		Method:               "native",
		SourceVersion:        "codex.token_usage_record.v1",
		Coverage:             "provider_response_usage",
		FallbackContentPaths: []string{"payload.content", "payload.text"},
		UsagePaths: map[string][]string{
			"responseId":            {"payload.response_id"},
			"sessionId":             {"payload.session_id", "payload.thread_id"},
			"turnId":                {"payload.turn_id"},
			"model":                 {"context.model"},
			"occurredAt":            {"timestamp"},
			"inputTokens":           {"payload.usage.input_tokens"},
			"outputTokens":          {"payload.usage.output_tokens"},
			"cachedInputTokens":     {"payload.usage.cached_input_tokens"},
			"cacheWriteInputTokens": {"payload.usage.cache_write_input_tokens"},
			"reasoningOutputTokens": {"payload.usage.reasoning_output_tokens"},
		},
	}
}

// ClaudeSpec returns the declarative spec for Claude Code JSONL logs.
func ClaudeSpec() AgentSpec {
	return AgentSpec{
		ID:                   "claude",
		Name:                 "Claude Code",
		Reader:               "jsonl",
		Provider:             "anthropic",
		Tone:                 "amber",
		Symbol:               "C",
		ConfigKey:            "claudeRoot",
		FilePattern:          `\.jsonl$`,
		Method:               "native",
		SourceVersion:        "claude.jsonl.v1",
		Coverage:             "provider_response_usage",
		FallbackContentPaths: []string{"message.content", "content"},
		UsagePaths: map[string][]string{
			"responseId":            {"message.id", "uuid"},
			"sessionId":             {"sessionId"},
			"occurredAt":            {"timestamp"},
			"model":                 {"message.model"},
			"inputTokens":           {"message.usage.input_tokens", "message.usage.cache_read_input_tokens", "message.usage.cache_creation_input_tokens"},
			"outputTokens":          {"message.usage.output_tokens"},
			"cachedInputTokens":     {"message.usage.cache_read_input_tokens"},
			"cacheWriteInputTokens": {"message.usage.cache_creation_input_tokens"},
		},
	}
}

// GeminiSpec returns the declarative spec for Gemini CLI JSONL logs.
func GeminiSpec() AgentSpec {
	return AgentSpec{
		ID:                   "gemini",
		Name:                 "Gemini CLI",
		Reader:               "jsonl",
		Provider:             "google",
		Tone:                 "blue",
		Symbol:               "G",
		ConfigKey:            "geminiRoot",
		FilePattern:          `\.jsonl$`,
		Method:               "native",
		SourceVersion:        "gemini.jsonl.v1",
		Coverage:             "provider_response_usage",
		FallbackContentPaths: []string{"candidates.0.content.parts", "content", "text"},
		UsagePaths: map[string][]string{
			"responseId":            {"responseId|response_id|id"},
			"sessionId":             {"sessionId|session_id"},
			"occurredAt":            {"timestamp|occurredAt|createdAt"},
			"model":                 {"model|modelVersion"},
			"provider":              {"provider"},
			"inputTokens":           {"usageMetadata.promptTokenCount|usage.input_tokens|inputTokens"},
			"outputTokens":          {"usageMetadata.candidatesTokenCount|usage.output_tokens|outputTokens", "usageMetadata.thoughtsTokenCount"},
			"cachedInputTokens":     {"usageMetadata.cachedContentTokenCount|usage.cached_input_tokens"},
			"reasoningOutputTokens": {"usageMetadata.thoughtsTokenCount|usage.reasoning_output_tokens"},
		},
	}
}

// CursorSpec returns the declarative spec for Cursor JSONL logs.
func CursorSpec() AgentSpec {
	return AgentSpec{
		ID:                   "cursor",
		Name:                 "Cursor",
		Reader:               "jsonl",
		Provider:             "unknown",
		Tone:                 "cyan",
		Symbol:               "U",
		ConfigKey:            "cursorRoot",
		FilePattern:          `\.jsonl$`,
		Method:               "native",
		SourceVersion:        "cursor.jsonl.v1",
		Coverage:             "provider_response_usage",
		FallbackContentPaths: []string{"message.content", "content", "text"},
		UsagePaths: map[string][]string{
			"responseId":            {"responseId|response_id|message.id|id"},
			"sessionId":             {"sessionId|session_id|conversationId"},
			"occurredAt":            {"timestamp|occurredAt|createdAt"},
			"model":                 {"model|message.model"},
			"provider":              {"provider"},
			"inputTokens":           {"usage.input_tokens|usage.prompt_tokens|inputTokens"},
			"outputTokens":          {"usage.output_tokens|usage.completion_tokens|outputTokens"},
			"cachedInputTokens":     {"usage.cached_input_tokens|usage.cache_read_input_tokens|cachedInputTokens"},
			"cacheWriteInputTokens": {"usage.cache_write_input_tokens|usage.cache_creation_input_tokens|cacheWriteInputTokens"},
			"reasoningOutputTokens": {"usage.reasoning_output_tokens|reasoningOutputTokens"},
		},
	}
}

// CopilotSpec returns the declarative spec for GitHub Copilot JSONL logs.
func CopilotSpec() AgentSpec {
	return AgentSpec{
		ID:                   "copilot",
		Name:                 "GitHub Copilot",
		Reader:               "jsonl",
		Provider:             "github-copilot",
		Tone:                 "rose",
		Symbol:               "P",
		ConfigKey:            "copilotRoot",
		FilePattern:          `\.jsonl$`,
		Method:               "native",
		SourceVersion:        "copilot.jsonl.v1",
		Coverage:             "provider_response_usage",
		FallbackContentPaths: []string{"message.content", "content", "text"},
		UsagePaths: map[string][]string{
			"responseId":            {"responseId|response_id|message.id|id"},
			"sessionId":             {"sessionId|session_id"},
			"occurredAt":            {"timestamp|occurredAt|createdAt"},
			"model":                 {"model|message.model"},
			"provider":              {"provider"},
			"inputTokens":           {"usage.prompt_tokens|usage.input_tokens|inputTokens"},
			"outputTokens":          {"usage.completion_tokens|usage.output_tokens|outputTokens"},
			"cachedInputTokens":     {"usage.prompt_tokens_details.cached_tokens|usage.cached_input_tokens|cachedInputTokens"},
			"cacheWriteInputTokens": {"usage.cache_write_input_tokens|cacheWriteInputTokens"},
			"reasoningOutputTokens": {"usage.completion_tokens_details.reasoning_tokens|usage.reasoning_output_tokens|reasoningOutputTokens"},
		},
	}
}

// HermesSpec returns the declarative spec for Hermes Agent JSONL logs.
func HermesSpec() AgentSpec {
	return AgentSpec{
		ID:                   "hermes",
		Name:                 "Hermes Agent",
		Reader:               "jsonl",
		Provider:             "nous",
		Tone:                 "violet",
		Symbol:               "H",
		ConfigKey:            "hermesRoot",
		FilePattern:          `\.jsonl$`,
		Method:               "native",
		SourceVersion:        "hermes.jsonl.v1",
		Coverage:             "provider_response_usage",
		FallbackContentPaths: []string{"message.content", "content", "text"},
		UsagePaths: map[string][]string{
			"responseId":            {"responseId|response_id|message.id|id"},
			"sessionId":             {"sessionId|session_id"},
			"occurredAt":            {"timestamp|occurredAt|createdAt"},
			"model":                 {"model|message.model"},
			"provider":              {"provider"},
			"inputTokens":           {"usage.input_tokens|usage.prompt_tokens|inputTokens"},
			"outputTokens":          {"usage.output_tokens|usage.completion_tokens|outputTokens"},
			"cachedInputTokens":     {"usage.cached_input_tokens|usage.cache_read_input_tokens|cachedInputTokens"},
			"cacheWriteInputTokens": {"usage.cache_write_input_tokens|usage.cache_creation_input_tokens|cacheWriteInputTokens"},
			"reasoningOutputTokens": {"usage.reasoning_output_tokens|reasoningOutputTokens"},
		},
	}
}

// OpenCodeSpec returns the built-in declarative spec for OpenCode SQLite DBs.
func OpenCodeSpec() AgentSpec {
	return AgentSpec{
		ID:                   "opencode",
		Name:                 "OpenCode",
		Reader:               "sqlite",
		Tone:                 "blue",
		Symbol:               "O",
		ConfigKey:            "openCodeDb",
		ExpectedVersion:      "2.1.4",
		Method:               "native",
		SourceVersion:        "opencode.2.1.4",
		Coverage:             "agent_normalized_usage_zero_defaults_possible",
		QuerySQL:             `SELECT m.id, m.session_id, m.time_updated, m.data, m.type, s.version FROM session_message m JOIN session_v2 s ON s.id=m.session_id WHERE m.type IN ('assistant','compaction') AND (m.time_updated>? OR (m.time_updated=? AND m.id>?)) ORDER BY m.time_updated, m.id LIMIT 200`,
		Columns:              map[string]string{"id": "id", "sessionId": "session_id", "updated": "time_updated", "version": "version", "payload": "data", "type": "type"},
		AcceptRules:          []AcceptRule{{Type: "assistant", Path: "time.completed"}, {Type: "compaction", Path: "status", Equals: "completed", HasEq: true}},
		RequireFields:        []string{"model", "provider"},
		OccurredAtFormat:     "epoch-ms",
		FallbackContentKey:   "content",
		FallbackContentPaths: []string{"parts", "text"},
		UsagePaths: map[string][]string{
			"responseId":            {"id"},
			"sessionId":             {"context.sessionId"},
			"occurredAt":            {"time.completed"},
			"occurredAtFallback":    {"time.created"},
			"model":                 {"model.id"},
			"provider":              {"model.providerID"},
			"inputTokens":           {"tokens.input", "tokens.cache.read", "tokens.cache.write"},
			"outputTokens":          {"tokens.output", "tokens.reasoning"},
			"cachedInputTokens":     {"tokens.cache.read"},
			"cacheWriteInputTokens": {"tokens.cache.write"},
			"reasoningOutputTokens": {"tokens.reasoning"},
		},
	}
}
