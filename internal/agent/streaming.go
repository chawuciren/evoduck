package agent

import (
	"context"

	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
)

// 模块日志器
var stLog = logger.NewModuleLogger("streaming")

type StreamingEngine struct {
	runtime *Runtime
}

func NewStreamingEngine(runtime *Runtime) *StreamingEngine {
	return &StreamingEngine{
		runtime: runtime,
	}
}

// Stream 使用带工具调用循环的流式响应
func (se *StreamingEngine) Stream(ctx context.Context, sess *session.Session, message string, config models.StreamConfig, handler func(event models.StreamEvent)) error {
	ch, err := se.runtime.RunStreamWithLoop(ctx, sess, message, config)
	if err != nil {
		return err
	}

	for event := range ch {
		handler(event)
		if event.Done {
			break
		}
	}

	return nil
}

func (se *StreamingEngine) HandleEvent(event models.StreamEvent) string {
	if event.Error != nil {
		stLog.Error("Stream error", logger.Fields{"error": event.Error.Error()})
		return ""
	}
	return event.Content
}
