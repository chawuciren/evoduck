package agent

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/internal/plugin"
	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

func blockHookTestProxyDecider() *proxy.Decider {
	return proxy.NewDecider(config.ProxyConfig{Enabled: false})
}

func TestRuntimeBlocksToolCallViaBeforeToolCallHook(t *testing.T) {
	tempDir := t.TempDir()
	echoToolPath := filepath.Clean(filepath.Join("..", "..", "plugins", "echo-tool"))
	mockHookPath := filepath.Clean(filepath.Join("..", "..", "plugins", "mock-hook"))

	pluginCfg := config.PluginConfig{
		WSServer: config.WSServerConfig{Host: "127.0.0.1", Port: freeBlockHookTestPort(t)},
		Plugins: map[string]config.PluginDef{
			"echo-tool": {
				Enabled:        true,
				Type:           "local",
				Command:        []string{"go", "run", echoToolPath},
				Restart:        "never",
				RequestTimeout: 3000,
			},
			"mock-hook": {
				Enabled: true,
				Type:    "local",
				Command: []string{"go", "run", mockHookPath},
				Environment: map[string]string{
					"MOCK_HOOK_BLOCK_TOOL_NAME": "echo_tool",
				},
				Restart:        "never",
				RequestTimeout: 3000,
			},
		},
	}

	pluginMgr := plugin.NewManager(pluginCfg, blockHookTestProxyDecider())
	if err := pluginMgr.Start(context.Background()); err != nil {
		t.Fatalf("start plugin manager: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = pluginMgr.Shutdown(shutdownCtx)
	})
	if err := pluginMgr.WaitReady(context.Background(), 10*time.Second); err != nil {
		t.Fatalf("wait plugin ready: %v", err)
	}
	pluginMgr.ListHookObservers()

	toolReg := tools.NewRegistry()
	for _, adapter := range pluginMgr.ListToolAdapters() {
		toolReg.Register(adapter)
	}

	promptBuilder := NewPromptBuilder(tempDir, "agent-block-hook-test", tempDir, toolReg, skill.NewLoader(tempDir, tempDir))
	runtime := NewRuntime("agent-block-hook-test", tempDir, nil, toolReg, promptBuilder, models.RoleAdmin, nil, true, pluginMgr)

	_, err := runtime.executeToolCall(context.Background(), models.ToolCall{
		ID: "tool-call-block-1",
		Function: models.ToolCallFunction{
			Name:      "echo_tool",
			Arguments: `{"text":"blocked"}`,
		},
	}, "hook-user", "agent:agent-block-hook-test:user:hook-user:ws")
	if err == nil {
		t.Fatalf("expected tool call to be blocked")
	}
	if !strings.Contains(err.Error(), "blocked by mock hook") {
		t.Fatalf("unexpected block error: %v", err)
	}
}

func freeBlockHookTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen ephemeral port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
