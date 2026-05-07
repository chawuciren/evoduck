package llm

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
)

var log = logger.NewModuleLogger("llm")

// normalizeOpenAICompatibleConfig normalizes the base URL for OpenAI-compatible providers.
func normalizeOpenAICompatibleConfig(cfg config.ProviderConfig) (config.ProviderConfig, error) {
	baseURL, err := normalizeCompatibleBaseURL(cfg.BaseURL, true,
		"/chat/completions",
		"/responses",
		"/v1/chat/completions",
		"/v1/responses",
	)
	if err != nil {
		return cfg, err
	}
	cfg.BaseURL = baseURL
	return cfg, nil
}

// normalizeAnthropicCompatibleConfig normalizes the base URL for Anthropic-compatible providers.
func normalizeAnthropicCompatibleConfig(cfg config.ProviderConfig) (config.ProviderConfig, error) {
	baseURL, err := normalizeCompatibleBaseURL(cfg.BaseURL, true,
		"/messages",
		"/v1/messages",
	)
	if err != nil {
		return cfg, err
	}
	cfg.BaseURL = baseURL
	return cfg, nil
}

// normalizeGeminiCompatibleConfig normalizes the base URL for Gemini-compatible providers.
func normalizeGeminiCompatibleConfig(cfg config.ProviderConfig) (config.ProviderConfig, error) {
	baseURL, err := normalizeCompatibleBaseURL(cfg.BaseURL, false,
		":generateContent",
		":streamGenerateContent",
		":streamGenerateContent?alt=sse",
	)
	if err != nil {
		return cfg, err
	}
	cfg.BaseURL = baseURL
	return cfg, nil
}

// getOpenAICompatibleMaxContextTokens returns the maximum context tokens for a model.
func getOpenAICompatibleMaxContextTokens(model string) int {
	model = strings.ToLower(model)

	if strings.Contains(model, "gpt-4o") || strings.Contains(model, "gpt-4-turbo") {
		return 128000
	}
	if strings.Contains(model, "gpt-4") {
		if strings.Contains(model, "turbo") || strings.Contains(model, "1106") || strings.Contains(model, "0125") {
			return 128000
		}
		return 8192
	}
	if strings.Contains(model, "gpt-3.5") || strings.Contains(model, "gpt-35") {
		return 16385
	}
	if strings.Contains(model, "claude") {
		return 200000
	}
	return 128000
}

// normalizeCompatibleBaseURL normalizes a base URL for compatible providers.
// It trims trailing slashes and removes known endpoint suffixes.
// If ensureV1 is true, it ensures the URL ends with /v1.
func normalizeCompatibleBaseURL(raw string, ensureV1 bool, suffixes ...string) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(raw), "/")
	if baseURL == "" {
		return "", fmt.Errorf("base_url is required")
	}

	lowerBaseURL := strings.ToLower(baseURL)
	for _, suffix := range suffixes {
		lowerSuffix := strings.ToLower(suffix)
		if strings.HasSuffix(lowerBaseURL, lowerSuffix) {
			baseURL = baseURL[:len(baseURL)-len(suffix)]
			lowerBaseURL = strings.ToLower(baseURL)
			break
		}
	}

	if ensureV1 && !strings.HasSuffix(lowerBaseURL, "/v1") {
		baseURL += "/v1"
	}

	return baseURL, nil
}

// cloneStringMap creates a copy of a string map.
func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

// cloneAnyMap creates a copy of an interface{} map.
func cloneAnyMap(input map[string]interface{}) map[string]any {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func cloneReasoningMetadata(input *models.ReasoningReplay) *models.ReasoningReplay {
	if input == nil || !input.HasData() {
		return nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return input
	}
	var out models.ReasoningReplay
	if err := json.Unmarshal(data, &out); err != nil {
		return input
	}
	return &out
}

func reasoningProvider(metadata *models.ReasoningReplay) string {
	if metadata == nil {
		return ""
	}
	return strings.TrimSpace(metadata.Provider)
}

func newAnthropicReasoningMetadata(provider, signature string) *models.ReasoningReplay {
	if strings.TrimSpace(signature) == "" {
		return nil
	}
	replay := &models.AnthropicReasoningReplay{Signature: signature}
	result := &models.ReasoningReplay{Provider: provider}
	if provider == "anthropic" {
		result.Anthropic = replay
	} else {
		result.AnthropicCompatible = replay
	}
	return result
}

func newBedrockReasoningMetadata(signature string) *models.ReasoningReplay {
	if strings.TrimSpace(signature) == "" {
		return nil
	}
	return &models.ReasoningReplay{Provider: "bedrock", Bedrock: &models.BedrockReasoningReplay{Signature: signature}}
}

func newGeminiReasoningMetadata(provider, thoughtSignature string) *models.ReasoningReplay {
	if strings.TrimSpace(thoughtSignature) == "" {
		return nil
	}
	replay := &models.GeminiReasoningReplay{ThoughtSignature: thoughtSignature}
	result := &models.ReasoningReplay{Provider: provider}
	if provider == "gemini" {
		result.Gemini = replay
	} else {
		result.GeminiCompatible = replay
	}
	return result
}

func base64EncodeBytes(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func base64DecodeBytes(value string) ([]byte, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(value)
}

type providerImageInput struct {
	MimeType string
	Data     []byte
}

func messageHasVisionMedia(message models.Message) bool {
	for _, media := range message.Media {
		if supportedVisionMimeType(media.MimeType) {
			return true
		}
	}
	return false
}

func supportedVisionMimeType(mimeType string) bool {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

func collectProviderImageInputs(message models.Message) ([]providerImageInput, error) {
	if len(message.Media) == 0 {
		return nil, nil
	}
	images := make([]providerImageInput, 0, len(message.Media))
	for _, media := range message.Media {
		mimeType := strings.ToLower(strings.TrimSpace(media.MimeType))
		if !supportedVisionMimeType(mimeType) {
			continue
		}
		data, err := loadProviderMediaBytes(media)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("image media %q is empty", media.Name)
		}
		images = append(images, providerImageInput{MimeType: mimeType, Data: data})
	}
	return images, nil
}

func loadProviderMediaBytes(media models.OutgoingMedia) ([]byte, error) {
	if strings.TrimSpace(media.Data) != "" {
		data, err := base64DecodeBytes(strings.TrimSpace(media.Data))
		if err != nil {
			return nil, fmt.Errorf("decode media %q: %w", media.Name, err)
		}
		return data, nil
	}
	if strings.TrimSpace(media.Path) != "" {
		data, err := os.ReadFile(strings.TrimSpace(media.Path))
		if err != nil {
			return nil, fmt.Errorf("read media %q: %w", media.Name, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("image media %q is missing local data", media.Name)
}

func buildOpenAICompatibleVisionContent(text string, images []providerImageInput) []map[string]any {
	parts := make([]map[string]any, 0, len(images)+1)
	if strings.TrimSpace(text) != "" || len(images) == 0 {
		parts = append(parts, map[string]any{"type": "text", "text": coalescePromptText(text)})
	}
	for _, image := range images {
		parts = append(parts, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": providerImageDataURL(image)},
		})
	}
	return parts
}

func buildResponsesVisionContent(text string, images []providerImageInput) []map[string]any {
	parts := make([]map[string]any, 0, len(images)+1)
	if strings.TrimSpace(text) != "" || len(images) == 0 {
		parts = append(parts, map[string]any{"type": "input_text", "text": coalescePromptText(text)})
	}
	for _, image := range images {
		parts = append(parts, map[string]any{
			"type":      "input_image",
			"image_url": providerImageDataURL(image),
		})
	}
	return parts
}

func buildToolResultTextParts(text string, images []providerImageInput) []map[string]any {
	parts := make([]map[string]any, 0, len(images)+1)
	if strings.TrimSpace(text) != "" || len(images) == 0 {
		parts = append(parts, map[string]any{"type": "text", "text": coalescePromptText(text)})
	}
	for _, image := range images {
		parts = append(parts, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": image.MimeType,
				"data":       base64EncodeBytes(image.Data),
			},
		})
	}
	return parts
}

func buildGeminiFunctionResponseParts(images []providerImageInput) []map[string]any {
	parts := make([]map[string]any, 0, len(images))
	for _, image := range images {
		parts = append(parts, map[string]any{
			"inlineData": map[string]any{
				"data":     base64EncodeBytes(image.Data),
				"mimeType": image.MimeType,
			},
		})
	}
	return parts
}

func providerImageDataURL(image providerImageInput) string {
	return "data:" + image.MimeType + ";base64," + base64EncodeBytes(image.Data)
}

func coalescePromptText(text string) string {
	if strings.TrimSpace(text) == "" {
		return " "
	}
	return text
}

// toolCallBuilder accumulates streaming tool call fragments.
type toolCallBuilder struct {
	id        string
	type_     string
	name      strings.Builder
	arguments strings.Builder
}

// newToolCallBuilder creates a new tool call builder.
func newToolCallBuilder(id, type_ string) *toolCallBuilder {
	return &toolCallBuilder{id: id, type_: type_}
}

// buildToolCallsFromBuilders converts tool call builders to ToolCall slice.
func buildToolCallsFromBuilders(builders map[int]*toolCallBuilder) []models.ToolCall {
	maxIndex := -1
	for idx := range builders {
		if idx > maxIndex {
			maxIndex = idx
		}
	}
	if maxIndex < 0 {
		return nil
	}
	result := make([]models.ToolCall, maxIndex+1)
	for idx, builder := range builders {
		result[idx] = models.ToolCall{
			ID:   builder.id,
			Type: builder.type_,
			Function: models.ToolCallFunction{
				Name:      builder.name.String(),
				Arguments: builder.arguments.String(),
			},
		}
	}
	return result
}

// truncateDesc truncates a description string to maxLen characters.
func truncateDesc(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// trimSSEFieldValue removes the leading space from SSE field values if present.
func trimSSEFieldValue(value string) string {
	if strings.HasPrefix(value, " ") {
		return value[1:]
	}
	return value
}

// sseEvent represents a Server-Sent Events event.
type sseEvent struct {
	Event string
	Data  string
}

// readSSEEvent reads a single SSE event from a bufio.Reader.
func readSSEEvent(reader *bufio.Reader) (sseEvent, error) {
	var event sseEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				line = strings.TrimRight(line, "\r\n")
				if line == "" {
					if event.Event == "" && event.Data == "" {
						return sseEvent{}, io.EOF
					}
					return event, nil
				}
			} else {
				return sseEvent{}, err
			}
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if event.Event == "" && event.Data == "" {
				if err == io.EOF {
					return sseEvent{}, io.EOF
				}
				continue
			}
			return event, nil
		}
		if strings.HasPrefix(line, ":") {
			if err == io.EOF {
				return event, nil
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event.Event = trimSSEFieldValue(line[len("event:"):])
		} else if strings.HasPrefix(line, "data:") {
			if event.Data != "" {
				event.Data += "\n"
			}
			event.Data += trimSSEFieldValue(line[len("data:"):])
		}
		if err == io.EOF {
			return event, nil
		}
	}
}
