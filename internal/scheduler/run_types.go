package scheduler

type ExecutionStatus string

const (
	ExecutionStatusSuccess ExecutionStatus = "success"
	ExecutionStatusFailed  ExecutionStatus = "failed"
	ExecutionStatusSkipped ExecutionStatus = "skipped"
)

type DeliveryStatus string

const (
	DeliveryStatusUnknown      DeliveryStatus = "unknown"
	DeliveryStatusNotAttempted DeliveryStatus = "not_attempted"
	DeliveryStatusWSDelivered  DeliveryStatus = "ws_delivered"
	DeliveryStatusFailed       DeliveryStatus = "failed"
)

type ScheduleRunRecord struct {
	RunID           string          `json:"run_id"`
	ScheduleID      string          `json:"schedule_id"`
	SessionKey      string          `json:"session_key"`
	TriggerSource   TriggerSource   `json:"trigger_source"`
	StartedAt       int64           `json:"started_at"`
	FinishedAt      int64           `json:"finished_at"`
	ExecutionStatus ExecutionStatus `json:"execution_status"`
	DeliveryStatus  DeliveryStatus  `json:"delivery_status"`
	Error           string          `json:"error,omitempty"`
}
