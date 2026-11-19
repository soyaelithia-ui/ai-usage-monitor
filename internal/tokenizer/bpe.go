package tokenizer

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Encoder encodes text into an integer token count using a real vocabulary table
// (tiktoken BPE, HuggingFace BPE, or SentencePiece Unigram) or the built-in calibrated encoder.
type Encoder interface {
	CountTokens(profile Profile, text string) int
}

// BPETable implements exact lowest-rank Byte-Pair Encoding merges in pure Go.
type BPETable struct {
	ranks      map[string]int
	byteMapper [256]rune // optional GPT-2 reversible byte-to-unicode mapping for HF tokenizer.json
	useHFBytes bool
}

// LoadTiktokenBPE parses a standard tiktoken BPE vocabulary file (`<base64-token> <rank>`).
func LoadTiktokenBPE(path string) (*BPETable, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	ranks := make(map[string]int, 16384)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		rawTok, err := base64.StdEncoding.DecodeString(fields[0])
		if err != nil {
			continue
		}
		rank, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		ranks[string(rawTok)] = rank
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(ranks) == 0 {
		return nil, errors.New("empty tiktoken vocabulary")
	}
	return &BPETable{ranks: ranks}, nil
}

func buildGPT2ByteToUnicode() [256]rune {
	var m [256]rune
	bs := make([]bool, 256)
	for b := int('!'); b <= int('~'); b++ {
		bs[b] = true
		m[b] = rune(b)
	}
	for b := 0xA1; b <= 0xAC; b++ {
		bs[b] = true
		m[b] = rune(b)
	}
	for b := 0xAE; b <= 0xFF; b++ {
		bs[b] = true
		m[b] = rune(b)
	}
	n := 0
	for b := 0; b < 256; b++ {
		if !bs[b] {
			m[b] = rune(256 + n)
			n++
		}
	}
	return m
}

// LoadHuggingFaceJSON parses a HuggingFace `tokenizer.json` BPE model in pure Go.
func LoadHuggingFaceJSON(path string) (*BPETable, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Model struct {
			Type   string         `json:"type"`
			Vocab  map[string]int `json:"vocab"`
			Merges []any          `json:"merges"`
		} `json:"model"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if len(doc.Model.Vocab) == 0 {
		return nil, errors.New("empty huggingface vocab")
	}
	ranks := make(map[string]int, len(doc.Model.Vocab)+len(doc.Model.Merges))
	for tok, id := range doc.Model.Vocab {
		ranks[tok] = id
	}
	// Merge order defines BPE merge priority when merges are present.
	if len(doc.Model.Merges) > 0 {
		mergeRanks := make(map[string]int, len(doc.Model.Merges))
		for idx, item := range doc.Model.Merges {
			switch x := item.(type) {
			case string:
				parts := strings.SplitN(x, " ", 2)
				if len(parts) == 2 {
					mergeRanks[parts[0]+parts[1]] = idx
				}
			case []any:
				if len(x) == 2 {
					a, okA := x[0].(string)
					b, okB := x[1].(string)
					if okA && okB {
						mergeRanks[a+b] = idx
					}
				}
			}
		}
		if len(mergeRanks) > 0 {
			for k, r := range mergeRanks {
				ranks[k] = r
			}
		}
	}
	return &BPETable{
		ranks:      ranks,
		byteMapper: buildGPT2ByteToUnicode(),
		useHFBytes: true,
	}, nil
}

func (b *BPETable) mapPiece(piece string) []string {
	if !b.useHFBytes {
		out := make([]string, len(piece))
		for i := 0; i < len(piece); i++ {
			out[i] = piece[i : i+1]
		}
		return out
	}
	out := make([]string, len(piece))
	for i := 0; i < len(piece); i++ {
		out[i] = string(b.byteMapper[piece[i]])
	}
	return out
}

func (b *BPETable) encodePiece(piece string) int {
	if piece == "" {
		return 0
	}
	if !b.useHFBytes {
		if _, ok := b.ranks[piece]; ok {
			return 1
		}
	}
	parts := b.mapPiece(piece)
	if len(parts) <= 1 {
		return len(parts)
	}
	for len(parts) > 1 {
		bestIdx := -1
		bestRank := math.MaxInt
		for i := 0; i < len(parts)-1; i++ {
			merged := parts[i] + parts[i+1]
			if rank, ok := b.ranks[merged]; ok && rank < bestRank {
				bestRank = rank
				bestIdx = i
			}
		}
		if bestIdx < 0 {
			break
		}
		parts[bestIdx] = parts[bestIdx] + parts[bestIdx+1]
		parts = append(parts[:bestIdx+1], parts[bestIdx+2:]...)
	}
	return len(parts)
}

// CountTokens pre-tokenizes text according to profile and applies exact BPE merges.
func (b *BPETable) CountTokens(profile Profile, text string) int {
	pieces := PreTokenize(profile, text)
	total := 0
	for _, piece := range pieces {
		total += b.encodePiece(piece)
	}
	return total
}

// SPModel implements pure-Go SentencePiece Unigram Viterbi tokenization from a .model protobuf.
type SPModel struct {
	scores   map[string]float32
	maxPiece int
}

// LoadSentencePieceModel parses a SentencePiece ModelProto protobuf file without CGo or protoc.
func LoadSentencePieceModel(path string) (*SPModel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSentencePieceModelBytes(data)
}

// ParseSentencePieceModelBytes decodes SentencePiece ModelProto wire bytes (field 1: repeated SentencePiece).
func ParseSentencePieceModelBytes(data []byte) (*SPModel, error) {
	scores := make(map[string]float32, 4096)
	maxLen := 0
	i := 0
	for i < len(data) {
		tag, n := binary.Uvarint(data[i:])
		if n <= 0 {
			return nil, errors.New("invalid protobuf varint")
		}
		i += n
		fieldNum := tag >> 3
		wireType := tag & 0x7

		switch wireType {
		case 0: // varint
			_, vn := binary.Uvarint(data[i:])
			if vn <= 0 {
				return nil, errors.New("invalid varint value")
			}
			i += vn
		case 1: // 64-bit
			if i+8 > len(data) {
				return nil, errors.New("truncated 64-bit field")
			}
			i += 8
		case 2: // length-delimited
			l, ln := binary.Uvarint(data[i:])
			if ln <= 0 || i+ln+int(l) > len(data) {
				return nil, errors.New("truncated length-delimited field")
			}
			i += ln
			sub := data[i : i+int(l)]
			i += int(l)
			if fieldNum == 1 {
				piece, score, ptype, ok := decodeSPPiece(sub)
				if ok && piece != "" && (ptype == 1 || ptype == 6) {
					scores[piece] = score
					if len(piece) > maxLen {
						maxLen = len(piece)
					}
				}
			}
		case 5: // 32-bit
			if i+4 > len(data) {
				return nil, errors.New("truncated 32-bit field")
			}
			i += 4
		default:
			return nil, fmt.Errorf("unsupported protobuf wire type %d", wireType)
		}
	}
	if len(scores) == 0 {
		return nil, errors.New("no sentencepiece entries decoded")
	}
	return &SPModel{scores: scores, maxPiece: maxLen}, nil
}

func decodeSPPiece(buf []byte) (piece string, score float32, ptype uint64, ok bool) {
	ptype = 1 // NORMAL default in SentencePiece proto
	i := 0
	for i < len(buf) {
		tag, n := binary.Uvarint(buf[i:])
		if n <= 0 {
			return "", 0, 0, false
		}
		i += n
		fieldNum := tag >> 3
		wireType := tag & 0x7
		switch wireType {
		case 0:
			v, vn := binary.Uvarint(buf[i:])
			if vn <= 0 {
				return "", 0, 0, false
			}
			i += vn
			if fieldNum == 3 {
				ptype = v
			}
		case 1:
			if i+8 > len(buf) {
				return "", 0, 0, false
			}
			i += 8
		case 2:
			l, ln := binary.Uvarint(buf[i:])
			if ln <= 0 || i+ln+int(l) > len(buf) {
				return "", 0, 0, false
			}
			i += ln
			if fieldNum == 1 {
				piece = string(buf[i : i+int(l)])
			}
			i += int(l)
		case 5:
			if i+4 > len(buf) {
				return "", 0, 0, false
			}
			if fieldNum == 2 {
				score = math.Float32frombits(binary.LittleEndian.Uint32(buf[i : i+4]))
			}
			i += 4
		default:
			return "", 0, 0, false
		}
	}
	return piece, score, ptype, piece != ""
}

// CountTokens runs SentencePiece metaspace normalization + Viterbi Unigram DP with byte fallback.
func (sp *SPModel) CountTokens(_ Profile, text string) int {
	if text == "" {
		return 0
	}
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
	s := b.String()
	n := len(s)

	bestScore := make([]float64, n+1)
	bestCount := make([]int, n+1)
	for i := 1; i <= n; i++ {
		bestScore[i] = math.Inf(-1)
	}

	const unknownBytePenalty = -100.0
	for i := 0; i < n; i++ {
		if math.IsInf(bestScore[i], -1) {
			continue
		}
		limit := i + sp.maxPiece
		if limit > n {
			limit = n
		}
		matched := false
		for j := i + 1; j <= limit; j++ {
			if sc, ok := sp.scores[s[i:j]]; ok {
				cand := bestScore[i] + float64(sc)
				if cand > bestScore[j] {
					bestScore[j] = cand
					bestCount[j] = bestCount[i] + 1
				}
				matched = true
			}
		}
		// Byte fallback (<0xXX>) or single UTF-8 rune fallback
		_, rsz := utf8.DecodeRuneInString(s[i:])
		if !matched || bestScore[i+rsz] < bestScore[i]+unknownBytePenalty {
			nextIdx := i + rsz
			byteToks := rsz
			if byteToks < 1 {
				byteToks = 1
			}
			cand := bestScore[i] + unknownBytePenalty*float64(byteToks)
			if cand > bestScore[nextIdx] {
				bestScore[nextIdx] = cand
				bestCount[nextIdx] = bestCount[i] + byteToks
			}
		}
	}
	return bestCount[n]
}

// builtinEncoder provides zero-dependency deterministic subword counting calibrated per family
// when external vocabulary files are not installed on disk.
type builtinEncoder struct{}

func (builtinEncoder) CountTokens(profile Profile, text string) int {
	pieces := PreTokenize(profile, text)
	if len(pieces) == 0 {
		return 0
	}
	wordSpan := profile.WordChunkRunes
	if wordSpan < 4 {
		wordSpan = 5
	}
	cjkSpan := profile.CJKCharsPerTok
	if cjkSpan < 1 {
		cjkSpan = 1
	}

	total := 0
	for _, piece := range pieces {
		if piece == "" {
			continue
		}
		rCount := utf8.RuneCountInString(piece)
		firstR, firstSz := utf8.DecodeRuneInString(piece)
		trimmed := piece
		if firstR == ' ' || firstR == '\u2581' {
			trimmed = piece[firstSz:]
		}
		if trimmed == "" {
			// Whitespace / indentation run (compressed in large BPE vocabs; ~4 spaces per token)
			if rCount <= 4 {
				total++
			} else {
				total += (rCount + 3) / 4
			}
			continue
		}

		trFirst, _ := utf8.DecodeRuneInString(trimmed)
		trRunes := utf8.RuneCountInString(trimmed)

		switch {
		case unicode.IsNumber(trFirst):
			// Pre-tokenizer already split by MaxDigitGroup (1 or 3 digits per piece)
			total++
		case isHanRune(trFirst):
			// CJK / Han density depends on vocabulary coverage (1 char/tok vs ~2 chars/tok)
			total += (trRunes + cjkSpan - 1) / cjkSpan
		case isWordRune(trFirst):
			if trRunes <= wordSpan {
				total++
			} else {
				// Long identifiers/words split into subword stems + morphemes
				rem := trRunes - wordSpan
				step := wordSpan - 1
				if step < 3 {
					step = 3
				}
				total += 1 + (rem+step-1)/step
			}
		case len(trimmed) > trRunes*2:
			// Multi-byte symbols/emoji fall back to UTF-8 byte tokens (~2 bytes per token in BPE, 1-3 in SP)
			bytes := len(trimmed)
			if profile.SentencePiece && profile.VocabSize < 64000 {
				total += bytes
			} else {
				total += (bytes + 1) / 2
			}
		default:
			// Punctuation / operator runs
			if trRunes <= 2 {
				total++
			} else {
				total += (trRunes + 1) / 2
			}
		}
	}
	return total
}
