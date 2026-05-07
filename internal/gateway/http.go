package gateway

// AgentInfo 用于 API/WS 响应的 Agent 信息
type AgentInfo struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Workspace string `json:"workspace"`
	Status    string `json:"status"`
}

// SkillInfo 用于 API/WS 响应的 Skill 信息
type SkillInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Role        string   `json:"role"`
	Tags        []string `json:"tags"`
}

// SkillDetailInfo 用于 API/WS 响应的 Skill 详情
type SkillDetailInfo struct {
	Name             string                   `json:"name"`
	Description      string                   `json:"description"`
	License          string                   `json:"license,omitempty"`
	Compatibility    []string                 `json:"compatibility,omitempty"`
	Metadata         map[string]interface{}   `json:"metadata,omitempty"`
	Role             string                   `json:"role"`
	Tags             []string                 `json:"tags"`
	Location         string                   `json:"location"`
	DeprecatedFields []string                 `json:"deprecated_fields,omitempty"`
	Parameters       []map[string]interface{} `json:"parameters"`
	Content          string                   `json:"content"`
}

// SettingsInfo 用于 API/WS 响应的配置信息
type SettingsInfo struct {
	Gateway GatewaySettings `json:"gateway"`
	LLM     LLMSettings     `json:"llm"`
	System  SystemSettings  `json:"system"`
}

type GatewaySettings struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type LLMSettings struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type SystemSettings struct {
	Version string `json:"version"`
	Uptime  string `json:"uptime"`
}

// LogEntry 用于 API/WS 响应的日志条目
type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}
