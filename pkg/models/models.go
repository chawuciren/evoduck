package models

import (
	"time"
)

type Role string

const (
	RoleAdmin    Role = "admin"    // 最高权限（WebChat 默认）
	RoleEmployee Role = "employee" // 员工权限
	RoleCustomer Role = "customer" // 客户权限（受限）
)

const (
	StreamEventContent    = "content"
	StreamEventDone       = "done"
	StreamEventError      = "error"
	StreamEventPlan       = "plan"
	StreamEventPlanUpdate = "plan_update"
	StreamEventToolEnd    = "tool_end"
	StreamEventCancelled  = "cancelled"
)

const (
	ChannelEventRunStart     = "run_start"
	ChannelEventThinking     = "thinking"
	ChannelEventPlan         = "plan"
	ChannelEventPlanUpdate   = "plan_update"
	ChannelEventToolStart    = "tool_start"
	ChannelEventToolEnd      = "tool_end"
	ChannelEventContentChunk = "content_chunk"
	ChannelEventFinal        = "final"
	ChannelEventError        = "error"
	ChannelEventCancelled    = "cancelled"
)

type Message struct {
	Role              string             `json:"role"`
	Content           string             `json:"content,omitempty"`
	Media             []OutgoingMedia    `json:"media,omitempty"`
	ThinkingContent   string             `json:"thinking_content,omitempty"`
	ReasoningMetadata *ReasoningReplay   `json:"reasoning_metadata,omitempty"`
	ToolCalls         []ToolCall         `json:"tool_calls,omitempty"`
	ToolCallID        string             `json:"tool_call_id,omitempty"`
	Timestamp         time.Time          `json:"timestamp"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolDefinition struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

type FunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// StreamEvent 流式事件
type StreamEvent struct {
	Type            string     // "content", "tool_start", "tool_end", "iteration", "done", "error", "plan", "plan_update", "cancelled"
	Content         string     // 文本内容
	ThinkingContent string     // 思考内容
	ReasoningMetadata *ReasoningReplay // 推理历史回放所需的 provider-specific 元数据
	ToolCalls       []ToolCall // 完整的工具调用列表（finish_reason为tool_calls时）
	ToolID          string     // 单个工具ID（用于进度通知）
	ToolName        string     // 工具名称
	ToolParams      string     // 工具参数 JSON 字符串（用于 tool_start 事件）
	ToolResult      string     // 工具执行结果
	Iteration       int        // 当前循环轮次
	Plan            *TaskPlan  // 任务计划（新增）
	Done            bool       // 是否完成
	Error           error      // 错误信息
}

// ChannelEvent 保留运行时事件的语义信息，供各渠道自行决定展示和投递策略。
type ChannelEvent struct {
	Type      string    // "run_start", "thinking", "plan", "plan_update", "tool_start", "tool_end", "content_chunk", "final", "error", "cancelled"
	Content   string    // 文本内容或终态消息
	ToolName  string    // 工具名称
	ToolParams string   // 工具参数 JSON 字符串（用于 tool_start 事件）
	Plan      *TaskPlan // 任务计划快照
	StreamID  string    // 渠道侧流式标识
	Done      bool      // 是否为终态事件
	ErrorText string    // 错误文本
}

// StreamConfig 流式配置
type StreamConfig struct {
	MaxIterations  int  // 最大循环次数，默认10
	SendToolEvents bool // 是否发送工具执行事件，默认true
}

// SubTask 子任务
type SubTask struct {
	Name        string `json:"name"`         // 简短描述
	Description string `json:"description"`  // 详细说明
	Type        string `json:"type"`         // 任务类型：short/search/research/coding/chat
	Status      string `json:"status"`       // pending | running | done | skipped
	ToolUsed    string `json:"tool_used"`    // 使用的工具名
	ToolCallID  string `json:"tool_call_id"` // 关联的工具调用实例ID（唯一标识一次调用）
	Result      string `json:"result"`       // 结果摘要 (100 字符)
}

// TaskPlan 任务计划
type TaskPlan struct {
	Intent         string    `json:"intent"`          // 用户意图总结
	OverallType    string    `json:"overall_type"`    // 整体任务类型
	CompletionHint string    `json:"completion_hint"` // 完成的判断依据
	SubTasks       []SubTask `json:"sub_tasks"`       // 子任务列表
}

type Response struct {
	Content          string
	ReasoningContent string
	ReasoningMetadata *ReasoningReplay
	ToolCalls        []ToolCall
}

type NormalizedMessage struct {
	Channel      string          `json:"channel,omitempty"`
	AccountID    string          `json:"account_id,omitempty"` // channel ID（如 "wecom-sales" 或 "weixin-cs"）
	SenderID     string          `json:"sender_id,omitempty"`  // 消息发送者ID
	UserID       string          `json:"user_id,omitempty"`    // 业务用户ID（微信个人号：配置的user_id；其他：同SenderID）
	Content      string          `json:"content,omitempty"`
	Media        []OutgoingMedia `json:"media,omitempty"`
	ThreadID     string          `json:"thread_id,omitempty"`
	IsDM         bool            `json:"is_dm,omitempty"`
	Role         Role            `json:"role,omitempty"`
	ContextToken string          `json:"context_token,omitempty"`
	ResponseURL  string          `json:"response_url,omitempty"` // WeCom AI Bot response URL (optional)
}

type OutgoingMedia struct {
	Type              string `json:"type,omitempty"`
	Name              string `json:"name,omitempty"`
	MimeType          string `json:"mime_type,omitempty"`
	URL               string `json:"url,omitempty"`
	Path              string `json:"path,omitempty"`
	Data              string `json:"data,omitempty"`
	EncryptQueryParam string `json:"encrypt_query_param,omitempty"`
	AESKey            string `json:"aes_key,omitempty"`
	FileSize          int64  `json:"file_size,omitempty"`
}

type OutgoingMessage struct {
	Channel      string          `json:"channel,omitempty"`
	TargetID     string          `json:"target_id,omitempty"`
	Content      string          `json:"content,omitempty"`
	Media        []OutgoingMedia `json:"media,omitempty"`
	ThreadID     string          `json:"thread_id,omitempty"`
	ContextToken string          `json:"context_token,omitempty"`
	ResponseURL  string          `json:"response_url,omitempty"` // WeCom AI Bot response URL (optional)
	StreamID     string          `json:"stream_id,omitempty"`
	StreamDone   bool            `json:"stream_done,omitempty"`
}
