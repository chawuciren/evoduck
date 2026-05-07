package agent

import (
	"context"

	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/pkg/logger"
)

// 模块日志器
var loopLog = logger.NewModuleLogger("loop")

type AgentLoop struct {
	runtime *Runtime
}

func NewAgentLoop(runtime *Runtime) *AgentLoop {
	return &AgentLoop{
		runtime: runtime,
	}
}

func (al *AgentLoop) ProcessMessage(ctx context.Context, sess *session.Session, message string) error {
	err := al.runtime.Run(ctx, sess, message)
	if err != nil {
		loopLog.Error("Loop error", logger.Fields{"error": err.Error()})
		return err
	}

	msgs := sess.GetMessages()
	if len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		if last.Role == "assistant" && last.Content != "" {
			loopLog.Debug("Agent response", logger.Fields{"content_preview": truncateString(last.Content, 100)})
		}
	}

	return nil
}
