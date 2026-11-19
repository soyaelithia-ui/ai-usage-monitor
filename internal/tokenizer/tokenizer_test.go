package tokenizer

import (
	"encoding/base64"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoSelectionAllModelFamilies(t *testing.T) {
	cases := []struct {
		provider   string
		model      string
		wantFamily Family
		wantEngine EngineKind
	}{
		{"openai", "gpt-6.2-astra", FamilyOpenAIO200K, EngineBPE},
		{"openai", "openai/gpt-6-sol-2026-10-15", FamilyOpenAIO200K, EngineBPE},
		{"openai", "gpt-4-turbo", FamilyOpenAICL100K, EngineBPE},
		{"openai", "text-embedding-3-small", FamilyOpenAICL100K, EngineBPE},
		{"anthropic", "claude-sonnet-4.6", FamilyAnthropicClaude, EngineBPE},
		{"openrouter", "openrouter/anthropic/claude-opus-5-5", FamilyAnthropicClaude, EngineBPE},
		{"bedrock", "us.anthropic.claude-haiku-5-5-v1:0", FamilyAnthropicClaude, EngineBPE},
		{"google", "gemini-4-carbon", FamilyGoogleGemini, EngineSentencePiece},
		{"vertex", "vertex_ai/google/gemini-3.8-flash-preview", FamilyGoogleGemini, EngineSentencePiece},
		{"mistral", "mistral-large-3", FamilyMistralTekken, EngineTekken},
		{"mistral", "codestral", FamilyMistralTekken, EngineTekken},
		{"mistral", "mixtral-8x22b-instruct", FamilyMistralSentencePce, EngineSentencePiece},
		{"mistral", "mistral-7b-v0.3", FamilyMistralSentencePce, EngineSentencePiece},
		{"minimax", "minimax-m3", FamilyMiniMax, EngineBPE},
		{"minimax", "minimax-m3.1-flash", FamilyMiniMax, EngineBPE},
		{"z-ai", "glm-5.3", FamilyZAIGLM, EngineBPE},
		{"zai", "openrouter/z-ai/glm-4.7-flash:free", FamilyZAIGLM, EngineBPE},
		{"deepseek", "deepseek-v4-pro", FamilyDeepSeek, EngineBPE},
		{"deepseek", "deepseek-flash", FamilyDeepSeek, EngineBPE},
		{"alibaba", "qwen3.5-plus", FamilyQwen, EngineBPE},
		{"qwen", "qwq-32b", FamilyQwen, EngineBPE},
		{"moonshot", "kimi-k3.1-pro", FamilyKimi, EngineBPE},
		{"moonshot", "kimi-k2.7-code", FamilyKimi, EngineBPE},
		{"meta", "meta-llama/llama-4-maverick", FamilyLlamaGrok, EngineBPE},
		{"x-ai", "grok-4.7", FamilyLlamaGrok, EngineBPE},
	}

	c := NewCounter("")
	sample := "Hola mundo! Hello world, 123456 — testing multi-family tokenization: 你好世界\n"

	for _, tc := range cases {
		prof, ok := ResolveProfile(tc.provider, tc.model)
		if !ok {
			t.Errorf("ResolveProfile(%q, %q) failed", tc.provider, tc.model)
			continue
		}
		if prof.Family != tc.wantFamily {
			t.Errorf("ResolveProfile(%q, %q).Family = %q, want %q", tc.provider, tc.model, prof.Family, tc.wantFamily)
		}
		if prof.Engine != tc.wantEngine {
			t.Errorf("ResolveProfile(%q, %q).Engine = %q, want %q", tc.provider, tc.model, prof.Engine, tc.wantEngine)
		}
		res := c.Count(tc.provider, tc.model, sample)
		if res.Status != "ok" || res.Tokens == nil || *res.Tokens <= 0 {
			t.Errorf("Count(%q, %q) = %+v, want status=ok and tokens>0", tc.provider, tc.model, res)
		}
	}
}

func TestFailClosedUnknownAndTextLimit(t *testing.T) {
	c := NewCounter("")

	// Unknown model + unknown provider must fail closed
	res := c.Count("acme-unknown", "mystery-model-9000", "some visible text")
	if res.Status != "unsupported" || res.Tokens != nil {
		t.Fatalf("expected fail-closed unsupported, got %+v", res)
	}

	// Text above 2 MiB must fail closed with text_limit
	huge := strings.Repeat("a", MaxTextBytes+1)
	resLimit := c.Count("openai", "gpt-6-astra", huge)
	if resLimit.Status != "text_limit" || resLimit.Tokens != nil {
		t.Fatalf("expected fail-closed text_limit, got %+v", resLimit)
	}

	// Empty text returns ok with 0 tokens
	resEmpty := c.Count("openai", "gpt-6-astra", "")
	if resEmpty.Status != "ok" || resEmpty.Tokens == nil || *resEmpty.Tokens != 0 {
		t.Fatalf("expected ok with 0 tokens for empty text, got %+v", resEmpty)
	}
}

func TestLoadTiktokenBPEAndHuggingFaceJSON(t *testing.T) {
	dir := t.TempDir()

	// 1. Real .tiktoken file (base64 token + rank)
	tiktokenPath := filepath.Join(dir, "o200k_base.tiktoken")
	var lines []string
	tokens := []string{"h", "e", "l", "o", " ", "w", "r", "d", "he", "ll", "hel", "hell", "hello", " w", " wo", " wor", " worl", " world"}
	for i, tok := range tokens {
		lines = append(lines, base64.StdEncoding.EncodeToString([]byte(tok))+" "+itoa(i))
	}
	if err := os.WriteFile(tiktokenPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write tiktoken: %v", err)
	}

	bpe, err := LoadTiktokenBPE(tiktokenPath)
	if err != nil {
		t.Fatalf("LoadTiktokenBPE: %v", err)
	}
	prof, _ := ResolveProfile("openai", "gpt-6-astra")
	if got := bpe.CountTokens(prof, "hello world"); got != 2 {
		t.Fatalf("tiktoken BPE count for 'hello world' = %d, want 2", got)
	}

	// Verify Counter lazily loads o200k_base.tiktoken from cacheDir
	c := NewCounter(dir)
	res := c.Count("openai", "gpt-6-astra", "hello world")
	if res.Status != "ok" || res.Tokens == nil || *res.Tokens != 2 {
		t.Fatalf("Counter lazy tiktoken load = %+v, want 2", res)
	}

	// 2. Real HuggingFace tokenizer.json (ByteLevel BPE with 'Ġ' space marker)
	hfPath := filepath.Join(dir, "qwen_tokenizer.json")
	hfJSON := `{
		"model": {
			"type": "BPE",
			"vocab": {
				"h": 0, "e": 1, "l": 2, "o": 3, "Ġ": 4, "w": 5, "r": 6, "d": 7,
				"he": 8, "ll": 9, "hel": 10, "hell": 11, "hello": 12,
				"Ġw": 13, "Ġwo": 14, "Ġwor": 15, "Ġworl": 16, "Ġworld": 17
			},
			"merges": [
				"h e", "l l", "he l", "hel l", "hell o",
				"Ġ w", "Ġw o", "Ġwo r", "Ġwor l", "Ġworl d"
			]
		}
	}`
	if err := os.WriteFile(hfPath, []byte(hfJSON), 0o600); err != nil {
		t.Fatalf("write hf json: %v", err)
	}
	hfBPE, err := LoadHuggingFaceJSON(hfPath)
	if err != nil {
		t.Fatalf("LoadHuggingFaceJSON: %v", err)
	}
	qProf, _ := ResolveProfile("alibaba", "qwen3.5-plus")
	if got := hfBPE.CountTokens(qProf, "hello world"); got != 2 {
		t.Fatalf("HF JSON BPE count for 'hello world' = %d, want 2", got)
	}
}

func appendProtoPiece(dst []byte, piece string, score float32) []byte {
	// Submessage SentencePiece: field 1 (string piece), field 2 (fixed32 float score), field 3 (varint type=1)
	var sub []byte
	sub = append(sub, (1<<3)|2)
	sub = binary.AppendUvarint(sub, uint64(len(piece)))
	sub = append(sub, piece...)
	sub = append(sub, (2<<3)|5)
	var fbuf [4]byte
	binary.LittleEndian.PutUint32(fbuf[:], math.Float32bits(score))
	sub = append(sub, fbuf[:]...)
	sub = append(sub, (3<<3)|0, 1)

	// Outer ModelProto field 1 (length-delimited)
	dst = append(dst, (1<<3)|2)
	dst = binary.AppendUvarint(dst, uint64(len(sub)))
	dst = append(dst, sub...)
	return dst
}

func TestParseAndLoadSentencePieceModelProtobuf(t *testing.T) {
	var raw []byte
	raw = appendProtoPiece(raw, "▁hello", -1.0)
	raw = appendProtoPiece(raw, "▁world", -1.2)
	raw = appendProtoPiece(raw, "▁", -3.0)
	raw = appendProtoPiece(raw, "h", -5.0)
	raw = appendProtoPiece(raw, "e", -5.0)
	raw = appendProtoPiece(raw, "l", -5.0)
	raw = appendProtoPiece(raw, "o", -5.0)
	raw = appendProtoPiece(raw, "w", -5.0)
	raw = appendProtoPiece(raw, "r", -5.0)
	raw = appendProtoPiece(raw, "d", -5.0)

	dir := t.TempDir()
	spPath := filepath.Join(dir, "gemini.model")
	if err := os.WriteFile(spPath, raw, 0o600); err != nil {
		t.Fatalf("write sp model: %v", err)
	}

	sp, err := LoadSentencePieceModel(spPath)
	if err != nil {
		t.Fatalf("LoadSentencePieceModel: %v", err)
	}
	gProf, _ := ResolveProfile("google", "gemini-4-carbon")
	if got := sp.CountTokens(gProf, "hello world"); got != 2 {
		t.Fatalf("SentencePiece Viterbi count for 'hello world' = %d, want 2", got)
	}

	// Verify Counter lazily loads gemini.model from cacheDir
	c := NewCounter(dir)
	res := c.Count("google", "gemini-4-carbon", "hello world")
	if res.Status != "ok" || res.Tokens == nil || *res.Tokens != 2 {
		t.Fatalf("Counter lazy SentencePiece load = %+v, want 2", res)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
