package agent

import (
	"context"
	"net"
	"os"
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

func pluginHookTestProxyDecider() *proxy.Decider {
	return proxy.NewDecider(config.ProxyConfig{Enabled: false})
}

func TestRuntimeTriggersAfterToolCallObserverHook(t *testing.T) {
	tempDir := t.TempDir()
	hookFile := filepath.Join(tempDir, "hook-event.json")
	echoToolPath := filepath.Clean(filepath.Join("..", "..", "plugins", "echo-tool"))
	mockHookPath := filepath.Clean(filepath.Join("..", "..", "plugins", "mock-hook"))

	pluginCfg := config.PluginConfig{
		WSServer: config.WSServerConfig{Host: "127.0.0.1", Port: freeTestPort(t)},
		Plugins: map[string]config.PluginDef{
			"echo-tool": {
				Enabled:        true,
				Type:           "local",
				Command:        []string{"go", "run", echoToolPath},
				Restart:        "never",
				RequestTimeout: 3000,
			},
			"mock-hook": {
				Enabled:        true,
				Type:           "local",
				Command:        []string{"go", "run", mockHookPath},
				Environment:    map[string]string{"MOCK_HOOK_EVENT_FILE": hookFile},
				Restart:        "never",
				RequestTimeout: 3000,
			},
		},
	}

	pluginMgr := plugin.NewManager(pluginCfg, pluginHookTestProxyDecider())
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

	promptBuilder := NewPromptBuilder(tempDir, "agent-hook-test", tempDir, toolReg, skill.NewLoader(tempDir, tempDir))
	runtime := NewRuntime("agent-hook-test", tempDir, nil, toolReg, promptBuilder, models.RoleAdmin, nil, true, pluginMgr)

	result, err := runtime.executeToolCall(context.Background(), models.ToolCall{
		ID: "tool-call-1",
		Function: models.ToolCallFunction{
			Name:      "echo_tool",
			Arguments: `{"text":"hello hook"}`,
		},
	}, "hook-user", "agent:agent-hook-test:user:hook-user:ws")
	if err != nil {
		t.Fatalf("execute tool call: %v", err)
	}
	if !strings.Contains(result, "hello hook") {
		t.Fatalf("unexpected tool result: %q", result)
	}

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(hookFile)
		if err == nil {
			body := string(content)
			if !strings.Contains(body, `"name":"echo_tool"`) {
				t.Fatalf("hook payload missing tool name: %s", body)
			}
			if !strings.Contains(body, `"result":"hello hook"`) {
				t.Fatalf("hook payload missing tool result: %s", body)
			}
			if !strings.Contains(body, `"ok":true`) {
				t.Fatalf("hook payload missing success flag: %s", body)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for hook event file")
}

func freeTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen ephemeral port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
