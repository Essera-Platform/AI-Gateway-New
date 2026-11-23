// Package apidocs is the OpenAPI document of the API a managed Gateway serves its customers,
// shown on the web UI's API Docs page. The document is written by hand in openapi.yaml; its
// Simplified Chinese version swaps in the translations of openapi.zh.yaml, keyed by the English text.
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

//go:embed openapi.zh.yaml
var zhSource []byte

const (
	English = "en"
	Chinese = "zh"
)

// Language is the document language a request asks for: Chinese for "zh" and its Simplified
// variants, else English.
func Language(requested string) string {
	switch strings.ToLower(strings.ReplaceAll(requested, "_", "-")) {
	case "zh", "zh-cn", "zh-hans", "zhcn":
		return Chinese
	}
	return English
}

// The document names a placeholder address and example models; Spec puts in the Gateway's
// address and models the reader may call.
const (
	placeholderServer     = "https://ai-gateway.example.com"
	ExampleChatModel      = "anthropic.claude-sonnet-4-6"
	ExampleEmbeddingModel = "amazon.titan-embed-text-v2:0"
)

// documents are openapi.yaml as JSON per language, in the order it is written: the viewer shows
// fields and operations in that order.
var documents = map[string]func() (string, error){
	English: sync.OnceValues(func() (string, error) { return render(nil) }),
	Chinese: sync.OnceValues(func() (string, error) {
		translations, err := chineseTranslations()
		if err != nil {
			return "", err
		}
		return render(translations)
	}),
}

func render(translations map[string]string) (string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(source, &root); err != nil {
		return "", err
	}
	if translations != nil {
		visitText(&root, func(text *yaml.Node) {
			if translated, ok := translations[text.Value]; ok {
				text.Value = translated
			}
		})
	}
	var out strings.Builder
	err := writeJSON(&out, &root)
	return out.String(), err
}

func chineseTranslations() (map[string]string, error) {
	var pairs []struct {
		EN string `yaml:"en"`
		ZH string `yaml:"zh"`
	}
	if err := yaml.Unmarshal(zhSource, &pairs); err != nil {
		return nil, err
	}
	translations := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		translations[pair.EN] = pair.ZH
	}
	return translations, nil
}

// visitText calls visit on each piece of the document a reader reads as prose: titles,
// summaries, descriptions and tag names. Example payloads, code samples and identifiers are
// left alone.
func visitText(node *yaml.Node, visit func(*yaml.Node)) {
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			visitText(child, visit)
		}
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i].Value, node.Content[i+1]
			switch {
			case key == "example" || key == "value" || key == "default" || key == "enum" || key == "const":
			case key == "tags" && value.Kind == yaml.SequenceNode:
				for _, tag := range value.Content {
					if tag.Kind == yaml.ScalarNode {
						visit(tag)
						continue
					}
					visitText(tag, visit)
					for j := 0; j+1 < len(tag.Content); j += 2 {
						if tag.Content[j].Value == "name" {
							visit(tag.Content[j+1])
						}
					}
				}
			case (key == "title" || key == "summary" || key == "description") && value.Kind == yaml.ScalarNode:
				visit(value)
			default:
				visitText(value, visit)
			}
		}
	}
}

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

// Spec is the OpenAPI document in language (see Language), as JSON, for a gateway called at
// serverAddress, with examples that name models the reader may call (exampleModels). With none,
// the examples keep theirs.
func Spec(serverAddress string, available []string, language string) ([]byte, error) {
	spec, err := documents[Language(language)]()
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
// call them, else the first chat and the first embedding model they may call. Nova Sonic
// speaks only through the Realtime API, so it never names a chat example.
func exampleModels(available []string) (chat, embedding string) {
	chat, embedding = ExampleChatModel, ExampleEmbeddingModel
	if len(available) == 0 {
		return chat, embedding
	}
	sorted := slices.Sorted(slices.Values(available))
	if !slices.Contains(sorted, ExampleChatModel) {
		if i := slices.IndexFunc(sorted, func(model string) bool { return !isEmbeddingModel(model) && !strings.Contains(model, "sonic") }); i >= 0 {
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
