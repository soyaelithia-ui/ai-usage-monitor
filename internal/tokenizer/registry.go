package tokenizer

import (
	"strings"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/pricing"
)

// Family identifies an internal tokenizer family. Kept strictly internal and
// never exposed as a UI control or public API status/selector.
type Family string

const (
	FamilyOpenAIO200K        Family = "openai_o200k"
	FamilyOpenAICL100K       Family = "openai_cl100k"
	FamilyAnthropicClaude    Family = "anthropic_claude"
	FamilyGoogleGemini       Family = "google_gemini"
	FamilyMistralTekken      Family = "mistral_tekken"
	FamilyMistralSentencePce Family = "mistral_sentencepiece"
	FamilyMiniMax            Family = "minimax_bpe"
	FamilyZAIGLM             Family = "zai_glm"
	FamilyDeepSeek           Family = "deepseek_bpe"
	FamilyQwen               Family = "qwen_bpe"
	FamilyKimi               Family = "kimi_bpe"
	FamilyLlamaGrok          Family = "llama_grok_bpe"
)

// EngineKind describes the underlying tokenization algorithm family.
type EngineKind string

const (
	EngineBPE           EngineKind = "bpe"
	EngineTekken        EngineKind = "tekken"
	EngineSentencePiece EngineKind = "sentencepiece"
)

// Profile configures pre-tokenization and subword granularity for a tokenizer family.
type Profile struct {
	Family         Family
	Engine         EngineKind
	VocabSize      int
	MaxDigitGroup  int  // 3 for tiktoken/Llama/Claude/Kimi/GLM, 1 for DeepSeek/Qwen/MiniMax/Tekken/SP
	SplitHanRuns   bool // isolate Han runs separately from Latin/punctuation
	SentencePiece  bool // use U+2581 metaspace + SP script splitting
	WordChunkRunes int  // calibrated single-token Latin/word span for built-in subword encoder
	CJKCharsPerTok int  // calibrated Han/CJK runes per token (1 or 2 for high-density CJK vocabs)
	VocabFileNames []string
}

var profiles = map[Family]Profile{
	FamilyOpenAIO200K: {
		Family:         FamilyOpenAIO200K,
		Engine:         EngineBPE,
		VocabSize:      200019,
		MaxDigitGroup:  3,
		WordChunkRunes: 7,
		CJKCharsPerTok: 1,
		VocabFileNames: []string{"o200k_base.tiktoken", "o200k_base.model"},
	},
	FamilyOpenAICL100K: {
		Family:         FamilyOpenAICL100K,
		Engine:         EngineBPE,
		VocabSize:      100277,
		MaxDigitGroup:  3,
		WordChunkRunes: 6,
		CJKCharsPerTok: 1,
		VocabFileNames: []string{"cl100k_base.tiktoken", "cl100k_base.model"},
	},
	FamilyAnthropicClaude: {
		Family:         FamilyAnthropicClaude,
		Engine:         EngineBPE,
		VocabSize:      65000,
		MaxDigitGroup:  3,
		WordChunkRunes: 5,
		CJKCharsPerTok: 1,
		VocabFileNames: []string{"claude.tokenizer.json"},
	},
	FamilyGoogleGemini: {
		Family:         FamilyGoogleGemini,
		Engine:         EngineSentencePiece,
		VocabSize:      256000,
		MaxDigitGroup:  1,
		SentencePiece:  true,
		WordChunkRunes: 7,
		CJKCharsPerTok: 2,
		VocabFileNames: []string{"gemini.model", "gemma.model"},
	},
	FamilyMistralTekken: {
		Family:         FamilyMistralTekken,
		Engine:         EngineTekken,
		VocabSize:      131072,
		MaxDigitGroup:  1,
		WordChunkRunes: 6,
		CJKCharsPerTok: 1,
		VocabFileNames: []string{"tekken.json", "mistral-tekken.tokenizer.json"},
	},
	FamilyMistralSentencePce: {
		Family:         FamilyMistralSentencePce,
		Engine:         EngineSentencePiece,
		VocabSize:      32768,
		MaxDigitGroup:  1,
		SentencePiece:  true,
		WordChunkRunes: 5,
		CJKCharsPerTok: 1,
		VocabFileNames: []string{"mistral-sp.model", "mistral-v0.3.tokenizer.json"},
	},
	FamilyMiniMax: {
		Family:         FamilyMiniMax,
		Engine:         EngineBPE,
		VocabSize:      200000,
		MaxDigitGroup:  1,
		SplitHanRuns:   true,
		WordChunkRunes: 6,
		CJKCharsPerTok: 2,
		VocabFileNames: []string{"minimax.tokenizer.json"},
	},
	FamilyZAIGLM: {
		Family:         FamilyZAIGLM,
		Engine:         EngineBPE,
		VocabSize:      151552,
		MaxDigitGroup:  3,
		SplitHanRuns:   true,
		WordChunkRunes: 6,
		CJKCharsPerTok: 2,
		VocabFileNames: []string{"glm.tokenizer.json", "chatglm4.model"},
	},
	FamilyDeepSeek: {
		Family:         FamilyDeepSeek,
		Engine:         EngineBPE,
		VocabSize:      128000,
		MaxDigitGroup:  1,
		SplitHanRuns:   true,
		WordChunkRunes: 6,
		CJKCharsPerTok: 2,
		VocabFileNames: []string{"deepseek.tokenizer.json"},
	},
	FamilyQwen: {
		Family:         FamilyQwen,
		Engine:         EngineBPE,
		VocabSize:      151936,
		MaxDigitGroup:  1,
		SplitHanRuns:   true,
		WordChunkRunes: 6,
		CJKCharsPerTok: 2,
		VocabFileNames: []string{"qwen.tokenizer.json"},
	},
	FamilyKimi: {
		Family:         FamilyKimi,
		Engine:         EngineBPE,
		VocabSize:      163840,
		MaxDigitGroup:  3,
		SplitHanRuns:   true,
		WordChunkRunes: 6,
		CJKCharsPerTok: 2,
		VocabFileNames: []string{"kimi.tiktoken", "kimi-k2.model"},
	},
	FamilyLlamaGrok: {
		Family:         FamilyLlamaGrok,
		Engine:         EngineBPE,
		VocabSize:      128256,
		MaxDigitGroup:  3,
		WordChunkRunes: 6,
		CJKCharsPerTok: 1,
		VocabFileNames: []string{"llama3.tokenizer.json"},
	},
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// ResolveProfile automatically selects the internal tokenizer profile for a (provider, model) pair.
// Fails closed (ok == false) when neither model nor provider maps to a known family.
func ResolveProfile(provider, model string) (Profile, bool) {
	nm := pricing.NormalizeModel(model)
	canon := strings.TrimSuffix(nm.Canonical, ":free")

	// 1. Model-first resolution (critical for multi-provider aggregators like OpenRouter/LiteLLM).
	if canon != "" {
		switch {
		// OpenAI: cl100k_base (GPT-3.5 / legacy GPT-4 / embeddings) vs o200k_base (GPT-4o/4.1/5/6/o1/o3/o4)
		case canon == "gpt-4" || hasAnyPrefix(canon, "gpt-4-turbo", "gpt-4-0", "gpt-4-32k", "gpt-3.5", "text-embedding-"):
			return profiles[FamilyOpenAICL100K], true
		case hasAnyPrefix(canon, "gpt-", "o1", "o3", "o4", "chatgpt-", "codex"):
			return profiles[FamilyOpenAIO200K], true

		// Anthropic / Claude
		case hasAnyPrefix(canon, "claude-", "claude"):
			return profiles[FamilyAnthropicClaude], true

		// Google / Gemini / Gemma
		case hasAnyPrefix(canon, "gemini-", "gemma-"):
			return profiles[FamilyGoogleGemini], true

		// Mistral: legacy v0.1/v0.2/v0.3/Mixtral/Large-2402/2407/Codestral-22B -> SentencePiece; modern -> Tekken
		case hasAnyPrefix(canon,
			"mistral-7b", "open-mistral-7b", "mixtral", "open-mixtral",
			"mistral-large-2402", "mistral-large-2407", "mistral-small-2402", "codestral-22b",
		):
			return profiles[FamilyMistralSentencePce], true
		case hasAnyPrefix(canon, "mistral", "ministral", "codestral", "devstral", "pixtral", "magistral"):
			return profiles[FamilyMistralTekken], true

		// MiniMax
		case hasAnyPrefix(canon, "minimax", "abab"):
			return profiles[FamilyMiniMax], true

		// Z.AI GLM / ChatGLM
		case hasAnyPrefix(canon, "glm-", "chatglm"):
			return profiles[FamilyZAIGLM], true

		// DeepSeek
		case hasAnyPrefix(canon, "deepseek"):
			return profiles[FamilyDeepSeek], true

		// Qwen / QwQ
		case hasAnyPrefix(canon, "qwen", "qwq"):
			return profiles[FamilyQwen], true

		// Kimi / Moonshot
		case hasAnyPrefix(canon, "kimi", "moonshot"):
			return profiles[FamilyKimi], true

		// Llama / Grok / Muse / Hermes / Nemotron / Solar
		case hasAnyPrefix(canon, "llama", "grok", "muse-", "hermes", "nemotron", "solar-", "step-", "longcat"):
			return profiles[FamilyLlamaGrok], true
		}
	}

	// 2. Provider-based fallback (using prefix hint first, then declared provider).
	for _, rawProv := range []string{nm.ProviderHint, provider} {
		p := pricing.NormalizeProvider(rawProv)
		switch p {
		case "openai":
			return profiles[FamilyOpenAIO200K], true
		case "anthropic":
			return profiles[FamilyAnthropicClaude], true
		case "google":
			return profiles[FamilyGoogleGemini], true
		case "mistral":
			return profiles[FamilyMistralTekken], true
		case "minimax":
			return profiles[FamilyMiniMax], true
		case "zai":
			return profiles[FamilyZAIGLM], true
		case "deepseek":
			return profiles[FamilyDeepSeek], true
		case "alibaba":
			return profiles[FamilyQwen], true
		case "moonshot":
			return profiles[FamilyKimi], true
		case "meta", "xai", "nous", "nvidia", "upstage":
			return profiles[FamilyLlamaGrok], true
		}
	}

	return Profile{}, false
}
