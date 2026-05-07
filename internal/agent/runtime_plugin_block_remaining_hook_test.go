package agent

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/internal/plugin"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

func remainingHookTestProxyDecider() *proxy.Decider {
	return proxy.NewDecider(config.ProxyConfig{Enabled: false})
}

func TestRuntimeBlocksLLMCallViaBeforeLLMCallHook(t *testing.T) {
	tempDir := t.TempDir()
	mockHookPath := filepath.Clean(filepath.Join("..", "..", "plugins", "mock-hook"))

	pluginCfg := config.PluginConfig{
		WSServer: config.WSServerConfig{Host: "127.0.0.1", Port: freeRemainingHookTestPort(t)},
		Plugins: map[string]config.PluginDef{
			"mock-hook": {
				Enabled:        true,
				Type:           "local",
				Command:        []string{"go", "run", mockHookPath},
				Environment:    map[string]string{"MOCK_HOOK_BLOCK_EVENT": "before_llm_call"},
				Restart:        "never",
				RequestTimeout: 3000,
			},
		},
	}

	pluginMgr := plugin.NewManager(pluginCfg, remainingHookTestProxyDecider())
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
	promptBuilder := NewPromptBuilder(tempDir, "agent-before-llm-hook-test", tempDir, toolReg, skill.NewLoader(tempDir, tempDir))
	runtime := NewRuntime("agent-before-llm-hook-test", tempDir, &stubProvider{}, toolReg, promptBuilder, models.RoleAdmin, nil, true, pluginMgr)
	sess := session.NewSession("webchat:before-llm-user", "before-llm-session", nil)

	err := runtime.Run(context.Background(), sess, "say hi")
	if err == nil {
		t.Fatalf("expected llm call to be blocked")
	}
	if !strings.Contains(err.Error(), "blocked by mock hook") {
		t.Fatalf("unexpected llm block error: %v", err)
	}
}

func TestRuntimeBlocksAgentStartViaBeforeAgentStartHook(t *testing.T) {
	tempDir := t.TempDir()
	mockHookPath := filepath.Clean(filepath.Join("..", "..", "plugins", "mock-hook"))

	pluginCfg := config.PluginConfig{
		WSServer: config.WSServerConfig{Host: "127.0.0.1", Port: freeRemainingHookTestPort(t)},
		Plugins: map[string]config.PluginDef{
			"mock-hook": {
				Enabled:        true,
				Type:           "local",
				Command:        []string{"go", "run", mockHookPath},
				Environment:    map[string]string{"MOCK_HOOK_BLOCK_EVENT": "before_agent_start"},
				Restart:        "never",
				RequestTimeout: 3000,
			},
		},
	}

	pluginMgr := plugin.NewManager(pluginCfg, remainingHookTestProxyDecider())
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
	promptBuilder := NewPromptBuilder(tempDir, "agent-before-start-hook-test", tempDir, toolReg, skill.NewLoader(tempDir, tempDir))
	runtime := NewRuntime("agent-before-start-hook-test", tempDir, &stubProvider{}, toolReg, promptBuilder, models.RoleAdmin, nil, true, pluginMgr)
	sess := session.NewSession("webchat:before-start-user", "before-start-session", nil)

	err := runtime.Run(context.Background(), sess, "say hi")
	if err == nil {
		t.Fatalf("expected agent start to be blocked")
	}
	if !strings.Contains(err.Error(), "blocked by mock hook") {
		t.Fatalf("unexpected agent block error: %v", err)
	}
}

func freeRemainingHookTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen ephemeral port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
