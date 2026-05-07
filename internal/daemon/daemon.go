package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/chawuciren/evoduck/pkg/logger"
)

// Daemon 进程守护
type Daemon struct {
	mu              sync.Mutex
	shutdownFns     []func(context.Context) error
	shutdownTimeout time.Duration
}

// New 创建新的 Daemon
func New() *Daemon {
	return &Daemon{
		shutdownFns:     make([]func(context.Context) error, 0),
		shutdownTimeout: 30 * time.Second,
	}
}

// OnShutdown 注册关闭钩子
func (d *Daemon) OnShutdown(fn func(context.Context) error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.shutdownFns = append(d.shutdownFns, fn)
}

// SetShutdownTimeout 设置关闭超时时间
func (d *Daemon) SetShutdownTimeout(timeout time.Duration) {
	d.shutdownTimeout = timeout
}

// WaitForSignal 等待信号
func (d *Daemon) WaitForSignal() os.Signal {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	sig := <-sigChan
	logger.Info("Received shutdown signal", logger.Fields{
		"signal": sig.String(),
	})

	return sig
}

// GracefulShutdown 优雅关闭
func (d *Daemon) GracefulShutdown() error {
	logger.Info("Starting graceful shutdown")

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), d.shutdownTimeout)
	defer cancel()

	// 执行所有关闭钩子
	d.mu.Lock()
	hooks := make([]func(context.Context) error, len(d.shutdownFns))
	copy(hooks, d.shutdownFns)
	d.mu.Unlock()

	var errors []error
	for i, fn := range hooks {
		if err := fn(ctx); err != nil {
			errors = append(errors, fmt.Errorf("shutdown hook %d failed: %w", i, err))
			logger.Error("Shutdown hook failed", logger.Fields{
				"hook":  i,
				"error": err.Error(),
			})
		} else {
			logger.Debug("Shutdown hook completed", logger.Fields{
				"hook": i,
			})
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("shutdown errors: %v", errors)
	}

	logger.Info("Graceful shutdown completed")
	return nil
}

// Run 运行守护进程（阻塞直到收到信号）
func (d *Daemon) Run(startFn func() error) error {
	// 启动服务
	if err := startFn(); err != nil {
		return fmt.Errorf("start failed: %w", err)
	}

	// 等待信号并记录
	_ = d.WaitForSignal()

	// 优雅关闭
	return d.GracefulShutdown()
}

// Stop 简单关闭（向后兼容）
func (d *Daemon) Stop() {
	ctx := context.Background()
	d.GracefulShutdownWithContext(ctx)
}

// GracefulShutdownWithContext 带上下文的优雅关闭
func (d *Daemon) GracefulShutdownWithContext(ctx context.Context) {
	d.mu.Lock()
	hooks := make([]func(context.Context) error, len(d.shutdownFns))
	copy(hooks, d.shutdownFns)
	d.mu.Unlock()

	for i, fn := range hooks {
		if err := fn(ctx); err != nil {
			logger.Error("Shutdown hook failed", logger.Fields{
				"hook":  i,
				"error": err.Error(),
			})
		}
	}
}
