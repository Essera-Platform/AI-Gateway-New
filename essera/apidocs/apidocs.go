// Package apidocs is the OpenAPI document of the API a managed Gateway serves its customers,
// shown on the web UI's API Docs page. The document is written by hand in openapi.yaml.
package apidocs

import (
	_ "embed"
	"slices"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"gopkg.in/yaml.v3"
)

//go:embed openapi.yaml
var source []byte

// The document names a placeholder address and example models; Spec puts in the Gateway's
// address and models the reader may call.
const (
	placeholderServer     = "https://ai-gateway.example.com"
	ExampleChatModel      = "anthropic.claude-sonnet-4-6"
	ExampleEmbeddingModel = "amazon.titan-embed-text-v2:0"
)

// document is openapi.yaml as JSON, in the order it is written: the viewer shows fields and
// operations in that order.
var document = sync.OnceValues(func() (string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(source, &root); err != nil {
		return "", err
	}
	var out strings.Builder
	err := writeJSON(&out, &root)
	return out.String(), err
})

func writeJSON(out *strings.Builder, node *yaml.Node) error {
	switch node.Kind {
	case yaml.DocumentNode:
		return writeJSON(out, node.Content[0])
	case yaml.AliasNode:
		return writeJSON(out, node.Alias)
	case yaml.MappingNode:
		out.WriteByte('{')
		for i := 0; i < len(node.Content); i += 2 {
			if i > 0 {
				out.WriteByte(',')
			}
			key, err := common.Marshal(node.Content[i].Value)
			if err != nil {
				return err
			}
			out.Write(key)
			out.WriteByte(':')
			if err := writeJSON(out, node.Content[i+1]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	case yaml.SequenceNode:
		out.WriteByte('[')
		for i, item := range node.Content {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeJSON(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	default:
		var value any
		if err := node.Decode(&value); err != nil {
			return err
		}
		encoded, err := common.Marshal(value)
		if err != nil {
			return err
		}
		out.Write(encoded)
	}
	return nil
}

// Spec is the OpenAPI document, as JSON, for a gateway called at serverAddress, with examples
// that name models the reader may call (exampleModels). With none, the examples keep theirs.
func Spec(serverAddress string, available []string) ([]byte, error) {
	spec, err := document()
	if err != nil {
		return nil, err
	}
	chat, embedding := exampleModels(available)
	replacements := []string{placeholderServer, strings.TrimRight(serverAddress, "/")}
	if chat != ExampleChatModel {
		replacements = append(replacements, ExampleChatModel, chat)
	}
	if embedding != ExampleEmbeddingModel {
		replacements = append(replacements, ExampleEmbeddingModel, embedding)
	}
	for i := 1; i < len(replacements); i += 2 {
		replacements[i] = jsonStringContent(replacements[i])
	}
	return []byte(strings.NewReplacer(replacements...).Replace(spec)), nil
}

// exampleModels picks the models the examples name: the example models when the reader may
// call them, else the first chat and the first embedding model they may call.
func exampleModels(available []string) (chat, embedding string) {
	chat, embedding = ExampleChatModel, ExampleEmbeddingModel
	if len(available) == 0 {
		return chat, embedding
	}
	sorted := slices.Sorted(slices.Values(available))
	if !slices.Contains(sorted, ExampleChatModel) {
		if i := slices.IndexFunc(sorted, func(model string) bool { return !isEmbeddingModel(model) }); i >= 0 {
			chat = sorted[i]
		}
	}
	if !slices.Contains(sorted, ExampleEmbeddingModel) {
		if i := slices.IndexFunc(sorted, isEmbeddingModel); i >= 0 {
			embedding = sorted[i]
		}
	}
	return chat, embedding
}

func isEmbeddingModel(model string) bool {
	return strings.Contains(model, "embed")
}

// jsonStringContent escapes a value for use inside a JSON string of the document.
func jsonStringContent(value string) string {
	encoded, err := common.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded[1 : len(encoded)-1])
}
