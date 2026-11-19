package tokenizer

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const metaspace = "\u2581"

func isNewline(r rune) bool {
	return r == '\r' || r == '\n'
}

func isHorizSpace(r rune) bool {
	return unicode.IsSpace(r) && !isNewline(r)
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsMark(r)
}

func isHanRune(r rune) bool {
	return unicode.Is(unicode.Han, r)
}

func matchEnglishContraction(s string) int {
	if len(s) < 2 || s[0] != '\'' {
		return 0
	}
	if len(s) >= 3 {
		two := strings.ToLower(s[1:3])
		if two == "re" || two == "ve" || two == "ll" {
			return 3
		}
	}
	one := strings.ToLower(s[1:2])
	if one == "s" || one == "t" || one == "m" || one == "d" {
		return 2
	}
	return 0
}

// PreTokenize segments text according to the family profile's pre-tokenization contract.
func PreTokenize(p Profile, text string) []string {
	if text == "" {
		return nil
	}
	if p.SentencePiece {
		return splitSentencePiece(text, p.MaxDigitGroup == 1)
	}
	maxDigits := p.MaxDigitGroup
	if maxDigits <= 0 {
		maxDigits = 3
	}
	return splitTiktokenStyle(text, maxDigits, p.SplitHanRuns)
}

// splitTiktokenStyle implements a pure-Go Unicode scanner matching tiktoken / GPT-4o / Llama-3 /
// Claude / DeepSeek / Qwen / Kimi / GLM / Tekken pre-tokenization without RE2 lookahead limits.
func splitTiktokenStyle(text string, maxDigits int, splitHan bool) []string {
	out := make([]string, 0, len(text)/4+4)
	i := 0
	n := len(text)

	for i < n {
		// 1. English contractions ('s, 't, 're, 've, 'm, 'll, 'd)
		if clen := matchEnglishContraction(text[i:]); clen > 0 {
			out = append(out, text[i:i+clen])
			i += clen
			continue
		}

		r, sz := utf8.DecodeRuneInString(text[i:])

		// 2. Han runs when isolated (Kimi, Qwen, DeepSeek, GLM, MiniMax)
		if splitHan && isHanRune(r) {
			j := i + sz
			for j < n {
				nr, nsz := utf8.DecodeRuneInString(text[j:])
				if !isHanRune(nr) {
					break
				}
				j += nsz
			}
			out = append(out, text[i:j])
			i = j
			continue
		}

		// 3. Digits grouped up to maxDigits (1 or 3)
		if unicode.IsNumber(r) {
			j := i + sz
			count := 1
			for j < n && count < maxDigits {
				nr, nsz := utf8.DecodeRuneInString(text[j:])
				if !unicode.IsNumber(nr) {
					break
				}
				j += nsz
				count++
			}
			out = append(out, text[i:j])
			i = j
			continue
		}

		// 4. Optional single leading non-newline non-alnum char followed by letters (e.g. " hello")
		if !isNewline(r) && !isWordRune(r) && !unicode.IsNumber(r) && i+sz < n {
			nextR, nextSz := utf8.DecodeRuneInString(text[i+sz:])
			if isWordRune(nextR) && !(splitHan && isHanRune(nextR)) {
				j := i + sz + nextSz
				for j < n {
					nr, nsz := utf8.DecodeRuneInString(text[j:])
					if !isWordRune(nr) || (splitHan && isHanRune(nr)) {
						break
					}
					j += nsz
				}
				out = append(out, text[i:j])
				i = j
				continue
			}
		}

		// 5. Bare letter run
		if isWordRune(r) {
			j := i + sz
			for j < n {
				nr, nsz := utf8.DecodeRuneInString(text[j:])
				if !isWordRune(nr) || (splitHan && isHanRune(nr)) {
					break
				}
				j += nsz
			}
			out = append(out, text[i:j])
			i = j
			continue
		}

		// 6. Whitespace & newline handling with \s+(?!\S) lookahead
		if unicode.IsSpace(r) {
			j := i + sz
			hasNL := isNewline(r)
			for j < n {
				nr, nsz := utf8.DecodeRuneInString(text[j:])
				if !unicode.IsSpace(nr) {
					break
				}
				if isNewline(nr) {
					hasNL = true
					j += nsz
					continue
				}
				if hasNL {
					break
				}
				j += nsz
			}
			if hasNL {
				out = append(out, text[i:j])
				i = j
				continue
			}
			// If followed by non-space and run length > 1, leave the last space to attach to the next token.
			if j < n {
				lastR, lastSz := utf8.DecodeLastRuneInString(text[i:j])
				if isHorizSpace(lastR) && j-lastSz > i {
					out = append(out, text[i:j-lastSz])
					i = j - lastSz
					continue
				}
			}
			out = append(out, text[i:j])
			i = j
			continue
		}

		// 7. Punctuation / symbol run (with optional trailing newlines)
		j := i + sz
		for j < n {
			nr, nsz := utf8.DecodeRuneInString(text[j:])
			if unicode.IsSpace(nr) || isWordRune(nr) || unicode.IsNumber(nr) {
				break
			}
			j += nsz
		}
		for j < n {
			nr, nsz := utf8.DecodeRuneInString(text[j:])
			if !isNewline(nr) {
				break
			}
			j += nsz
		}
		out = append(out, text[i:j])
		i = j
	}
	return out
}

// splitSentencePiece normalizes leading/inter-word spaces into SentencePiece metaspace (▁)
// and segments digits and script boundaries (used by Google Gemini/Gemma and Mistral SP).
func splitSentencePiece(text string, splitDigits bool) []string {
	var b strings.Builder
	b.Grow(len(text) + 16)
	b.WriteString(metaspace)
	for _, r := range text {
		if r == ' ' {
			b.WriteString(metaspace)
		} else {
			b.WriteRune(r)
		}
	}
	norm := b.String()

	out := make([]string, 0, len(norm)/4+4)
	i := 0
	n := len(norm)
	for i < n {
		r, sz := utf8.DecodeRuneInString(norm[i:])

		// Single digit split (Gemini/Gemma & Mistral SP)
		if splitDigits && unicode.IsNumber(r) {
			out = append(out, norm[i:i+sz])
			i += sz
			continue
		}

		// Newline / tab control tokens
		if isNewline(r) || r == '\t' {
			out = append(out, norm[i:i+sz])
			i += sz
			continue
		}

		// Metaspace followed by a word or Han run
		if r == '\u2581' {
			j := i + sz
			for j < n {
				nr, nsz := utf8.DecodeRuneInString(norm[j:])
				if nr != '\u2581' {
					break
				}
				j += nsz
			}
			// If single metaspace followed by letters, attach metaspace to the word
			if j-i == sz && j < n {
				nr, nsz := utf8.DecodeRuneInString(norm[j:])
				if isWordRune(nr) && !isHanRune(nr) {
					k := j + nsz
					for k < n {
						kr, ksz := utf8.DecodeRuneInString(norm[k:])
						if !isWordRune(kr) || isHanRune(kr) {
							break
						}
						k += ksz
					}
					out = append(out, norm[i:k])
					i = k
					continue
				}
			}
			out = append(out, norm[i:j])
			i = j
			continue
		}

		// Han run
		if isHanRune(r) {
			j := i + sz
			for j < n {
				nr, nsz := utf8.DecodeRuneInString(norm[j:])
				if !isHanRune(nr) {
					break
				}
				j += nsz
			}
			out = append(out, norm[i:j])
			i = j
			continue
		}

		// Non-Han letter run
		if isWordRune(r) {
			j := i + sz
			for j < n {
				nr, nsz := utf8.DecodeRuneInString(norm[j:])
				if !isWordRune(nr) || isHanRune(nr) {
					break
				}
				j += nsz
			}
			out = append(out, norm[i:j])
			i = j
			continue
		}

		// Punctuation / symbols (single rune or short repeated run)
		j := i + sz
		for j < n && j-i < 4 {
			nr, nsz := utf8.DecodeRuneInString(norm[j:])
			if nr != r {
				break
			}
			j += nsz
		}
		out = append(out, norm[i:j])
		i = j
	}
	return out
}
