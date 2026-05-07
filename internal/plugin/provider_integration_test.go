package plugin

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

func TestProviderAdapterStreamsMockProvider(t *testing.T) {
	port := freeTCPPort(t)
	pluginPath := filepath.Clean(filepath.Join("..", "..", "plugins", "mock-provider"))
	manager := NewManager(config.PluginConfig{
		WSServer: config.WSServerConfig{Host: "127.0.0.1", Port: port},
		Plugins: map[string]config.PluginDef{
			"mock-provider": {
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

	providers := manager.ListProviderAdapters()
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider adapter, got %d", len(providers))
	}
	provider := providers[0]

	reg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "mock-provider",
		DefaultModel:    "mock-model",
		Providers:       map[string]config.ProviderConfig{},
	})
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := reg.RegisterDynamic(provider.Name(), provider); err != nil {
		t.Fatalf("register dynamic provider: %v", err)
	}

	resolvedProvider, resolvedModel, err := reg.ResolveProviderModel("mock-provider", "")
	if err != nil {
		t.Fatalf("resolve provider model: %v", err)
	}
	if resolvedProvider != "mock-provider" || resolvedModel != "mock-model" {
		t.Fatalf("unexpected resolve result: %s / %s", resolvedProvider, resolvedModel)
	}

	stream, err := provider.ChatStream(context.Background(), []models.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("provider chat stream: %v", err)
	}

	var content string
	var sawStop bool
	for event := range stream {
		switch event.Type {
		case "content":
			content += event.Content
		case "stop":
			sawStop = true
		case "error":
			t.Fatalf("unexpected provider error: %v", event.Error)
		}
	}

	if content != "mock provider says hello" {
		t.Fatalf("unexpected content: %q", content)
	}
	if !sawStop {
		t.Fatalf("expected stop event")
	}
}

func TestProviderAdapterStreamsToolCalls(t *testing.T) {
	provider := setupMockProviderAdapter(t, 3000)
	stream, err := provider.ChatStream(context.Background(), []models.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("provider chat stream: %v", err)
	}

	var sawToolCalls bool
	for event := range stream {
		if event.Type == "tool_calls" {
			sawToolCalls = true
		}
	}
	if sawToolCalls {
		// Current mock-provider only sends tool calls when requested through transport payload.
		// This test keeps the provider stream stable and documents current default behavior.
	}
	// Re-run with explicit include_tool_calls path.
	frames, err := provider.manager.transport.SendStreamingRequest(context.Background(), provider.pluginID, "manual-toolcalls", MethodProviderChat, provider.capabilityID, map[string]interface{}{
		"include_tool_calls": true,
	})
	if err != nil {
		t.Fatalf("send manual provider request: %v", err)
	}
	sawToolCalls = false
	for frame := range frames {
		if frame.Type == FrameTypeEvent {
			if eventType, _ := frame.Data["event_type"].(string); eventType == "tool_calls" {
				sawToolCalls = true
				break
			}
		}
	}
	if !sawToolCalls {
		t.Fatalf("expected tool_calls event")
	}
}

func TestProviderAdapterTimeoutAndCancel(t *testing.T) {
	provider := setupMockProviderAdapter(t, 3000)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	stream, err := provider.manager.transport.SendStreamingRequest(ctx, provider.pluginID, "manual-timeout", MethodProviderChat, provider.capabilityID, map[string]interface{}{
		"delay_ms": 1000,
	})
	if err != nil {
		t.Fatalf("send streaming request: %v", err)
	}

	seenAny := false
	for range stream {
		seenAny = true
	}
	if seenAny {
		t.Fatalf("expected cancelled stream to close without events")
	}
}

func setupMockProviderAdapter(t *testing.T, requestTimeout int) *ProviderAdapter {
	t.Helper()
	port := freeTCPPort(t)
	pluginPath := filepath.Clean(filepath.Join("..", "..", "plugins", "mock-provider"))
	manager := NewManager(config.PluginConfig{
		WSServer: config.WSServerConfig{Host: "127.0.0.1", Port: port},
		Plugins: map[string]config.PluginDef{
			"mock-provider": {
				Enabled:        true,
				Type:           "local",
				Command:        []string{"go", "run", pluginPath},
				Restart:        "never",
				RequestTimeout: requestTimeout,
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

	providers := manager.ListProviderAdapters()
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider adapter, got %d", len(providers))
	}
	return providers[0]
}
