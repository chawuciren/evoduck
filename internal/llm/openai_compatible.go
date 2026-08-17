package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type OpenAICompatibleProvider struct {
	name             string
	baseURL          string
	apiKey           string
	headers          map[string]string
	model            string
	config           config.ProviderConfig
	httpClient       *http.Client
	streamHTTPClient *http.Client
	defaultOptions   ChatOptions
	adapter          openAICompatibleRequestAdapter
	decider          *proxy.Decider
}

type openAICompatibleRequest struct {
	Model               string                 `json:"model"`
	Messages            []openAIChatMessage    `json:"messages"`
	Tools               []openAIChatTool       `json:"tools,omitempty"`
	ToolChoice          interface{}            `json:"tool_choice,omitempty"`
	ParallelToolCalls   *bool                  `json:"parallel_tool_calls,omitempty"`
	ResponseFormat      map[string]interface{} `json:"response_format,omitempty"`
	Stop                []string               `json:"stop,omitempty"`
	PresencePenalty     *float64               `json:"presence_penalty,omitempty"`
	FrequencyPenalty    *float64               `json:"frequency_penalty,omitempty"`
	MaxCompletionTokens int                    `json:"max_completion_tokens,omitempty"`
	MaxTokens           int                    `json:"max_tokens,omitempty"`
	ReasoningEffort     string                 `json:"reasoning_effort,omitempty"`
	Verbosity           string                 `json:"verbosity,omitempty"`
	User                string                 `json:"user,omitempty"`
	SafetyIdentifier    string                 `json:"safety_identifier,omitempty"`
	ServiceTier         string                 `json:"service_tier,omitempty"`
	N                   int                    `json:"n,omitempty"`
	Seed                *int                   `json:"seed,omitempty"`
	LogProbs            bool                   `json:"logprobs,omitempty"`
	TopLogProbs         int                    `json:"top_logprobs,omitempty"`
	Store               *bool                  `json:"store,omitempty"`
	Metadata            map[string]string      `json:"metadata,omitempty"`
	ChatTemplateKwargs  map[string]interface{} `json:"chat_template_kwargs,omitempty"`
	Temperature         *float64               `json:"temperature,omitempty"`
	TopP                *float64               `json:"top_p,omitempty"`
	Stream              bool                   `json:"stream,omitempty"`
	StreamOptions       map[string]bool        `json:"stream_options,omitempty"`
	Extra               map[string]any         `json:"-"`
}

func (r openAICompatibleRequest) MarshalJSON() ([]byte, error) {
	type requestAlias openAICompatibleRequest
	payload, err := json.Marshal(requestAlias(r))
	if err != nil {
		return nil, err
	}
	if len(r.Extra) == 0 {
		return payload, nil
	}

	var merged map[string]any
	if err := json.Unmarshal(payload, &merged); err != nil {
		return nil, err
	}
	for key, value := range r.Extra {
		if strings.TrimSpace(key) == "" || value == nil {
			continue
		}
		merged[key] = value
	}
	return json.Marshal(merged)
}

type openAIChatMessage struct {
	Role             string               `json:"role"`
	Content          interface{}          `json:"content,omitempty"`
	ReasoningContent string               `json:"reasoning_content,omitempty"`
	ToolCalls        []openAIChatToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string               `json:"tool_call_id,omitempty"`
	Name             string               `json:"name,omitempty"`
}

type openAIChatTool struct {
	Type     string                 `json:"type"`
	Function openAIChatFunctionSpec `json:"function"`
}

type openAIChatFunctionSpec struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type openAIChatToolCall struct {
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type"`
	Function openAIChatToolFunction `json:"function"`
}

type openAIChatToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

type openAICompatibleResponse struct {
	Choices []openAICompatibleChoice `json:"choices"`
}

type openAICompatibleChoice struct {
	Message      openAIChatMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type openAICompatibleModelListResponse struct {
	Data   []map[string]any `json:"data"`
	Models []map[string]any `json:"models"`
}

type openAICompatibleStreamChunk struct {
	Choices []openAICompatibleStreamChoice `json:"choices"`
}

type openAICompatibleStreamChoice struct {
	Delta        openAICompatibleStreamDelta `json:"delta"`
	FinishReason string                      `json:"finish_reason"`
}

type openAICompatibleStreamDelta struct {
	Content          string                 `json:"content"`
	ReasoningContent string                 `json:"reasoning_content"`
	ToolCalls        []openAIStreamToolCall `json:"tool_calls"`
}

type openAIStreamToolCall struct {
	Index    *int                   `json:"index"`
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openAIChatToolFunction `json:"function"`
}

type openAICompatibleSSEEvent struct {
	Event string
	Data  string
}

type openAICompatibleRequestAdapter interface {
	ConvertMessages(*OpenAICompatibleProvider, []models.Message) ([]openAIChatMessage, error)
	ApplyConfig(*openAICompatibleRequest, config.ProviderConfig)
}

type defaultOpenAICompatibleAdapter struct{}

type reasoningReplayPolicy string

const (
	reasoningReplayAll           reasoningReplayPolicy = "all"
	reasoningReplayNone          reasoningReplayPolicy = "none"
	reasoningReplayToolCallsOnly reasoningReplayPolicy = "tool_calls_only"
)

func (defaultOpenAICompatibleAdapter) ConvertMessages(p *OpenAICompatibleProvider, messages []models.Message) ([]openAIChatMessage, error) {
	return p.convertMessagesWithReasoningPolicy(messages, reasoningReplayAll)
}

func (defaultOpenAICompatibleAdapter) ApplyConfig(req *openAICompatibleRequest, cfg config.ProviderConfig) {
	applyCompatibleProviderConfig(req, cfg)
}

func NewOpenAICompatibleProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (Provider, error) {
	return newOpenAICompatibleProviderWithAdapter(name, cfg, defaultOpenAICompatibleAdapter{}, decider)
}

func newOpenAICompatibleProviderWithAdapter(name string, cfg config.ProviderConfig, adapter openAICompatibleRequestAdapter, decider *proxy.Decider) (*OpenAICompatibleProvider, error) {
	normalized, err := normalizeOpenAICompatibleConfig(cfg)
	if err != nil {
		return nil, err
	}
	if adapter == nil {
		adapter = defaultOpenAICompatibleAdapter{}
	}
	httpClient := &http.Client{Timeout: 5 * time.Minute}
	if decider != nil {
		httpClient = decider.ForLLM(name).HTTPClient
	}
	streamHTTPClient := cloneHTTPClientWithoutTimeout(httpClient)
	log.Debug("Created OpenAI-compatible HTTP client", logger.Fields{
		"provider":              name,
		"client_timeout_sec":    httpClient.Timeout.Seconds(),
		"stream_client_timeout": streamHTTPClient.Timeout.Seconds(),
		"has_decider":           decider != nil,
	})
	return &OpenAICompatibleProvider{
		name:             name,
		baseURL:          normalized.BaseURL,
		apiKey:           strings.TrimSpace(normalized.APIKey),
		headers:          cloneStringMap(normalized.Headers),
		model:            normalized.DefaultModel,
		config:           normalized,
		httpClient:       httpClient,
		streamHTTPClient: streamHTTPClient,
		adapter:          adapter,
		decider:          decider,
	}, nil
}

func cloneHTTPClientWithoutTimeout(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{}
	}
	clone := *client
	clone.Timeout = 0
	return &clone
}

func (p *OpenAICompatibleProvider) Name() string {
	return p.name
}

func (p *OpenAICompatibleProvider) RequiresDeferredToolImageReplay() bool { return true }

func (p *OpenAICompatibleProvider) SetDefaultOptions(opts ChatOptions) {
	p.defaultOptions = opts
}

func (p *OpenAICompatibleProvider) resolveModel(opts ChatOptions) string {
	if opts.Model != "" {
		return opts.Model
	}
	if p.defaultOptions.Model != "" {
		return p.defaultOptions.Model
	}
	return p.model
}

func (p *OpenAICompatibleProvider) mergeOptions(defaultOpts, overrideOpts ChatOptions) ChatOptions {
	result := defaultOpts
	if overrideOpts.Model != "" {
		result.Model = overrideOpts.Model
	}
	if overrideOpts.Temperature != nil {
		result.Temperature = overrideOpts.Temperature
	}
	if overrideOpts.MaxTokens > 0 {
		result.MaxTokens = overrideOpts.MaxTokens
	}
	if overrideOpts.TopP != nil {
		result.TopP = overrideOpts.TopP
	}
	return result
}

func (p *OpenAICompatibleProvider) Chat(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error) {
	return p.ChatWithOptions(ctx, messages, tools, p.defaultOptions)
}

func (p *OpenAICompatibleProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (*models.Response, error) {
	requestBody, err := p.buildRequest(messages, tools, p.mergeOptions(p.defaultOptions, opts), false)
	if err != nil {
		return nil, err
	}

	resp, err := p.doRequest(ctx, requestBody)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read openai-compatible body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai-compatible request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var envelope openAICompatibleResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode openai-compatible body: %w", err)
	}
	if len(envelope.Choices) == 0 {
		return &models.Response{}, nil
	}

	choice := envelope.Choices[0]
	result := &models.Response{}
	appendCompatibleMessageToResponse(result, choice.Message)
	return result, nil
}

func (p *OpenAICompatibleProvider) ChatStream(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	return p.ChatStreamWithOptions(ctx, messages, tools, p.defaultOptions)
}

// ChatStreamWithOptions 使用指定 options 进行流式调用，不修改共享的 defaultOptions 字段，并发安全。
// 供 fusion 圆桌等需要并发调用同一 provider 实例、但各成员 model 不同的场景使用。
func (p *OpenAICompatibleProvider) ChatStreamWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (<-chan models.StreamEvent, error) {
	requestBody, err := p.buildRequest(messages, tools, p.mergeOptions(p.defaultOptions, opts), true)
	if err != nil {
		return nil, err
	}

	resp, err := p.doStreamRequestWithRetry(ctx, requestBody)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("openai-compatible stream failed: status=%d", resp.StatusCode)
		}
		return nil, fmt.Errorf("openai-compatible stream failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	ch := make(chan models.StreamEvent, 100)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		if err := p.streamSSE(ctx, resp.Body, ch); err != nil {
			if ctx.Err() != nil {
				ch <- models.StreamEvent{Type: "cancelled", Done: true}
			} else {
				ch <- models.StreamEvent{Type: "error", Done: true, Error: err}
			}
		}
	}()
	return ch, nil
}

func (p *OpenAICompatibleProvider) doStreamRequestWithRetry(ctx context.Context, requestBody openAICompatibleRequest) (*http.Response, error) {
	const maxAttempts = 3

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := p.doRequest(ctx, requestBody)
		if err != nil {
			return nil, err
		}
		if !shouldRetryOpenAICompatibleStreamStatus(resp.StatusCode) || attempt == maxAttempts {
			return resp, nil
		}
		resp.Body.Close()

		wait := openAICompatibleRetryDelay(resp, attempt)
		if wait <= 0 {
			continue
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}

	return nil, fmt.Errorf("openai-compatible stream retry exhausted")
}

func shouldRetryOpenAICompatibleStreamStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func openAICompatibleRetryDelay(resp *http.Response, attempt int) time.Duration {
	if resp != nil {
		if retryAfter := strings.TrimSpace(resp.Header.Get("Retry-After")); retryAfter != "" {
			if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
				return time.Duration(seconds) * time.Second
			}
		}
	}

	base := 300 * time.Millisecond
	if attempt < 1 {
		attempt = 1
	}
	return time.Duration(attempt) * base
}

func (p *OpenAICompatibleProvider) GetMaxContextTokens() int {
	return getOpenAICompatibleMaxContextTokens(strings.ToLower(p.resolveModel(p.defaultOptions)))
}

func (p *OpenAICompatibleProvider) BuiltinModels() []ProviderModel {
	models := builtinModelsFromConfig(p.config.DefaultModel, p.config.Models)
	for i := range models {
		models[i].SupportsTools = true
		models[i].SupportsStreaming = true
	}
	return models
}

func (p *OpenAICompatibleProvider) FetchModels(ctx context.Context) ([]ProviderModel, error) {
	resp, err := p.doModelsRequest(ctx)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read openai-compatible models body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai-compatible models request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var envelope openAICompatibleModelListResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode openai-compatible models body: %w", err)
	}

	items := envelope.Data
	if len(items) == 0 {
		items = envelope.Models
	}
	result := make([]ProviderModel, 0, len(items))
	for _, item := range items {
		model := openAICompatibleProviderModelFromMap(item)
		if model.ID == "" {
			continue
		}
		if model.ContextWindow == 0 {
			model.ContextWindow = getOpenAICompatibleMaxContextTokens(strings.ToLower(model.ID))
		}
		if !model.SupportsStreaming {
			model.SupportsStreaming = true
		}
		result = append(result, model)
	}
	return result, nil
}

func (p *OpenAICompatibleProvider) ListModels(ctx context.Context) ([]ProviderModel, error) {
	return listProviderModels(ctx, p)
}

func (p *OpenAICompatibleProvider) buildRequest(messages []models.Message, tools []models.ToolDefinition, opts ChatOptions, stream bool) (openAICompatibleRequest, error) {
	convertedMessages, err := p.adapter.ConvertMessages(p, messages)
	if err != nil {
		return openAICompatibleRequest{}, err
	}

	request := openAICompatibleRequest{
		Model:    p.resolveModel(opts),
		Messages: convertedMessages,
		Stream:   stream,
	}
	if len(tools) > 0 {
		request.Tools = convertCompatibleTools(tools)
	}
	if opts.Temperature != nil {
		request.Temperature = opts.Temperature
	}
	if opts.TopP != nil {
		request.TopP = opts.TopP
	}
	if opts.MaxTokens > 0 {
		request.MaxTokens = opts.MaxTokens
	}
	if stream && p.config.IncludeUsage != nil {
		request.StreamOptions = map[string]bool{"include_usage": *p.config.IncludeUsage}
	}
	p.adapter.ApplyConfig(&request, p.config)
	return request, nil
}

func (p *OpenAICompatibleProvider) convertMessages(messages []models.Message) ([]openAIChatMessage, error) {
	return p.convertMessagesWithReasoningPolicy(messages, reasoningReplayAll)
}

func (p *OpenAICompatibleProvider) convertMessagesWithReasoningPolicy(messages []models.Message, reasoningPolicy reasoningReplayPolicy) ([]openAIChatMessage, error) {
	messages = sanitizeOpenAICompatibleToolMessages(messages)
	result := make([]openAIChatMessage, 0, len(messages))
	for _, m := range messages {
		msg := openAIChatMessage{Role: normalizeCompatibleRole(m.Role, p.config.Type)}
		images, err := collectProviderImageInputs(m)
		if err != nil {
			return nil, err
		}
		if msg.Role == "user" && len(images) > 0 {
			msg.Content = buildOpenAICompatibleVisionContent(m.Content, images)
		} else if strings.TrimSpace(m.Content) != "" {
			msg.Content = m.Content
		}
		if shouldIncludeCompatibleReasoning(m, reasoningPolicy) {
			msg.ReasoningContent = m.ThinkingContent
		}
		if m.ToolCallID != "" {
			msg.ToolCallID = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = make([]openAIChatToolCall, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, openAIChatToolCall{
					ID:   tc.ID,
					Type: normalizeCompatibleToolCallType(tc.Type),
					Function: openAIChatToolFunction{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		}
		// Assistant messages require content or tool_calls per OpenAI API spec.
		// ReasoningContent alone is not sufficient and causes 400 errors.
		if msg.Role == "assistant" && msg.Content == nil && len(msg.ToolCalls) == 0 {
			msg.Content = " "
		}
		// Tool messages require content (tool_call_id is validated elsewhere).
		if msg.Role == "tool" && msg.Content == nil && msg.ToolCallID != "" {
			msg.Content = " "
		}
		result = append(result, msg)
	}
	result = normalizeCompatibleSystemMessages(result)
	result = ensureCompatibleUserMessage(result)
	if len(result) == 0 {
		result = append(result, openAIChatMessage{Role: "user", Content: " "})
	}
	return result, nil
}

// ensureCompatibleUserMessage 保证 messages 中至少存在一条 user 消息。
// qwen 等模型的 chat template 要求至少一条 user query，否则渲染失败，上游返回
// "no user query found in messages"（经 new-api 中转表现为 500）。
// 触发场景：curation/摘要等内部运行构造的消息序列可能只有 system+assistant+tool
// （如 developer 提醒上提合并后 user 被压缩掉、或纯工具回放序列）。
// 兜底策略：在末尾追加一条中性的 user 消息，让模板可渲染；不改变原有消息语义。
func ensureCompatibleUserMessage(messages []openAIChatMessage) []openAIChatMessage {
	for _, msg := range messages {
		if msg.Role == "user" {
			return messages
		}
	}
	if len(messages) == 0 {
		return messages
	}
	return append(messages, openAIChatMessage{Role: "user", Content: "Please continue."})
}

// normalizeCompatibleSystemMessages 将 system 角色收敛为开头至多一条消息：
// 多条前导 system 合并，中途出现的 system（典型来源：runtime 注入的 developer
// 角色提醒，经 normalizeCompatibleRole 映射为 system）上提合并到开头。
// Ollama 等上游的 OpenAI 兼容端点要求 system message 必须位于 messages 最前
// （"system message must be at the beginning"，经 new-api 等中转表现为 500），
// 智谱 GLM 等也要求 system 仅一条；与 anthropic_compatible 的 system 上提策略保持一致。
func normalizeCompatibleSystemMessages(messages []openAIChatMessage) []openAIChatMessage {
	if len(messages) == 0 {
		return messages
	}

	var systemTexts []string
	rest := make([]openAIChatMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Role != "system" {
			rest = append(rest, msg)
			continue
		}
		if text, ok := msg.Content.(string); ok && strings.TrimSpace(text) != "" {
			systemTexts = append(systemTexts, text)
		}
	}
	if len(systemTexts) == 0 {
		return rest
	}
	merged := openAIChatMessage{Role: "system", Content: strings.Join(systemTexts, "\n\n")}
	return append([]openAIChatMessage{merged}, rest...)
}

func shouldIncludeCompatibleReasoning(msg models.Message, policy reasoningReplayPolicy) bool {
	if strings.TrimSpace(msg.ThinkingContent) == "" {
		return false
	}
	switch policy {
	case reasoningReplayNone:
		return false
	case reasoningReplayToolCallsOnly:
		return len(msg.ToolCalls) > 0
	default:
		return true
	}
}

func shouldSkipSyntheticToolMessage(m models.Message) bool {
	return strings.TrimSpace(strings.ToLower(m.Role)) == "tool" && strings.HasPrefix(m.ToolCallID, "runtime_task_plan_reminder_")
}

func sanitizeOpenAICompatibleToolMessages(messages []models.Message) []models.Message {
	if len(messages) == 0 {
		return nil
	}

	availableToolResults := make(map[string]int)
	for _, msg := range messages {
		if shouldSkipSyntheticToolMessage(msg) {
			continue
		}
		if normalizeCompatibleRole(msg.Role, "") != "tool" || strings.TrimSpace(msg.ToolCallID) == "" {
			continue
		}
		availableToolResults[msg.ToolCallID]++
	}

	remainingToolResults := cloneIntMap(availableToolResults)
	validToolResults := make(map[string]int)
	sanitized := make([]models.Message, 0, len(messages))
	for _, msg := range messages {
		role := normalizeCompatibleRole(msg.Role, "")
		if shouldSkipSyntheticToolMessage(msg) {
			continue
		}

		if role == "assistant" && len(msg.ToolCalls) > 0 {
			validCalls := make([]models.ToolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				id := strings.TrimSpace(tc.ID)
				if id == "" || remainingToolResults[id] <= 0 {
					continue
				}
				remainingToolResults[id]--
				validToolResults[id]++
				validCalls = append(validCalls, tc)
			}

			msg.ToolCalls = validCalls
			if len(msg.ToolCalls) == 0 && strings.TrimSpace(msg.Content) == "" && strings.TrimSpace(msg.ThinkingContent) == "" && (msg.ReasoningMetadata == nil || !msg.ReasoningMetadata.HasData()) {
				continue
			}
		}

		if role == "tool" {
			id := strings.TrimSpace(msg.ToolCallID)
			if id == "" || validToolResults[id] <= 0 {
				continue
			}
			validToolResults[id]--
		}

		sanitized = append(sanitized, msg)
	}

	return sanitized
}

func cloneIntMap(src map[string]int) map[string]int {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]int, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func convertCompatibleTools(tools []models.ToolDefinition) []openAIChatTool {
	result := make([]openAIChatTool, 0, len(tools))
	for _, tool := range tools {
		result = append(result, openAIChatTool{
			Type: normalizeCompatibleToolType(tool.Type),
			Function: openAIChatFunctionSpec{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		})
	}
	return result
}

func applyCompatibleProviderConfig(req *openAICompatibleRequest, cfg config.ProviderConfig) {
	if len(cfg.Stop) > 0 {
		req.Stop = append([]string(nil), cfg.Stop...)
	}
	if cfg.PresencePenalty != nil {
		req.PresencePenalty = cfg.PresencePenalty
	}
	if cfg.FrequencyPenalty != nil {
		req.FrequencyPenalty = cfg.FrequencyPenalty
	}
	if cfg.MaxCompletionTokens > 0 {
		req.MaxCompletionTokens = cfg.MaxCompletionTokens
	}
	if cfg.ReasoningEffort != "" {
		req.ReasoningEffort = strings.TrimSpace(strings.ToLower(cfg.ReasoningEffort))
	}
	if cfg.Verbosity != "" {
		req.Verbosity = cfg.Verbosity
	}
	if cfg.User != "" {
		req.User = cfg.User
	}
	if cfg.UserID != "" {
		req.User = cfg.UserID
	}
	if cfg.SafetyIdentifier != "" {
		req.SafetyIdentifier = cfg.SafetyIdentifier
	}
	if cfg.ServiceTier != "" {
		req.ServiceTier = cfg.ServiceTier
	}
	if cfg.N > 0 {
		req.N = cfg.N
	}
	if cfg.Seed != nil {
		req.Seed = cfg.Seed
	}
	if cfg.LogProbs {
		req.LogProbs = true
	}
	if cfg.TopLogProbs > 0 {
		req.TopLogProbs = cfg.TopLogProbs
		req.LogProbs = true
	}
	if cfg.Store != nil {
		req.Store = cfg.Store
	}
	if len(cfg.Metadata) > 0 {
		req.Metadata = cloneStringMap(cfg.Metadata)
	}
	if len(cfg.ChatTemplateKwargs) > 0 {
		req.ChatTemplateKwargs = cloneAnyMap(cfg.ChatTemplateKwargs)
	}
	if cfg.ParallelToolCalls != nil {
		req.ParallelToolCalls = cfg.ParallelToolCalls
	}
	if len(cfg.ResponseFormat) > 0 {
		req.ResponseFormat = cloneAnyMap(cfg.ResponseFormat)
	}
	if cfg.ToolChoice != "" {
		switch cfg.ToolChoice {
		case "auto", "none", "required":
			req.ToolChoice = cfg.ToolChoice
		default:
			req.ToolChoice = map[string]any{
				"type":     "function",
				"function": map[string]any{"name": cfg.ToolChoice},
			}
		}
	}
}

func normalizeCompatibleRole(role, providerType string) string {
	normalized := strings.TrimSpace(strings.ToLower(role))
	switch normalized {
	case "system", "user", "assistant", "tool":
		return normalized
	case "developer":
		return "system"
	default:
		if normalized == "" {
			return "user"
		}
		return normalized
	}
}

func normalizeCompatibleToolType(toolType string) string {
	if strings.TrimSpace(toolType) == "" {
		return "function"
	}
	return strings.TrimSpace(toolType)
}

func normalizeCompatibleToolCallType(toolType string) string {
	if strings.TrimSpace(toolType) == "" || toolType == "tool_use" {
		return "function"
	}
	return strings.TrimSpace(toolType)
}

func appendCompatibleMessageToResponse(result *models.Response, message openAIChatMessage) {
	if strings.TrimSpace(message.ReasoningContent) != "" {
		if result.ReasoningContent != "" {
			result.ReasoningContent += "\n"
		}
		result.ReasoningContent += message.ReasoningContent
	}
	if message.Content != nil {
		content, reasoning := extractCompatibleContent(message.Content)
		result.Content += content
		if reasoning != "" {
			if result.ReasoningContent != "" {
				result.ReasoningContent += "\n"
			}
			result.ReasoningContent += reasoning
		}
	}
	for _, toolCall := range message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, models.ToolCall{
			ID:   toolCall.ID,
			Type: normalizeCompatibleToolCallType(toolCall.Type),
			Function: models.ToolCallFunction{
				Name:      toolCall.Function.Name,
				Arguments: toolCall.Function.Arguments,
			},
		})
	}
}

func extractCompatibleContent(content interface{}) (string, string) {
	switch v := content.(type) {
	case string:
		return v, ""
	case []interface{}:
		var contentBuilder strings.Builder
		var reasoningBuilder strings.Builder
		for _, item := range v {
			part, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			kind, _ := part["type"].(string)
			switch kind {
			case "text", "output_text", "input_text":
				text, _ := part["text"].(string)
				contentBuilder.WriteString(text)
			case "reasoning", "thinking":
				text, _ := part["thinking"].(string)
				if text == "" {
					text, _ = part["text"].(string)
				}
				if text != "" {
					if reasoningBuilder.Len() > 0 {
						reasoningBuilder.WriteString("\n")
					}
					reasoningBuilder.WriteString(text)
				}
			}
		}
		return contentBuilder.String(), reasoningBuilder.String()
	default:
		return "", ""
	}
}

func (p *OpenAICompatibleProvider) doRequest(ctx context.Context, requestBody openAICompatibleRequest) (*http.Response, error) {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal openai-compatible request: %w", err)
	}
	log.Debug("OpenAI-compatible request summary", logger.Fields{
		"model":         requestBody.Model,
		"stream":        requestBody.Stream,
		"messages":      summarizeCompatibleMessagesForLog(requestBody.Messages),
		"tools_count":   len(requestBody.Tools),
		"payload_chars": len(payload),
	})

	url := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create openai-compatible request: %w", err)
	}
	if requestBody.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	req.Header.Set("Content-Type", "application/json")
	return p.doHTTPRequest(req, requestBody.Stream, false)
}

func summarizeCompatibleMessagesForLog(messages []openAIChatMessage) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		summary := map[string]any{
			"role":           msg.Role,
			"has_content":    msg.Content != nil,
			"content_type":   fmt.Sprintf("%T", msg.Content),
			"thinking_chars": len(strings.TrimSpace(msg.ReasoningContent)),
			"tool_calls":     len(msg.ToolCalls),
			"tool_call_id":   msg.ToolCallID,
		}
		if text, ok := msg.Content.(string); ok {
			summary["content_preview"] = truncateCompatibleLogText(text, 120)
		}
		result = append(result, summary)
	}
	return result
}

func truncateCompatibleLogText(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func (p *OpenAICompatibleProvider) doModelsRequest(ctx context.Context) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create openai-compatible models request: %w", err)
	}
	return p.doHTTPRequest(req, false, true)
}

func (p *OpenAICompatibleProvider) doHTTPRequest(req *http.Request, stream, models bool) (*http.Response, error) {
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	for key, value := range p.headers {
		if strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}

	client := p.httpClient
	if stream {
		client = p.streamHTTPClient
	}
	if deadline, ok := req.Context().Deadline(); ok {
		log.Debug("LLM HTTP request starting", logger.Fields{
			"url":                   req.URL.String(),
			"stream":                stream,
			"client_timeout_sec":    client.Timeout.Seconds(),
			"context_deadline_sec":  time.Until(deadline).Seconds(),
		})
	}
	resp, err := client.Do(req)
	if err != nil {
		switch {
		case models:
			return nil, fmt.Errorf("openai-compatible models request: %w", err)
		case stream:
			return nil, fmt.Errorf("openai-compatible stream: %w", err)
		default:
			return nil, fmt.Errorf("openai-compatible request: %w", err)
		}
	}
	return resp, nil
}

func (p *OpenAICompatibleProvider) streamSSE(ctx context.Context, body io.Reader, ch chan<- models.StreamEvent) error {
	reader := bufio.NewReader(body)
	toolCallBuilders := make(map[int]*toolCallBuilder)
	for {
		// Check for context cancellation before blocking read
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		event, err := readOpenAICompatibleSSEEvent(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read openai-compatible sse: %w", err)
		}
		if event.Data == "" || event.Data == "[DONE]" {
			continue
		}

		var chunk openAICompatibleStreamChunk
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return fmt.Errorf("decode openai-compatible sse: %w", err)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.Delta.ReasoningContent != "" {
			ch <- models.StreamEvent{Type: "thinking", ThinkingContent: choice.Delta.ReasoningContent}
		}
		if choice.Delta.Content != "" {
			ch <- models.StreamEvent{Type: "content", Content: choice.Delta.Content}
		}
		for _, tc := range choice.Delta.ToolCalls {
			index := 0
			if tc.Index != nil {
				index = *tc.Index
			}
			builder := toolCallBuilders[index]
			if builder == nil {
				builder = newToolCallBuilder(tc.ID, normalizeCompatibleToolCallType(tc.Type))
				toolCallBuilders[index] = builder
			}
			if tc.ID != "" {
				builder.id = tc.ID
			}
			if tc.Function.Name != "" {
				builder.name.WriteString(tc.Function.Name)
			}
			if tc.Function.Arguments != "" {
				builder.arguments.WriteString(tc.Function.Arguments)
			}
		}
		if choice.FinishReason != "" {
			toolCalls := buildToolCallsFromBuilders(toolCallBuilders)
			if len(toolCalls) > 0 {
				ch <- models.StreamEvent{Type: "tool_calls", ToolCalls: toolCalls}
				return nil
			}
			ch <- models.StreamEvent{Type: choice.FinishReason, Done: choice.FinishReason == "stop"}
			return nil
		}
	}
}

func readOpenAICompatibleSSEEvent(reader *bufio.Reader) (openAICompatibleSSEEvent, error) {
	var event openAICompatibleSSEEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				line = strings.TrimRight(line, "\r\n")
				if line == "" {
					if event.Event == "" && event.Data == "" {
						return openAICompatibleSSEEvent{}, io.EOF
					}
					return event, nil
				}
			} else {
				return openAICompatibleSSEEvent{}, err
			}
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if event.Event == "" && event.Data == "" {
				if err == io.EOF {
					return openAICompatibleSSEEvent{}, io.EOF
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

func openAICompatibleProviderModelFromMap(item map[string]any) ProviderModel {
	id := firstNonEmptyString(
		stringFromAny(item["id"]),
		stringFromAny(item["name"]),
		stringFromAny(item["model"]),
	)
	name := firstNonEmptyString(
		stringFromAny(item["name"]),
		id,
	)

	model := ProviderModel{
		ID:                strings.TrimSpace(id),
		Name:              strings.TrimSpace(name),
		ContextWindow:     firstPositiveInt(item["context_window"], item["contextWindow"], item["max_context_tokens"], item["input_token_limit"]),
		MaxTokens:         firstPositiveInt(item["max_tokens"], item["max_output_tokens"], item["output_token_limit"]),
		SupportsTools:     firstBool(item["supports_tools"], item["tool_calling"], item["function_calling"]),
		SupportsStreaming: firstBool(item["supports_streaming"], item["streaming"]),
		SupportsVision:    firstBool(item["supports_vision"], item["vision"]),
		Reasoning:         firstBool(item["reasoning"], item["supports_reasoning"]),
	}

	capabilities, _ := item["capabilities"].(map[string]any)
	if len(capabilities) > 0 {
		if !model.SupportsTools {
			model.SupportsTools = firstBool(capabilities["tools"], capabilities["tool_calling"], capabilities["function_calling"])
		}
		if !model.SupportsStreaming {
			model.SupportsStreaming = firstBool(capabilities["streaming"])
		}
		if !model.SupportsVision {
			model.SupportsVision = firstBool(capabilities["vision"])
		}
		if !model.Reasoning {
			model.Reasoning = firstBool(capabilities["reasoning"])
		}
	}

	return model
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func stringFromAny(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func firstPositiveInt(values ...any) int {
	for _, value := range values {
		switch v := value.(type) {
		case int:
			if v > 0 {
				return v
			}
		case int32:
			if v > 0 {
				return int(v)
			}
		case int64:
			if v > 0 {
				return int(v)
			}
		case float64:
			if v > 0 {
				return int(v)
			}
		case json.Number:
			if n, err := v.Int64(); err == nil && n > 0 {
				return int(n)
			}
		}
	}
	return 0
}

func firstBool(values ...any) bool {
	for _, value := range values {
		if v, ok := value.(bool); ok {
			return v
		}
	}
	return false
}

var _ Provider = (*OpenAICompatibleProvider)(nil)
