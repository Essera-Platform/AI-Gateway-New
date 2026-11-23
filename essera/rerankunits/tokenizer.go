// Package rerankunits counts the search units AWS bills for a Cohere Rerank 3.5 request on
// Bedrock, which Bedrock does not report. AWS splits each document, together with the query,
// into chunks of 500 tokens of Cohere's rerank tokenizer, and bills one search unit per 100
// chunks of a request, at least one (checked against CloudWatch SearchUnits).
//
// The tokenizer reproduces Hugging Face tokenizers' encoding of Cohere's tokenizer file
// (rerank-v3.5.json): added tokens split out of the raw text, then per piece Strip,
// SentencePiece's precompiled normalizer and the collapse of runs of spaces, Metaspace
// pre-tokenization and a Unigram model. Only the number of tokens is computed.
package rerankunits

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

const (
	metaspace = "▁"
	// unknownPenalty is what Unigram scores an unknown character below the lowest piece.
	unknownPenalty = 10.0
)

// Tokenizer counts the tokens of a text, without the special tokens an encoding adds.
type Tokenizer struct {
	added      []string
	charsmap   []uint32
	normalized string
	ids        map[string]int
	scores     []float64
	unknownID  int
	minScore   float64
	longest    int
}

type tokenizerFile struct {
	AddedTokens []struct {
		Content    string `json:"content"`
		SingleWord bool   `json:"single_word"`
		Lstrip     bool   `json:"lstrip"`
		Rstrip     bool   `json:"rstrip"`
		Normalized bool   `json:"normalized"`
	} `json:"added_tokens"`
	Normalizer struct {
		Type        string `json:"type"`
		Normalizers []struct {
			Type                string `json:"type"`
			StripLeft           bool   `json:"strip_left"`
			StripRight          bool   `json:"strip_right"`
			PrecompiledCharsmap string `json:"precompiled_charsmap"`
			Pattern             struct {
				Regex string `json:"Regex"`
			} `json:"pattern"`
			Content string `json:"content"`
		} `json:"normalizers"`
	} `json:"normalizer"`
	PreTokenizer struct {
		Type          string `json:"type"`
		Pretokenizers []struct {
			Type           string `json:"type"`
			Replacement    string `json:"replacement"`
			AddPrefixSpace *bool  `json:"add_prefix_space"`
			PrependScheme  string `json:"prepend_scheme"`
			Split          *bool  `json:"split"`
		} `json:"pretokenizers"`
	} `json:"pre_tokenizer"`
	Model struct {
		Type         string   `json:"type"`
		UnkID        *int     `json:"unk_id"`
		ByteFallback bool     `json:"byte_fallback"`
		Vocab        [][2]any `json:"vocab"`
	} `json:"model"`
}

// Parse reads a tokenizer file. It accepts only the pipeline it reproduces, so another
// tokenizer is refused rather than counted wrongly.
func Parse(data []byte) (*Tokenizer, error) {
	var file tokenizerFile
	if err := common.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("read rerank tokenizer: %w", err)
	}
	normalizers := file.Normalizer.Normalizers
	if file.Normalizer.Type != "Sequence" || len(normalizers) != 3 ||
		normalizers[0].Type != "Strip" || !normalizers[0].StripLeft || !normalizers[0].StripRight ||
		normalizers[1].Type != "Precompiled" ||
		normalizers[2].Type != "Replace" || normalizers[2].Pattern.Regex != " {2,}" || normalizers[2].Content != " " {
		return nil, errors.New("rerank tokenizer: unexpected normalizer")
	}
	pre := file.PreTokenizer.Pretokenizers
	if file.PreTokenizer.Type != "Sequence" || len(pre) != 1 || pre[0].Type != "Metaspace" || pre[0].Replacement != metaspace ||
		(pre[0].Split != nil && !*pre[0].Split) ||
		!(pre[0].PrependScheme == "always" || (pre[0].PrependScheme == "" && pre[0].AddPrefixSpace != nil && *pre[0].AddPrefixSpace)) {
		return nil, errors.New("rerank tokenizer: unexpected pre-tokenizer")
	}
	if file.Model.Type != "Unigram" || file.Model.ByteFallback || file.Model.UnkID == nil || len(file.Model.Vocab) == 0 {
		return nil, errors.New("rerank tokenizer: unexpected model")
	}
	tokenizer := &Tokenizer{
		ids:       make(map[string]int, len(file.Model.Vocab)),
		scores:    make([]float64, len(file.Model.Vocab)),
		unknownID: *file.Model.UnkID,
		minScore:  0,
	}
	for _, added := range file.AddedTokens {
		if added.Normalized || added.SingleWord || added.Lstrip || added.Rstrip || added.Content == "" {
			return nil, errors.New("rerank tokenizer: unexpected added token")
		}
		tokenizer.added = append(tokenizer.added, added.Content)
	}
	for id, entry := range file.Model.Vocab {
		piece, isText := entry[0].(string)
		score, isNumber := entry[1].(float64)
		if !isText || !isNumber {
			return nil, errors.New("rerank tokenizer: unexpected vocabulary entry")
		}
		tokenizer.ids[piece] = id
		tokenizer.scores[id] = score
		if id == 0 || score < tokenizer.minScore {
			tokenizer.minScore = score
		}
		tokenizer.longest = max(tokenizer.longest, len(piece))
	}
	if tokenizer.unknownID < 0 || tokenizer.unknownID >= len(tokenizer.scores) {
		return nil, errors.New("rerank tokenizer: unknown token out of range")
	}
	charsmap, err := base64.StdEncoding.DecodeString(normalizers[1].PrecompiledCharsmap)
	if err != nil || len(charsmap) < 4 {
		return nil, errors.New("rerank tokenizer: unreadable precompiled charsmap")
	}
	trieBytes := int(binary.LittleEndian.Uint32(charsmap))
	if trieBytes%4 != 0 || 4+trieBytes > len(charsmap) {
		return nil, errors.New("rerank tokenizer: unreadable precompiled charsmap")
	}
	tokenizer.charsmap = make([]uint32, trieBytes/4)
	for i := range tokenizer.charsmap {
		tokenizer.charsmap[i] = binary.LittleEndian.Uint32(charsmap[4+4*i:])
	}
	tokenizer.normalized = string(charsmap[4+trieBytes:])
	if !utf8.ValidString(tokenizer.normalized) {
		return nil, errors.New("rerank tokenizer: unreadable precompiled charsmap")
	}
	return tokenizer, nil
}

// Count is the number of tokens of text, without special tokens.
func (t *Tokenizer) Count(text string) int {
	count := 0
	for text != "" {
		at, added := t.nextAdded(text)
		count += t.countPiece(text[:at])
		if added == "" {
			break
		}
		count++
		text = text[at+len(added):]
	}
	return count
}

// nextAdded finds the leftmost added token, the longest at that position.
func (t *Tokenizer) nextAdded(text string) (int, string) {
	at, found := len(text), ""
	for _, added := range t.added {
		index := strings.Index(text, added)
		if index >= 0 && (index < at || (index == at && len(added) > len(found))) {
			at, found = index, added
		}
	}
	return at, found
}

func (t *Tokenizer) countPiece(piece string) int {
	normalized := collapseSpaces(t.precompiled(strings.TrimFunc(piece, unicode.IsSpace)))
	if normalized == "" {
		return 0
	}
	text := strings.ReplaceAll(normalized, " ", metaspace)
	if !strings.HasPrefix(text, metaspace) {
		text = metaspace + text
	}
	count := 0
	for _, word := range metaspaceWords(text) {
		count += t.unigramCount(word)
	}
	return count
}

// metaspaceWords splits before every run of the replacement character: Metaspace's split
// with the delimiter merged with what follows.
func metaspaceWords(text string) []string {
	var words []string
	start, previousDelimiter := 0, false
	for at := 0; at < len(text); {
		isDelimiter := strings.HasPrefix(text[at:], metaspace)
		if isDelimiter && !previousDelimiter && at > start {
			words = append(words, text[start:at])
			start = at
		}
		previousDelimiter = isDelimiter
		if isDelimiter {
			at += len(metaspace)
		} else {
			_, size := utf8.DecodeRuneInString(text[at:])
			at += size
		}
	}
	return append(words, text[start:])
}

func collapseSpaces(text string) string {
	if !strings.Contains(text, "  ") {
		return text
	}
	var builder strings.Builder
	previousSpace := false
	for i := 0; i < len(text); i++ {
		if text[i] == ' ' && previousSpace {
			continue
		}
		previousSpace = text[i] == ' '
		builder.WriteByte(text[i])
	}
	return builder.String()
}

// precompiled applies SentencePiece's precompiled charsmap as Hugging Face does: a grapheme
// cluster shorter than 6 bytes is replaced as a whole when the charsmap has its start, else
// each character is replaced on its own.
func (t *Tokenizer) precompiled(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))
	graphemes(text, func(cluster string) {
		if len(cluster) < 6 {
			if replacement, ok := t.transform(cluster); ok {
				builder.WriteString(replacement)
				return
			}
		}
		for _, r := range cluster {
			character := string(r)
			if replacement, ok := t.transform(character); ok {
				builder.WriteString(replacement)
			} else {
				builder.WriteString(character)
			}
		}
	})
	return builder.String()
}

// transform is spm_precompiled's: the replacement of the shortest prefix of chunk the
// charsmap holds, up to its terminating zero byte.
func (t *Tokenizer) transform(chunk string) (string, bool) {
	array := t.charsmap
	at := func(position uint32) (uint32, bool) {
		if int(position) >= len(array) {
			return 0, false
		}
		return array[position], true
	}
	offset := func(unit uint32) uint32 { return (unit >> 10) << ((unit & (1 << 9)) >> 6) }
	unit, ok := at(0)
	if !ok {
		return "", false
	}
	position := offset(unit)
	for i := 0; i < len(chunk); i++ {
		c := chunk[i]
		if c == 0 {
			break
		}
		position ^= uint32(c)
		if unit, ok = at(position); !ok || unit&((1<<31)|0xFF) != uint32(c) {
			return "", false
		}
		position ^= offset(unit)
		if (unit>>8)&1 == 1 {
			leaf, ok := at(position)
			if !ok {
				return "", false
			}
			start := int(leaf & ((1 << 31) - 1))
			if start > len(t.normalized) {
				return "", false
			}
			end := strings.IndexByte(t.normalized[start:], 0)
			if end < 0 {
				return t.normalized[start:], true
			}
			return t.normalized[start : start+end], true
		}
	}
	return "", false
}

// unigramCount is the number of pieces of Unigram's best segmentation of word, consecutive
// unknown characters fused into one.
func (t *Tokenizer) unigramCount(word string) int {
	type node struct {
		score  float64
		start  int
		id     int
		filled bool
	}
	unknownScore := t.minScore - unknownPenalty
	best := make([]node, len(word)+1)
	for start := 0; start < len(word); {
		here := best[start].score
		_, width := utf8.DecodeRuneInString(word[start:])
		single := false
		for end := start + 1; end <= min(len(word), start+t.longest); end++ {
			id, known := t.ids[word[start:end]]
			if !known {
				continue
			}
			candidate := t.scores[id] + here
			if target := &best[end]; !target.filled || candidate > target.score {
				*target = node{score: candidate, start: start, id: id, filled: true}
			}
			if end-start == width {
				single = true
			}
		}
		if !single {
			candidate := unknownScore + here
			if target := &best[start+width]; !target.filled || candidate > target.score {
				*target = node{score: candidate, start: start, id: t.unknownID, filled: true}
			}
		}
		start += width
	}
	count, inUnknown := 0, false
	for end := len(word); end > 0; end = best[end].start {
		isUnknown := best[end].id == t.unknownID
		if !isUnknown || !inUnknown {
			count++
		}
		inUnknown = isUnknown
	}
	return count
}
