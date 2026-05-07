package command

import (
	"context"
	"github.com/chawuciren/evoduck/internal/scheduler"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/gorilla/websocket"
)

// Context 命令执行上下文
type Context struct {
	// 连接信息
	Conn   *websocket.Conn // WebSocket 连接 (用于响应)
	ConnID string          // 连接 ID

	// 会话信息
	SessionKey string           // Session Key
	Session    *session.Session // Session 对象 (可能为 nil)

	// 用户信息
	AgentID string      // 当前 Agent ID
	Role    models.Role // 用户角色
	UserID  string      // 用户 ID

	// 命令信息
	Command string // 原始命令 (如 "/help")
	Name    string // 命令名称 (如 "help")
	Args    string // 命令参数 (如 "status" 或 "")

	// Gateway 引用 (用于访问 Agent Manager, LLM Registry 等)
	Gateway GatewayAccessor // Gateway 访问接口
}

// GatewayAccessor Gateway 访问接口 (避免直接依赖 Gateway 类型)
type GatewayAccessor interface {
	// Agent 相关
	GetAgentManager() AgentManagerAccessor
	GetDefaultAgentID() string

	// Session 相关
	GetSessionManager() SessionManagerAccessor
	GetOrCreateSession(sessionKey string) *session.Session

	// LLM 相关
	GetLLMInfo() (provider string, model string)
	ListLLMProviders(ctx context.Context) ([]LLMProviderInfo, error)

	// 日志相关
	GetLogs(level string, limit int) []LogEntry

	// 记忆系统相关
	GetMemoryStatus(agentID, userID string) *MemoryStatus
	FlushSessionMemory(agentID string, sess *session.Session, role models.Role, userID string) (*MemoryFlushResult, error)
	RunMemoryCuration() (*SystemTaskRunResult, error)
	RunExperienceCuration() (*SystemTaskRunResult, error)
	CompactSession(agentID string, sess *session.Session) (*CompactResult, error)
	ListSchedulerJobs() []SchedulerJobInfo
	ListSchedules(agentID, userID string) []ScheduleInfo
	ListScheduleRuns(agentID, userID, id string, limit int) ([]ScheduleRunInfo, error)
	CreateSchedule(agentID, userID string, role models.Role, req CreateScheduleRequest) (*ScheduleInfo, error)
	SetScheduleEnabled(agentID, userID, id string, enabled bool) error
	DeleteSchedule(agentID, userID, id string) error
	TriggerSchedule(agentID, userID, id string, source scheduler.TriggerSource) error
	SendSessionMessage(ctx context.Context, sessionKey string, content string) (int, error)
	SendSessionOutgoingMessage(ctx context.Context, sessionKey string, outgoing *models.OutgoingMessage) (int, error)
	RunSessionInput(ctx context.Context, agentID, sessionKey, input string) error

	// 其他
	GetStartTime() int64
}

// AgentManagerAccessor Agent Manager 访问接口
type AgentManagerAccessor interface {
	List() []AgentInfo
	Get(id string) (*AgentInfo, error)
}

// AgentInfo Agent 信息
type AgentInfo struct {
	ID        string
	Role      models.Role
	Provider  string
	Model     string
	Workspace string
	Status    string
}

type LLMModelInfo struct {
	ID                string
	Name              string
	ContextWindow     int
	MaxTokens         int
	SupportsTools     bool
	SupportsStreaming bool
	SupportsVision    bool
	Reasoning         bool
}

type LLMProviderInfo struct {
	Name      string
	IsDefault bool
	Models    []LLMModelInfo
	Error     string
}

// SessionManagerAccessor Session Manager 访问接口
type SessionManagerAccessor interface {
	List() []SessionInfo
	Get(key string) (*session.Session, error)
	GetOrCreate(key string) *session.Session
	NewSession(key string) *session.Session
}

// SessionInfo Session 信息
type SessionInfo struct {
	Key          string
	MessageCount int
	UpdatedAt    int64
}

// LogEntry 日志条目
type LogEntry struct {
	Time    int64  // 时间戳
	Level   string // info, warn, error
	Message string // 日志内容
}

// MemoryStatus 记忆系统状态
type MemoryStatus struct {
	MemoryMDEnabled bool
	MemoryMDSize    int
}

// SchedulerJobInfo 调度任务信息
type SchedulerJobInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Scope       string `json:"scope"`
	AgentID     string `json:"agent_id"`
	Schedule    string `json:"schedule"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

type ScheduleInfo struct {
	ID                  string `json:"id"`
	Scope               string `json:"scope"`
	AgentID             string `json:"agent_id"`
	UserID              string `json:"user_id"`
	Name                string `json:"name"`
	Description         string `json:"description"`
	Schedule            string `json:"schedule"`
	Prompt              string `json:"prompt"`
	Enabled             bool   `json:"enabled"`
	LastRunAt           int64  `json:"last_run_at"`
	LastSuccessAt       int64  `json:"last_success_at"`
	LastError           string `json:"last_error"`
	RunCount            int    `json:"run_count"`
	OriginSessionKey    string `json:"origin_session_key"`
	ExecutionSessionKey string `json:"execution_session_key"`
	ConcurrencyPolicy   string `json:"concurrency_policy"`
	LastTriggerSource   string `json:"last_trigger_source"`
}

type ScheduleRunInfo struct {
	RunID           string `json:"run_id"`
	ScheduleID      string `json:"schedule_id"`
	SessionKey      string `json:"session_key"`
	TriggerSource   string `json:"trigger_source"`
	StartedAt       int64  `json:"started_at"`
	FinishedAt      int64  `json:"finished_at"`
	ExecutionStatus string `json:"execution_status"`
	DeliveryStatus  string `json:"delivery_status"`
	Error           string `json:"error"`
}

type CreateScheduleRequest struct {
	Name                string
	Description         string
	Schedule            string
	Prompt              string
	Enabled             *bool
	Channel             string
	OriginSessionKey    string
	ExecutionSessionKey string
}

// MemoryFlushResult 会话记忆同步结果
type MemoryFlushResult struct {
	Flushed        bool
	Skipped        bool
	MediumCount    int
	LongtermCount  int
	SkippedReason  string
	FailureMessage string
}

type SystemTaskRunResult struct {
	ScheduleID  string
	SessionKey  string
	TaskKind    string
	TriggeredBy string
}

type CompactResult struct {
	Compacted       bool
	Skipped         bool
	BeforeMessages  int
	AfterMessages   int
	SummaryInserted bool
	SkippedReason   string
	FailureMessage  string
}

// SchedulerJobInfo 调度任务信息
// Result 命令执行结果
type Result struct {
	Content    string         // 响应内容 (Markdown 格式)
	ActionType string         // 可选后续动作: "new_session", "switch_agent", "clear_ui"
	ActionData map[string]any // 动作参数
	Error      error          // 错误信息 (如果有)
}

// NewResult 创建成功结果
func NewResult(content string) *Result {
	return &Result{
		Content:    content,
		ActionData: make(map[string]any),
	}
}

// NewResultWithAction 创建带动作的结果
func NewResultWithAction(content string, actionType string, actionData map[string]any) *Result {
	return &Result{
		Content:    content,
		ActionType: actionType,
		ActionData: actionData,
	}
}

// NewErrorResult 创建错误结果
func NewErrorResult(err error) *Result {
	return &Result{
		Error: err,
	}
}
