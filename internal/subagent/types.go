package subagent

type Kind string

const (
	KindInternal       Kind = "internal"
	KindExternal       Kind = "external"
	KindSystemInternal Kind = "system_internal"
)

type Status string

const (
	StatusStarting        Status = "starting"
	StatusRunning         Status = "running"
	StatusBlocked         Status = "blocked"
	StatusCompleted       Status = "completed"
	StatusFailed          Status = "failed"
	StatusCancelRequested Status = "cancel_requested"
	StatusCancelled       Status = "cancelled"
	StatusStale           Status = "stale"
)

type Record struct {
	ID                  string            `json:"id"`
	Kind                Kind              `json:"kind"`
	Status              Status            `json:"status"`
	CallerAgentID       string            `json:"caller_agent_id,omitempty"`
	TargetAgentID       string            `json:"target_agent_id,omitempty"`
	UserID              string            `json:"user_id,omitempty"`
	Role                string            `json:"role,omitempty"`
	ParentSessionKey    string            `json:"parent_session_key,omitempty"`
	ExecutionSessionKey string            `json:"execution_session_key,omitempty"`
	Description         string            `json:"description,omitempty"`
	Prompt              string            `json:"prompt,omitempty"`
	CheckerPrompt       string            `json:"checker_prompt,omitempty"`
	WatchScheduleID     string            `json:"watch_schedule_id,omitempty"`
	CreatedAt           int64             `json:"created_at"`
	StartedAt           int64             `json:"started_at,omitempty"`
	UpdatedAt           int64             `json:"updated_at"`
	FinishedAt          int64             `json:"finished_at,omitempty"`
	LastHeartbeatAt     int64             `json:"last_heartbeat_at,omitempty"`
	ProgressSummary     string            `json:"progress_summary,omitempty"`
	ResultSummary       string            `json:"result_summary,omitempty"`
	Error               string            `json:"error,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
}

type StartInternalRequest struct {
	CallerAgentID       string
	TargetAgentID       string
	UserID              string
	Role                string
	ParentSessionKey    string
	Description         string
	Prompt              string
	CheckerPrompt       string
	CheckerSchedule     string
	ExecutionSessionKey string
	Metadata            map[string]string
}

type StartExternalRequest struct {
	CallerAgentID    string
	UserID           string
	Role             string
	ParentSessionKey string
	Provider         string
	Description      string
	Command          string
	WorkingDirectory string
	CheckerPrompt    string
	CheckerSchedule  string
	Metadata         map[string]string
}
