package gateway

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/internal/agent"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/pkg/models"
)

type BackgroundAgentRunRequest struct {
	RunID               string
	Kind                string
	AgentID             string
	UserID              string
	ParentSessionKey    string
	ExecutionSessionKey string
	Prompt              string
	Media               []models.OutgoingMedia
	Metadata            map[string]string
	StreamConfig        models.StreamConfig
	EphemeralSession    bool
	LogPath             string
}

type BackgroundAgentRunResult struct {
	RunID               string
	ExecutionSessionKey string
	LastAssistantText   string
}

type BackgroundAgentRuntime struct {
	agentMgr   *agent.Manager
	sessionMgr *session.Manager
}

func NewBackgroundAgentRuntime(agentMgr *agent.Manager, sessionMgr *session.Manager) *BackgroundAgentRuntime {
	return &BackgroundAgentRuntime{agentMgr: agentMgr, sessionMgr: sessionMgr}
}

func (r *BackgroundAgentRuntime) StartInternalRun(ctx context.Context, req BackgroundAgentRunRequest) (<-chan models.StreamEvent, error) {
	if r == nil || r.agentMgr == nil || r.sessionMgr == nil {
		return nil, fmt.Errorf("background agent runtime unavailable")
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent id is required")
	}
	sessionKey := strings.TrimSpace(req.ExecutionSessionKey)
	if sessionKey == "" {
		return nil, fmt.Errorf("execution session key is required")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	ag, err := r.agentMgr.Get(agentID)
	if err != nil {
		return nil, err
	}
	if ag == nil || ag.Runtime == nil {
		return nil, fmt.Errorf("agent runtime unavailable: %s", agentID)
	}
	var sess *session.Session
	if req.EphemeralSession {
		sess = session.NewSession(sessionKey, sessionKey, nil)
	} else {
		sess = r.sessionMgr.GetOrCreate(sessionKey)
		if sess == nil {
			return nil, fmt.Errorf("session unavailable: %s", sessionKey)
		}
	}
	if strings.TrimSpace(req.UserID) != "" {
		sess.SetUserID(strings.TrimSpace(req.UserID))
	}
	if strings.TrimSpace(req.Kind) != "" {
		sess.SetMetadataValue("background_run_kind", strings.TrimSpace(req.Kind))
	}
	if strings.TrimSpace(req.RunID) != "" {
		sess.SetMetadataValue("background_run_id", strings.TrimSpace(req.RunID))
	}
	if strings.TrimSpace(req.ParentSessionKey) != "" {
		sess.SetMetadataValue("parent_session_key", strings.TrimSpace(req.ParentSessionKey))
	}
	sess.SetMetadataValue("agent_id", agentID)
	for key, value := range req.Metadata {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		sess.SetMetadataValue(key, value)
	}
	stream, err := ag.Runtime.RunStreamWithLoopWithMedia(ctx, sess, prompt, req.Media, req.StreamConfig)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.LogPath) == "" {
		return stream, nil
	}
	return logStream(req.LogPath, req, stream), nil
}

func (r *BackgroundAgentRuntime) RunInternalSync(ctx context.Context, req BackgroundAgentRunRequest) (*BackgroundAgentRunResult, error) {
	stream, err := r.StartInternalRun(ctx, req)
	if err != nil {
		return nil, err
	}
	var streamed strings.Builder
	for event := range stream {
		if event.Error != nil {
			return nil, event.Error
		}
		if strings.TrimSpace(event.Content) != "" {
			streamed.WriteString(event.Content)
		}
	}
	var sess *session.Session
	if req.EphemeralSession {
		// The runtime-owned ephemeral session is not retained after the stream closes.
		// Use streamed/logged output as the durable result for ephemeral sync runs.
		sess = nil
	} else {
		sess = r.sessionMgr.GetOrCreate(strings.TrimSpace(req.ExecutionSessionKey))
	}
	result := &BackgroundAgentRunResult{RunID: req.RunID, ExecutionSessionKey: req.ExecutionSessionKey}
	if strings.TrimSpace(streamed.String()) != "" {
		result.LastAssistantText = strings.TrimSpace(streamed.String())
	}
	if sess == nil {
		return result, nil
	}
	msgs := sess.GetMessages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && strings.TrimSpace(msgs[i].Content) != "" {
			result.LastAssistantText = strings.TrimSpace(msgs[i].Content)
			break
		}
	}
	return result, nil
}

func logStream(logPath string, req BackgroundAgentRunRequest, stream <-chan models.StreamEvent) <-chan models.StreamEvent {
	out := make(chan models.StreamEvent, 100)
	go func() {
		defer close(out)
		path := strings.TrimSpace(logPath)
		if path == "" {
			for event := range stream {
				out <- event
			}
			return
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			for event := range stream {
				out <- event
			}
			return
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			for event := range stream {
				out <- event
			}
			return
		}
		defer file.Close()
		_, _ = fmt.Fprintf(file, "# background run\nstarted_at=%s\nrun_id=%s\nkind=%s\nagent_id=%s\nsession_key=%s\n\n", time.Now().Format(time.RFC3339), req.RunID, req.Kind, req.AgentID, req.ExecutionSessionKey)
		if strings.TrimSpace(req.Prompt) != "" {
			_, _ = fmt.Fprintf(file, "## prompt\n%s\n\n", req.Prompt)
		}
		_, _ = fmt.Fprintln(file, "## stream")
		for event := range stream {
			writeStreamEventLog(file, event)
			out <- event
		}
		_, _ = fmt.Fprintf(file, "\nfinished_at=%s\n", time.Now().Format(time.RFC3339))
	}()
	return out
}

func writeStreamEventLog(file *os.File, event models.StreamEvent) {
	if file == nil {
		return
	}
	if event.Error != nil {
		_, _ = fmt.Fprintf(file, "[error] %s\n", event.Error.Error())
		return
	}
	content := strings.TrimSpace(event.Content)
	thinking := strings.TrimSpace(event.ThinkingContent)
	if content == "" && thinking == "" && !event.Done {
		return
	}
	if content != "" {
		_, _ = fmt.Fprintf(file, "[%s] %s\n", event.Type, content)
	}
	if thinking != "" {
		_, _ = fmt.Fprintf(file, "[thinking] %s\n", thinking)
	}
	if event.Done {
		_, _ = fmt.Fprintln(file, "[done]")
	}
}
