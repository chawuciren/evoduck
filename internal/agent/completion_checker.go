package agent

import (
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
)

// CompletionChecker 检查 Agent 执行是否应该停止
//
// 改进后的架构：
// - 不再依赖 ToolCallHistory 做 loop 检测（LLM 自主管理计划，不需要事后检测）
// - 仅检查：子任务完成状态 + 连续错误 + 迭代限制
type CompletionChecker struct{}

// NewCompletionChecker 创建完成检查器
func NewCompletionChecker() *CompletionChecker {
	return &CompletionChecker{}
}

var ccLog = logger.NewModuleLogger("completion")

// Check 检查是否应该停止执行
// 返回 (false, "") 继续, (true, "reason") 停止
func (c *CompletionChecker) Check(
	plan *models.TaskPlan,
	iteration, maxIterations int,
	recentErrors int,
) (shouldStop bool, reason string) {
	// Check 1: 所有子任务已完成 (done 或 skipped)
	if plan != nil && c.allSubtasksCompleted(plan) {
		ccLog.Info("all subtasks completed", logger.Fields{
			"intent":        plan.Intent,
			"subtask_count": len(plan.SubTasks),
		})
		return true, "all subtasks completed"
	}

	// Check 2: 连续 3 次空结果或错误
	if recentErrors >= 3 {
		ccLog.Warn("too many consecutive errors", logger.Fields{
			"recent_errors": recentErrors,
		})
		return true, "consecutive errors limit reached (3)"
	}

	// Check 3: 达到迭代限制
	if iteration >= maxIterations {
		ccLog.Info("iteration limit reached", logger.Fields{
			"iteration":      iteration,
			"max_iterations": maxIterations,
		})
		// 注意：这里不强制停止，让 Runtime 做兜底总结
		// 返回 false，让 Runtime 的自然循环结束
	}

	return false, ""
}

// allSubtasksCompleted 检查所有子任务是否已完成
func (c *CompletionChecker) allSubtasksCompleted(plan *models.TaskPlan) bool {
	if len(plan.SubTasks) == 0 {
		ccLog.Debug("no subtasks to check", nil)
		return false
	}

	done := 0
	pending := 0
	running := 0
	for _, subtask := range plan.SubTasks {
		switch subtask.Status {
		case "done", "skipped":
			done++
		case "running":
			running++
		default:
			pending++
		}
	}

	ccLog.Debug("subtask status check", logger.Fields{
		"total":   len(plan.SubTasks),
		"done":    done,
		"running": running,
		"pending": pending,
	})

	for _, subtask := range plan.SubTasks {
		if subtask.Status != "done" && subtask.Status != "skipped" {
			return false
		}
	}

	return true
}
