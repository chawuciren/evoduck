package llm

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"

	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/chawuciren/evoduck/pkg/models"
)

func TestBuildResponsesInputIncludesReasoningReplayItem(t *testing.T) {
	items, err := buildResponsesInput([]models.Message{{
		Role:            "assistant",
		ThinkingContent: "brief reasoning summary",
		ReasoningMetadata: &models.ReasoningReplay{
			Provider: "openai_responses",
			OpenAIResponses: &models.OpenAIResponsesReasoningReplay{
				ItemID:  "rs_123",
				Summary: []string{"brief reasoning summary"},
				Status:  "completed",
			},
		},
	}})
	if err != nil {
		t.Fatalf("buildResponsesInput() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 input item, got %d", len(items))
	}
	if items[0].Type != "reasoning" {
		t.Fatalf("expected reasoning item, got %+v", items[0])
	}
	if items[0].Name != "rs_123" {
		t.Fatalf("expected reasoning item id in Name field, got %+v", items[0])
	}
	parts, ok := items[0].Content.([]responsesTextContent)
	if !ok || len(parts) != 1 {
		t.Fatalf("expected reasoning content payload, got %#v", items[0].Content)
	}
	if parts[0].Type != "summary_text" || parts[0].Text != "brief reasoning summary" {
		t.Fatalf("unexpected reasoning payload: %#v", parts)
	}
	if items[0].Status != "completed" {
		t.Fatalf("expected completed status, got %+v", items[0])
	}
}

func TestConvertResponsesOutputCapturesReasoningMetadata(t *testing.T) {
	resp := convertResponsesOutput([]responsesOutputItem{{
		ID:               "rs_123",
		Type:             "reasoning",
		Status:           "completed",
		Summary:          []responsesOutputContent{{Type: "summary_text", Text: "reasoning summary"}},
		Content:          []responsesOutputContent{{Type: "reasoning_text", Text: "full reasoning"}},
		EncryptedContent: "enc_payload",
	}})

	if resp.ReasoningContent != "reasoning summary" {
		t.Fatalf("expected reasoning summary, got %q", resp.ReasoningContent)
	}
	if resp.ReasoningMetadata == nil {
		t.Fatal("expected reasoning metadata")
	}
	if resp.ReasoningMetadata.Provider != "openai_responses" {
		t.Fatalf("unexpected provider metadata: %+v", resp.ReasoningMetadata)
	}
	if resp.ReasoningMetadata.OpenAIResponses == nil || resp.ReasoningMetadata.OpenAIResponses.ItemID != "rs_123" {
		t.Fatalf("unexpected item id metadata: %+v", resp.ReasoningMetadata)
	}
	if resp.ReasoningMetadata.OpenAIResponses.EncryptedContent != "enc_payload" {
		t.Fatalf("unexpected encrypted metadata: %+v", resp.ReasoningMetadata)
	}
}

func TestAnthropicConvertMessagesReplaysThinkingSignature(t *testing.T) {
	provider := &AnthropicProvider{}
	_, messages, err := provider.convertMessages([]models.Message{{
		Role:              "assistant",
		ThinkingContent:   "chain",
		ReasoningMetadata: &models.ReasoningReplay{Provider: "anthropic", Anthropic: &models.AnthropicReasoningReplay{Signature: "sig-123"}},
	}})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	if len(messages) != 1 || len(messages[0].Content) != 1 {
		t.Fatalf("unexpected anthropic messages: %+v", messages)
	}
	block := messages[0].Content[0].OfThinking
	if block == nil {
		t.Fatalf("expected thinking block, got %#v", messages[0].Content[0])
	}
	if block.Thinking != "chain" || block.Signature != "sig-123" {
		t.Fatalf("unexpected anthropic thinking block: %+v", block)
	}
}

func TestAnthropicConvertMessagesIncludesToolImageBlock(t *testing.T) {
	provider := &AnthropicProvider{}
	file := t.TempDir() + "/image.png"
	if err := os.WriteFile(file, []byte("png-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, messages, err := provider.convertMessages([]models.Message{
		{Role: "assistant", ToolCalls: []models.ToolCall{{ID: "call_1", Type: "function", Function: models.ToolCallFunction{Name: "browser_screenshot", Arguments: `{}`}}}},
		{Role: "tool", ToolCallID: "call_1", Content: "Screenshot captured", Media: []models.OutgoingMedia{{Name: "image.png", MimeType: "image/png", Path: file}}},
	})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	if len(messages) != 2 || len(messages[1].Content) != 1 {
		t.Fatalf("unexpected anthropic messages: %+v", messages)
	}
	toolResult := messages[1].Content[0].OfToolResult
	if toolResult == nil {
		t.Fatalf("expected tool result block, got %#v", messages[1].Content[0])
	}
	if toolResult.ToolUseID != "call_1" || len(toolResult.Content) != 2 {
		t.Fatalf("unexpected tool result payload: %+v", toolResult)
	}
	if toolResult.Content[0].OfText == nil || toolResult.Content[0].OfText.Text != "Screenshot captured" {
		t.Fatalf("expected text block, got %#v", toolResult.Content[0])
	}
	if toolResult.Content[1].OfImage == nil || toolResult.Content[1].OfImage.Source.OfBase64 == nil || string(toolResult.Content[1].OfImage.Source.OfBase64.MediaType) != "image/png" || toolResult.Content[1].OfImage.Source.OfBase64.Data == "" {
		t.Fatalf("expected image block, got %#v", toolResult.Content[1])
	}
}

func TestBedrockConvertMessagesReplaysReasoningSignature(t *testing.T) {
	provider := &BedrockProvider{}
	_, messages, err := provider.convertMessages([]models.Message{{
		Role:              "assistant",
		ThinkingContent:   "chain",
		ReasoningMetadata: &models.ReasoningReplay{Provider: "bedrock", Bedrock: &models.BedrockReasoningReplay{Signature: "sig-123"}},
	}})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	if len(messages) != 1 || len(messages[0].Content) != 1 {
		t.Fatalf("unexpected bedrock messages: %+v", messages)
	}
	block, ok := messages[0].Content[0].(*bedrocktypes.ContentBlockMemberReasoningContent)
	if !ok {
		t.Fatalf("expected reasoning content block, got %#v", messages[0].Content[0])
	}
	reasoning, ok := block.Value.(*bedrocktypes.ReasoningContentBlockMemberReasoningText)
	if !ok || reasoning.Value.Text == nil || *reasoning.Value.Text != "chain" || reasoning.Value.Signature == nil || *reasoning.Value.Signature != "sig-123" {
		t.Fatalf("unexpected bedrock reasoning block: %#v", block.Value)
	}
}

func TestGeminiConvertMessagesReplaysThoughtSignature(t *testing.T) {
	provider := &GeminiProvider{}
	signature := base64.StdEncoding.EncodeToString([]byte("sig-123"))
	_, messages, err := provider.convertMessages([]models.Message{{
		Role:              "assistant",
		ThinkingContent:   "chain",
		ReasoningMetadata: &models.ReasoningReplay{Provider: "gemini", Gemini: &models.GeminiReasoningReplay{ThoughtSignature: signature}},
	}})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	if len(messages) != 1 || len(messages[0].Parts) != 1 {
		t.Fatalf("unexpected gemini messages: %+v", messages)
	}
	part := messages[0].Parts[0]
	if part == nil || !part.Thought || part.Text != "chain" || string(part.ThoughtSignature) != "sig-123" {
		t.Fatalf("unexpected gemini thought part: %#v", part)
	}
}

func TestBuildResponsesInputIncludesImageItems(t *testing.T) {
	file := t.TempDir() + "/image.png"
	if err := os.WriteFile(file, []byte("png-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	items, err := buildResponsesInput([]models.Message{{
		Role:    "user",
		Content: "look",
		Media:   []models.OutgoingMedia{{Name: "image.png", MimeType: "image/png", Path: file}},
	}})
	if err != nil {
		t.Fatalf("buildResponsesInput() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 input item, got %d", len(items))
	}
	payload, err := json.Marshal(items[0].Content)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	jsonText := string(payload)
	if !strings.Contains(jsonText, `"type":"input_text"`) || !strings.Contains(jsonText, `"text":"look"`) {
		t.Fatalf("expected input_text part, got %s", jsonText)
	}
	if !strings.Contains(jsonText, `"type":"input_image"`) || !strings.Contains(jsonText, `"image_url":"data:image/png;base64,`) {
		t.Fatalf("expected input_image data url part, got %s", jsonText)
	}
}

func TestBuildResponsesInputIncludesToolImageItems(t *testing.T) {
	file := t.TempDir() + "/image.png"
	if err := os.WriteFile(file, []byte("png-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	items, err := buildResponsesInput([]models.Message{
		{Role: "assistant", ToolCalls: []models.ToolCall{{ID: "call_1", Type: "function", Function: models.ToolCallFunction{Name: "browser_screenshot", Arguments: `{}`}}}},
		{Role: "tool", ToolCallID: "call_1", Content: "Screenshot captured", Media: []models.OutgoingMedia{{Name: "image.png", MimeType: "image/png", Path: file}}},
	})
	if err != nil {
		t.Fatalf("buildResponsesInput() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected assistant call and tool result items, got %d", len(items))
	}
	if items[1].Type != "function_call_output" {
		t.Fatalf("expected function_call_output, got %+v", items[1])
	}
	payload, err := json.Marshal(items[1].Output)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	jsonText := string(payload)
	if !strings.Contains(jsonText, `"type":"input_text"`) || !strings.Contains(jsonText, `"text":"Screenshot captured"`) {
		t.Fatalf("expected tool text part, got %s", jsonText)
	}
	if !strings.Contains(jsonText, `"type":"input_image"`) || !strings.Contains(jsonText, `"image_url":"data:image/png;base64,`) {
		t.Fatalf("expected tool image part, got %s", jsonText)
	}
}

func TestAnthropicCompatibleMessageBlocksIncludeImageBlock(t *testing.T) {
	file := t.TempDir() + "/image.png"
	if err := os.WriteFile(file, []byte("png-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	blocks, err := anthropicCompatibleMessageBlocks(models.Message{
		Role:    "user",
		Content: "look",
		Media:   []models.OutgoingMedia{{Name: "image.png", MimeType: "image/png", Path: file}},
	})
	if err != nil {
		t.Fatalf("anthropicCompatibleMessageBlocks() error = %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected text and image blocks, got %#v", blocks)
	}
	if blocks[0].Type != "text" || blocks[0].Text != "look" {
		t.Fatalf("unexpected text block: %#v", blocks[0])
	}
	if blocks[1].Type != "image" || blocks[1].Source == nil {
		t.Fatalf("unexpected image block: %#v", blocks[1])
	}
	if blocks[1].Source.Type != "base64" || blocks[1].Source.MediaType != "image/png" || blocks[1].Source.Data == "" {
		t.Fatalf("unexpected image source: %#v", blocks[1].Source)
	}
}

func TestAnthropicCompatibleConvertMessagesIncludesToolImageBlock(t *testing.T) {
	provider := &AnthropicCompatibleProvider{}
	file := t.TempDir() + "/image.png"
	if err := os.WriteFile(file, []byte("png-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, messages, err := provider.convertMessages([]models.Message{
		{Role: "assistant", ToolCalls: []models.ToolCall{{ID: "call_1", Type: "function", Function: models.ToolCallFunction{Name: "browser_screenshot", Arguments: `{}`}}}},
		{Role: "tool", ToolCallID: "call_1", Content: "Screenshot captured", Media: []models.OutgoingMedia{{Name: "image.png", MimeType: "image/png", Path: file}}},
	})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected assistant and tool result messages, got %d", len(messages))
	}
	block := messages[1].Content[0]
	if block.Type != "tool_result" {
		t.Fatalf("expected tool_result block, got %#v", block)
	}
	payload, err := json.Marshal(block.Content)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	jsonText := string(payload)
	if !strings.Contains(jsonText, `"type":"text"`) || !strings.Contains(jsonText, `"text":"Screenshot captured"`) {
		t.Fatalf("expected tool text block, got %s", jsonText)
	}
	if !strings.Contains(jsonText, `"type":"image"`) || !strings.Contains(jsonText, `"media_type":"image/png"`) {
		t.Fatalf("expected tool image block, got %s", jsonText)
	}
}

func TestGeminiCompatibleMessagePartsIncludeInlineData(t *testing.T) {
	file := t.TempDir() + "/image.png"
	if err := os.WriteFile(file, []byte("png-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	parts, err := geminiCompatibleMessageParts(models.Message{
		Role:    "user",
		Content: "look",
		Media:   []models.OutgoingMedia{{Name: "image.png", MimeType: "image/png", Path: file}},
	})
	if err != nil {
		t.Fatalf("geminiCompatibleMessageParts() error = %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected text and image parts, got %#v", parts)
	}
	if parts[0].Text != "look" {
		t.Fatalf("unexpected text part: %#v", parts[0])
	}
	if parts[1].InlineData == nil || parts[1].InlineData.MIMEType != "image/png" || parts[1].InlineData.Data == "" {
		t.Fatalf("unexpected inline data part: %#v", parts[1])
	}
}

func TestGeminiCompatibleConvertMessagesIncludesToolImagePart(t *testing.T) {
	provider := &GeminiCompatibleProvider{}
	file := t.TempDir() + "/image.png"
	if err := os.WriteFile(file, []byte("png-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, messages, err := provider.convertMessages([]models.Message{
		{Role: "assistant", ToolCalls: []models.ToolCall{{ID: "call_1", Type: "function", Function: models.ToolCallFunction{Name: "browser_screenshot", Arguments: `{}`}}}},
		{Role: "tool", ToolCallID: "call_1", Content: "Screenshot captured", Media: []models.OutgoingMedia{{Name: "image.png", MimeType: "image/png", Path: file}}},
	})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected assistant and tool response contents, got %d", len(messages))
	}
	fr := messages[1].Parts[0].FunctionResponse
	if fr == nil || fr.Name != "browser_screenshot" || fr.ID != "call_1" {
		t.Fatalf("unexpected function response: %#v", messages[1].Parts[0])
	}
	if len(fr.Parts) != 1 || fr.Parts[0].InlineData == nil || fr.Parts[0].InlineData.MIMEType != "image/png" || fr.Parts[0].InlineData.Data == "" {
		t.Fatalf("expected tool image inline data, got %#v", fr.Parts)
	}
}

func TestGeminiMessagePartsIncludeInlineData(t *testing.T) {
	provider := &GeminiProvider{}
	file := t.TempDir() + "/image.png"
	if err := os.WriteFile(file, []byte("png-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	parts, err := provider.messagePartsFromMessage(models.Message{
		Role:    "user",
		Content: "look",
		Media:   []models.OutgoingMedia{{Name: "image.png", MimeType: "image/png", Path: file}},
	})
	if err != nil {
		t.Fatalf("messagePartsFromMessage() error = %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected text and image parts, got %#v", parts)
	}
	if parts[0] == nil || parts[0].Text != "look" {
		t.Fatalf("unexpected text part: %#v", parts[0])
	}
	if parts[1] == nil || parts[1].InlineData == nil {
		t.Fatalf("expected inline image data, got %#v", parts[1])
	}
}

func TestGeminiConvertMessagesIncludesToolImagePart(t *testing.T) {
	provider := &GeminiProvider{}
	file := t.TempDir() + "/image.png"
	if err := os.WriteFile(file, []byte("png-bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, messages, err := provider.convertMessages([]models.Message{
		{Role: "assistant", ToolCalls: []models.ToolCall{{ID: "call_1", Type: "function", Function: models.ToolCallFunction{Name: "browser_screenshot", Arguments: `{}`}}}},
		{Role: "tool", ToolCallID: "call_1", Content: "Screenshot captured", Media: []models.OutgoingMedia{{Name: "image.png", MimeType: "image/png", Path: file}}},
	})
	if err != nil {
		t.Fatalf("convertMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected assistant and tool response contents, got %d", len(messages))
	}
	fr := messages[1].Parts[0].FunctionResponse
	if fr == nil || fr.Name != "browser_screenshot" || fr.ID != "call_1" {
		t.Fatalf("unexpected function response: %#v", messages[1].Parts[0])
	}
	if len(fr.Parts) != 1 || fr.Parts[0] == nil || fr.Parts[0].InlineData == nil || fr.Parts[0].InlineData.MIMEType != "image/png" || string(fr.Parts[0].InlineData.Data) != "png-bytes" {
		t.Fatalf("expected tool image inline data, got %#v", fr.Parts)
	}
}
