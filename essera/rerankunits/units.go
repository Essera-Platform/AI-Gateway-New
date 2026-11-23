package rerankunits

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// chunkTokens: AWS splits a document, with the query, into chunks of this many tokens.
	chunkTokens = 500
	// chunksPerSearchUnit: one search unit covers this many chunks of a request.
	chunksPerSearchUnit = 100

	// TokenizerURL is where Cohere publishes the rerank-v3.5 tokenizer. The file is fetched at
	// run time and never distributed with the Gateway; ESSERA_RERANK_TOKENIZER may name
	// another URL or a local file with the same content.
	TokenizerURL = "https://storage.googleapis.com/cohere-public/tokenizers/rerank-v3.5.json"
	// tokenizerSHA256 pins the file the counts were checked against: another file is refused,
	// since it may count differently.
	tokenizerSHA256   = "6f06fc3bbcbbc8f07be861b9a6a69e177247bc549b2bcbf0483e4dd98c06b6f6"
	maxTokenizerBytes = 64 << 20
	retryAfter        = time.Minute
)

// SearchUnits is what AWS bills for a rerank of documents against query.
func (t *Tokenizer) SearchUnits(query string, documents []string) int64 {
	queryTokens := t.Count(query)
	chunks := 0
	for _, document := range documents {
		chunks += (queryTokens + t.Count(document) + chunkTokens - 1) / chunkTokens
	}
	return int64(max(1, (chunks+chunksPerSearchUnit-1)/chunksPerSearchUnit))
}

var (
	loadMutex sync.Mutex
	loaded    *Tokenizer
	failedAt  time.Time
	failure   error
)

// Load returns the tokenizer, reading it the first time it is needed. A failed read is
// tried again a minute later; meanwhile the same error is returned.
func Load(ctx context.Context) (*Tokenizer, error) {
	loadMutex.Lock()
	defer loadMutex.Unlock()
	if loaded != nil {
		return loaded, nil
	}
	if failure != nil && time.Since(failedAt) < retryAfter {
		return nil, failure
	}
	tokenizer, err := read(context.WithoutCancel(ctx))
	if err != nil {
		failure, failedAt = err, time.Now()
		return nil, err
	}
	loaded, failure = tokenizer, nil
	return loaded, nil
}

// Install makes t the tokenizer Load returns, in place of Cohere's file.
func Install(t *Tokenizer) {
	loadMutex.Lock()
	defer loadMutex.Unlock()
	loaded, failure = t, nil
}

func read(ctx context.Context) (*Tokenizer, error) {
	source := strings.TrimSpace(os.Getenv("ESSERA_RERANK_TOKENIZER"))
	if source == "" {
		source = TokenizerURL
	}
	var data []byte
	var err error
	if strings.HasPrefix(source, "https://") {
		data, err = fetch(ctx, source)
	} else {
		data, err = os.ReadFile(source)
	}
	if err != nil {
		return nil, fmt.Errorf("read the rerank tokenizer: %w", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != tokenizerSHA256 {
		return nil, errors.New("the rerank tokenizer is not the file its counts were checked against")
	}
	return Parse(data)
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d", url, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxTokenizerBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxTokenizerBytes {
		return nil, errors.New("the rerank tokenizer is too large")
	}
	return data, nil
}
