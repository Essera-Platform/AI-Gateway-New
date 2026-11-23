package rerankunits

import (
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTokenizer is Cohere's tokenizer file, which is not in the repository: its path is in
// ESSERA_RERANK_TOKENIZER_FILE.
func testTokenizer(t *testing.T) *Tokenizer {
	t.Helper()
	path := os.Getenv("ESSERA_RERANK_TOKENIZER_FILE")
	if path == "" {
		t.Skip("ESSERA_RERANK_TOKENIZER_FILE names no tokenizer file")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	tokenizer, err := Parse(data)
	require.NoError(t, err)
	return tokenizer
}

// testdata/counts.json holds Hugging Face tokenizers' counts for a varied corpus: English,
// Chinese, Japanese, Korean, Russian, Croatian, Hindi, Arabic, Thai, code, emoji sequences,
// combining marks, compatibility characters, whitespace runs, the added tokens written out,
// control characters, very long texts and random mixes of all of them.
func TestCountsMatchHuggingFaceTokenizers(t *testing.T) {
	tokenizer := testTokenizer(t)
	raw, err := os.ReadFile("testdata/counts.json")
	require.NoError(t, err)
	var corpus []struct {
		Text   string `json:"text"`
		Tokens int    `json:"tokens"`
	}
	require.NoError(t, common.Unmarshal(raw, &corpus))
	mismatches := 0
	for i, entry := range corpus {
		if got := tokenizer.Count(entry.Text); got != entry.Tokens {
			mismatches++
			if mismatches <= 20 {
				t.Errorf("text %d %.80q: %d tokens, Hugging Face %d", i, entry.Text, got, entry.Tokens)
			}
		}
	}
	assert.Zero(t, mismatches, "of %d texts", len(corpus))
}

// testdata/tiny-tokenizer.json has the pipeline of Cohere's file and a vocabulary in which
// each word "w" is one token and anything else is unknown.
func tinyTokenizer(t *testing.T) *Tokenizer {
	t.Helper()
	data, err := os.ReadFile("testdata/tiny-tokenizer.json")
	require.NoError(t, err)
	tokenizer, err := Parse(data)
	require.NoError(t, err)
	return tokenizer
}

func words(n int) string {
	return strings.TrimSpace(strings.Repeat("w ", n))
}

// The chunking that AWS's CloudWatch SearchUnits showed for Cohere Rerank 3.5: a document
// with the query is split every 500 tokens, and 100 chunks are one search unit.
func TestSearchUnits(t *testing.T) {
	tokenizer := tinyTokenizer(t)
	repeat := func(document string, n int) []string {
		documents := make([]string, n)
		for i := range documents {
			documents[i] = document
		}
		return documents
	}
	query := words(3)
	tests := []struct {
		name      string
		documents []string
		want      int64
	}{
		{"51 documents of 500 tokens with the query: 51 chunks", repeat(words(497), 51), 1},
		{"51 documents of 501 tokens with the query: 102 chunks", repeat(words(498), 51), 2},
		{"34 documents of 1000 tokens with the query: 68 chunks", repeat(words(997), 34), 1},
		{"34 documents of 1001 tokens with the query: 102 chunks", repeat(words(998), 34), 2},
		{"100 short documents", repeat("w", 100), 1},
		{"101 short documents", repeat("w", 101), 2},
		{"an empty document and query still bill one unit", []string{""}, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			q := query
			if test.name == "an empty document and query still bill one unit" {
				q = ""
			}
			assert.Equal(t, test.want, tokenizer.SearchUnits(q, test.documents))
		})
	}
}

func TestCountsAddedTokensUnknownsAndSpaces(t *testing.T) {
	tokenizer := tinyTokenizer(t)
	assert.Equal(t, 0, tokenizer.Count("   \t\n"))
	assert.Equal(t, 3, tokenizer.Count("  w    w w  "), "runs of spaces are one")
	assert.Equal(t, 3, tokenizer.Count("w[SEP]w"), "an added token written out is one token")
	assert.Equal(t, 2, tokenizer.Count("xyz"), "the word piece, then the unknown characters fused")
}

func TestAnotherPipelineIsRefused(t *testing.T) {
	data, err := os.ReadFile("testdata/tiny-tokenizer.json")
	require.NoError(t, err)
	_, err = Parse([]byte(strings.Replace(string(data), `"Unigram"`, `"BPE"`, 1)))
	assert.Error(t, err)
	_, err = Parse([]byte(strings.Replace(string(data), `" {2,}"`, `"\\s+"`, 1)))
	assert.Error(t, err)
}
