package plugin

import "time"

type ProtocolVersion int

const CurrentProtocolVersion ProtocolVersion = 1

type FrameType string

const (
	FrameTypeRequest  FrameType = "request"
	FrameTypeResponse FrameType = "response"
	FrameTypeEvent    FrameType = "event"
	FrameTypeError    FrameType = "error"
)

type Method string

const (
	MethodRegister       Method = "register"
	MethodToolExecute    Method = "tool.execute"
	MethodProviderChat   Method = "provider.chat"
	MethodProviderEvent  Method = "provider.event"
	MethodChannelSend    Method = "channel.send"
	MethodChannelMessage Method = "channel.message"
	MethodHookTrigger    Method = "hook.trigger"
	MethodCancel         Method = "cancel"
	MethodPing           Method = "ping"
	MethodPong           Method = "pong"
)

type CapabilityType string

const (
	CapabilityTypeTool     CapabilityType = "tool"
	CapabilityTypeProvider CapabilityType = "provider"
	CapabilityTypeChannel  CapabilityType = "channel"
	CapabilityTypeHook     CapabilityType = "hook"
)

type Frame struct {
	ID           string                 `json:"id"`
	Type         FrameType              `json:"type"`
	Method       Method                 `json:"method"`
	ReplyTo      string                 `json:"reply_to,omitempty"`
	PluginID     string                 `json:"plugin_id,omitempty"`
	CapabilityID string                 `json:"capability_id,omitempty"`
	Timestamp    int64                  `json:"timestamp"`
	Data         map[string]interface{} `json:"data,omitempty"`
}

type Registration struct {
	PluginID        string       `json:"plugin_id"`
	ProtocolVersion int          `json:"protocol_version"`
	PluginVersion   string       `json:"plugin_version"`
	Name            string       `json:"name"`
	Capabilities    []Capability `json:"capabilities"`
}

type RegistrationResult struct {
	Accepted []CapabilityStatus `json:"accepted,omitempty"`
	Rejected []CapabilityStatus `json:"rejected,omitempty"`
}

type CapabilityStatus struct {
	CapabilityID string `json:"capability_id"`
	Reason       string `json:"reason,omitempty"`
}

type Capability struct {
	Type         CapabilityType         `json:"type"`
	CapabilityID string                 `json:"capability_id"`
	BridgeName   string                 `json:"bridge_name,omitempty"`
	AccountID    string                 `json:"account_id,omitempty"`
	ToolName     string                 `json:"tool_name,omitempty"`
	ProviderName string                 `json:"provider_name,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Parameters   map[string]interface{} `json:"parameters,omitempty"`
	Models       []ProviderModel        `json:"models,omitempty"`
	ConfigSchema map[string]interface{} `json:"config_schema,omitempty"`
	Events       []string               `json:"events,omitempty"`
	Priority     int                    `json:"priority,omitempty"`
}

type ProviderModel struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	ContextWindow     int    `json:"context_window"`
	MaxTokens         int    `json:"max_tokens"`
	SupportsTools     bool   `json:"supports_tools"`
	SupportsStreaming bool   `json:"supports_streaming"`
	SupportsVision    bool   `json:"supports_vision"`
	Reasoning         bool   `json:"reasoning"`
}

type Status string

const (
	StatusConnected    Status = "connected"
	StatusReady        Status = "ready"
	StatusDisconnected Status = "disconnected"
	StatusUnhealthy    Status = "unhealthy"
	StatusRestarting   Status = "restarting"
)

type Plugin struct {
	PluginID     string
	Name         string
	Version      string
	Protocol     ProtocolVersion
	Capabilities []Capability
	Status       Status
	ConnectedAt  time.Time
	LastSeenAt   time.Time
}
