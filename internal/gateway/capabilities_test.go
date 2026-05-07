package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/internal/agent"
	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/gorilla/websocket"
)

type capabilityTestProvider struct{}

func (p *capabilityTestProvider) Name() string { return "stub" }
func (p *capabilityTestProvider) Chat(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (*models.Response, error) {
	return &models.Response{Content: "ok"}, nil
}
func (p *capabilityTestProvider) ChatStream(_ context.Context, _ []models.Message, _ []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	ch := make(chan models.StreamEvent)
	close(ch)
	return ch, nil
}
func (p *capabilityTestProvider) ChatWithOptions(_ context.Context, _ []models.Message, _ []models.ToolDefinition, _ llm.ChatOptions) (*models.Response, error) {
	return &models.Response{Content: "ok"}, nil
}
func (p *capabilityTestProvider) SetDefaultOptions(_ llm.ChatOptions) {}
func (p *capabilityTestProvider) GetMaxContextTokens() int            { return 8192 }
func (p *capabilityTestProvider) BuiltinModels() []llm.ProviderModel {
	return []llm.ProviderModel{{ID: "stub-model", Name: "Stub Model", ContextWindow: 8192, MaxTokens: 4096, SupportsTools: true, SupportsStreaming: true}}
}
func (p *capabilityTestProvider) FetchModels(_ context.Context) ([]llm.ProviderModel, error) {
	return nil, nil
}
func (p *capabilityTestProvider) ListModels(_ context.Context) ([]llm.ProviderModel, error) {
	return p.BuiltinModels(), nil
}

func newCapabilityTestGateway(t *testing.T) *Gateway {
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
	if err := llmReg.RegisterDynamic("stub", &capabilityTestProvider{}); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}
	agentMgr := agent.NewManager(llmReg, root, filepath.Join(root, "shared", "skills"), config.BackendCallConfig{}, config.SessionToolConfig{Enabled: true}, config.MemoryConfig{}, nil, nil, nil)
	if err := agentMgr.Register("agent-test", config.AgentConfig{
		Workspace: filepath.Join(root, "agent-test"),
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleAdmin),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	gw := New(&config.Config{
		DataDir:      root,
		DefaultAgent: "agent-test",
		Channels: config.ChannelsConfig{
			"webchat":   {Type: "webchat", Agent: "agent-test", Role: "admin"},
			"weixin-cs": {Type: "weixin", Agent: "agent-test", Role: "admin"},
		},
		Memory: defaultTestMemoryConfig(root),
	}, filepath.Join(root, "config.yaml"), llmReg, agentMgr, nil, nil)
	gw.initSlashHandler()
	return gw
}

func TestGetCapabilityAuditIncludesGatewayWebchatAndTools(t *testing.T) {
	gw := newCapabilityTestGateway(t)
	audit := gw.GetCapabilityAudit()
	if audit == nil {
		t.Fatal("expected capability audit")
	}
	if audit.Gateway.ResolvedAgent != "agent-test" {
		t.Fatalf("expected resolved agent agent-test, got %q", audit.Gateway.ResolvedAgent)
	}
	if len(audit.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(audit.Agents))
	}
	if audit.Agents[0].ToolCount == 0 {
		t.Fatal("expected tools to be reported")
	}
	foundTaskPlan := false
	for _, tool := range audit.Agents[0].Tools {
		if tool.Name == "task_plan" {
			foundTaskPlan = true
			if tool.Source != "builtin" {
				t.Fatalf("expected task_plan source builtin, got %q", tool.Source)
			}
		}
	}
	if !foundTaskPlan {
		t.Fatal("expected task_plan in tool list")
	}
	foundWebchat := false
	for _, ch := range audit.Channels {
		if ch.ID == "webchat" {
			foundWebchat = true
			if ch.Kind != "gateway_web" {
				t.Fatalf("expected webchat kind gateway_web, got %q", ch.Kind)
			}
			if ch.Registered {
				t.Fatal("expected webchat to not be bridge-registered")
			}
		}
	}
	if !foundWebchat {
		t.Fatal("expected webchat channel entry in audit")
	}
	if audit.Summary.AgentCount != 1 {
		t.Fatalf("expected summary agent count 1, got %d", audit.Summary.AgentCount)
	}
}

func TestHandleWSCapabilitiesReturnsAuditPayload(t *testing.T) {
	gw := newCapabilityTestGateway(t)
	server := httptest.NewServer(gw.withAuth(http.HandlerFunc(gw.handleWebSocket)))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?user_id=admin"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]interface{}{"action": "get_capabilities"}); err != nil {
		t.Fatalf("write capabilities request: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read capabilities response: %v", err)
	}
	var resp struct {
		Type         string           `json:"type"`
		Capabilities *CapabilityAudit `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Type != "capabilities" {
		t.Fatalf("expected response type capabilities, got %q", resp.Type)
	}
	if resp.Capabilities == nil {
		t.Fatal("expected capabilities payload")
	}
	if len(resp.Capabilities.Agents) != 1 {
		t.Fatalf("expected 1 agent in payload, got %d", len(resp.Capabilities.Agents))
	}
}
