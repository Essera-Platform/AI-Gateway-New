// Essera fork: OpenAI embeddings for the Bedrock embedding models (Titan Text
// Embeddings G1 and v2, Titan Multimodal Embeddings, Cohere Embed v3 and v4, TwelveLabs
// Marengo Embed 3.0)
// through InvokeModel with each model's native body.
package aws

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type embeddingFamily int

const (
	embeddingTitanTextV2 embeddingFamily = iota + 1
	embeddingTitanTextG1
	embeddingTitanMultimodal
	embeddingCohere
	embeddingCohereV4
	embeddingMarengo
)

const (
	// maxEmbeddingInputs bounds the inputs of one request; models that embed
	// one input per call make one InvokeModel call for each.
	maxEmbeddingInputs = 256
	// cohereBatchSize is the most texts Cohere Embed takes in one call.
	cohereBatchSize = 96
	// titanTextG1Dimensions is the only vector length Titan Text Embeddings G1 makes.
	titanTextG1Dimensions = 1536
)

func embeddingFamilyOf(modelID string) embeddingFamily {
	switch {
	case strings.Contains(modelID, "titan-embed-image"):
		return embeddingTitanMultimodal
	case strings.Contains(modelID, "titan-embed-text-v2"):
		return embeddingTitanTextV2
	case strings.Contains(modelID, "titan-embed-text-v1"), strings.Contains(modelID, "titan-embed-g1-text"):
		return embeddingTitanTextG1
	case strings.Contains(modelID, "twelvelabs.marengo-embed-3"):
		return embeddingMarengo
	case strings.Contains(modelID, "cohere.embed-v4"):
		return embeddingCohereV4
	case strings.Contains(modelID, "cohere.embed"):
		return embeddingCohere
	}
	return 0
}

// embeddingCall is one InvokeModel call: its native body and the positions of
// the inputs it embeds.
type embeddingCall struct {
	Inputs []int           `json:"inputs"`
	Body   json.RawMessage `json:"body"`
	Image  bool            `json:"image,omitempty"`
}

// embeddingPlan is the request the embedding relay forwards to DoRequest.
type embeddingPlan struct {
	Calls []embeddingCall `json:"calls"`
}

// embeddingImage returns the base64 data of an input given as an image data URL.
func embeddingImage(input string) (string, bool) {
	rest, found := strings.CutPrefix(input, "data:image/")
	if !found {
		return "", false
	}
	_, data, found := strings.Cut(rest, ";base64,")
	return data, found
}

// convertEmbeddingRequest plans the calls for an OpenAI embedding request.
// An input that is an image data URL embeds the image on the multimodal
// models.
func convertEmbeddingRequest(modelID string, request dto.EmbeddingRequest) (*embeddingPlan, error) {
	family := embeddingFamilyOf(modelID)
	if family == 0 {
		return nil, newBadRequest("model %s does not produce embeddings on AWS Bedrock", modelID)
	}
	inputs := request.ParseInput()
	if len(inputs) == 0 {
		return nil, newBadRequest("input must be a string or an array of strings")
	}
	if len(inputs) > maxEmbeddingInputs {
		return nil, newBadRequest("input may hold at most %d items for AWS Bedrock", maxEmbeddingInputs)
	}
	if family == embeddingMarengo {
		return marengoEmbeddingPlan(modelID, inputs, request)
	}
	plan := &embeddingPlan{}
	addCall := func(positions []int, body any) error {
		raw, err := common.Marshal(body)
		if err != nil {
			return err
		}
		plan.Calls = append(plan.Calls, embeddingCall{Inputs: positions, Body: raw})
		return nil
	}

	if family == embeddingCohere || family == embeddingCohereV4 {
		var texts []int
		for i, input := range inputs {
			if _, isImage := embeddingImage(input); isImage {
				body := map[string]any{"images": []string{input}, "input_type": "image", "embedding_types": []string{"float"}}
				if family == embeddingCohereV4 && request.Dimensions != nil {
					body["output_dimension"] = *request.Dimensions
				}
				if err := addCall([]int{i}, body); err != nil {
					return nil, err
				}
				plan.Calls[len(plan.Calls)-1].Image = true
				continue
			}
			texts = append(texts, i)
		}
		for start := 0; start < len(texts); start += cohereBatchSize {
			positions := texts[start:min(start+cohereBatchSize, len(texts))]
			batch := make([]string, len(positions))
			for j, position := range positions {
				batch[j] = inputs[position]
			}
			body := map[string]any{"texts": batch, "input_type": "search_document", "embedding_types": []string{"float"}}
			if family == embeddingCohereV4 && request.Dimensions != nil {
				body["output_dimension"] = *request.Dimensions
			}
			if err := addCall(positions, body); err != nil {
				return nil, err
			}
		}
		return plan, nil
	}

	for i, input := range inputs {
		image, isImage := embeddingImage(input)
		if isImage && family != embeddingTitanMultimodal {
			return nil, newBadRequest("model %s embeds text only", modelID)
		}
		var body map[string]any
		switch family {
		case embeddingTitanTextV2:
			body = map[string]any{"inputText": input}
			if request.Dimensions != nil {
				body["dimensions"] = *request.Dimensions
			}
		case embeddingTitanTextG1:
			if request.Dimensions != nil && *request.Dimensions != titanTextG1Dimensions {
				return nil, newBadRequest("model %s embeds at %d dimensions only", modelID, titanTextG1Dimensions)
			}
			body = map[string]any{"inputText": input}
		case embeddingTitanMultimodal:
			if isImage {
				body = map[string]any{"inputImage": image}
			} else {
				body = map[string]any{"inputText": input}
			}
			if request.Dimensions != nil {
				body["embeddingConfig"] = map[string]any{"outputEmbeddingLength": *request.Dimensions}
			}
		}
		if err := addCall([]int{i}, body); err != nil {
			return nil, err
		}
		plan.Calls[len(plan.Calls)-1].Image = isImage
	}
	return plan, nil
}

// imagesPricedApart counts the images of the plan Bedrock prices per image:
// Titan Multimodal's, Cohere Embed v3's and Marengo's. Cohere Embed v4 prices
// an image by its tokens.
func imagesPricedApart(family embeddingFamily, plan *embeddingPlan) int64 {
	if plan == nil || (family != embeddingTitanMultimodal && family != embeddingCohere && family != embeddingMarengo) {
		return 0
	}
	var images int64
	for _, call := range plan.Calls {
		if call.Image {
			images += int64(len(call.Inputs))
		}
	}
	return images
}

// embeddingVectors reads the vectors of one native embedding answer, in the
// order of the call's inputs, and the input tokens its body reports.
func embeddingVectors(family embeddingFamily, body []byte) ([][]float64, int, error) {
	switch family {
	case embeddingMarengo:
		vectors, err := marengoVectors(body)
		return vectors, 0, err
	case embeddingCohere, embeddingCohereV4:
		var native struct {
			Embeddings json.RawMessage `json:"embeddings"`
		}
		if err := common.Unmarshal(body, &native); err != nil {
			return nil, 0, err
		}
		var byType struct {
			Float [][]float64 `json:"float"`
		}
		if err := common.Unmarshal(native.Embeddings, &byType); err == nil && byType.Float != nil {
			return byType.Float, 0, nil
		}
		var vectors [][]float64
		if err := common.Unmarshal(native.Embeddings, &vectors); err != nil {
			return nil, 0, err
		}
		return vectors, 0, nil
	}
	var native struct {
		Embedding           []float64 `json:"embedding"`
		InputTextTokenCount int       `json:"inputTextTokenCount"`
	}
	if err := common.Unmarshal(body, &native); err != nil {
		return nil, 0, err
	}
	return [][]float64{native.Embedding}, native.InputTextTokenCount, nil
}

// embeddingItem is an OpenAI embedding: floats, or with encoding_format
// "base64" the little-endian float32 bytes OpenAI's SDKs decode.
type embeddingItem struct {
	Object    string `json:"object"`
	Index     int    `json:"index"`
	Embedding any    `json:"embedding"`
}

type embeddingResponse struct {
	Object string          `json:"object"`
	Data   []embeddingItem `json:"data"`
	Model  string          `json:"model"`
	Usage  dto.Usage       `json:"usage"`
}

func encodeEmbedding(vector []float64, encodingFormat string) any {
	if encodingFormat != "base64" {
		return vector
	}
	buffer := make([]byte, 4*len(vector))
	for i, value := range vector {
		binary.LittleEndian.PutUint32(buffer[4*i:], math.Float32bits(float32(value)))
	}
	return base64.StdEncoding.EncodeToString(buffer)
}

// embeddingResponseFromReplies assembles the OpenAI response from the answers
// to the plan's calls. Input tokens come from Bedrock's token-count header, or
// the body's own count where the header is missing.
func embeddingResponseFromReplies(model string, family embeddingFamily, plan *embeddingPlan, replies []bedrockReply, encodingFormat string) (*embeddingResponse, *types.NewAPIError) {
	inputs := 0
	for _, call := range plan.Calls {
		inputs += len(call.Inputs)
	}
	response := &embeddingResponse{Object: "list", Model: model, Data: make([]embeddingItem, inputs)}
	tokens := 0
	for i, call := range plan.Calls {
		vectors, bodyTokens, err := embeddingVectors(family, replies[i].body)
		if err == nil && len(vectors) != len(call.Inputs) {
			err = fmt.Errorf("%d embeddings for %d inputs", len(vectors), len(call.Inputs))
		}
		if err != nil {
			return nil, types.NewOpenAIError(fmt.Errorf("decode embedding response: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		for j, position := range call.Inputs {
			response.Data[position] = embeddingItem{Object: "embedding", Index: position, Embedding: encodeEmbedding(vectors[j], encodingFormat)}
		}
		callTokens := invokeInputTokens(replies[i].header)
		if callTokens == 0 && bodyTokens > 0 && bodyTokens <= maxBedrockTokenCount {
			callTokens = bodyTokens
		}
		tokens = min(tokens+callTokens, maxBedrockTokenCount)
	}
	response.Usage = dto.Usage{PromptTokens: tokens, TotalTokens: tokens}
	return response, nil
}
