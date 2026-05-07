package scheduler

import (
	"time"

	cronpkg "github.com/chawuciren/evoduck/internal/cron"
	"github.com/chawuciren/evoduck/pkg/models"
)

type ScheduleScope = cronpkg.JobScope

const (
	ScheduleScopeSystem = cronpkg.JobScopeSystem
	ScheduleScopeAgent  = cronpkg.JobScopeAgent
	ScheduleScopeUser   = cronpkg.JobScopeUser
)

type ConcurrencyPolicy string

const (
	ConcurrencyPolicySkipIfRunning ConcurrencyPolicy = "skip_if_running"
)

type TriggerSource string

const (
	TriggerSourceCron   TriggerSource = "cron"
	TriggerSourceManual TriggerSource = "manual"
	TriggerSourceTool   TriggerSource = "tool"
)

type ScheduleRecord struct {
	ID            string            `json:"id"`
	Scope         ScheduleScope     `json:"scope"`
	AgentID       string            `json:"agent_id,omitempty"`
	UserID        string            `json:"user_id,omitempty"`
	Role          models.Role       `json:"role,omitempty"`
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	Schedule      string            `json:"schedule"`
	Prompt        string            `json:"prompt,omitempty"`
	Channel       string            `json:"channel,omitempty"`
	OriginSessionKey    string         `json:"origin_session_key,omitempty"`
	ExecutionSessionKey string         `json:"execution_session_key,omitempty"`
	ConcurrencyPolicy   ConcurrencyPolicy `json:"concurrency_policy,omitempty"`
	Enabled       bool              `json:"enabled"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	CreatedAt     int64             `json:"created_at"`
	UpdatedAt     int64             `json:"updated_at"`
	LastRunAt     int64             `json:"last_run_at,omitempty"`
	LastSuccessAt int64             `json:"last_success_at,omitempty"`
	LastError     string            `json:"last_error,omitempty"`
	RunCount      int               `json:"run_count"`
	LastTriggerSource TriggerSource `json:"last_trigger_source,omitempty"`
}

func (r *ScheduleRecord) TouchCreated(now time.Time) {
	ts := now.Unix()
	if r.CreatedAt == 0 {
		r.CreatedAt = ts
	}
	r.UpdatedAt = ts
}

func (r *ScheduleRecord) MarkRunSuccess(now time.Time, source TriggerSource) {
	ts := now.Unix()
	r.LastRunAt = ts
	r.LastSuccessAt = ts
	r.LastError = ""
	r.RunCount++
	r.LastTriggerSource = source
	r.UpdatedAt = ts
}

func (r *ScheduleRecord) MarkRunFailure(now time.Time, source TriggerSource, err error) {
	ts := now.Unix()
	r.LastRunAt = ts
	if err != nil {
		r.LastError = err.Error()
	}
	r.RunCount++
	r.LastTriggerSource = source
	r.UpdatedAt = ts
}

type ExecutionRequest struct {
	Schedule      ScheduleRecord
	TriggerSource TriggerSource
	DeliveryStatus DeliveryStatus
}

type Executor interface {
	Execute(req *ExecutionRequest) error
}
