package models

import "encoding/json"

type ReasoningReplay struct {
	Provider             string                           `json:"provider,omitempty"`
	OpenAIResponses      *OpenAIResponsesReasoningReplay  `json:"openai_responses,omitempty"`
	Anthropic            *AnthropicReasoningReplay       `json:"anthropic,omitempty"`
	AnthropicCompatible  *AnthropicReasoningReplay       `json:"anthropic_compatible,omitempty"`
	Bedrock              *BedrockReasoningReplay         `json:"bedrock,omitempty"`
	Gemini               *GeminiReasoningReplay          `json:"gemini,omitempty"`
	GeminiCompatible     *GeminiReasoningReplay          `json:"gemini_compatible,omitempty"`
}

type OpenAIResponsesReasoningReplay struct {
	ItemID           string   `json:"item_id,omitempty"`
	Summary          []string `json:"summary,omitempty"`
	Content          []string `json:"content,omitempty"`
	Status           string   `json:"status,omitempty"`
	EncryptedContent string   `json:"encrypted_content,omitempty"`
}

type AnthropicReasoningReplay struct {
	Signature string `json:"signature,omitempty"`
}

type BedrockReasoningReplay struct {
	Signature string `json:"signature,omitempty"`
}

type GeminiReasoningReplay struct {
	ThoughtSignature string `json:"thought_signature,omitempty"`
}

func (r *ReasoningReplay) HasData() bool {
	if r == nil {
		return false
	}
	return r.Provider != "" || r.OpenAIResponses != nil || r.Anthropic != nil || r.AnthropicCompatible != nil || r.Bedrock != nil || r.Gemini != nil || r.GeminiCompatible != nil
}

func CloneReasoningReplay(input *ReasoningReplay) *ReasoningReplay {
	if input == nil || !input.HasData() {
		return nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return input
	}
	var out ReasoningReplay
	if err := json.Unmarshal(data, &out); err != nil {
		return input
	}
	return &out
}

func MergeReasoningReplay(base, incoming *ReasoningReplay) *ReasoningReplay {
	if incoming == nil || !incoming.HasData() {
		return CloneReasoningReplay(base)
	}
	return CloneReasoningReplay(incoming)
}
