package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/chawuciren/evoduck/pkg/models"
)

type SleepTool struct{}

func NewSleepTool() *SleepTool {
	return &SleepTool{}
}

func (t *SleepTool) Name() string {
	return "sleep"
}

// IsTimeoutExempt sleep 的语义本身就是按指定时长等待，豁免 Registry 全局兜底
func (t *SleepTool) IsTimeoutExempt() bool {
	return true
}

func (t *SleepTool) Description() string {
	return `Pause execution for a short, explicit delay.

**When to use:**
- You intentionally want to wait before the next tool call
- You started a background process and want to pause before polling again
- You need a simple delay without spawning a shell command

**Do not use when:**
- A shell command itself should keep running; use process instead
- You need interactive command execution; use process instead

**Parameters:**
- seconds: Number of seconds to wait

**Returns:**
- Confirmation that the delay completed or was cancelled`
}

func (t *SleepTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"seconds": map[string]interface{}{
				"type":        "number",
				"description": "Number of seconds to wait",
			},
		},
		"required": []string{"seconds"},
	}
}

func (t *SleepTool) Execute(args map[string]interface{}) (string, error) {
	return t.ExecuteWithRole(context.Background(), args, models.RoleAdmin)
}

func (t *SleepTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	if role != models.RoleEmployee && role != models.RoleAdmin {
		return "", fmt.Errorf("access denied: sleep tool requires employee or admin role")
	}

	raw, ok := args["seconds"].(float64)
	if !ok {
		return "", fmt.Errorf("seconds is required")
	}
	if raw < 0 {
		return "", fmt.Errorf("seconds must be non-negative")
	}

	duration := time.Duration(raw * float64(time.Second))
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-timer.C:
		return fmt.Sprintf("Slept for %.3f seconds", raw), nil
	case <-ctx.Done():
		return "", fmt.Errorf("sleep cancelled: %v", ctx.Err())
	}
}
