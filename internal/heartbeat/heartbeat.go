package heartbeat

import (
	"time"

	"github.com/chawuciren/evoduck/pkg/logger"
)

// 模块日志器
var hbLog = logger.NewModuleLogger("heartbeat")

type Heartbeat struct {
	interval time.Duration
	prompt   string
	enabled  bool
	stopCh   chan struct{}
	handler  func(prompt string) string
}

func New(enabled bool, interval time.Duration, prompt string) *Heartbeat {
	return &Heartbeat{
		enabled:  enabled,
		interval: interval,
		prompt:   prompt,
		stopCh:   make(chan struct{}),
	}
}

func (h *Heartbeat) SetHandler(handler func(prompt string) string) {
	h.handler = handler
}

func (h *Heartbeat) Start() {
	if !h.enabled {
		return
	}

	hbLog.Info("Started", logger.Fields{"interval": h.interval.String()})

	go func() {
		ticker := time.NewTicker(h.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				h.tick()
			case <-h.stopCh:
				hbLog.Info("Stopped", nil)
				return
			}
		}
	}()
}

func (h *Heartbeat) Stop() {
	close(h.stopCh)
}

func (h *Heartbeat) tick() {
	if h.handler == nil {
		return
	}

	result := h.handler(h.prompt)
	if result != "" && result != "HEARTBEAT_OK" {
		hbLog.Warn("Alert", logger.Fields{"result": result})
	}
}
