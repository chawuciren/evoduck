package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/internal/command"
	"github.com/chawuciren/evoduck/internal/subagent"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
)

func (g *Gateway) ListSubagents(agentID, userID string) []subagent.Record {
	if g.subagentManager == nil {
		return nil
	}
	return g.subagentManager.List(agentID, userID)
}

func (g *Gateway) GetSubagent(agentID, userID, id string) (*subagent.Record, error) {
	if g.subagentManager == nil {
		return nil, fmt.Errorf("subagent manager unavailable")
	}
	record, ok := g.subagentManager.Get(id)
	if !ok {
		return nil, fmt.Errorf("subagent not found: %s", id)
	}
	if agentID != "" && record.CallerAgentID != agentID && record.TargetAgentID != agentID {
		return nil, fmt.Errorf("subagent does not belong to current agent: %s", id)
	}
	if userID != "" && record.UserID != userID {
		return nil, fmt.Errorf("subagent does not belong to current user: %s", id)
	}
	return &record, nil
}

func (g *Gateway) CreateInternalSubagent(req subagent.StartInternalRequest) (*subagent.Record, error) {
	if g.subagentManager == nil {
		return nil, fmt.Errorf("subagent manager unavailable")
	}
	if g.backgroundRuntime == nil {
		g.backgroundRuntime = NewBackgroundAgentRuntime(g.agentMgr, g.sessionMgr)
	}
	callerAgentID := strings.TrimSpace(req.CallerAgentID)
	targetAgentID := strings.TrimSpace(req.TargetAgentID)
	if targetAgentID == "" {
		targetAgentID = callerAgentID
	}
	if callerAgentID == "" || targetAgentID == "" {
		return nil, fmt.Errorf("caller and target agent ids are required")
	}
	if err := g.canStartInternalSubagent(callerAgentID, targetAgentID); err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	parentSessionKey := strings.TrimSpace(req.ParentSessionKey)
	if parentSessionKey == "" {
		return nil, fmt.Errorf("parent session key is required")
	}
	checkerPrompt := strings.TrimSpace(req.CheckerPrompt)
	if checkerPrompt == "" {
		return nil, fmt.Errorf("checker_prompt is required")
	}
	checkerSchedule := strings.TrimSpace(req.CheckerSchedule)
	if checkerSchedule == "" {
		checkerSchedule = "*/3 * * * *"
	}
	record := subagent.Record{
		Kind:             subagent.KindInternal,
		Status:           subagent.StatusStarting,
		CallerAgentID:    callerAgentID,
		TargetAgentID:    targetAgentID,
		UserID:           strings.TrimSpace(req.UserID),
		Role:             strings.TrimSpace(req.Role),
		ParentSessionKey: parentSessionKey,
		Description:      strings.TrimSpace(req.Description),
		Prompt:           prompt,
		CheckerPrompt:    checkerPrompt,
		Metadata:         req.Metadata,
	}
	created, err := g.subagentManager.Create(record)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ExecutionSessionKey) != "" {
		created.ExecutionSessionKey = strings.TrimSpace(req.ExecutionSessionKey)
	} else {
		created.ExecutionSessionKey = fmt.Sprintf("agent:%s:user:%s:subagent:%s", targetAgentID, created.UserID, created.ID)
	}
	watchPrompt := buildSubagentWatcherPrompt(created, checkerPrompt)
	enabled := true
	watch, err := g.CreateSchedule(callerAgentID, created.UserID, models.Role(created.Role), command.CreateScheduleRequest{
		Name:             "watch " + created.ID,
		Description:      "Watch subagent " + created.ID,
		Schedule:         checkerSchedule,
		Prompt:           watchPrompt,
		Enabled:          &enabled,
		OriginSessionKey: parentSessionKey,
	})
	if err != nil {
		created.Status = subagent.StatusFailed
		created.Error = "create watcher: " + err.Error()
		_ = g.subagentManager.Update(created)
		return nil, err
	}
	created.WatchScheduleID = watch.ID
	if created.Metadata == nil {
		created.Metadata = map[string]string{}
	}
	created.Metadata["log_path"] = g.backgroundRunLogPath("subagent", created.ID)
	created.Status = subagent.StatusRunning
	created.StartedAt = time.Now().Unix()
	created.LastHeartbeatAt = created.StartedAt
	if err := g.subagentManager.Update(created); err != nil {
		return nil, err
	}
	go g.runInternalSubagent(created)
	return &created, nil
}

func (g *Gateway) CreateExternalSubagent(req subagent.StartExternalRequest) (*subagent.Record, error) {
	if g.subagentManager == nil {
		return nil, fmt.Errorf("subagent manager unavailable")
	}
	callerAgentID := strings.TrimSpace(req.CallerAgentID)
	if callerAgentID == "" {
		return nil, fmt.Errorf("caller agent id is required")
	}
	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = "process"
	}
	if err := g.canStartExternalSubagent(callerAgentID, provider); err != nil {
		return nil, err
	}
	cmdText := strings.TrimSpace(req.Command)
	if cmdText == "" {
		return nil, fmt.Errorf("command is required")
	}
	parentSessionKey := strings.TrimSpace(req.ParentSessionKey)
	if parentSessionKey == "" {
		return nil, fmt.Errorf("parent session key is required")
	}
	checkerPrompt := strings.TrimSpace(req.CheckerPrompt)
	if checkerPrompt == "" {
		return nil, fmt.Errorf("checker_prompt is required")
	}
	workdir := strings.TrimSpace(req.WorkingDirectory)
	if workdir == "" {
		if ag, err := g.agentMgr.Get(callerAgentID); err == nil && ag != nil {
			workdir = ag.Config.Workspace
		}
	}
	if workdir == "" {
		workdir = "."
	}
	absWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absWorkdir, 0o755); err != nil {
		return nil, err
	}
	record := subagent.Record{
		Kind:             subagent.KindExternal,
		Status:           subagent.StatusStarting,
		CallerAgentID:    callerAgentID,
		TargetAgentID:    provider,
		UserID:           strings.TrimSpace(req.UserID),
		Role:             strings.TrimSpace(req.Role),
		ParentSessionKey: parentSessionKey,
		Description:      strings.TrimSpace(req.Description),
		Prompt:           cmdText,
		CheckerPrompt:    checkerPrompt,
		Metadata:         req.Metadata,
	}
	if record.Metadata == nil {
		record.Metadata = map[string]string{}
	}
	record.Metadata["provider"] = provider
	record.Metadata["command"] = cmdText
	record.Metadata["working_directory"] = absWorkdir
	created, err := g.subagentManager.Create(record)
	if err != nil {
		return nil, err
	}
	logPath := filepath.Join(g.config.DataDir, "subagents", created.ID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		created.Status = subagent.StatusFailed
		created.Error = "open log: " + err.Error()
		_ = g.subagentManager.Update(created)
		return nil, err
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", cmdText)
	} else {
		cmd = exec.Command("sh", "-c", cmdText)
	}
	cmd.Dir = absWorkdir
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Use proxyDecider to inject proxy environment variables
	env := os.Environ()
	if g.proxyDecider != nil {
		commandName := extractCommandName(cmdText)
		env = g.proxyDecider.BuildExecEnv(commandName, env)
	}
	cmd.Env = env

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		created.Status = subagent.StatusFailed
		created.Error = "start process: " + err.Error()
		_ = g.subagentManager.Update(created)
		return nil, err
	}
	created.Metadata["pid"] = fmt.Sprintf("%d", cmd.Process.Pid)
	created.Metadata["log_path"] = logPath
	checkerSchedule := strings.TrimSpace(req.CheckerSchedule)
	if checkerSchedule == "" {
		checkerSchedule = "*/3 * * * *"
	}
	watchPrompt := buildSubagentWatcherPrompt(created, checkerPrompt)
	enabled := true
	watch, err := g.CreateSchedule(callerAgentID, created.UserID, models.Role(created.Role), command.CreateScheduleRequest{
		Name:             "watch " + created.ID,
		Description:      "Watch external subagent " + created.ID,
		Schedule:         checkerSchedule,
		Prompt:           watchPrompt,
		Enabled:          &enabled,
		OriginSessionKey: parentSessionKey,
	})
	if err != nil {
		_ = cmd.Process.Kill()
		_ = logFile.Close()
		created.Status = subagent.StatusFailed
		created.Error = "create watcher: " + err.Error()
		_ = g.subagentManager.Update(created)
		return nil, err
	}
	created.WatchScheduleID = watch.ID
	created.Status = subagent.StatusRunning
	created.StartedAt = time.Now().Unix()
	created.LastHeartbeatAt = created.StartedAt
	if err := g.subagentManager.Update(created); err != nil {
		_ = cmd.Process.Kill()
		_ = logFile.Close()
		return nil, err
	}
	go g.waitExternalSubagent(created.ID, cmd, logFile)
	return &created, nil
}

func (g *Gateway) CancelSubagent(agentID, userID, id string) (*subagent.Record, error) {
	record, err := g.GetSubagent(agentID, userID, id)
	if err != nil {
		return nil, err
	}
	updated := *record
	if watchID := strings.TrimSpace(updated.WatchScheduleID); watchID != "" {
		if g.schedulerService != nil {
			if _, ok := g.schedulerService.Get(watchID); ok {
				if err := g.DeleteSchedule(agentID, userID, watchID); err != nil {
					return nil, err
				}
			}
		}
		updated.WatchScheduleID = ""
	}
	updated.Status = subagent.StatusCancelRequested
	updated.UpdatedAt = time.Now().Unix()
	if err := g.subagentManager.Update(updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (g *Gateway) waitExternalSubagent(id string, cmd *exec.Cmd, logFile *os.File) {
	defer logFile.Close()
	err := cmd.Wait()
	record, ok := g.subagentManager.Get(id)
	if !ok {
		return
	}
	record.FinishedAt = time.Now().Unix()
	record.LastHeartbeatAt = record.FinishedAt
	if record.Metadata == nil {
		record.Metadata = map[string]string{}
	}
	if err != nil {
		record.Status = subagent.StatusFailed
		record.Error = err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			record.Metadata["exit_code"] = fmt.Sprintf("%d", exitErr.ExitCode())
		}
	} else {
		record.Status = subagent.StatusCompleted
		record.Metadata["exit_code"] = "0"
	}
	if logPath := strings.TrimSpace(record.Metadata["log_path"]); logPath != "" {
		if data, readErr := os.ReadFile(logPath); readErr == nil {
			text := strings.TrimSpace(string(data))
			if len(text) > 4000 {
				text = text[len(text)-4000:]
			}
			record.ResultSummary = text
		}
	}
	_ = g.subagentManager.Update(record)
}

func (g *Gateway) canStartExternalSubagent(callerAgentID, provider string) error {
	if g.agentMgr == nil {
		return fmt.Errorf("agent manager unavailable")
	}
	ag, err := g.agentMgr.Get(callerAgentID)
	if err != nil {
		return err
	}
	for _, item := range ag.Config.Permissions.AuthorizedExternalSubagents {
		item = strings.TrimSpace(item)
		if item == "*" || item == provider {
			return nil
		}
	}
	return fmt.Errorf("external subagent %s is not authorized for agent %s", provider, callerAgentID)
}

func (g *Gateway) runInternalSubagent(record subagent.Record) {
	ctx := context.Background()
	stream, err := g.backgroundRuntime.StartInternalRun(ctx, BackgroundAgentRunRequest{
		RunID:               record.ID,
		Kind:                "internal_subagent",
		AgentID:             record.TargetAgentID,
		UserID:              record.UserID,
		ParentSessionKey:    record.ParentSessionKey,
		ExecutionSessionKey: record.ExecutionSessionKey,
		Prompt:              record.Prompt,
		Metadata: map[string]string{
			"session_kind":       "internal_subagent",
			"memory_policy":      "ignore",
			"subagent_id":        record.ID,
			"caller_agent_id":    record.CallerAgentID,
			"target_agent_id":    record.TargetAgentID,
			"parent_session_key": record.ParentSessionKey,
		},
		StreamConfig:     models.StreamConfig{SendToolEvents: false},
		EphemeralSession: true,
		LogPath:          record.Metadata["log_path"],
	})
	if err != nil {
		g.finishInternalSubagent(record.ID, subagent.StatusFailed, "", err)
		return
	}
	for event := range stream {
		if event.Error != nil {
			g.finishInternalSubagent(record.ID, subagent.StatusFailed, "", event.Error)
			return
		}
	}
	result := g.lastAssistantText(record.ExecutionSessionKey)
	g.finishInternalSubagent(record.ID, subagent.StatusCompleted, result, nil)
}

func (g *Gateway) finishInternalSubagent(id string, status subagent.Status, result string, err error) {
	record, ok := g.subagentManager.Get(id)
	if !ok {
		return
	}
	record.Status = status
	record.FinishedAt = time.Now().Unix()
	record.LastHeartbeatAt = record.FinishedAt
	record.ResultSummary = strings.TrimSpace(result)
	if err != nil {
		record.Error = err.Error()
		logger.Warn("Internal subagent failed", logger.Fields{"subagent_id": id, "error": err.Error()})
	}
	_ = g.subagentManager.Update(record)
}

func (g *Gateway) lastAssistantText(sessionKey string) string {
	if g.sessionMgr == nil {
		return ""
	}
	sess, err := g.sessionMgr.Get(sessionKey)
	if err != nil || sess == nil {
		return ""
	}
	msgs := sess.GetMessages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && strings.TrimSpace(msgs[i].Content) != "" {
			return strings.TrimSpace(msgs[i].Content)
		}
	}
	return ""
}

func (g *Gateway) canStartInternalSubagent(callerAgentID, targetAgentID string) error {
	if callerAgentID == targetAgentID {
		return nil
	}
	if g.agentMgr == nil {
		return fmt.Errorf("agent manager unavailable")
	}
	ag, err := g.agentMgr.Get(callerAgentID)
	if err != nil {
		return err
	}
	allowed := ag.Config.Permissions.AuthorizedSubagents
	for _, item := range allowed {
		item = strings.TrimSpace(item)
		if item == "*" || item == targetAgentID {
			return nil
		}
	}
	return fmt.Errorf("subagent %s is not authorized for agent %s", targetAgentID, callerAgentID)
}

func buildSubagentWatcherPrompt(record subagent.Record, checkerPrompt string) string {
	data, _ := json.MarshalIndent(record, "", "  ")
	return strings.TrimSpace(checkerPrompt) + "\n\nSubagent record:\n```json\n" + string(data) + "\n```\n\nWhen the subagent is still running, send a brief status to the parent session with sessions_send. When it is completed, failed, blocked, or stale, call sessions_run on parent_session_key with a concise wake-up summary, then delete this watcher schedule if possible."
}

// extractCommandName extracts the command name (first word) from a command string
func extractCommandName(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	// Handle pipes, redirects etc - only take the first command
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return ""
	}
	// Return the first word, stripping path prefixes
	cmdName := parts[0]
	if idx := strings.LastIndex(cmdName, "/"); idx >= 0 {
		cmdName = cmdName[idx+1:]
	}
	if idx := strings.LastIndex(cmdName, "\\"); idx >= 0 {
		cmdName = cmdName[idx+1:]
	}
	return cmdName
}
