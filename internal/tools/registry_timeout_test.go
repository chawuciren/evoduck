package tools

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/pkg/models"
)

// blockingTool 模拟一个会无限阻塞的工具（如挂死的 MCP 搜索）
type blockingTool struct {
	name     string
	started  chan struct{}
	released chan struct{}
}

func newBlockingTool(name string) *blockingTool {
	return &blockingTool{
		name:     name,
		started:  make(chan struct{}),
		released: make(chan struct{}),
	}
}

func (t *blockingTool) Name() string { return t.name }
func (t *blockingTool) Description() string {
	return "blocks until released, simulating a hung external tool"
}
func (t *blockingTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (t *blockingTool) Execute(args map[string]interface{}) (string, error) {
	return t.ExecuteWithRole(context.Background(), args, models.RoleAdmin)
}
func (t *blockingTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	close(t.started)
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-t.released:
		return "done", nil
	}
}

// TestRegistryGlobalTimeoutFallback 验证 Registry 全局兜底超时能把挂死的工具打掉
func TestRegistryGlobalTimeoutFallback(t *testing.T) {
	r := NewRegistry()
	r.SetDefaultTimeout(50 * time.Millisecond)

	bt := newBlockingTool("mcp_search_hung")
	r.Register(bt)

	start := time.Now()
	_, err := r.ExecuteWithRole(context.Background(), "mcp_search_hung", nil, models.RoleAdmin)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v (elapsed=%v)", err, elapsed)
	}
	if elapsed > time.Second {
		t.Fatalf("timeout did not fire in time, elapsed=%v", elapsed)
	}
}

// TestRegistryNoTimeoutWhenDisabled 验证 defaultTimeout=0 时不施加兜底
func TestRegistryNoTimeoutWhenDisabled(t *testing.T) {
	r := NewRegistry()
	r.SetDefaultTimeout(0) // 禁用兜底

	bt := newBlockingTool("mcp_search_no_timeout")
	r.Register(bt)

	doneCh := make(chan error, 1)
	go func() {
		_, err := r.ExecuteWithRole(context.Background(), "mcp_search_no_timeout", nil, models.RoleAdmin)
		doneCh <- err
	}()

	<-bt.started
	select {
	case <-doneCh:
		t.Fatalf("tool returned even though no timeout was set")
	case <-time.After(80 * time.Millisecond):
		// 预期：未超时仍在阻塞
	}
	close(bt.released)
	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("expected success after release, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("tool did not return after release")
	}
}

// TestRegistryTimeoutExempt 验证 TimeoutExempt 工具不被兜底打断
func TestRegistryTimeoutExempt(t *testing.T) {
	r := NewRegistry()
	r.SetDefaultTimeout(50 * time.Millisecond)

	bt := newBlockingTool("long_running")
	et := &exemptTool{inner: bt}
	r.Register(et)

	doneCh := make(chan error, 1)
	go func() {
		_, err := r.ExecuteWithRole(context.Background(), "long_running", nil, models.RoleAdmin)
		doneCh <- err
	}()

	<-bt.started
	select {
	case <-doneCh:
		t.Fatalf("exempt tool was cut by global timeout")
	case <-time.After(150 * time.Millisecond):
		// 预期：超过兜底超时仍在运行
	}
	close(bt.released)
	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("exempt tool did not return after release")
	}
}

// exemptTool 包装 blockingTool 并声明豁免全局兜底
type exemptTool struct {
	inner *blockingTool
}

func (e *exemptTool) Name() string        { return e.inner.name }
func (e *exemptTool) Description() string  { return e.inner.Description() }
func (e *exemptTool) Parameters() map[string]interface{} { return e.inner.Parameters() }
func (e *exemptTool) IsTimeoutExempt() bool { return true }
func (e *exemptTool) Execute(args map[string]interface{}) (string, error) {
	return e.ExecuteWithRole(context.Background(), args, models.RoleAdmin)
}
func (e *exemptTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return e.inner.ExecuteWithRole(ctx, args, role)
}

// TestRegistryToolWithTimeoutOverridesGlobal 验证工具自声明超时覆盖全局默认
func TestRegistryToolWithTimeoutOverridesGlobal(t *testing.T) {
	r := NewRegistry()
	r.SetDefaultTimeout(10 * time.Second) // 全局设很大

	wt := &toolWithCustomTimeout{started: make(chan struct{})}
	r.Register(wt)

	start := time.Now()
	_, err := r.ExecuteWithRole(context.Background(), wt.Name(), nil, models.RoleAdmin)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded from tool-declared timeout, got %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("tool timeout did not fire in time, elapsed=%v", elapsed)
	}
}

type toolWithCustomTimeout struct {
	started chan struct{}
	calls   atomic.Int32
}

func (t *toolWithCustomTimeout) Name() string { return "custom_timeout_tool" }
func (t *toolWithCustomTimeout) Description() string {
	return "declares its own short call timeout"
}
func (t *toolWithCustomTimeout) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (t *toolWithCustomTimeout) CallTimeout() time.Duration {
	return 50 * time.Millisecond
}
func (t *toolWithCustomTimeout) Execute(args map[string]interface{}) (string, error) {
	return t.ExecuteWithRole(context.Background(), args, models.RoleAdmin)
}
func (t *toolWithCustomTimeout) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	t.calls.Add(1)
	close(t.started)
	<-ctx.Done()
	return "", ctx.Err()
}
