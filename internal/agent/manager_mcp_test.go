package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/mcp"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

// stubMCPProvider 用于 MCP wiring 测试的 LLM provider stub。
type stubMCPProvider struct{}

func (p *stubMCPProvider) Name() string { return "stub" }
func (p *stubMCPProvider) Chat(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	return &models.Response{Content: "ok"}, nil
}
func (p *stubMCPProvider) ChatStream(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	ch := make(chan models.StreamEvent)
	close(ch)
	return ch, nil
}
func (p *stubMCPProvider) ChatWithOptions(_ context.Context, _ []models.Message, _ []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return &models.Response{Content: "ok"}, nil
}
func (p *stubMCPProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *stubMCPProvider) GetMaxContextTokens() int            { return 8192 }
func (p *stubMCPProvider) BuiltinModels() []llm.ProviderModel {
	return []llm.ProviderModel{{ID: "stub-model", Name: "Stub", ContextWindow: 8192, MaxTokens: 4096, SupportsTools: true, SupportsStreaming: true}}
}
func (p *stubMCPProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) { return nil, nil }
func (p *stubMCPProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) { return p.BuiltinModels(), nil }

// newMCPWiringManager 构造一个 Manager，注入伪造的（总是成功的）MCP 连接逻辑。
func newMCPWiringManager(t *testing.T, serverTool string) *Manager {
	t.Helper()
	root := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &stubMCPProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}

	mcpCfg := &config.MCPConfig{
		Servers: map[string]config.MCPServerConfig{
			"mcp-srv": {Enabled: true, Type: "local", Command: []string{"x"}},
		},
	}
	mgr := NewManager(llmReg, root, filepath.Join(root, "skills"), config.BackendCallConfig{}, config.SessionToolConfig{Enabled: true}, config.MemoryConfig{}, mcpCfg, nil, nil)

	// 注册一个 agent
	if err := mgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agent-test"),
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	// 创建 mcp.Manager 并注入伪造连接（server 连接成功后把工具注册到 agent）
	mcpMgr := mcp.NewManager(mcpCfg, nil)
	mcpMgr.SetToolRegistrar(mgr)
	mcpMgr.SetConnectFunc(func(ctx context.Context, name string, cfg config.MCPServerConfig) (*mcp.Client, []*mcp.MCPToolWrapper, error) {
		c := mcp.NewFakeClient(name, true, serverTool)
		ws := make([]*mcp.MCPToolWrapper, 0)
		for _, tool := range c.GetAllTools() {
			ws = append(ws, mcp.NewMCPToolWrapper(c, tool))
		}
		return c, ws, nil
	})
	mgr.mu.Lock()
	mgr.mcpManager = mcpMgr
	mgr.mcpInitialized = true
	mgr.mu.Unlock()
	mcpMgr.Start(context.Background())
	return mgr
}

// TestMCPToolsPropagateToAgents 验证 server 连接成功后工具被注册到 agent。
func TestMCPToolsPropagateToAgents(t *testing.T) {
	mgr := newMCPWiringManager(t, "search")
	mgr.StartMCP()

	// 等待异步连接完成
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(mgr.MCPStatus().Servers) > 0 && mgr.MCPStatus().Online > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	ag, err := mgr.Get("agent-test")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if _, err := ag.Tools.Get("mcp-srv_search"); err != nil {
		t.Fatalf("expected MCP tool mcp-srv_search registered in agent, got: %v", err)
	}

	snap := mgr.MCPStatus()
	if snap.Online != 1 {
		t.Fatalf("expected 1 MCP server online, got %d", snap.Online)
	}
}

// TestMCPStatusReflectsState 验证 MCPStatus 返回正确状态。
func TestMCPStatusReflectsState(t *testing.T) {
	mgr := newMCPWiringManager(t, "t1")
	mgr.StartMCP()

	// 等待连接完成
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.MCPStatus().Online > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	snap := mgr.MCPStatus()
	if len(snap.Servers) != 1 {
		t.Fatalf("expected 1 server in status, got %d", len(snap.Servers))
	}
	s := snap.Servers[0]
	if s.Name != "mcp-srv" {
		t.Fatalf("expected server name mcp-srv, got %s", s.Name)
	}
	if !s.Online {
		t.Fatalf("expected server online, state=%s", s.State)
	}
	if s.ToolCount != 1 {
		t.Fatalf("expected 1 tool, got %d", s.ToolCount)
	}
}

// TestReconnectMCPTriggersReconnect 验证 ReconnectMCP 触发重连并返回 server 名。
func TestReconnectMCPTriggersReconnect(t *testing.T) {
	mgr := newMCPWiringManager(t, "tool")
	mgr.StartMCP()

	// 等待初始连接
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.MCPStatus().Online > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 全量 reconnect：已 connected 的 server 默认会被跳过 → 返回空列表
	triggered := mgr.ReconnectMCP(context.Background(), "", nil)
	if len(triggered) != 0 {
		t.Fatalf("expected no targets when all connected, got %v", triggered)
	}

	// 强制全量：返回该 server
	triggered = mgr.ReconnectMCP(context.Background(), "all", nil)
	if len(triggered) != 1 || triggered[0] != "mcp-srv" {
		t.Fatalf("expected [mcp-srv] forced, got %v", triggered)
	}
	// 等待强制重连完成
	deadline2 := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline2) {
		if mgr.MCPStatus().Online > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 指定名称重连
	triggered = mgr.ReconnectMCP(context.Background(), "mcp-srv", nil)
	if len(triggered) != 1 {
		t.Fatalf("expected single reconnect, got %v", triggered)
	}

	// 不存在的名称 → 空列表（静默 no-op）
	triggered = mgr.ReconnectMCP(context.Background(), "nope", nil)
	if len(triggered) != 0 {
		t.Fatalf("expected empty for unknown name, got %v", triggered)
	}
}

// TestUnregisterMCPToolsRemovesFromAgent 验证反注册会从 agent 工具表移除。
func TestUnregisterMCPToolsRemovesFromAgent(t *testing.T) {
	mgr := newMCPWiringManager(t, "alpha")
	mgr.StartMCP()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.MCPStatus().Online > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	ag, _ := mgr.Get("agent-test")
	if _, err := ag.Tools.Get("mcp-srv_alpha"); err != nil {
		t.Fatalf("expected tool present before unregister: %v", err)
	}

	mgr.UnregisterMCPTools("mcp-srv")

	if _, err := ag.Tools.Get("mcp-srv_alpha"); err == nil {
		t.Fatal("expected tool removed after unregister, but still present")
	}
}

// 防止未使用 import
var _ = strings.TrimSpace
