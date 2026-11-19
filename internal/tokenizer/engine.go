package tokenizer

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// MaxTextBytes caps per-record fallback text tokenization to 2 MiB to bound memory and CPU.
const MaxTextBytes = 2 * 1024 * 1024

// Result represents the internal outcome of fallback visible-text tokenization.
type Result struct {
	Status string     // "ok", "unsupported", "text_limit"
	Tokens *int       // nil when Status != "ok"
	Family Family     // internal family selected (never exposed on public API)
	Engine EngineKind // internal engine kind (never exposed on public API)
}

// Counter manages hidden pure-Go tokenizers across all supported model families.
// Zero CGo, zero network access at runtime, thread-safe, and bounded to MaxTextBytes.
type Counter struct {
	cacheDir string

	mu       sync.RWMutex
	encoders map[Family]Encoder
}

// NewCounter constructs a hidden pure-Go tokenizer Counter.
// If cacheDir is non-empty and contains local vocabulary files, they are loaded lazily on demand.
func NewCounter(cacheDir string) *Counter {
	return &Counter{
		cacheDir: strings.TrimSpace(cacheDir),
		encoders: make(map[Family]Encoder),
	}
}

// RegisterEncoder installs a custom vocabulary-backed Encoder for a family (used by local cache or tests).
func (c *Counter) RegisterEncoder(family Family, enc Encoder) {
	if c == nil || enc == nil {
		return
	}
	c.mu.Lock()
	c.encoders[family] = enc
	c.mu.Unlock()
}

// Count silently selects the tokenizer for (provider, model) and counts tokens in text.
// Fails closed with Status="unsupported" for unknown families and Status="text_limit" above 2 MiB.
func (c *Counter) Count(provider, model, text string) Result {
	if len(text) > MaxTextBytes {
		return Result{Status: "text_limit"}
	}
	prof, ok := ResolveProfile(provider, model)
	if !ok {
		return Result{Status: "unsupported"}
	}
	enc := c.encoderFor(prof)
	count := enc.CountTokens(prof, text)
	return Result{
		Status: "ok",
		Tokens: &count,
		Family: prof.Family,
		Engine: prof.Engine,
	}
}

func (c *Counter) encoderFor(prof Profile) Encoder {
	if c == nil {
		return builtinEncoder{}
	}
	c.mu.RLock()
	enc, exists := c.encoders[prof.Family]
	c.mu.RUnlock()
	if exists && enc != nil {
		return enc
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if enc, exists := c.encoders[prof.Family]; exists && enc != nil {
		return enc
	}

	var loaded Encoder
	if c.cacheDir != "" {
		for _, fname := range prof.VocabFileNames {
			full := filepath.Join(c.cacheDir, fname)
			info, err := os.Lstat(full)
			if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				continue
			}
			switch {
			case strings.HasSuffix(fname, ".tiktoken"):
				if t, err := LoadTiktokenBPE(full); err == nil {
					loaded = t
				}
			case strings.HasSuffix(fname, ".json"):
				if t, err := LoadHuggingFaceJSON(full); err == nil {
					loaded = t
				}
			case strings.HasSuffix(fname, ".model"):
				if prof.SentencePiece {
					if sp, err := LoadSentencePieceModel(full); err == nil {
						loaded = sp
					}
				} else {
					if t, err := LoadTiktokenBPE(full); err == nil {
						loaded = t
					}
				}
			}
			if loaded != nil {
				break
			}
		}
	}
	if loaded == nil {
		loaded = builtinEncoder{}
	}
	c.encoders[prof.Family] = loaded
	return loaded
}
