package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PlanApplier 任务计划应用器接口（由 Runtime 实现）
type PlanApplier interface {
	ApplyPlan(intent string, subTasks []map[string]interface{}) error
}

// TaskPlanTool 任务规划工具（LLM 内生管理 todo list）
//
// 参考 OpenCode 的 todowrite 机制：
// - LLM 主动调用此工具来创建/更新任务计划
// - 全量替换策略：每次调用时传入完整的最新 subTasks 数组
// - 后端直接覆盖旧的 SubTasks，无需 diff/merge
// - completion_checker 基于 SubTask.Status 判断是否提前退出
type TaskPlanTool struct {
	applier PlanApplier
}

func NewTaskPlanTool(applier PlanApplier) *TaskPlanTool {
	return &TaskPlanTool{
		applier: applier,
	}
}

func (t *TaskPlanTool) Name() string {
	return "task_plan"
}

func (t *TaskPlanTool) Description() string {
	return `Create or update your task plan (todo list).

## When to Use

Use this tool by default for work that is not obviously single-step.

- **Direct answer / obviously one non-task_plan tool call**: you may execute directly without this tool
- **Anything else**: default to this tool early
- **Two or more expected non-task_plan tool calls**: use this tool
- **Always default to this tool** for investigation, exploration, multi-file analysis, read/search/analyze/fix, or search/read/modify/test workflows

## Preferred Workflow

1. If the task is not obviously single-step, call task_plan early
2. You may do ONE lightweight probe before the first plan when needed to discover scope:
   - one grep/search
   - one read
   - one list/query call
3. After that probe, if the task is anything more than obviously single-step, call task_plan before continuing
4. Update task_plan later when scope changes, new subtasks are discovered, or your understanding improves

## Progress Protection

1. Always include ALL existing subtasks when updating the plan
2. Keep completed progress:
   - ALWAYS include previous done/skipped/running/pending tasks
   - Add new subtasks below the existing ones when needed
   - Do not remove tasks just because they failed or became irrelevant; use status="skipped" instead
3. Mark a subtask as "done" immediately after completing it

**NEVER reset completed tasks back to "pending" or "running".**

If a subtask is marked "done", it STAYS "done". Even if:
- The next step fails
- You need a different approach
- You discover more work later

**Do NOT:**
✗ Restart the whole plan from scratch
✗ Turn completed tasks back into pending work
✗ Forget progress already made

**Do INSTEAD:**
✓ Keep done tasks as done
✓ Mark blocked work as skipped when appropriate
✓ Add a NEW subtask for the next approach if needed
✓ Refine the plan as understanding improves

## Status Rules

- Only ONE subtask may be "running" at a time
- You may have multiple "pending" subtasks
- "done" and "skipped" count as completed

## SubTask Status Values

- "pending"     : Not yet started
- "running"     : Currently working on it
- "done"        : Completed successfully
- "skipped"     : Intentionally skipped (keep in list, don't remove)
` + `
## Parameters

- intent: Brief summary of the user's overall goal (1-2 sentences)
- sub_tasks: Complete array of ALL subtasks (existing + new, never remove)

Each subtask object must have:
- name: Short task name (2-5 words)
- description: What this subtask accomplishes
- type: Category ("search", "research", "coding", "short", "chat")
- status: Current status ("pending", "running", "done", "skipped")

## Example — Probe Then Plan

task_plan(
  intent="User wants root-cause analysis for a bug in the task planner",
  sub_tasks=[
    {name: "Read planner files", description: "Inspect the task planner and prompt rules after an initial probe", type: "research", status: "running"},
    {name: "Trace root cause", description: "Identify why planning is skipped", type: "research", status: "pending"},
    {name: "Summarize findings", description: "Explain the root cause and impact", type: "chat", status: "pending"}
  ]
)

## Example — Code Investigation / Edit Workflow

task_plan(
  intent="User wants a multi-file fix for task planning behavior",
  sub_tasks=[
    {name: "Search relevant code", description: "Find prompt and task-plan definitions", type: "search", status: "done"},
    {name: "Read target files", description: "Inspect prompt.go and task_plan.go", type: "research", status: "running"},
    {name: "Update prompt rules", description: "Rewrite planning guidance and examples", type: "coding", status: "pending"},
    {name: "Run verification", description: "Build or test the changed packages", type: "coding", status: "pending"}
  ]
)

## Example — Update Plan After Discovering More Scope

task_plan(
  intent="User wants a multi-file fix for task planning behavior",
  sub_tasks=[
    {name: "Search relevant code", description: "Find prompt and task-plan definitions", type: "search", status: "done"},
    {name: "Read target files", description: "Inspect prompt.go and task_plan.go", type: "research", status: "done"},
    {name: "Update prompt rules", description: "Rewrite planning guidance and examples", type: "coding", status: "running"},
    {name: "Check runtime impact", description: "Confirm runtime behavior still matches the new prompt policy", type: "research", status: "pending"},
    {name: "Run verification", description: "Build or test the changed packages", type: "coding", status: "pending"}
  ]
)

## Example — Retry After Failure (NEVER reset done tasks)

task_plan(
  intent="User wants weather info but first search returned empty",
  sub_tasks=[
    {name: "Get user location", description: "Found: Fuzhou", type: "search", status: "done"},
    {name: "Query weather", description: "First web_search returned no results, trying different query", type: "search", status: "running"},
    {name: "Provide advice", description: "Based on weather data", type: "chat", status: "pending"}
  ]
)

See how "Get user location" stays "done"? That is the correct pattern.`
}

func (t *TaskPlanTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"intent": map[string]interface{}{
				"type":        "string",
				"description": "Brief summary of the user's overall goal (1-2 sentences)",
			},
			"sub_tasks": map[string]interface{}{
				"type":        "array",
				"description": "Complete list of all subtasks (full replacement, not incremental)",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{
							"type":        "string",
							"description": "Short task name (2-5 words)",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "What this subtask accomplishes",
						},
						"type": map[string]interface{}{
							"type":        "string",
							"description": "Task category: search, research, coding, short, or chat",
							"enum":        []string{"search", "research", "coding", "short", "chat"},
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Current status: pending, running, done, or skipped",
							"enum":        []string{"pending", "running", "done", "skipped"},
						},
					},
					"required": []string{"name", "description", "type", "status"},
				},
			},
		},
		"required": []string{"intent", "sub_tasks"},
	}
}

func (t *TaskPlanTool) Execute(args map[string]interface{}) (string, error) {
	intent, _ := args["intent"].(string)
	if intent == "" {
		return "", fmt.Errorf("intent is required")
	}

	rawSubTasks, ok := args["sub_tasks"]
	if !ok {
		return "", fmt.Errorf("sub_tasks is required")
	}

	// 将 sub_tasks 转换为 []map[string]interface{}
	// LLM 传入的是 JSON 数组，可能是 []interface{} 或 string
	var subTasks []map[string]interface{}

	switch v := rawSubTasks.(type) {
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				subTasks = append(subTasks, m)
			}
		}
	case string:
		// 如果 LLM 把数组序列化为字符串，反序列化
		if err := json.Unmarshal([]byte(v), &subTasks); err != nil {
			return "", fmt.Errorf("sub_tasks must be an array or valid JSON array: %v", err)
		}
	default:
		return "", fmt.Errorf("sub_tasks must be an array")
	}

	if len(subTasks) == 0 {
		return "", fmt.Errorf("sub_tasks cannot be empty")
	}

	// 应用计划（全量替换）
	if t.applier == nil {
		return "", fmt.Errorf("task_plan: PlanApplier not configured")
	}

	if err := t.applier.ApplyPlan(intent, subTasks); err != nil {
		return "", fmt.Errorf("failed to apply task plan: %v", err)
	}

	return fmt.Sprintf("Task plan updated. Intent: %s\n\nCurrent subtask status:\n%s",
		intent, formatSubTaskSummary(subTasks)), nil
}

// formatSubTaskSummary 格式化子任务状态摘要，供 LLM 参考
func formatSubTaskSummary(subTasks []map[string]interface{}) string {
	var sb strings.Builder
	for i, t := range subTasks {
		name, _ := t["name"].(string)
		status, _ := t["status"].(string)
		sb.WriteString(fmt.Sprintf("  %d. [%s] %s\n", i+1, status, name))
	}
	done := 0
	for _, t := range subTasks {
		if s, _ := t["status"].(string); s == "done" || s == "skipped" {
			done++
		}
	}
	sb.WriteString(fmt.Sprintf("\nProgress: %d/%d completed", done, len(subTasks)))
	return sb.String()
}
