// Essera fork: OpenAI image edits (/v1/images/edits) for Stability AI's image
// services on Bedrock (upscale, inpaint, outpaint, erase, remove background,
// search and replace or recolor, control sketch or structure, style guide and
// style transfer), through InvokeModel with their native body. Each call makes
// one image, which AWS bills per image.
package aws

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// maxStabilityImageBytes bounds one input image; Bedrock's own limit on the
// pixels of each service is left to Bedrock.
const maxStabilityImageBytes = 16 << 20

// stabilityService is one Stability AI image service: the member its first
// image goes in, the member of a second image (style transfer's style), and
// the other members it takes besides output_format. Bedrock refuses a member
// a service does not take, so an OpenAI edit's prompt, a mask or a parameter
// the service has no use for is left out (fast upscale takes the image alone).
type stabilityService struct {
	image       string
	secondImage string
	takes       []string
}

var stabilityServices = map[string]stabilityService{
	"stable-fast-upscale":            {image: "image"},
	"stable-conservative-upscale":    {image: "image", takes: []string{"prompt", "negative_prompt", "seed", "creativity"}},
	"stable-creative-upscale":        {image: "image", takes: []string{"prompt", "negative_prompt", "seed", "creativity", "style_preset"}},
	"stable-image-inpaint":           {image: "image", takes: []string{"prompt", "mask", "negative_prompt", "grow_mask", "seed", "style_preset"}},
	"stable-outpaint":                {image: "image", takes: []string{"prompt", "left", "right", "up", "down", "creativity", "seed", "style_preset"}},
	"stable-image-erase-object":      {image: "image", takes: []string{"mask", "grow_mask", "seed"}},
	"stable-image-remove-background": {image: "image"},
	"stable-image-search-replace":    {image: "image", takes: []string{"prompt", "search_prompt", "negative_prompt", "grow_mask", "seed", "style_preset"}},
	"stable-image-search-recolor":    {image: "image", takes: []string{"prompt", "select_prompt", "negative_prompt", "grow_mask", "seed", "style_preset"}},
	"stable-image-control-sketch":    {image: "image", takes: []string{"prompt", "control_strength", "negative_prompt", "seed", "style_preset"}},
	"stable-image-control-structure": {image: "image", takes: []string{"prompt", "control_strength", "negative_prompt", "seed", "style_preset"}},
	"stable-image-style-guide":       {image: "image", takes: []string{"prompt", "negative_prompt", "aspect_ratio", "fidelity", "seed", "style_preset"}},
	"stable-style-transfer": {image: "init_image", secondImage: "style_image", takes: []string{
		"prompt", "negative_prompt", "seed", "composition_fidelity", "style_strength", "change_strength",
	}},
}

// stabilityParameters are the services' own request members a caller may send
// along, as form fields or JSON members, and whether each is a number.
var stabilityParameters = map[string]bool{
	"negative_prompt": false, "style_preset": false, "search_prompt": false, "select_prompt": false, "aspect_ratio": false,
	"seed": true, "grow_mask": true, "creativity": true, "left": true, "right": true, "up": true, "down": true,
	"control_strength": true, "fidelity": true, "composition_fidelity": true, "style_strength": true, "change_strength": true,
}

func stabilityServiceOf(modelID string) (stabilityService, bool) {
	_, name, found := strings.Cut(modelID, "stability.")
	if !found {
		return stabilityService{}, false
	}
	name, _, _ = strings.Cut(name, "-v1")
	service, ok := stabilityServices[name]
	return service, ok
}

// convertStabilityRequest builds the native body from an OpenAI image edit, as
// a multipart form or JSON.
func convertStabilityRequest(c *gin.Context, mode int, modelID string, request dto.ImageRequest) (map[string]any, error) {
	service, ok := stabilityServiceOf(modelID)
	if !ok {
		return nil, newBadRequest("model %s does not make images on AWS Bedrock", modelID)
	}
	if mode != relayconstant.RelayModeImagesEdits {
		return nil, newBadRequest("model %s edits an image: use /v1/images/edits", modelID)
	}
	if request.N != nil && *request.N > 1 {
		return nil, newBadRequest("model %s makes one image per request: n must be 1", modelID)
	}
	if request.ResponseFormat != "" && request.ResponseFormat != "b64_json" {
		return nil, newBadRequest("AWS Bedrock images are returned as b64_json only")
	}
	body := map[string]any{}
	if request.Prompt != "" && slices.Contains(service.takes, "prompt") {
		body["prompt"] = request.Prompt
	}
	var images []string
	var mask string
	parameters := map[string]string{}
	if form := c.Request.MultipartForm; form != nil {
		for _, field := range []string{"image", "image[]"} {
			for _, header := range form.File[field] {
				data, err := readFormImage(header)
				if err != nil {
					return nil, err
				}
				images = append(images, data)
			}
		}
		if headers := form.File["mask"]; len(headers) > 0 {
			data, err := readFormImage(headers[0])
			if err != nil {
				return nil, err
			}
			mask = data
		}
		for name := range stabilityParameters {
			if values := form.Value[name]; len(values) > 0 && strings.TrimSpace(values[0]) != "" {
				parameters[name] = strings.TrimSpace(values[0])
			}
		}
		if values := form.Value["output_format"]; len(values) > 0 {
			parameters["output_format"] = values[0]
		}
	} else {
		var err error
		if images, err = jsonImages(request); err != nil {
			return nil, err
		}
		if len(request.Mask) > 0 {
			if mask, err = jsonImage(request.Mask); err != nil {
				return nil, err
			}
		}
		for name, raw := range request.Extra {
			if _, known := stabilityParameters[name]; known {
				parameters[name] = strings.Trim(string(raw), `"`)
			}
		}
		if len(request.OutputFormat) > 0 {
			parameters["output_format"] = strings.Trim(string(request.OutputFormat), `"`)
		}
	}
	wanted := 1
	if service.secondImage != "" {
		wanted = 2
	}
	if len(images) != wanted {
		return nil, newBadRequest("model %s takes %d image(s), got %d", modelID, wanted, len(images))
	}
	body[service.image] = images[0]
	if service.secondImage != "" {
		body[service.secondImage] = images[1]
	}
	if mask != "" && slices.Contains(service.takes, "mask") {
		body["mask"] = mask
	}
	for name, value := range parameters {
		if name != "output_format" && !slices.Contains(service.takes, name) {
			continue
		}
		if name == "output_format" {
			if value != "png" && value != "jpeg" && value != "webp" {
				return nil, newBadRequest("output_format must be png, jpeg or webp")
			}
			body[name] = value
			continue
		}
		if !stabilityParameters[name] {
			body[name] = value
			continue
		}
		number, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, newBadRequest("%s must be a number", name)
		}
		body[name] = number
	}
	return body, nil
}

func readFormImage(header *multipart.FileHeader) (string, error) {
	if header.Size > maxStabilityImageBytes {
		return "", newBadRequest("an image may be at most %d MB", maxStabilityImageBytes>>20)
	}
	file, err := header.Open()
	if err != nil {
		return "", newBadRequest("read image: %v", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxStabilityImageBytes+1))
	if err != nil {
		return "", newBadRequest("read image: %v", err)
	}
	if len(data) > maxStabilityImageBytes {
		return "", newBadRequest("an image may be at most %d MB", maxStabilityImageBytes>>20)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// jsonImage reads an image given as a data URL or base64 string, or as an
// object with image_url or b64_json.
func jsonImage(raw json.RawMessage) (string, error) {
	var text string
	if err := common.Unmarshal(raw, &text); err != nil {
		var object struct {
			ImageURL any    `json:"image_url"`
			B64JSON  string `json:"b64_json"`
		}
		if err := common.Unmarshal(raw, &object); err != nil {
			return "", newBadRequest("an image must be a data URL or base64 string")
		}
		switch url := object.ImageURL.(type) {
		case string:
			text = url
		case map[string]any:
			text, _ = url["url"].(string)
		}
		if text == "" {
			text = object.B64JSON
		}
	}
	if strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://") {
		return "", newBadRequest("an image must be sent inline, as a data URL or base64")
	}
	if _, data, found := strings.Cut(text, ";base64,"); found {
		text = data
	}
	if text == "" {
		return "", newBadRequest("an image must be a data URL or base64 string")
	}
	if base64.StdEncoding.DecodedLen(len(text)) > maxStabilityImageBytes {
		return "", newBadRequest("an image may be at most %d MB", maxStabilityImageBytes>>20)
	}
	return text, nil
}

func jsonImages(request dto.ImageRequest) ([]string, error) {
	var raws []json.RawMessage
	if len(request.Images) > 0 {
		if err := common.Unmarshal(request.Images, &raws); err != nil {
			return nil, newBadRequest("images must be an array")
		}
	} else if len(request.Image) > 0 {
		if err := common.Unmarshal(request.Image, &raws); err != nil {
			raws = []json.RawMessage{request.Image}
		}
	}
	images := make([]string, 0, len(raws))
	for _, raw := range raws {
		image, err := jsonImage(raw)
		if err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	return images, nil
}

// stabilityImageResponse is the OpenAI answer and the images it carries,
// counted from their payloads.
func stabilityImageResponse(reply bedrockReply) (*dto.ImageResponse, int64, *types.NewAPIError) {
	var native struct {
		Images []string `json:"images"`
	}
	if err := common.Unmarshal(reply.body, &native); err != nil {
		return nil, 0, types.NewOpenAIError(fmt.Errorf("decode image response: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	response := &dto.ImageResponse{Created: time.Now().Unix(), Data: []dto.ImageData{}}
	for _, image := range native.Images {
		if image != "" {
			response.Data = append(response.Data, dto.ImageData{B64Json: image})
		}
	}
	return response, int64(len(response.Data)), nil
}
