package pricing

// ModelRate defines exact decimal USD rates per 1,000,000 tokens.
type ModelRate struct {
	Provider      string `json:"provider"`
	Input         string `json:"input"`
	Cached        string `json:"cached,omitempty"`
	CacheWrite    string `json:"cacheWrite,omitempty"`
	CacheWrite12h string `json:"cacheWrite12h,omitempty"`
	Output        string `json:"output"`
	Modality      string `json:"modality,omitempty"`
	Source        string `json:"source,omitempty"`
}

// LongContextTier defines the token threshold and multipliers for long-context requests.
type LongContextTier struct {
	Threshold int64  `json:"threshold"`
	Input     string `json:"input"`
	Output    string `json:"output"`
}

// ScheduledRateChange defines a future rate override effective from From (RFC3339).
type ScheduledRateChange struct {
	From   string `json:"from"`
	Input  string `json:"input,omitempty"`
	Cached string `json:"cached,omitempty"`
	Output string `json:"output,omitempty"`
}

// DeprecatedModelInfo records retired or deprecated models that must fail closed
// unless an agent explicitly declares a custom rate.
type DeprecatedModelInfo struct {
	Provider     string `json:"provider"`
	ShutdownDate string `json:"shutdownDate,omitempty"`
	Replacement  string `json:"replacement,omitempty"`
}

// AgentCatalogEntry declares a built-in agent and its passive reader contract.
type AgentCatalogEntry struct {
	ID              string
	Name            string
	Reader          string
	Source          string
	Provider        string
	Tone            string
	Symbol          string
	ConfigKey       string
	FilePattern     string
	Markers         []string
	RecordTypes     []string
	ExpectedVersion string
	Method          string
	SourceVersion   string
	Coverage        string
}

const (
	PricingVersion = "2026-11-19.v1"

	PricingNote = "Equivalente API Standard, USD; nivel supuesto, no cargo real. El contexto se cuenta en cada llamada, incluso cuando se reutiliza. Tarifas verificadas el 19-11-2026; no representa facturas de suscripción. Caché escrita sin duración: se supone 30 min (OpenAI) o 5 min (Anthropic). Sin recargos regionales ni herramientas. Umbrales de contexto largo: OpenAI GPT-6 >272.000 (entrada/caché ×2, salida ×1,5), Claude Haiku 5.5 >100.000 (×5), Grok 4.7 >200.000 (×2), MiniMax M3 >512.000 (×2). Google Gemini 4 Carbon/Argon con tarifa introductoria hasta 30-11-2026 y Gemini 3.8 Flash hasta 31-12-2026; Google excluye almacenamiento y herramientas. Modelos depreciados o sin tarifa verificable quedan sin estimar."
)

const (
	NoteUnknownRate           = "Tarifa no verificada"
	NoteDeprecatedRate        = "Tarifa no verificada: modelo depreciado"
	NoteIncompleteCache       = "Desglose de caché incompleto"
	NoteModalityUnverified    = "Modalidad de entrada no verificada: la tarifa difiere por modalidad"
	NoteCacheWriteUnavailable = "Escritura/almacenamiento de caché sin tarifa verificada"
	NoteCacheWriteAssumed     = "Escritura de caché: 30 min supuestos"
	NoteAssumedStandard       = "API Standard estimada"
)

var ProviderNotes = map[string]string{
	"google":   "API Standard supuesta; no incluye almacenamiento de caché, herramientas ni suscripción",
	"deepseek": "Tarifa API Standard Peak; los descuentos Off-Peak dependen de franja horaria y festivos",
}

// BuiltinAgents lists the immutable core agent entries.
var BuiltinAgents = []AgentCatalogEntry{
	{
		ID:            "codex",
		Name:          "Codex",
		Reader:        "jsonl",
		Source:        "codex",
		Provider:      "openai",
		Tone:          "mint",
		Symbol:        "C",
		ConfigKey:     "codexRoot",
		FilePattern:   `\.jsonl(?:\.[\w-]+)?$`,
		Markers:       []string{`"turn_context"`, `"token_usage_record"`},
		RecordTypes:   []string{"token_usage_record"},
		Method:        "native",
		SourceVersion: "codex.token_usage_record.v1",
		Coverage:      "provider_response_usage",
	},
	{
		ID:              "opencode",
		Name:            "OpenCode",
		Reader:          "sqlite",
		Source:          "opencode",
		Tone:            "blue",
		Symbol:          "O",
		ConfigKey:       "openCodeDb",
		ExpectedVersion: "2.1.4",
		Method:          "native",
		SourceVersion:   "opencode.2.1.4",
		Coverage:        "agent_normalized_usage_zero_defaults_possible",
	},
	{
		ID:            "antigravity",
		Name:          "Antigravity",
		Reader:        "contract",
		Source:        "antigravity",
		Tone:          "violet",
		Symbol:        "A",
		Method:        "reported",
		SourceVersion: "usage-monitor.usage.v1",
		Coverage:      "reported_usage",
	},
}

// DeprecatedModels tracks models deprecated or shut down as of November 2026.
// These models are intentionally absent from Catalog so they fail closed unless
// an agent explicitly registers verified custom rates.
var DeprecatedModels = map[string]DeprecatedModelInfo{
	"gpt-5":            {Provider: "openai", ShutdownDate: "2026-12-11", Replacement: "gpt-6.1-sol"},
	"gpt-5-mini":       {Provider: "openai", ShutdownDate: "2026-12-11", Replacement: "gpt-6.1-terra"},
	"gpt-5-nano":       {Provider: "openai", ShutdownDate: "2026-12-11", Replacement: "gpt-6-luna"},
	"gpt-5-pro":        {Provider: "openai", ShutdownDate: "2026-12-11", Replacement: "gpt-6.2-astra"},
	"gpt-5.4-nano":     {Provider: "openai", Replacement: "gpt-6-luna"},
	"gpt-4o":           {Provider: "openai", Replacement: "gpt-6.2-sol"},
	"gpt-4.1":          {Provider: "openai", Replacement: "gpt-6.2-sol"},
	"o1":               {Provider: "openai", ShutdownDate: "2026-10-23", Replacement: "gpt-6.1-sol"},
	"o1-pro":           {Provider: "openai", ShutdownDate: "2026-10-23", Replacement: "gpt-6.2-astra"},
	"o3":               {Provider: "openai", ShutdownDate: "2026-10-23", Replacement: "gpt-6.1-sol"},
	"o3-mini":          {Provider: "openai", ShutdownDate: "2026-10-23", Replacement: "gpt-6.1-terra"},
	"o4-mini":          {Provider: "openai", ShutdownDate: "2026-10-23", Replacement: "gpt-6.1-terra"},
	"gemini-2.5-pro":   {Provider: "google", ShutdownDate: "2026-12-01", Replacement: "gemini-3.8-flash"},
	"gemini-2.5-flash": {Provider: "google", ShutdownDate: "2026-12-01", Replacement: "gemini-3.5-flash-lite"},
	"gemini-3.7-flash": {Provider: "google", ShutdownDate: "2026-12-01", Replacement: "gemini-3.8-flash"},
	"deepseek-v3":      {Provider: "deepseek", Replacement: "deepseek-flash"},
	"deepseek-r1":      {Provider: "deepseek", Replacement: "deepseek-v4-pro"},
	"kimi-k2":          {Provider: "moonshot", Replacement: "kimi-k2.7-code"},
	"moonshot-v1":      {Provider: "moonshot", Replacement: "kimi-k3"},
	"glm-4.5":          {Provider: "zai", Replacement: "glm-5.3"},
	"minimax-m2.5":     {Provider: "minimax", Replacement: "minimax-m3"},
	"minimax-m2.7":     {Provider: "minimax", Replacement: "minimax-m3"},
}

// IsDeprecated checks whether a model (after canonical normalization) is retired/deprecated.
func IsDeprecated(model string) (DeprecatedModelInfo, bool) {
	nm := NormalizeModel(model)
	for _, cand := range nm.Candidates {
		if info, ok := DeprecatedModels[cand]; ok {
			return info, true
		}
	}
	return DeprecatedModelInfo{}, false
}

// Catalog holds the private internal exact decimal pricing table for verified active models
// as of November 19, 2026. Never exposed directly as a fixed list to the web or public API.
var Catalog = map[string]ModelRate{
	// OpenAI (openai.com / developers.openai.com/api/docs/pricing, Nov 2026)
	"gpt-6.2-astra": {Provider: "openai", Input: "8", Cached: "0.4", CacheWrite: "10", CacheWrite12h: "20", Output: "40", Source: "catalog"},
	"gpt-6.2-sol":   {Provider: "openai", Input: "2", Cached: "0.1", CacheWrite: "2.5", CacheWrite12h: "5", Output: "10", Source: "catalog"},
	"gpt-6-astra":   {Provider: "openai", Input: "10", Cached: "1", CacheWrite: "12.5", CacheWrite12h: "25", Output: "50", Source: "catalog"},
	"gpt-6.1-sol":   {Provider: "openai", Input: "2", Cached: "0.1", CacheWrite: "2.5", CacheWrite12h: "5", Output: "10", Source: "catalog"},
	"gpt-6-sol":     {Provider: "openai", Input: "2", Cached: "0.2", CacheWrite: "2.5", CacheWrite12h: "5", Output: "10", Source: "catalog"},
	"gpt-6.1-terra": {Provider: "openai", Input: "1", Cached: "0.05", CacheWrite: "1.25", CacheWrite12h: "2.5", Output: "5", Source: "catalog"},
	"gpt-6-luna":    {Provider: "openai", Input: "0.1", Cached: "0.01", CacheWrite: "0.125", CacheWrite12h: "0.25", Output: "0.5", Source: "catalog"},
	"gpt-5.5":       {Provider: "openai", Input: "5", Cached: "0.5", Output: "30", Source: "catalog"},
	"gpt-5.4":       {Provider: "openai", Input: "2.5", Cached: "0.25", Output: "15", Source: "catalog"},
	"gpt-5.4-mini":  {Provider: "openai", Input: "0.75", Cached: "0.075", Output: "4.5", Source: "catalog"},

	// Anthropic / Claude (claude.com / anthropic.com, Nov 2026; 5m cache write 1.25x, no 12h tier)
	"claude-fable-5-2":  {Provider: "anthropic", Input: "8", Cached: "0.2", CacheWrite: "10", Output: "40", Source: "catalog"},
	"claude-opus-5-5":   {Provider: "anthropic", Input: "4", Cached: "0.2", CacheWrite: "5", Output: "20", Source: "catalog"},
	"claude-sonnet-5-5": {Provider: "anthropic", Input: "2", Cached: "0.1", CacheWrite: "2.5", Output: "10", Source: "catalog"},
	"claude-haiku-5-5":  {Provider: "anthropic", Input: "0.1", Cached: "0.01", CacheWrite: "0.125", Output: "0.5", Source: "catalog"},
	"claude-opus-4-8":   {Provider: "anthropic", Input: "5", Cached: "0.5", CacheWrite: "6.25", Output: "25", Source: "catalog"},
	"claude-opus-4-6":   {Provider: "anthropic", Input: "5", Cached: "0.5", CacheWrite: "6.25", Output: "25", Source: "catalog"},
	"claude-sonnet-4-6": {Provider: "anthropic", Input: "3", Cached: "0.3", CacheWrite: "3.75", Output: "15", Source: "catalog"},
	"claude-sonnet-4-5": {Provider: "anthropic", Input: "3", Cached: "0.3", CacheWrite: "3.75", Output: "15", Source: "catalog"},
	"claude-haiku-4-5":  {Provider: "anthropic", Input: "1", Cached: "0.1", CacheWrite: "1.25", Output: "5", Source: "catalog"},

	// Google / Gemini (ai.google.dev/gemini-api/docs/pricing, Nov 2026; no cacheWrite rate)
	"gemini-4-carbon":       {Provider: "google", Input: "2", Cached: "0.10", Output: "10", Source: "catalog"},
	"gemini-4-argon":        {Provider: "google", Input: "2", Cached: "0.10", Output: "10", Source: "catalog"},
	"gemini-4-hydrogen":     {Provider: "google", Input: "1.50", Cached: "0.075", Output: "7.50", Source: "catalog"},
	"gemini-3.8-flash":      {Provider: "google", Input: "0.75", Cached: "0.075", Output: "3.75", Source: "catalog"},
	"gemini-3.5-flash-lite": {Provider: "google", Input: "0.30", Cached: "0.03", Output: "2.50", Source: "catalog"},
	"gemini-2.5-flash-lite": {Provider: "google", Input: "0.10", Cached: "0.01", Output: "0.40", Modality: "text", Source: "catalog"},

	// DeepSeek (deepseek.com official API docs, Nov 2026; Standard Peak rates)
	"deepseek-flash":    {Provider: "deepseek", Input: "0.30", Cached: "0.006", Output: "1.20", Source: "catalog"},
	"deepseek-v4-flash": {Provider: "deepseek", Input: "0.30", Cached: "0.006", Output: "1.20", Source: "catalog"},
	"deepseek-v4-pro":   {Provider: "deepseek", Input: "1.32", Cached: "0.044", Output: "3.96", Source: "catalog"},

	// Mistral AI (mistral.ai La Plateforme, Nov 2026)
	"mistral-large-3": {Provider: "mistral", Input: "0.50", Output: "1.50", Source: "catalog"},
	"mistral-small-4": {Provider: "mistral", Input: "0.15", Cached: "0.015", Output: "0.60", Source: "catalog"},
	"codestral":       {Provider: "mistral", Input: "0.30", Cached: "0.03", Output: "0.90", Source: "catalog"},

	// xAI / Grok (x.ai, Nov 2026)
	"grok-4.7": {Provider: "xai", Input: "2", Cached: "0.50", Output: "6", Source: "catalog"},
	"grok-4.3": {Provider: "xai", Input: "1.25", Cached: "0.20", Output: "2.50", Source: "catalog"},

	// Z.AI / GLM (z.ai, Nov 2026)
	"glm-5.3":       {Provider: "zai", Input: "1.40", Cached: "0.26", Output: "4.40", Source: "catalog"},
	"glm-5.2":       {Provider: "zai", Input: "1.40", Cached: "0.26", Output: "4.40", Source: "catalog"},
	"glm-5":         {Provider: "zai", Input: "1", Cached: "0.20", Output: "3.20", Source: "catalog"},
	"glm-4.7":       {Provider: "zai", Input: "0.60", Cached: "0.11", Output: "2.20", Source: "catalog"},
	"glm-5.3-flash": {Provider: "zai", Input: "0.15", Cached: "0.03", Output: "0.50", Source: "catalog"},
	"glm-4.7-flash": {Provider: "zai", Input: "0", Cached: "0", Output: "0", Source: "catalog"},

	// Moonshot AI / Kimi (Moonshot AI Developer Portal, Nov 2026)
	"kimi-k3":        {Provider: "moonshot", Input: "3", Cached: "0.30", Output: "15", Source: "catalog"},
	"kimi-k3.1-pro":  {Provider: "moonshot", Input: "2", Cached: "0.20", Output: "10", Source: "catalog"},
	"kimi-k2.7-code": {Provider: "moonshot", Input: "0.95", Cached: "0.19", Output: "4", Source: "catalog"},
	"kimi-k2.6":      {Provider: "moonshot", Input: "0.95", Cached: "0.19", Output: "4", Source: "catalog"},

	// MiniMax (MiniMax official pricing docs, Nov 2026)
	"minimax-m3":         {Provider: "minimax", Input: "0.30", Cached: "0.06", Output: "1.20", Source: "catalog"},
	"minimax-m3.1-flash": {Provider: "minimax", Input: "0.15", Cached: "0.015", Output: "0.60", Source: "catalog"},

	// Alibaba Cloud / Qwen (Qwen / ModelStudio, Nov 2026)
	"qwen3.5-plus": {Provider: "alibaba", Input: "0.40", Output: "2.40", Source: "catalog"},

	// Meta / StepFun / Meituan (verified Nov 2026)
	"muse-spark-1.3": {Provider: "meta", Input: "1.25", Cached: "0.15", Output: "4.25", Source: "catalog"},
	"step-5-preview": {Provider: "stepfun", Input: "1", Cached: "0.10", Output: "2.70", Source: "catalog"},
	"longcat-2.5":    {Provider: "meituan", Input: "0.30", Cached: "0.006", Output: "1.20", Source: "catalog"},
}

// LongContextTiers defines context-window multipliers per canonical model.
var LongContextTiers = map[string]LongContextTier{
	"gpt-6.2-astra":    {Threshold: 272000, Input: "2", Output: "1.5"},
	"gpt-6.2-sol":      {Threshold: 272000, Input: "2", Output: "1.5"},
	"gpt-6-astra":      {Threshold: 272000, Input: "2", Output: "1.5"},
	"gpt-6.1-sol":      {Threshold: 272000, Input: "2", Output: "1.5"},
	"gpt-6-sol":        {Threshold: 272000, Input: "2", Output: "1.5"},
	"gpt-6.1-terra":    {Threshold: 272000, Input: "2", Output: "1.5"},
	"gpt-6-luna":       {Threshold: 272000, Input: "2", Output: "1.5"},
	"claude-haiku-5-5": {Threshold: 100000, Input: "5", Output: "5"},
	"grok-4.7":         {Threshold: 200000, Input: "2", Output: "2"},
	"minimax-m3":       {Threshold: 512000, Input: "2", Output: "2"},
}

// PricingSchedule defines scheduled future rate overrides per canonical model.
var PricingSchedule = map[string][]ScheduledRateChange{
	"gemini-4-argon": {
		{From: "2026-12-01T00:00:00Z", Input: "4", Cached: "0.20", Output: "20"},
	},
	"gemini-4-carbon": {
		{From: "2026-12-01T00:00:00Z", Input: "4", Cached: "0.20", Output: "20"},
	},
	"gemini-3.8-flash": {
		{From: "2027-01-01T00:00:00Z", Input: "1.50", Cached: "0.15", Output: "7.50"},
	},
}
