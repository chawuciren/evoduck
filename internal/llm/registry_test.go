package llm

import (
	"context"
	"testing"

	"github.com/chawuciren/evoduck/pkg/config"
)

func TestListModelsForProviderConfigUsesSingleProviderConfig(t *testing.T) {
	models, err := ListModelsForProviderConfig(context.Background(), "openai-compatible", config.ProviderConfig{
		Type:         "openai-compatible",
		BaseURL:      "http://127.0.0.1:8080",
		DefaultModel: "seed-model",
		Models:       []config.ProviderModelConfig{{ID: "seed-model", Name: "seed-model", Type: config.ProviderModelTypeChat}, {ID: "other-model", Name: "other-model", Type: config.ProviderModelTypeChat}},
	})
	if err != nil {
		t.Fatalf("ListModelsForProviderConfig returned error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 builtin models, got %d", len(models))
	}
	if models[0].ID != "seed-model" || models[1].ID != "other-model" {
		t.Fatalf("unexpected models order/content: %+v", models)
	}
}

func TestRegistryRoutesGoogleAIStudioToGeminiProvider(t *testing.T) {
	r := &Registry{}
	provider, err := r.newProvider("google-ai-studio", config.ProviderConfig{
		Type:         "google-ai-studio",
		APIKey:       "test-key",
		DefaultModel: "gemini-2.0-flash",
	})
	if err != nil {
		t.Fatalf("newProvider returned error: %v", err)
	}
	if _, ok := provider.(*GeminiProvider); !ok {
		t.Fatalf("expected *GeminiProvider, got %T", provider)
	}
}

func TestRegistryRoutesOpenRouterToOpenRouterProvider(t *testing.T) {
	r := &Registry{}
	provider, err := r.newProvider("openrouter", config.ProviderConfig{
		Type:         "openrouter",
		APIKey:       "test-key",
		DefaultModel: "openai/gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("newProvider returned error: %v", err)
	}
	if _, ok := provider.(*OpenRouterProvider); !ok {
		t.Fatalf("expected *OpenRouterProvider, got %T", provider)
	}
}

func TestRegistryRoutesCohereToCohereProvider(t *testing.T) {
	r := &Registry{}
	provider, err := r.newProvider("cohere", config.ProviderConfig{
		Type:         "cohere",
		APIKey:       "test-key",
		DefaultModel: "command-a-03-2025",
	})
	if err != nil {
		t.Fatalf("newProvider returned error: %v", err)
	}
	if _, ok := provider.(*CohereProvider); !ok {
		t.Fatalf("expected *CohereProvider, got %T", provider)
	}
}

func TestRegistryRoutesReplicateToReplicateProvider(t *testing.T) {
	r := &Registry{}
	provider, err := r.newProvider("replicate", config.ProviderConfig{
		Type:         "replicate",
		APIKey:       "test-key",
		DefaultModel: "meta/llama-3.1-70b-instruct",
	})
	if err != nil {
		t.Fatalf("newProvider returned error: %v", err)
	}
	if _, ok := provider.(*ReplicateProvider); !ok {
		t.Fatalf("expected *ReplicateProvider, got %T", provider)
	}
}

func TestRegistryRoutesDashScopeToDashScopeProvider(t *testing.T) {
	dashScopeTypes := []string{"dashscope", "dashscope-cn", "dashscope-coding", "dashscope-coding-cn"}
	for _, providerType := range dashScopeTypes {
		r := &Registry{}
		provider, err := r.newProvider(providerType, config.ProviderConfig{
			Type:         providerType,
			APIKey:       "test-key",
			DefaultModel: "qwen-plus",
		})
		if err != nil {
			t.Fatalf("newProvider for %s returned error: %v", providerType, err)
		}
		if _, ok := provider.(*DashScopeProvider); !ok {
			t.Fatalf("expected *DashScopeProvider for %s, got %T", providerType, provider)
		}
	}
}

func TestRegistryRoutesXAIToXAIProvider(t *testing.T) {
	r := &Registry{}
	provider, err := r.newProvider("xai", config.ProviderConfig{
		Type:         "xai",
		APIKey:       "test-key",
		DefaultModel: "grok-3-mini",
	})
	if err != nil {
		t.Fatalf("newProvider returned error: %v", err)
	}
	if _, ok := provider.(*XAIProvider); !ok {
		t.Fatalf("expected *XAIProvider, got %T", provider)
	}
}

func TestRegistryRoutesMistralToMistralProvider(t *testing.T) {
	r := &Registry{}
	provider, err := r.newProvider("mistral", config.ProviderConfig{
		Type:         "mistral",
		APIKey:       "test-key",
		DefaultModel: "mistral-small-latest",
	})
	if err != nil {
		t.Fatalf("newProvider returned error: %v", err)
	}
	if _, ok := provider.(*MistralProvider); !ok {
		t.Fatalf("expected *MistralProvider, got %T", provider)
	}
}

func TestRegistryRoutesPerplexityToPerplexityProvider(t *testing.T) {
	r := &Registry{}
	provider, err := r.newProvider("perplexity", config.ProviderConfig{
		Type:         "perplexity",
		APIKey:       "test-key",
		DefaultModel: "sonar-pro",
	})
	if err != nil {
		t.Fatalf("newProvider returned error: %v", err)
	}
	if _, ok := provider.(*PerplexityProvider); !ok {
		t.Fatalf("expected *PerplexityProvider, got %T", provider)
	}
}
