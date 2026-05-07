package plugin

import (
	"context"
	"net"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/pkg/config"
)

func TestManagerRegistersAndExecutesEchoTool(t *testing.T) {
	port := freeTCPPort(t)
	pluginPath := filepath.Clean(filepath.Join("..", "..", "plugins", "echo-tool"))

	manager := NewManager(config.PluginConfig{
		WSServer: config.WSServerConfig{
			Host: "127.0.0.1",
			Port: port,
		},
		Plugins: map[string]config.PluginDef{
			"echo-tool": {
				Enabled:        true,
				Type:           "local",
				Command:        []string{"go", "run", pluginPath},
				Restart:        "never",
				RequestTimeout: 3000,
			},
		},
	}, false)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	})

	if err := manager.WaitReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("wait ready: %v", err)
	}

	adapters := manager.ListToolAdapters()
	if len(adapters) != 1 {
		t.Fatalf("expected 1 adapter, got %d", len(adapters))
	}
	if adapters[0].Name() != "echo_tool" {
		t.Fatalf("expected adapter echo_tool, got %s", adapters[0].Name())
	}

	callCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := adapters[0].ExecuteWithRole(callCtx, map[string]interface{}{
		"text":   "world",
		"prefix": "hello",
	}, "")
	if err != nil {
		t.Fatalf("execute tool: %v", err)
	}
	if result != "hello world" {
		t.Fatalf("unexpected tool result: %q", result)
	}
}

func TestManagerExecuteToolTimeoutAndCancel(t *testing.T) {
	manager := newEchoToolTestManager(t, 200)
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	})

	if err := manager.WaitReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("wait ready: %v", err)
	}

	adapters := manager.ListToolAdapters()
	if len(adapters) != 1 {
		t.Fatalf("expected 1 adapter, got %d", len(adapters))
	}

	callCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := adapters[0].ExecuteWithRole(callCtx, map[string]interface{}{
		"text":     "slow",
		"delay_ms": 1000,
	}, "")
	if err == nil {
		t.Fatalf("expected timeout error")
	}
}

func TestManagerExecuteToolReturnsPluginError(t *testing.T) {
	manager := newEchoToolTestManager(t, 3000)
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	})

	if err := manager.WaitReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("wait ready: %v", err)
	}

	adapters := manager.ListToolAdapters()
	if len(adapters) != 1 {
		t.Fatalf("expected 1 adapter, got %d", len(adapters))
	}

	callCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := adapters[0].ExecuteWithRole(callCtx, map[string]interface{}{
		"text": "boom",
		"fail": true,
	}, "")
	if err == nil {
		t.Fatalf("expected plugin error")
	}
}

func newEchoToolTestManager(t *testing.T, requestTimeout int) *Manager {
	t.Helper()
	port := freeTCPPort(t)
	pluginPath := filepath.Clean(filepath.Join("..", "..", "plugins", "echo-tool"))
	return NewManager(config.PluginConfig{
		WSServer: config.WSServerConfig{
			Host: "127.0.0.1",
			Port: port,
		},
		Plugins: map[string]config.PluginDef{
			"echo-tool": {
				Enabled:        true,
				Type:           "local",
				Command:        []string{"go", "run", pluginPath},
				Restart:        "never",
				RequestTimeout: requestTimeout,
			},
		},
	}, false)
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on ephemeral port: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if runtime.GOOS == "windows" {
		// Windows port reuse can be slower; a short delay reduces flakiness.
		time.Sleep(100 * time.Millisecond)
	}
	return port
}
