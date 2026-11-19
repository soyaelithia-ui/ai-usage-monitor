package pricing

import (
	"regexp"
	"strings"
)

// NormalizedModel holds the canonical representation and ordered lookup candidates
// for a raw model identifier across pricing, discovery, caching, and tokenization.
type NormalizedModel struct {
	Raw           string
	Canonical     string
	ProviderHint  string
	IsFreeVariant bool
	Candidates    []string
}

var (
	providerAliases = map[string]string{
		"x-ai":             "xai",
		"x.ai":             "xai",
		"z-ai":             "zai",
		"z.ai":             "zai",
		"zhipu":            "zai",
		"zhipuai":          "zai",
		"zai-org":          "zai",
		"moonshotai":       "moonshot",
		"kimi":             "moonshot",
		"qwen":             "alibaba",
		"dashscope":        "alibaba",
		"aliyun":           "alibaba",
		"alibaba-cloud":    "alibaba",
		"deepseek-ai":      "deepseek",
		"mistralai":        "mistral",
		"google-vertex":    "google",
		"vertex":           "google",
		"vertex_ai":        "google",
		"gemini":           "google",
		"google-ai-studio": "google",
		"meta-llama":       "meta",
		"nousresearch":     "nous",
		"stepfun-ai":       "stepfun",
	}

	aggregatorProviders = map[string]bool{
		"openrouter":     true,
		"litellm":        true,
		"github-copilot": true,
		"copilot":        true,
		"bedrock":        true,
		"vertex":         true,
		"vertex_ai":      true,
		"vercel":         true,
		"azure":          true,
		"together":       true,
		"fireworks":      true,
		"groq":           true,
		"deepinfra":      true,
		"replicate":      true,
		"huggingface":    true,
	}

	aggregatorPrefixes = []string{
		"openrouter/",
		"litellm/",
		"azure/",
		"bedrock/",
		"vertex_ai/",
		"vertex/",
		"google-vertex/",
		"github-copilot/",
		"copilot/",
		"models/",
		"vercel/",
		"together/",
		"fireworks/",
		"groq/",
		"deepinfra/",
		"replicate/",
		"huggingface/",
	}

	directProviderPrefixes = []string{
		"openai/",
		"anthropic/",
		"google/",
		"mistralai/",
		"mistral/",
		"deepseek-ai/",
		"deepseek/",
		"qwen/",
		"alibaba/",
		"dashscope/",
		"z-ai/",
		"zai/",
		"zhipu/",
		"moonshotai/",
		"moonshot/",
		"minimax/",
		"x-ai/",
		"xai/",
		"meta-llama/",
		"meta/",
		"stepfun-ai/",
		"stepfun/",
		"meituan/",
		"nousresearch/",
		"nous/",
		"nvidia/",
		"upstage/",
	}

	bedrockRegionVendorRe = regexp.MustCompile(`^(?:(?:us|eu|ap|global)\.)?(anthropic|meta|amazon|mistral|deepseek|cohere)\.(.+)$`)
	bedrockVersionRe      = regexp.MustCompile(`(?:-v\d+:\d+|:\d+)$`)
	isoDateSuffixRe       = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)
	compactDateSuffixRe   = regexp.MustCompile(`[-@]\d{8}$`)
	shortDateSuffixRe     = regexp.MustCompile(`-\d{4}$`)
	trailingAliasRe       = regexp.MustCompile(`(?:-latest|:latest|-preview|-exp|-beta)$`)
	claudeDotVersionRe    = regexp.MustCompile(`^(claude-(?:opus|sonnet|haiku|fable)-)(\d+)\.(\d+)(.*)$`)
)

// NormalizeProvider canonicalizes a provider name to a stable lowercase identifier.
func NormalizeProvider(raw string) string {
	p := strings.ToLower(strings.TrimSpace(raw))
	if p == "" {
		return ""
	}
	if canon, ok := providerAliases[p]; ok {
		return canon
	}
	return p
}

// IsAggregatorProvider reports whether raw refers to a multi-provider routing aggregator.
func IsAggregatorProvider(raw string) bool {
	p := strings.ToLower(strings.TrimSpace(raw))
	return aggregatorProviders[p]
}

// ProvidersCompatible verifies that an event's declared provider is compatible with a
// resolved rate's provider without allowing cross-provider mismatches.
func ProvidersCompatible(rateProvider, eventProvider, modelPrefixProvider string) bool {
	rp := NormalizeProvider(rateProvider)
	epRaw := strings.ToLower(strings.TrimSpace(eventProvider))
	ep := NormalizeProvider(eventProvider)
	mp := NormalizeProvider(modelPrefixProvider)

	if rp == "" || ep == "" {
		return true
	}
	if rp == ep {
		return true
	}
	if IsAggregatorProvider(epRaw) {
		if rp == epRaw {
			return true
		}
		if mp != "" {
			return mp == rp
		}
		return true
	}
	return false
}

func normalizeClaudeDots(s string) string {
	return claudeDotVersionRe.ReplaceAllString(s, "${1}${2}-${3}${4}")
}

// NormalizeModel normalizes aggregator/provider prefixes, Bedrock paths, Claude version dots,
// and date/snapshot suffixes into a canonical model ID and ordered lookup candidates.
func NormalizeModel(rawModel string) NormalizedModel {
	raw := strings.TrimSpace(rawModel)
	if raw == "" {
		return NormalizedModel{}
	}

	seen := make(map[string]struct{}, 12)
	candidates := make([]string, 0, 12)
	addCand := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		candidates = append(candidates, s)
	}

	addCand(raw)
	lower := strings.ToLower(raw)
	addCand(lower)

	isFree := strings.HasSuffix(lower, ":free")
	freeSuffix := ""
	work := lower
	if isFree {
		freeSuffix = ":free"
		work = strings.TrimSuffix(work, ":free")
	}

	// Strip outer aggregator prefixes (may be nested, e.g. openrouter/google/...)
	providerHint := ""
	changed := true
	for changed {
		changed = false
		for _, pref := range aggregatorPrefixes {
			if strings.HasPrefix(work, pref) {
				if pref == "vertex_ai/" || pref == "vertex/" || pref == "google-vertex/" || pref == "models/" {
					if providerHint == "" {
						providerHint = "google"
					}
				}
				work = strings.TrimPrefix(work, pref)
				addCand(work + freeSuffix)
				changed = true
				break
			}
		}
	}

	// Strip direct vendor slash prefix (e.g. anthropic/, openai/, z-ai/, moonshotai/)
	for _, pref := range directProviderPrefixes {
		if strings.HasPrefix(work, pref) {
			vendor := strings.TrimSuffix(pref, "/")
			providerHint = NormalizeProvider(vendor)
			work = strings.TrimPrefix(work, pref)
			addCand(work + freeSuffix)
			break
		}
	}

	// Strip AWS Bedrock region + vendor prefix (e.g. us.anthropic.claude-sonnet-4-6-20260217-v1:0)
	if m := bedrockRegionVendorRe.FindStringSubmatch(work); len(m) == 3 {
		if providerHint == "" {
			providerHint = NormalizeProvider(m[1])
		}
		work = m[2]
		addCand(work + freeSuffix)
	}

	// Strip Bedrock version suffix (-v1:0, :0)
	if stripped := bedrockVersionRe.ReplaceAllString(work, ""); stripped != work {
		work = stripped
		addCand(work + freeSuffix)
	}

	// Normalize Claude dot versions (claude-sonnet-4.6 -> claude-sonnet-4-6)
	if claudeNorm := normalizeClaudeDots(work); claudeNorm != work {
		work = claudeNorm
		addCand(work + freeSuffix)
	}

	// Strip ISO date (-YYYY-MM-DD) and compact 8-digit date (-YYYYMMDD / @YYYYMMDD)
	if stripped := isoDateSuffixRe.ReplaceAllString(work, ""); stripped != work {
		work = stripped
		addCand(work + freeSuffix)
	}
	if stripped := compactDateSuffixRe.ReplaceAllString(work, ""); stripped != work {
		work = stripped
		addCand(work + freeSuffix)
	}

	// Re-apply Claude dot normalization in case date stripping exposed it
	if claudeNorm := normalizeClaudeDots(work); claudeNorm != work {
		work = claudeNorm
		addCand(work + freeSuffix)
	}

	canonical := work + freeSuffix

	// Secondary candidate: strip trailing -latest / -preview / -exp / -beta
	if stripped := trailingAliasRe.ReplaceAllString(work, ""); stripped != work {
		addCand(stripped + freeSuffix)
		if claudeNorm := normalizeClaudeDots(stripped); claudeNorm != stripped {
			addCand(claudeNorm + freeSuffix)
		}
	}

	// Secondary candidate: strip 4-digit YYMM/MMDD date suffix (e.g. -2604)
	if stripped := shortDateSuffixRe.ReplaceAllString(work, ""); stripped != work {
		addCand(stripped + freeSuffix)
	}

	return NormalizedModel{
		Raw:           raw,
		Canonical:     canonical,
		ProviderHint:  providerHint,
		IsFreeVariant: isFree,
		Candidates:    candidates,
	}
}
