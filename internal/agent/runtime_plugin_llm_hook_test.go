package agent

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/plugin"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/internal/skill"
	"github.com/chawuciren/evoduck/internal/tools"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type stubProvider struct{}

func (s *stubProvider) Name() string { return "stub" }
func (s *stubProvider) Chat(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	return &models.Response{Content: "stub llm reply"}, nil
}
func (s *stubProvider) ChatStream(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	ch := make(chan models.StreamEvent, 2)
	ch <- models.StreamEvent{Type: "content", Content: "stub llm reply"}
	ch <- models.StreamEvent{Type: "stop"}
	close(ch)
	return ch, nil
}
func (s *stubProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return s.Chat(ctx, messages, tools)
}
func (s *stubProvider) SetDefaultOptions(_ llm.ChatOptions)                        {}
func (s *stubProvider) GetMaxContextTokens() int                                   { return 8192 }
func (s *stubProvider) BuiltinModels() []llm.ProviderModel                         { return nil }
func (s *stubProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) { return nil, nil }
func (s *stubProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error)  { return nil, nil }

func llmHookTestProxyDecider() *proxy.Decider {
	return proxy.NewDecider(config.ProxyConfig{Enabled: false})
}

func TestRuntimeTriggersAfterLLMCompleteObserverHook(t *testing.T) {
	tempDir := t.TempDir()
	hookFile := filepath.Join(tempDir, "llm-hook-event.json")
	mockHookPath := filepath.Clean(filepath.Join("..", "..", "plugins", "mock-hook"))

	pluginCfg := config.PluginConfig{
		WSServer: config.WSServerConfig{Host: "127.0.0.1", Port: freeLLMHookTestPort(t)},
		Plugins: map[string]config.PluginDef{
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

	pluginMgr := plugin.NewManager(pluginCfg, llmHookTestProxyDecider())
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
	promptBuilder := NewPromptBuilder(tempDir, "agent-llm-hook-test", tempDir, toolReg, skill.NewLoader(tempDir, tempDir))
	runtime := NewRuntime("agent-llm-hook-test", tempDir, &stubProvider{}, toolReg, promptBuilder, models.RoleAdmin, nil, true, pluginMgr)

	sess := session.NewSession("webchat:llm-hook-user", "llm-hook-session", nil)
	if err := runtime.Run(context.Background(), sess, "say hi"); err != nil {
		t.Fatalf("runtime run: %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(hookFile)
		if err == nil {
			body := string(content)
			if !strings.Contains(body, `"content":"stub llm reply"`) {
				t.Fatalf("hook payload missing llm content: %s", body)
			}
			if !strings.Contains(body, `"user_id":"llm-hook-user"`) {
				t.Fatalf("hook payload missing user id: %s", body)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for llm hook event file")
}

func freeLLMHookTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen ephemeral port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
