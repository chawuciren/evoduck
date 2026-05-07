package daemon

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/chawuciren/evoduck/pkg/logger"
)

// SignalHandler 信号处理器
type SignalHandler struct {
	daemon   *Daemon
	onReload func()
}

// NewSignalHandler 创建信号处理器
func NewSignalHandler(daemon *Daemon) *SignalHandler {
	return &SignalHandler{
		daemon: daemon,
	}
}

// OnReload 设置重载回调（SIGHUP）
func (sh *SignalHandler) OnReload(fn func()) {
	sh.onReload = fn
}

// Handle 处理信号（非阻塞）
func (sh *SignalHandler) Handle(ctx context.Context) {
	sigChan := make(chan os.Signal, 1)

	// 监听信号
	signal.Notify(sigChan,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGHUP,
	)

	go func() {
		for {
			select {
			case sig := <-sigChan:
				sh.handleSignal(sig)
			case <-ctx.Done():
				signal.Stop(sigChan)
				return
			}
		}
	}()
}

func (sh *SignalHandler) handleSignal(sig os.Signal) {
	switch sig {
	case syscall.SIGTERM, syscall.SIGINT:
		// 关闭信号
		logger.Info("Shutdown signal received", logger.Fields{
			"signal": sig.String(),
		})
		sh.daemon.GracefulShutdown()

	case syscall.SIGHUP:
		// 重载配置
		logger.Info("Reload signal received")
		if sh.onReload != nil {
			sh.onReload()
		}
	}
}

// IsRunningAsService 检查是否作为服务运行
func IsRunningAsService() bool {
	// Windows 服务检测
	if len(os.Args) > 1 && os.Args[1] == "-s" {
		return true
	}

	// Linux 服务检测（检查是否由 systemd 启动）
	if os.Getenv("INVOCATION_ID") != "" {
		return true
	}

	return false
}

// GetProcessInfo 获取进程信息
func GetProcessInfo() map[string]interface{} {
	return map[string]interface{}{
		"pid":     os.Getpid(),
		"ppid":    os.Getppid(),
		"service": IsRunningAsService(),
	}
}
