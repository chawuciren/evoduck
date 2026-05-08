package config

import (
	"os"
	"strings"
	"time"
)

type Config struct {
	Gateway      GatewayConfig          `yaml:"gateway"`
	DefaultAgent string                 `yaml:"default_agent"` // 默认 Agent ID
	DataDir      string                 `yaml:"data_dir"`      // 数据目录 (默认: ./data)
	Agents       map[string]AgentConfig `yaml:"agents"`
	Shared       SharedConfig           `yaml:"shared"`
	LLM          LLMConfig              `yaml:"llm"`
	Channels     ChannelsConfig         `yaml:"channels"`
	Plugins      PluginConfig           `yaml:"plugins"`
	Tools        ToolsConfig            `yaml:"tools"`
	Memory       MemoryConfig           `yaml:"memory"`
	Heartbeat    HeartbeatConfig        `yaml:"heartbeat"`
	Scheduler    SchedulerConfig        `yaml:"scheduler"`
	MCP          MCPConfig              `yaml:"mcp"`     // MCP 服务器配置
	Logging      LoggingConfig          `yaml:"logging"` // 日志配置
	Proxy        ProxyConfig            `yaml:"proxy"`   // 代理配置
	Daemon       DaemonConfig           `yaml:"daemon"`  // Daemon 配置
}

// DaemonConfig daemon 进程配置
type DaemonConfig struct {
	ControlPort int `yaml:"control_port"` // daemon 控制端口 (默认: gateway port + 2)
}

// ProxyConfig 代理配置
type ProxyConfig struct {
	Enabled  bool              `yaml:"enabled"`  // 全局开关
	Type     string            `yaml:"type"`     // 默认代理类型: "http" 或 "socks5"
	HTTP     HTTPProxyConfig   `yaml:"http"`     // HTTP 代理配置
	SOCKS5   SOCKS5ProxyConfig `yaml:"socks5"`   // SOCKS5 代理配置
	NoProxy  []string          `yaml:"no_proxy"` // 跳过代理的域名列表
	Controls ProxyControls     `yaml:"controls"` // 精细化控制
}

// HTTPProxyConfig HTTP 代理配置
type HTTPProxyConfig struct {
	URL      string `yaml:"url"`      // 代理 URL
	Username string `yaml:"username"` // 认证用户名
	Password string `yaml:"password"` // 认证密码
}

// SOCKS5ProxyConfig SOCKS5 代理配置
type SOCKS5ProxyConfig struct {
	URL      string `yaml:"url"`      // 代理 URL
	Username string `yaml:"username"` // 认证用户名
	Password string `yaml:"password"` // 认证密码
}

// ProxyControls 精细化代理控制
type ProxyControls struct {
	LLM         ProxyLLMControl         `yaml:"llm"`         // LLM 调用控制
	Channels    ProxyChannelsControl    `yaml:"channels"`    // Channel 连接控制
	Tools       ProxyToolsControl       `yaml:"tools"`       // 内置工具控制
	MCP         ProxyMCPControl         `yaml:"mcp"`         // MCP 控制（工具调用+子进程）
	Plugin      ProxyPluginControl      `yaml:"plugin"`      // Plugin 控制（工具调用+子进程）
	Exec        ProxyExecControl        `yaml:"exec"`        // Exec 命令控制
	Subagents   ProxySubagentsControl   `yaml:"subagents"`   // 子 Agent 控制
	Update      ProxyUpdateControl      `yaml:"update"`      // Update 命令控制
}

// ProxyUpdateControl Update 命令代理控制
type ProxyUpdateControl struct {
	Enabled bool   `yaml:"enabled"` // Update 命令是否走代理
	Type    string `yaml:"type"`    // 可选：指定使用哪种代理类型（默认使用全局配置）
}

// ProxyLLMControl LLM 调用代理控制
type ProxyLLMControl struct {
	Enabled   bool              `yaml:"enabled"`   // LLM 调用是否走代理
	Providers map[string]*bool  `yaml:"providers"` // Provider 级别控制
	Type      string            `yaml:"type"`      // 可选：指定使用哪种代理类型
}

// ProxyChannelsControl Channel 连接代理控制
type ProxyChannelsControl struct {
	Default    bool              `yaml:"default"`     // 默认行为
	PerChannel map[string]*bool  `yaml:"per_channel"` // 按 Channel 控制
	Type       string            `yaml:"type"`        // 可选：指定代理类型
}

// ProxyToolsControl 内置工具代理控制
type ProxyToolsControl struct {
	Enabled  bool              `yaml:"enabled"`   // 内置工具默认是否走代理
	PerTool  map[string]*bool  `yaml:"per_tool"`  // 按工具控制
	Type     string            `yaml:"type"`      // 可选：指定代理类型
}

// ProxyMCPControl MCP 代理控制（同时控制工具调用和子进程启动）
type ProxyMCPControl struct {
	Enabled   bool              `yaml:"enabled"`   // MCP 默认是否走代理（工具调用和子进程共用）
	PerServer map[string]*bool  `yaml:"per_server"` // 按 Server 控制（同时生效于工具调用和子进程）
	Type      string            `yaml:"type"`      // 可选：指定代理类型
}

// ProxyPluginControl Plugin 代理控制（同时控制工具调用和子进程启动）
type ProxyPluginControl struct {
	Enabled   bool              `yaml:"enabled"`   // Plugin 默认是否走代理
	PerPlugin map[string]*bool  `yaml:"per_plugin"` // 按 Plugin 控制（同时生效于工具调用和子进程）
	Type      string            `yaml:"type"`      // 可选：指定代理类型
}

// ProxyExecControl Exec 命令代理控制
type ProxyExecControl struct {
	Enabled    bool              `yaml:"enabled"`     // Exec 默认行为
	PerCommand map[string]*bool  `yaml:"per_command"` // 按命令控制
	Type       string            `yaml:"type"`        // 可选：指定代理类型
}

// ProxySubagentsControl 子 Agent 代理控制
type ProxySubagentsControl struct {
	Internal  ProxyInternalSubagentControl  `yaml:"internal"`  // 内部子 Agent
	External  ProxyExternalSubagentControl  `yaml:"external"`  // 外部子 Agent
}

// ProxyInternalSubagentControl 内部子 Agent 代理控制
type ProxyInternalSubagentControl struct {
	Enabled *bool `yaml:"enabled"` // null 表示继承父进程
}

// ProxyExternalSubagentControl 外部子 Agent 代理控制
type ProxyExternalSubagentControl struct {
	Enabled  bool              `yaml:"enabled"`   // 外部子 Agent 默认
	PerAgent map[string]*bool  `yaml:"per_agent"` // 按 Agent 控制
	Type     string            `yaml:"type"`      // 可选：指定代理类型
}

type GatewayConfig struct {
	Host  string `yaml:"host"`
	Port  int    `yaml:"port"`
	Token string `yaml:"token"`
}

type AgentConfig struct {
	Role          string                `yaml:"role"`
	Workspace     string                `yaml:"workspace"`
	Permissions   AgentPermissionConfig `yaml:"permissions"`
	Provider      string                `yaml:"provider"`
	Model         string                `yaml:"model"`
	UserIsolation UserIsolationConfig   `yaml:"user_isolation"` // 用户隔离配置
	// LLM 参数
	Temperature   *float64 `yaml:"temperature"`    // 温度参数 (0.0-2.0)，nil 表示使用默认值
	MaxTokens     int      `yaml:"max_tokens"`     // 最大生成 token 数
	TopP          *float64 `yaml:"top_p"`          // Top-p 采样
	MaxIterations int      `yaml:"max_iterations"` // 最大循环迭代次数，0 表示使用默认值 (100)
}

type AgentPermissionConfig struct {
	AuthorizedDirectories       []string `yaml:"authorized_directories"`
	AuthorizedTools             []string `yaml:"authorized_tools"`
	AuthorizedSubagents         []string `yaml:"authorized_subagents"`
	AuthorizedExternalSubagents []string `yaml:"authorized_external_subagents"`
}

// UserIsolationConfig 用户隔离配置
type UserIsolationConfig struct {
	AutoCreate  bool `yaml:"auto_create"`  // 自动创建用户目录
	AutoProfile bool `yaml:"auto_profile"` // 自动生成用户画像
}

type SharedConfig struct {
	SkillsDir string `yaml:"skills_dir"`
}

type LLMConfig struct {
	DefaultProvider string                    `yaml:"default_provider"`
	DefaultModel    string                    `yaml:"default_model"`
	Providers       map[string]ProviderConfig `yaml:"providers"`
}

type ProviderModelType string

const (
	ProviderModelTypeChat      ProviderModelType = "chat"
	ProviderModelTypeEmbedding ProviderModelType = "embedding"
	ProviderModelTypeRerank    ProviderModelType = "rerank"
)

type ProviderModelCapabilities struct {
	Vision    bool `yaml:"vision"`
	Reasoning bool `yaml:"reasoning"`
	ToolUse   bool `yaml:"tool_use"`
}

type ProviderModelConfig struct {
	ID              string                    `yaml:"id"`
	Name            string                    `yaml:"name"`
	Type            ProviderModelType         `yaml:"type"`
	Capabilities    ProviderModelCapabilities `yaml:"capabilities"`
	ContextWindow   int                       `yaml:"context_window"`
	MaxOutputTokens int                       `yaml:"max_output_tokens"`
}

type ProviderConfig struct {
	Type                string                 `yaml:"type"`
	BaseURL             string                 `yaml:"base_url"`
	APIKey              string                 `yaml:"api_key"`
	Headers             map[string]string      `yaml:"headers"`
	DefaultModel        string                 `yaml:"default_model"`
	Models              []ProviderModelConfig  `yaml:"models"`
	ToolChoice          string                 `yaml:"tool_choice"`
	Thinking            *ThinkingConfig        `yaml:"thinking"`
	ParallelToolCalls   *bool                  `yaml:"parallel_tool_calls"`
	ResponseFormat      map[string]interface{} `yaml:"response_format"`
	Stop                []string               `yaml:"stop"`
	PresencePenalty     *float64               `yaml:"presence_penalty"`
	FrequencyPenalty    *float64               `yaml:"frequency_penalty"`
	MaxCompletionTokens int                    `yaml:"max_completion_tokens"`
	ReasoningEffort     string                 `yaml:"reasoning_effort"`
	ReasoningReplay     string                 `yaml:"reasoning_replay"`
	Verbosity           string                 `yaml:"verbosity"`
	User                string                 `yaml:"user"`
	UserID              string                 `yaml:"user_id"`
	SafetyIdentifier    string                 `yaml:"safety_identifier"`
	ServiceTier         string                 `yaml:"service_tier"`
	N                   int                    `yaml:"n"`
	Seed                *int                   `yaml:"seed"`
	LogProbs            bool                   `yaml:"logprobs"`
	TopLogProbs         int                    `yaml:"top_logprobs"`
	Store               *bool                  `yaml:"store"`
	IncludeUsage        *bool                  `yaml:"include_usage"`
	Metadata            map[string]string      `yaml:"metadata"`
	ChatTemplateKwargs  map[string]interface{} `yaml:"chat_template_kwargs"`
}

type ThinkingConfig struct {
	Type string `yaml:"type"`
}

type ChannelsConfig map[string]ChannelConfig

type ChannelConfig struct {
	Type  string `yaml:"type"`  // Channel type: wecom, weixin, or plugin-defined. 'webchat' is reserved for the gateway web layer.
	Name  string `yaml:"name"`  // Channel display name
	Role  string `yaml:"role"`  // Role: admin, employee, customer
	Agent string `yaml:"agent"` // Bound agent ID

	// Weixin (personal WeChat via QR login)
	Token      string `yaml:"token"`        // Channel token (from QR login)
	UserID     string `yaml:"user_id"`      // User ID for this WeChat account
	APIBaseURL string `yaml:"api_base_url"` // API base URL (from QR login)

	// WeCom (AI Bot via WebSocket)
	BotID  string `yaml:"bot_id"` // AI Bot ID
	Secret string `yaml:"secret"` // AI Bot Secret for WebSocket authentication
}

type PluginConfig struct {
	WSServer WSServerConfig       `yaml:"ws_server"`
	Plugins  map[string]PluginDef `yaml:"plugins"`
}

type WSServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type PluginDef struct {
	Enabled        bool                     `yaml:"enabled"`
	Type           string                   `yaml:"type"`
	Command        []string                 `yaml:"command"`
	Environment    map[string]string        `yaml:"environment"`
	URL            string                   `yaml:"url"`
	Token          string                   `yaml:"token"`
	Restart        string                   `yaml:"restart"`
	RestartDelay   int                      `yaml:"restart_delay"`
	MaxRestarts    int                      `yaml:"max_restarts"`
	Override       bool                     `yaml:"override"`
	Capabilities   PluginCapabilitiesConfig `yaml:"capabilities"`
	ConnectTimeout int                      `yaml:"connect_timeout"`
	RequestTimeout int                      `yaml:"request_timeout"`
}

type PluginCapabilitiesConfig struct {
	Allow []string `yaml:"allow"`
}

type ToolsConfig struct {
	BackendCall BackendCallConfig `yaml:"backend_call"`
	Session     SessionToolConfig `yaml:"session"`
}

type SessionToolConfig struct {
	Enabled    bool                    `yaml:"enabled"`
	Visibility SessionVisibilityConfig `yaml:"visibility"`
	Allow      SessionAllowConfig      `yaml:"allow"`
}

type SessionVisibilityConfig struct {
	Employee string `yaml:"employee"`
	Customer string `yaml:"customer"`
}

type SessionAllowConfig struct {
	Employee []string `yaml:"employee"`
	Customer []string `yaml:"customer"`
}

type BackendCallConfig struct {
	Endpoints map[string]EndpointConfig `yaml:"endpoints"`
}

type EndpointConfig struct {
	URL          string        `yaml:"url"`
	Method       string        `yaml:"method"`
	Auth         EndpointAuth  `yaml:"auth"`
	AllowedRoles []string      `yaml:"allowed_roles"`
	RateLimit    int           `yaml:"rate_limit"`
	Timeout      time.Duration `yaml:"timeout"`
}

type EndpointAuth struct {
	Type   string `yaml:"type"`
	Token  string `yaml:"token"`
	Header string `yaml:"header"`
}

type MemoryConfig struct {
	ShortTerm  ShortTermConfig  `yaml:"short_term"`
	MediumTerm MediumTermConfig `yaml:"medium_term"`
	LongTerm   LongTermConfig   `yaml:"long_term"`
	CoreMemory CoreMemoryConfig `yaml:"core_memory"` // 关键记忆
	Bootstrap  BootstrapConfig  `yaml:"bootstrap"`   // Bootstrap 文件长度限制
}

// BootstrapConfig Bootstrap 文件长度限制配置
// Bootstrap 指的是每次对话开始时自动注入到 prompt 中的文件
// 如 AGENTS.md、SOUL.md、user USER.md 等
type BootstrapConfig struct {
	MaxFileChars       int     `yaml:"max_file_chars"`      // 单文件最大字符数（默认 20000）
	MaxTotalChars      int     `yaml:"max_total_chars"`     // 总计最大字符数（默认 150000）
	WarningThreshold   float64 `yaml:"warning_threshold"`   // 警告阈值（默认 0.8，达到 80% 时警告）
	TruncationStrategy string  `yaml:"truncation_strategy"` // 截断策略：head（保留开头）或 tail（保留结尾）
}

type ShortTermConfig struct {
	MaxMessages        int           `yaml:"max_messages"`
	MaxTokens          int           `yaml:"max_tokens"`
	KeepRecent         int           `yaml:"keep_recent"`
	SessionTTL         time.Duration `yaml:"session_ttl"`
	CleanupInterval    time.Duration `yaml:"cleanup_interval"`
	FlushBeforeCompact bool          `yaml:"flush_before_compact"` // 压缩前自动保存关键信息
}

type MediumTermConfig struct {
	Dir                  string `yaml:"dir"`
	MaxSize              int    `yaml:"max_size"`
	LoadDays             int    `yaml:"load_days"`               // prompt 加载最近 N 天
	MinMessagesToExtract int    `yaml:"min_messages_to_extract"` // 最少消息数才触发提取
	CompressionThreshold int    `yaml:"compression_threshold"`   // 触发压缩的总字符数阈值
}

type LongTermConfig struct {
	Vector               VectorConfig        `yaml:"vector"`
	DedupThreshold       float64             `yaml:"dedup_threshold"`       // 去重相似度阈值
	CleanupPolicy        CleanupPolicyConfig `yaml:"cleanup_policy"`        // 清理策略
	CompressionThreshold int                 `yaml:"compression_threshold"` // 触发压缩的总字符数阈值
}

type VectorConfig struct {
	Enabled        bool           `yaml:"enabled"`
	Embedder       EmbedderConfig `yaml:"embedder"`
	PrefetchLimit  int            `yaml:"prefetch_limit"`
	ScoreThreshold float64        `yaml:"score_threshold"`
}

type EmbedderConfig struct {
	Type       string `yaml:"type"`       // "openai" | "local" | "ollama"
	Model      string `yaml:"model"`      // text-embedding-3-small, etc.
	Dimensions int    `yaml:"dimensions"` // 向量维度

	// 可选：独立的 API 配置（不配置则使用 LLM Provider 的配置）
	APIKey  string `yaml:"api_key"`  // 独立的 API Key
	BaseURL string `yaml:"base_url"` // 独立的 Base URL
}

type CleanupPolicyConfig struct {
	CheckInterval time.Duration   `yaml:"check_interval"` // 检查间隔
	MinAgeDays    int             `yaml:"min_age_days"`   // 最少存在天数
	BatchSize     int             `yaml:"batch_size"`     // 批量大小
	Reference     ReferenceConfig `yaml:"reference"`      // 参考信息
}

type ReferenceConfig struct {
	MediumMemoryDays   int  `yaml:"medium_memory_days"`   // 参考中期记忆天数
	IncludeCoreMemory  bool `yaml:"include_core_memory"`  // 包含关键记忆
	IncludeAccessStats bool `yaml:"include_access_stats"` // 包含访问统计
}

type CoreMemoryConfig struct {
	File                string  `yaml:"file"`                 // MEMORY.md
	AutoConsolidate     bool    `yaml:"auto_consolidate"`     // 自动筛选
	ImportanceThreshold float64 `yaml:"importance_threshold"` // 重要性阈值
}

type HeartbeatConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"`
	Prompt   string        `yaml:"prompt"`
}

type SchedulerConfig struct {
	SystemTasks SystemSchedulerTasksConfig `yaml:"system_tasks"`
}

type SystemSchedulerTasksConfig struct {
	MemoryCuration     SystemTaskConfig `yaml:"memory_curation"`
	ExperienceCuration SystemTaskConfig `yaml:"experience_curation"`
}

type SystemTaskConfig struct {
	Schedule string `yaml:"schedule"`
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Level    string `yaml:"level" env:"LOG_LEVEL"` // 日志级别: DEBUG, INFO, WARN, ERROR
	JSONMode bool   `yaml:"json_mode"`             // true=JSON格式, false=彩色文本格式
	Color    bool   `yaml:"color"`                 // 是否启用颜色输出 (仅文本格式有效)
}

// MCPConfig MCP 服务器配置
type MCPConfig struct {
	Servers map[string]MCPServerConfig `yaml:"servers"` // MCP 服务器列表
}

// MCPServerConfig MCP 服务器配置
type MCPServerConfig struct {
	Type        string            `yaml:"type"`        // "local" 或 "remote"
	Enabled     bool              `yaml:"enabled"`     // 是否启用
	Command     []string          `yaml:"command"`     // local: 启动命令
	Environment map[string]string `yaml:"environment"` // local: 环境变量
	URL         string            `yaml:"url"`         // remote: 服务器 URL
	Headers     map[string]string `yaml:"headers"`     // remote: HTTP 头
	Timeout     int               `yaml:"timeout"`     // 超时时间（毫秒）
}

func Load(path string) (*Config, error) {
	paths, err := EnsureInitialized(path)
	if err != nil {
		return nil, err
	}
	return loadConfigFromDisk(paths.ConfigPath, paths)
}

func expandEnv(cfg *Config) {
	cfg.Gateway.Token = expand(cfg.Gateway.Token)
	cfg.LLM.DefaultProvider = expand(cfg.LLM.DefaultProvider)
	cfg.LLM.DefaultModel = expand(cfg.LLM.DefaultModel)
	for k, p := range cfg.LLM.Providers {
		p.APIKey = expand(p.APIKey)
		p.BaseURL = expand(p.BaseURL)
		for header, value := range p.Headers {
			p.Headers[header] = expand(value)
		}
		p.DefaultModel = expand(p.DefaultModel)
		for i := range p.Models {
			p.Models[i].ID = expand(p.Models[i].ID)
			p.Models[i].Name = expand(p.Models[i].Name)
		}
		cfg.LLM.Providers[k] = p
	}
	for k, ch := range cfg.Channels {
		ch.Token = expand(ch.Token)
		cfg.Channels[k] = ch
	}
	for name, plugin := range cfg.Plugins.Plugins {
		plugin.URL = expand(plugin.URL)
		plugin.Token = expand(plugin.Token)
		for k, v := range plugin.Environment {
			plugin.Environment[k] = expand(v)
		}
		cfg.Plugins.Plugins[name] = plugin
	}
	for k, ep := range cfg.Tools.BackendCall.Endpoints {
		ep.Auth.Token = expand(ep.Auth.Token)
		cfg.Tools.BackendCall.Endpoints[k] = ep
	}
	// Embedder API Key（如果配置）
	cfg.Memory.LongTerm.Vector.Embedder.APIKey = expand(cfg.Memory.LongTerm.Vector.Embedder.APIKey)
	cfg.Memory.LongTerm.Vector.Embedder.BaseURL = expand(cfg.Memory.LongTerm.Vector.Embedder.BaseURL)

	// MCP Headers 环境变量展开
	for name, server := range cfg.MCP.Servers {
		for k, v := range server.Headers {
			server.Headers[k] = expand(v)
		}
		cfg.MCP.Servers[name] = server
	}

	// 日志级别环境变量支持（LOG_LEVEL 可覆盖配置文件）
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		cfg.Logging.Level = envLevel
	}
	// JSON 模式环境变量支持
	if envJSON := os.Getenv("LOG_JSON_MODE"); envJSON != "" {
		cfg.Logging.JSONMode = envJSON == "true" || envJSON == "1"
	}
	// 颜色输出环境变量支持
	// LOG_COLOR=true 强制启用颜色（用于 Windows Terminal 等支持 ANSI 的环境）
	// LOG_COLOR 未设置或 false → 由 logger 包自动检测环境
	if envColor := os.Getenv("LOG_COLOR"); envColor == "true" || envColor == "1" {
		cfg.Logging.Color = true
	}
}

func expand(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		return os.Getenv(s[2 : len(s)-1])
	}
	return s
}

func resolveAgentLLM(providerName, modelName string, cfg *Config) (string, string) {
	resolvedProvider := providerName
	if resolvedProvider == "" {
		resolvedProvider = cfg.LLM.DefaultProvider
	}

	providerCfg, ok := cfg.LLM.Providers[resolvedProvider]
	if !ok {
		return resolvedProvider, modelName
	}

	resolvedModel := modelName
	if resolvedModel == "" {
		resolvedModel = providerCfg.DefaultModel
	}
	if resolvedModel == "" && resolvedProvider == cfg.LLM.DefaultProvider {
		resolvedModel = cfg.LLM.DefaultModel
	}
	if resolvedModel == "" {
		resolvedModel = firstProviderModelID(providerCfg.Models)
	}

	return resolvedProvider, resolvedModel
}

func applyAgentLLMDefaults(cfg *Config) {
	for id, agent := range cfg.Agents {
		resolvedProvider, resolvedModel := resolveAgentLLM(agent.Provider, agent.Model, cfg)
		agent.Provider = resolvedProvider
		agent.Model = resolvedModel
		cfg.Agents[id] = agent
	}
}

func normalizeProviderModelType(modelType ProviderModelType) ProviderModelType {
	switch strings.ToLower(strings.TrimSpace(string(modelType))) {
	case "", string(ProviderModelTypeChat):
		return ProviderModelTypeChat
	case string(ProviderModelTypeEmbedding):
		return ProviderModelTypeEmbedding
	case string(ProviderModelTypeRerank):
		return ProviderModelTypeRerank
	default:
		return modelType
	}
}

func firstProviderModelID(models []ProviderModelConfig) string {
	for _, model := range models {
		if id := strings.TrimSpace(model.ID); id != "" {
			return id
		}
	}
	return ""
}

func providerHasModel(models []ProviderModelConfig, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, model := range models {
		if strings.TrimSpace(model.ID) == target {
			return true
		}
	}
	return false
}

const defaultAgentMaxIterations = 100

func setDefaults(cfg *Config, defaultDataDir string) {
	if cfg.DataDir == "" {
		cfg.DataDir = defaultDataDir
	}
	if cfg.Gateway.Host == "" {
		cfg.Gateway.Host = "127.0.0.1"
	}
	if cfg.Gateway.Port == 0 {
		cfg.Gateway.Port = 18789
	}
	if cfg.Plugins.WSServer.Host == "" {
		cfg.Plugins.WSServer.Host = "127.0.0.1"
	}
	if cfg.Plugins.WSServer.Port == 0 {
		cfg.Plugins.WSServer.Port = 19000
	}
	if cfg.Memory.ShortTerm.MaxMessages == 0 {
		cfg.Memory.ShortTerm.MaxMessages = 200
	}
	if cfg.Memory.ShortTerm.MaxTokens == 0 {
		cfg.Memory.ShortTerm.MaxTokens = 128000
	}
	if cfg.Memory.ShortTerm.KeepRecent == 0 {
		cfg.Memory.ShortTerm.KeepRecent = 10
	}
	if cfg.Memory.ShortTerm.SessionTTL == 0 {
		cfg.Memory.ShortTerm.SessionTTL = 7 * 24 * time.Hour
	}
	if cfg.Memory.ShortTerm.CleanupInterval == 0 {
		cfg.Memory.ShortTerm.CleanupInterval = 1 * time.Hour
	}
	if cfg.Memory.MediumTerm.Dir == "" {
		cfg.Memory.MediumTerm.Dir = "memory"
	}
	if cfg.Memory.MediumTerm.MaxSize == 0 {
		cfg.Memory.MediumTerm.MaxSize = 5000
	}
	if cfg.Memory.MediumTerm.LoadDays == 0 {
		cfg.Memory.MediumTerm.LoadDays = 7
	}
	if cfg.Memory.MediumTerm.MinMessagesToExtract == 0 {
		cfg.Memory.MediumTerm.MinMessagesToExtract = 5
	}
	if cfg.Memory.MediumTerm.CompressionThreshold == 0 {
		cfg.Memory.MediumTerm.CompressionThreshold = 10000 // 1万字符触发压缩
	}
	if cfg.Memory.LongTerm.Vector.PrefetchLimit == 0 {
		cfg.Memory.LongTerm.Vector.PrefetchLimit = 5
	}
	if cfg.Memory.LongTerm.Vector.ScoreThreshold == 0 {
		cfg.Memory.LongTerm.Vector.ScoreThreshold = 0.7
	}
	if cfg.Memory.LongTerm.DedupThreshold == 0 {
		cfg.Memory.LongTerm.DedupThreshold = 0.95
	}
	if cfg.Memory.LongTerm.CleanupPolicy.CheckInterval == 0 {
		cfg.Memory.LongTerm.CleanupPolicy.CheckInterval = 24 * time.Hour
	}
	if cfg.Memory.LongTerm.CleanupPolicy.MinAgeDays == 0 {
		cfg.Memory.LongTerm.CleanupPolicy.MinAgeDays = 30
	}
	if cfg.Memory.LongTerm.CleanupPolicy.BatchSize == 0 {
		cfg.Memory.LongTerm.CleanupPolicy.BatchSize = 30
	}
	if cfg.Memory.LongTerm.CleanupPolicy.Reference.MediumMemoryDays == 0 {
		cfg.Memory.LongTerm.CleanupPolicy.Reference.MediumMemoryDays = 7
	}
	if cfg.Memory.LongTerm.CompressionThreshold == 0 {
		cfg.Memory.LongTerm.CompressionThreshold = 15000 // 1.5万字符触发压缩
	}
	if cfg.Memory.CoreMemory.File == "" {
		cfg.Memory.CoreMemory.File = "MEMORY.md"
	}
	if cfg.Memory.CoreMemory.ImportanceThreshold == 0 {
		cfg.Memory.CoreMemory.ImportanceThreshold = 0.9
	}
	if cfg.Heartbeat.Interval == 0 {
		cfg.Heartbeat.Interval = 30 * time.Minute
	}
	if cfg.Scheduler.SystemTasks.MemoryCuration.Schedule == "" {
		cfg.Scheduler.SystemTasks.MemoryCuration.Schedule = "0 */3 * * *"
	}
	if cfg.Scheduler.SystemTasks.ExperienceCuration.Schedule == "" {
		cfg.Scheduler.SystemTasks.ExperienceCuration.Schedule = "0 3 * * *"
	}
	// Bootstrap 默认值
	if cfg.Memory.Bootstrap.MaxFileChars == 0 {
		cfg.Memory.Bootstrap.MaxFileChars = 20000 // 2万字符
	}
	if cfg.Memory.Bootstrap.MaxTotalChars == 0 {
		cfg.Memory.Bootstrap.MaxTotalChars = 150000 // 15万字符
	}
	if cfg.Memory.Bootstrap.WarningThreshold == 0 {
		cfg.Memory.Bootstrap.WarningThreshold = 0.8 // 80% 时警告
	}
	if cfg.Memory.Bootstrap.TruncationStrategy == "" {
		cfg.Memory.Bootstrap.TruncationStrategy = "head" // 默认保留开头
	}

	// 为每个 Agent 设置默认值
	for id, agent := range cfg.Agents {
		modified := false
		if agent.MaxIterations <= 0 {
			agent.MaxIterations = defaultAgentMaxIterations
			modified = true
		}
		// AutoCreate 默认启用
		if !agent.UserIsolation.AutoCreate {
			agent.UserIsolation.AutoCreate = true
			modified = true
		}
		// AutoProfile 默认启用
		if !agent.UserIsolation.AutoProfile {
			agent.UserIsolation.AutoProfile = true
			modified = true
		}
		if modified {
			cfg.Agents[id] = agent
		}
	}

	// Logging 默认值
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "INFO"
	}
	// Color 不在配置文件中设置默认值，由 logger 包根据环境自动检测
	// 这样在 Windows CMD 下会自动禁用颜色，Windows Terminal 下会启用

	for name, provider := range cfg.LLM.Providers {
		for i := range provider.Models {
			provider.Models[i].ID = strings.TrimSpace(provider.Models[i].ID)
			provider.Models[i].Name = strings.TrimSpace(provider.Models[i].Name)
			provider.Models[i].Type = normalizeProviderModelType(provider.Models[i].Type)
			if provider.Models[i].Name == "" {
				provider.Models[i].Name = provider.Models[i].ID
			}
		}
		if provider.DefaultModel == "" {
			provider.DefaultModel = firstProviderModelID(provider.Models)
		}
		cfg.LLM.Providers[name] = provider
	}
	if cfg.LLM.DefaultProvider == "" {
		cfg.LLM.DefaultProvider = "openai"
	}
	if cfg.LLM.DefaultModel == "" {
		if provider, ok := cfg.LLM.Providers[cfg.LLM.DefaultProvider]; ok {
			cfg.LLM.DefaultModel = provider.DefaultModel
		}
	}

	applyAgentLLMDefaults(cfg)
}
