package apidocs

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/managed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type openAPIDocument struct {
	OpenAPI string `json:"openapi"`
	Servers []struct {
		URL string `json:"url"`
	} `json:"servers"`
	Paths map[string]map[string]any `json:"paths"`
}

func decode(t *testing.T, spec []byte) openAPIDocument {
	t.Helper()
	var document openAPIDocument
	require.NoError(t, common.Unmarshal(spec, &document), "the document is JSON")
	return document
}

func TestTheDocumentNamesTheGatewaysAddressEverywhere(t *testing.T) {
	spec, err := Spec("https://ai-gateway-dev.esseratech.com/", nil, English)
	require.NoError(t, err)

	document := decode(t, spec)
	assert.Equal(t, "3.1.0", document.OpenAPI)
	require.Len(t, document.Servers, 1)
	assert.Equal(t, "https://ai-gateway-dev.esseratech.com", document.Servers[0].URL)
	assert.NotContains(t, string(spec), placeholderServer, "code samples and the introduction name the real address too")
	assert.Contains(t, string(spec), `base_url=\"https://ai-gateway-dev.esseratech.com/v1\"`)
}

func TestTheDocumentDescribesOnlyTheCustomerRoutes(t *testing.T) {
	spec, err := Spec("https://gateway.example.com", nil, English)
	require.NoError(t, err)

	document := decode(t, spec)
	var operations []string
	for path, methods := range document.Paths {
		for method := range methods {
			operations = append(operations, strings.ToUpper(method)+" "+path)
		}
	}
	assert.ElementsMatch(t, []string{
		"GET /v1/models", "POST /v1/chat/completions", "POST /v1/responses", "POST /v1/messages", "POST /v1/embeddings",
	}, operations)
	t.Setenv("ESSERA_MANAGED", "true")
	for _, operation := range operations {
		method, path, _ := strings.Cut(operation, " ")
		assert.True(t, managed.CustomerRequestAllowed(method, path), "%s is served to API keys", operation)
	}
	assert.False(t, managed.CustomerRequestAllowed(http.MethodGet, "/v1/responses"))
}

func TestTheExamplesNameModelsTheReaderMayCall(t *testing.T) {
	tests := []struct {
		name        string
		available   []string
		chat, embed string
	}{
		{name: "nobody's models: the examples keep theirs", available: nil, chat: ExampleChatModel, embed: ExampleEmbeddingModel},
		{name: "the example models when allowed", available: []string{"amazon.nova-lite-v1:0", ExampleChatModel, ExampleEmbeddingModel}, chat: ExampleChatModel, embed: ExampleEmbeddingModel},
		{name: "else the first allowed of each kind", available: []string{"cohere.embed-v4:0", "mistral.ministral-3-8b-instruct", "amazon.nova-lite-v1:0"}, chat: "amazon.nova-lite-v1:0", embed: "cohere.embed-v4:0"},
		{name: "no embedding model allowed", available: []string{"eu.amazon.nova-lite-v1:0"}, chat: "eu.amazon.nova-lite-v1:0", embed: ExampleEmbeddingModel},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec, err := Spec("https://gateway.example.com", test.available, English)
			require.NoError(t, err)
			decode(t, spec)
			assert.Contains(t, string(spec), `model=\"`+test.chat+`\"`)
			assert.Contains(t, string(spec), `\"model\": \"`+test.embed+`\"`)
			if test.chat != ExampleChatModel {
				assert.NotContains(t, string(spec), ExampleChatModel)
			}
		})
	}
}

func TestEveryTextHasAChineseTranslation(t *testing.T) {
	translations, err := chineseTranslations()
	require.NoError(t, err)
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(source, &root))

	inEnglish := map[string]bool{}
	visitText(&root, func(text *yaml.Node) { inEnglish[text.Value] = true })
	require.NotEmpty(t, inEnglish)
	untouched := regexp.MustCompile("(?s)```.*?```|`[^`\n]+`")
	for english := range inEnglish {
		chinese, ok := translations[english]
		if !assert.True(t, ok, "no Chinese for %q", english) {
			continue
		}
		assert.NotEmpty(t, chinese)
		assert.NotEqual(t, english, chinese)
		for _, code := range untouched.FindAllString(english, -1) {
			assert.Contains(t, chinese, code, "the Chinese of %.40q keeps its code as it is", english)
		}
	}
	for english := range translations {
		assert.True(t, inEnglish[english], "the Chinese of %.60q translates a text openapi.yaml no longer has", english)
	}
}

func TestTheChineseDocumentIsTheSameAPIInChinese(t *testing.T) {
	english, err := Spec("https://gateway.example.com", []string{"amazon.nova-lite-v1:0"}, "")
	require.NoError(t, err)
	chinese, err := Spec("https://gateway.example.com", []string{"amazon.nova-lite-v1:0"}, "zh")
	require.NoError(t, err)

	var en, zh struct {
		openAPIDocument
		Info struct {
			Title string `json:"title"`
		} `json:"info"`
	}
	require.NoError(t, common.Unmarshal(english, &en))
	require.NoError(t, common.Unmarshal(chinese, &zh))
	assert.Equal(t, "Essera AI Gateway API", en.Info.Title)
	assert.Equal(t, "Essera AI 网关 API", zh.Info.Title)
	assert.Equal(t, en.Servers, zh.Servers)
	assert.Equal(t, len(en.Paths), len(zh.Paths))
	for path := range en.Paths {
		assert.Contains(t, zh.Paths, path)
	}
	assert.Contains(t, string(chinese), `model=\"amazon.nova-lite-v1:0\"`, "code samples name the reader's models")
	assert.Contains(t, string(chinese), `"tags":["模型"]`)
}

func TestLanguage(t *testing.T) {
	for requested, want := range map[string]string{
		"": English, "en": English, "zh": Chinese, "zh-CN": Chinese, "zhCN": Chinese, "zh_Hans": Chinese, "zh-TW": English, "fr": English,
	} {
		assert.Equal(t, want, Language(requested), requested)
	}
}
