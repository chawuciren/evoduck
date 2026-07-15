package mcp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/mark3labs/mcp-go/mcp"
)

// recordingRegistrar 记录 Register/Unregister 调用，用于断言 MCP 工具的动态增删。
type recordingRegistrar struct {
	mu          sync.Mutex
	registered  map[string]int // serverName -> 注册的工具数
	unregistered []string      // serverName 列表
}

func newRecordingRegistrar() *recordingRegistrar {
	return &recordingRegistrar{registered: make(map[string]int)}
}

func (r *recordingRegistrar) RegisterMCPTools(serverName string, wrappers []*MCPToolWrapper) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registered[serverName] += len(wrappers)
}

func (r *recordingRegistrar) UnregisterMCPTools(serverName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unregistered = append(r.unregistered, serverName)
}

func (r *recordingRegistrar) regCount(serverName string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.registered[serverName]
}

func (r *recordingRegistrar) unregCount(serverName string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.unregistered {
		if s == serverName {
			n++
		}
	}
	return n
}

// fakeClient 构造一个仅含 name + tools 的 Client（无需真实 transport）。
func fakeClient(name string, toolNames ...string) *Client {
	c := &Client{name: name, tools: make(map[string]*mcp.Tool), initialized: true}
	for _, tn := range toolNames {
		c.tools[tn] = &mcp.Tool{Name: tn}
	}
	return c
}

// newTestManager 创建带伪造连接器的 Manager。
func newTestManager(servers map[string]config.MCPServerConfig, connect func(name string) (*Client, []*MCPToolWrapper, error)) *Manager {
	mgr := NewManager(&config.MCPConfig{Servers: servers}, nil)
	mgr.SetConnectFunc(func(ctx context.Context, name string, cfg config.MCPServerConfig) (*Client, []*MCPToolWrapper, error) {
		return connect(name)
	})
	return mgr
}

// TestStartIsNonBlocking 验证 Start 立即返回，即使连接很慢也不阻塞。
func TestStartIsNonBlocking(t *testing.T) {
	slow := make(chan struct{})
	mgr := newTestManager(map[string]config.MCPServerConfig{
		"slow": {Enabled: true, Type: "local", Command: []string{"x"}},
	}, func(name string) (*Client, []*MCPToolWrapper, error) {
		select {
		case <-slow:
		case <-time.After(5 * time.Second):
			t.Error("test timed out waiting for slow connect")
		}
		return fakeClient(name, "a"), wrappersFor(fakeClient(name, "a")), nil
	})

	startedAt := time.Now()
	mgr.Start(context.Background())
	elapsed := time.Since(startedAt)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("Start should return immediately, took %v", elapsed)
	}
	close(slow)

	waitForState(t, mgr, "slow", StateConnected, 2*time.Second)
}

// TestFailedServerDoesNotBlockOthers 验证一个 server 失败不会阻塞其它 server，且整体不报错。
func TestFailedServerDoesNotBlockOthers(t *testing.T) {
	mgr := newTestManager(map[string]config.MCPServerConfig{
		"good": {Enabled: true, Type: "local", Command: []string{"x"}},
		"bad":  {Enabled: true, Type: "local", Command: []string{"y"}},
	}, func(name string) (*Client, []*MCPToolWrapper, error) {
		if name == "bad" {
			return nil, nil, errors.New("boom")
		}
		c := fakeClient(name, "toolA")
		return c, wrappersFor(c), nil
	})

	mgr.Start(context.Background())

	waitForState(t, mgr, "good", StateConnected, 2*time.Second)
	waitForState(t, mgr, "bad", StateFailed, 2*time.Second)

	snap := mgr.Status()
	if snap.Online != 1 {
		t.Fatalf("expected 1 online, got %d", snap.Online)
	}
	if snap.Failed != 1 {
		t.Fatalf("expected 1 failed, got %d", snap.Failed)
	}
}

// TestDisabledServerExcludedFromStatus 验证 disabled server 不出现在状态中。
func TestDisabledServerExcludedFromStatus(t *testing.T) {
	mgr := newTestManager(map[string]config.MCPServerConfig{
		"on":  {Enabled: true, Type: "local", Command: []string{"x"}},
		"off": {Enabled: false, Type: "local", Command: []string{"x"}},
	}, func(name string) (*Client, []*MCPToolWrapper, error) {
		c := fakeClient(name, "t")
		return c, wrappersFor(c), nil
	})

	mgr.Start(context.Background())
	waitForState(t, mgr, "on", StateConnected, 2*time.Second)

	snap := mgr.Status()
	for _, s := range snap.Servers {
		if s.Name == "off" {
			t.Fatalf("disabled server should not appear in status, got %+v", s)
		}
	}
	if snap.Total != 1 {
		t.Fatalf("expected total 1 (only enabled), got %d", snap.Total)
	}
}

// TestReconnectFromFailedToConnected 验证失败后重连能恢复到 connected。
func TestReconnectFromFailedToConnected(t *testing.T) {
	mu := sync.Mutex{}
	failing := true
	connects := 0
	mgr := newTestManager(map[string]config.MCPServerConfig{
		"flaky": {Enabled: true, Type: "local", Command: []string{"x"}},
	}, func(name string) (*Client, []*MCPToolWrapper, error) {
		mu.Lock()
		failingNow := failing
		connects++
		mu.Unlock()
		if failingNow {
			return nil, nil, errors.New("not ready")
		}
		c := fakeClient(name, "tool1")
		return c, wrappersFor(c), nil
	})

	mgr.Start(context.Background())
	waitForState(t, mgr, "flaky", StateFailed, 2*time.Second)

	// 修复后重连
	mu.Lock()
	failing = false
	mu.Unlock()
	mgr.Reconnect(context.Background(), "flaky")
	// 重连是异步的：最终应恢复到 connected（中间 reconnecting 状态可能很快跳过）
	waitForState(t, mgr, "flaky", StateConnected, 2*time.Second)

	snap := mgr.Status()
	if snap.Online != 1 {
		t.Fatalf("expected 1 online after reconnect, got %d", snap.Online)
	}
}

// TestReconnectAll 重连所有 server。
func TestReconnectAll(t *testing.T) {
	mgr := newTestManager(map[string]config.MCPServerConfig{
		"s1": {Enabled: true, Type: "local", Command: []string{"x"}},
		"s2": {Enabled: true, Type: "local", Command: []string{"x"}},
	}, func(name string) (*Client, []*MCPToolWrapper, error) {
		c := fakeClient(name, "t")
		return c, wrappersFor(c), nil
	})

	mgr.Start(context.Background())
	waitForState(t, mgr, "s1", StateConnected, 2*time.Second)
	waitForState(t, mgr, "s2", StateConnected, 2*time.Second)

	// 空 target：已 connected 默认跳过 → 返回空列表
	if got := mgr.Reconnect(context.Background(), ""); len(got) != 0 {
		t.Fatalf("expected no targets when all connected, got %v", got)
	}
	// "all" 强制重连全部
	mgr.Reconnect(context.Background(), "all")
	// 重连后应回到 connected
	waitForState(t, mgr, "s1", StateConnected, 2*time.Second)
	waitForState(t, mgr, "s2", StateConnected, 2*time.Second)
}

// TestRegistrarInvokedOnConnectAndTeardown 验证 ToolRegistrar 在连接/断开时被正确回调。
func TestRegistrarInvokedOnConnectAndTeardown(t *testing.T) {
	reg := newRecordingRegistrar()
	mgr := newTestManager(map[string]config.MCPServerConfig{
		"srv": {Enabled: true, Type: "local", Command: []string{"x"}},
	}, func(name string) (*Client, []*MCPToolWrapper, error) {
		c := fakeClient(name, "alpha", "beta")
		return c, wrappersFor(c), nil
	})
	mgr.SetToolRegistrar(reg)

	mgr.Start(context.Background())
	waitForState(t, mgr, "srv", StateConnected, 2*time.Second)

	if got := reg.regCount("srv"); got != 2 {
		t.Fatalf("expected 2 tools registered, got %d", got)
	}

	// 重连前应先反注册（reconnect 是异步的，轮询等待其完成）
	mgr.Reconnect(context.Background(), "srv")
	if !waitFor(time.Second, func() bool { return reg.unregCount("srv") >= 1 }) {
		t.Fatalf("expected at least 1 unregister on reconnect, got %d", reg.unregCount("srv"))
	}
	if !waitFor(2*time.Second, func() bool { return reg.regCount("srv") == 4 }) { // 2 (initial) + 2 (reconnect)
		t.Fatalf("expected 4 total registrations, got %d", reg.regCount("srv"))
	}
	waitForState(t, mgr, "srv", StateConnected, 2*time.Second)

	// Close 时也应反注册
	mgr.Close()
	if got := reg.unregCount("srv"); got < 2 {
		t.Fatalf("expected at least 2 unregisters total, got %d", got)
	}
}

// TestGetAllToolsReflectsConnectedServers 验证 GetAllTools 只返回已连接 server 的工具。
func TestGetAllToolsReflectsConnectedServers(t *testing.T) {
	mgr := newTestManager(map[string]config.MCPServerConfig{
		"on":  {Enabled: true, Type: "local", Command: []string{"x"}},
		"off": {Enabled: true, Type: "local", Command: []string{"x"}},
	}, func(name string) (*Client, []*MCPToolWrapper, error) {
		if name == "off" {
			return nil, nil, errors.New("fail")
		}
		c := fakeClient(name, "tool1", "tool2")
		return c, wrappersFor(c), nil
	})

	mgr.Start(context.Background())
	waitForState(t, mgr, "on", StateConnected, 2*time.Second)
	waitForState(t, mgr, "off", StateFailed, 2*time.Second)

	tools := mgr.GetAllTools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools from connected server, got %d", len(tools))
	}
}

// TestCloseSetsClosedState 验证 Close 后状态变为 closed。
func TestCloseSetsClosedState(t *testing.T) {
	mgr := newTestManager(map[string]config.MCPServerConfig{
		"s": {Enabled: true, Type: "local", Command: []string{"x"}},
	}, func(name string) (*Client, []*MCPToolWrapper, error) {
		c := fakeClient(name, "t")
		return c, wrappersFor(c), nil
	})

	mgr.Start(context.Background())
	waitForState(t, mgr, "s", StateConnected, 2*time.Second)
	mgr.Close()

	mgr.mu.RLock()
	st := mgr.states["s"].state
	mgr.mu.RUnlock()
	if st != StateClosed {
		t.Fatalf("expected closed state, got %s", st)
	}
}

// ---- helpers ----

// wrappersFor 基于 Client 已缓存的 tools 构造 wrapper 列表（带 server 前缀）。
func wrappersFor(c *Client) []*MCPToolWrapper {
	ws := make([]*MCPToolWrapper, 0)
	for _, tool := range c.GetAllTools() {
		ws = append(ws, NewMCPToolWrapper(c, tool))
	}
	return ws
}

// waitForState 轮询直到指定 server 到达期望状态或超时。
func waitForState(t *testing.T, mgr *Manager, name string, want ConnState, timeout time.Duration) {
	t.Helper()
	if !waitFor(timeout, func() bool {
		mgr.mu.RLock()
		st := mgr.states[name]
		mgr.mu.RUnlock()
		return st != nil && st.state == want
	}) {
		mgr.mu.RLock()
		st := mgr.states[name]
		mgr.mu.RUnlock()
		got := ConnState("")
		if st != nil {
			got = st.state
		}
		t.Fatalf("server %s did not reach state %s (got %s) within %v", name, want, got, timeout)
	}
}

// waitFor 轮询 cond 直到返回 true 或超时。
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// 防止 import 未使用（fmt 在未来扩展时使用）
var _ = fmt.Sprintf
