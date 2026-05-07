package agent

import (
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
)

// 模块日志器
var tpLog = logger.NewModuleLogger("task-planner")

// TaskPlanner 任务规划器（LLM 内生管理）
//
// 改进后的架构：
// - 不再事后反推（AnalyzeFromCalls/UpdatePlan 已废弃）
// - LLM 通过 task_plan 工具主动管理 todo list
// - ApplyPlan 全量替换 SubTasks（简单一致，由 LLM 保证一致性）
// - max_iterations 由 config.yaml 控制，不再在 TaskPlan 中记录
type TaskPlanner struct{}


// NewTaskPlanner 创建任务规划器
func NewTaskPlanner() *TaskPlanner {
	return &TaskPlanner{}
}

// ApplyPlan 应用全量任务计划（由 task_plan 工具调用）
//
// 全量替换策略：直接覆盖 SubTasks 数组，不做 diff/merge。
// LLM 负责传入完整、一致的最新子任务列表。
func (tp *TaskPlanner) ApplyPlan(intent string, rawSubTasks []map[string]interface{}) *models.TaskPlan {
	tpLog.Info("Applying task plan", logger.Fields{
		"intent":        intent,
		"subtask_count": len(rawSubTasks),
	})

	var subTasks []models.SubTask
	for _, raw := range rawSubTasks {
		st := models.SubTask{
			Name:   getStringField(raw, "name", "Unnamed Task"),
			Status: getStringField(raw, "status", "pending"),
			Type:   getStringField(raw, "type", "chat"),
		}

		// description 可选
		if desc, ok := raw["description"].(string); ok {
			st.Description = desc
		}

		subTasks = append(subTasks, st)
	}

	plan := &models.TaskPlan{
		Intent:    intent,
		SubTasks:  subTasks,
	}

	tpLog.Info("Task plan applied", logger.Fields{
		"intent":         intent,
		"subtask_count":  len(subTasks),
	})

	return plan
}

// getStringField 安全获取字符串字段（带默认值）
func getStringField(m map[string]interface{}, key, defaultVal string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return defaultVal
}
