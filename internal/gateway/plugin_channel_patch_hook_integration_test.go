package gateway

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/internal/agent"
	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/plugin"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

func testProxyDecider() *proxy.Decider {
	return proxy.NewDecider(config.ProxyConfig{Enabled: false})
}

func TestGatewayAppliesBeforeMessageSendPatch(t *testing.T) {
	tempDir := t.TempDir()
	ackFile := filepath.Join(tempDir, "mock-channel-ack.txt")
	mockProviderPath := filepath.Clean(filepath.Join("..", "..", "plugins", "mock-provider"))
	mockChannelPath := filepath.Clean(filepath.Join("..", "..", "plugins", "mock-channel"))
	mockHookPath := filepath.Clean(filepath.Join("..", "..", "plugins", "mock-hook"))

	cfg := &config.Config{
		Gateway:      config.GatewayConfig{Host: "127.0.0.1", Port: 18791},
		DefaultAgent: "admin-bot",
		DataDir:      tempDir,
		Shared:       config.SharedConfig{SkillsDir: filepath.Join(tempDir, "skills")},
		LLM:          config.LLMConfig{DefaultProvider: "mock-provider", DefaultModel: "mock-model", Providers: map[string]config.ProviderConfig{}},
		Agents: map[string]config.AgentConfig{
			"admin-bot": {Workspace: filepath.Join(tempDir, "agent-workspace"), Role: "admin", Provider: "mock-provider", Model: "mock-model", UserIsolation: config.UserIsolationConfig{AutoCreate: true, AutoProfile: false}},
		},
		Channels: config.ChannelsConfig{"mock-channel": {Type: "webchat", Agent: "admin-bot", Role: "admin"}},
		Tools:    config.ToolsConfig{BackendCall: config.BackendCallConfig{Endpoints: map[string]config.EndpointConfig{}}},
		Memory:   defaultTestMemoryConfig(tempDir),
		Scheduler: config.SchedulerConfig{SystemTasks: config.SystemSchedulerTasksConfig{
			MemoryCuration:     config.SystemTaskConfig{Schedule: "0 * * * *"},
			ExperienceCuration: config.SystemTaskConfig{Schedule: "0 3 * * *"},
		}},
		MCP: config.MCPConfig{Servers: map[string]config.MCPServerConfig{}},
		Plugins: config.PluginConfig{
			WSServer: config.WSServerConfig{Host: "127.0.0.1", Port: freeGatewayPatchTestPort(t)},
			Plugins: map[string]config.PluginDef{
				"mock-provider": {Enabled: true, Type: "local", Command: []string{"go", "run", mockProviderPath}, Restart: "never", RequestTimeout: 3000},
				"mock-channel":  {Enabled: true, Type: "local", Command: []string{"go", "run", mockChannelPath}, Environment: map[string]string{"MOCK_CHANNEL_ACK_FILE": ackFile, "MOCK_CHANNEL_MESSAGE_DELAY_MS": "2500"}, Restart: "never", RequestTimeout: 3000},
				"mock-hook":     {Enabled: true, Type: "local", Command: []string{"go", "run", mockHookPath}, Environment: map[string]string{"MOCK_HOOK_PATCH_EVENT": "before_message_send", "MOCK_HOOK_PATCH_VALUE": "patched outgoing content"}, Restart: "never", RequestTimeout: 3000},
			},
		},
	}

	llmReg, err := llm.NewRegistry(cfg.LLM, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	decider := testProxyDecider()
	pluginMgr := plugin.NewManager(cfg.Plugins, decider)
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
	for _, providerAdapter := range pluginMgr.ListProviderAdapters() {
		if err := llmReg.RegisterDynamic(providerAdapter.Name(), providerAdapter); err != nil {
			t.Fatalf("register provider adapter: %v", err)
		}
	}
	agentMgr := agent.NewManager(llmReg, cfg.DataDir, cfg.Shared.SkillsDir, cfg.Tools.BackendCall, cfg.Tools.Session, cfg.Memory, &cfg.MCP, decider, pluginMgr)
	if err := agentMgr.Register("admin-bot", cfg.Agents["admin-bot"]); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	gw := New(cfg, "", llmReg, agentMgr, pluginMgr, nil)
	gw.startChannels()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(ackFile)
		if err == nil {
			if string(content) != "patched outgoing content" {
				t.Fatalf("unexpected patched outgoing content: %q", string(content))
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for patched channel reply")
}

func freeGatewayPatchTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen ephemeral port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
