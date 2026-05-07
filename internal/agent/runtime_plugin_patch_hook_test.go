package agent

import (
	"context"
	"net"
	"path/filepath"
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

func patchHookTestProxyDecider() *proxy.Decider {
	return proxy.NewDecider(config.ProxyConfig{Enabled: false})
}

type captureProvider struct {
	messages []models.Message
}

func (p *captureProvider) Name() string { return "capture" }
func (p *captureProvider) Chat(_ context.Context, messages []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	p.messages = append([]models.Message(nil), messages...)
	return &models.Response{Content: "captured"}, nil
}
func (p *captureProvider) ChatStream(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	ch := make(chan models.StreamEvent)
	close(ch)
	return ch, nil
}
func (p *captureProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return p.Chat(ctx, messages, tools)
}
func (p *captureProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *captureProvider) GetMaxContextTokens() int            { return 8192 }
func (p *captureProvider) BuiltinModels() []llm.ProviderModel  { return nil }
func (p *captureProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *captureProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) { return nil, nil }

func TestRuntimeAppliesBeforeLLMCallPatch(t *testing.T) {
	tempDir := t.TempDir()
	mockHookPath := filepath.Clean(filepath.Join("..", "..", "plugins", "mock-hook"))

	pluginCfg := config.PluginConfig{
		WSServer: config.WSServerConfig{Host: "127.0.0.1", Port: freePatchHookTestPort(t)},
		Plugins: map[string]config.PluginDef{
			"mock-hook": {
				Enabled: true,
				Type:    "local",
				Command: []string{"go", "run", mockHookPath},
				Environment: map[string]string{
					"MOCK_HOOK_PATCH_EVENT": "before_llm_call",
					"MOCK_HOOK_PATCH_VALUE": "patched system instruction",
				},
				Restart:        "never",
				RequestTimeout: 3000,
			},
		},
	}

	pluginMgr := plugin.NewManager(pluginCfg, patchHookTestProxyDecider())
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
	promptBuilder := NewPromptBuilder(tempDir, "agent-patch-hook-test", tempDir, toolReg, skill.NewLoader(tempDir, tempDir))
	provider := &captureProvider{}
	runtime := NewRuntime("agent-patch-hook-test", tempDir, provider, toolReg, promptBuilder, models.RoleAdmin, nil, true, pluginMgr)
	sess := session.NewSession("webchat:patch-user", "patch-session", nil)

	if err := runtime.Run(context.Background(), sess, "say hi"); err != nil {
		t.Fatalf("runtime run: %v", err)
	}
	if len(provider.messages) == 0 {
		t.Fatalf("expected provider to receive messages")
	}
	if provider.messages[0].Role != "system" || provider.messages[0].Content != "patched system instruction" {
		t.Fatalf("unexpected patched message: %+v", provider.messages[0])
	}
}

func freePatchHookTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen ephemeral port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
