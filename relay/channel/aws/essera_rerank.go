// Essera fork: rerank (/v1/rerank) for Cohere Rerank 3.5 on Bedrock, through
// InvokeModel with its native body. Bedrock reports no usage; a call is billed
// in search units as AWS defines them: one per 100 documents, at least one.
package aws

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

const (
	// maxRerankDocuments is the most documents Cohere Rerank takes in one call.
	maxRerankDocuments = 1000
	// rerankDocumentsPerSearchUnit: AWS bills one search unit per 100 documents
	// of a query. It also splits a document that with the query is over 512
	// tokens, which Bedrock does not report; the Console reconciles the search
	// units AWS counted (CloudWatch SearchUnits) with the ones reported.
	rerankDocumentsPerSearchUnit = 100
)

// rerankBody is Cohere Rerank's native InvokeModel body.
type rerankBody struct {
	Query      string   `json:"query"`
	Documents  []string `json:"documents"`
	TopN       *int     `json:"top_n,omitempty"`
	APIVersion int      `json:"api_version"`
}

// rerankPlan is the native body and what the answer needs from the request.
type rerankPlan struct {
	documents       []string
	returnDocuments bool
}

func isRerankModel(modelID string) bool {
	return strings.Contains(modelID, "cohere.rerank")
}

// rerankSearchUnits is what AWS bills for a query over n documents.
func rerankSearchUnits(documents int) int64 {
	return int64(max(1, (documents+rerankDocumentsPerSearchUnit-1)/rerankDocumentsPerSearchUnit))
}

func convertRerankRequest(request dto.RerankRequest) (*rerankBody, *rerankPlan, error) {
	if strings.TrimSpace(request.Query) == "" {
		return nil, nil, newBadRequest("query is required")
	}
	if len(request.Documents) == 0 || len(request.Documents) > maxRerankDocuments {
		return nil, nil, newBadRequest("documents must hold between 1 and %d items", maxRerankDocuments)
	}
	if request.TopN != nil && (*request.TopN < 1 || *request.TopN > len(request.Documents)) {
		return nil, nil, newBadRequest("top_n must be between 1 and the number of documents")
	}
	documents := make([]string, len(request.Documents))
	for i, document := range request.Documents {
		switch value := document.(type) {
		case string:
			documents[i] = value
		case map[string]any:
			text, ok := value["text"].(string)
			if !ok {
				return nil, nil, newBadRequest("documents[%d] must be a string or an object with a text string", i)
			}
			documents[i] = text
		default:
			return nil, nil, newBadRequest("documents[%d] must be a string or an object with a text string", i)
		}
	}
	body := &rerankBody{Query: request.Query, Documents: documents, TopN: request.TopN, APIVersion: 2}
	plan := &rerankPlan{documents: documents, returnDocuments: request.ReturnDocuments != nil && *request.ReturnDocuments}
	return body, plan, nil
}

// rerankResponseFromReply is the Jina/Cohere-style answer, best first.
func rerankResponseFromReply(plan *rerankPlan, reply bedrockReply) (*dto.RerankResponse, *types.NewAPIError) {
	var native struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := common.Unmarshal(reply.body, &native); err != nil {
		return nil, types.NewOpenAIError(fmt.Errorf("decode rerank response: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	response := &dto.RerankResponse{Results: make([]dto.RerankResponseResult, 0, len(native.Results))}
	for _, result := range native.Results {
		if result.Index < 0 || result.Index >= len(plan.documents) {
			return nil, types.NewOpenAIError(fmt.Errorf("rerank result index %d out of range", result.Index), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		item := dto.RerankResponseResult{Index: result.Index, RelevanceScore: result.RelevanceScore}
		if plan.returnDocuments {
			item.Document = dto.RerankDocument{Text: plan.documents[result.Index]}
		}
		response.Results = append(response.Results, item)
	}
	sort.SliceStable(response.Results, func(i, j int) bool {
		return response.Results[i].RelevanceScore > response.Results[j].RelevanceScore
	})
	return response, nil
}
