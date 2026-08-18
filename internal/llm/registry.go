package llm

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type Registry struct {
	mu               sync.RWMutex
	providerConfigs  map[string]config.ProviderConfig
	dynamicProviders map[string]Provider
	defaultProvider  string
	defaultModel     string
	decider          *proxy.Decider
}

func NewRegistry(cfg config.LLMConfig, decider *proxy.Decider) (*Registry, error) {
	r := &Registry{
		providerConfigs:  make(map[string]config.ProviderConfig, len(cfg.Providers)),
		dynamicProviders: make(map[string]Provider),
		defaultProvider:  cfg.DefaultProvider,
		defaultModel:     cfg.DefaultModel,
		decider:          decider,
	}

	for name, pCfg := range cfg.Providers {
		r.providerConfigs[name] = pCfg
		if _, err := r.newProvider(name, pCfg); err != nil {
			return nil, fmt.Errorf("init provider %s: %w", name, err)
		}
	}

	return r, nil
}

// UpdateProviders 热替换静态 provider 配置（保留插件注册的动态 provider）。
// 先试创建所有新 provider 进行校验，任何一个失败则整体拒绝，旧配置不变。
func (r *Registry) UpdateProviders(cfg config.LLMConfig) error {
	// 校验阶段：试创建每个 provider，确保配置合法
	newConfigs := make(map[string]config.ProviderConfig, len(cfg.Providers))
	for name, pCfg := range cfg.Providers {
		if _, err := r.newProvider(name, pCfg); err != nil {
			return fmt.Errorf("validate provider %s: %w", name, err)
		}
		newConfigs[name] = pCfg
	}

	// 原子替换阶段
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providerConfigs = newConfigs
	r.defaultProvider = cfg.DefaultProvider
	r.defaultModel = cfg.DefaultModel
	return nil
}

func (r *Registry) newProvider(name string, pCfg config.ProviderConfig) (Provider, error) {
	decider := r.decider
	if presetCfg, ok := applyPreset(pCfg.Type, pCfg); ok {
		if presetCfg.Type == "deepseek" {
			return NewDeepSeekProvider(name, presetCfg, decider)
		}
		if presetCfg.Type == "google-ai-studio" {
			return NewGeminiProvider(name, presetCfg, decider)
		}
		if presetCfg.Type == "openrouter" {
			return NewOpenRouterProvider(name, presetCfg, decider)
		}
		if presetCfg.Type == "cohere" {
			return NewCohereProvider(name, presetCfg, decider)
		}
		if presetCfg.Type == "replicate" {
			return NewReplicateProvider(name, presetCfg, decider)
		}
		if isDashScopeProviderType(presetCfg.Type) {
			return NewDashScopeProvider(name, presetCfg, decider)
		}
		if presetCfg.Type == "xai" {
			return NewXAIProvider(name, presetCfg, decider)
		}
		if presetCfg.Type == "mistral" {
			return NewMistralProvider(name, presetCfg, decider)
		}
		if presetCfg.Type == "perplexity" {
			return NewPerplexityProvider(name, presetCfg, decider)
		}
		return NewOpenAICompatibleProvider(name, presetCfg, decider)
	}

	switch pCfg.Type {
	case "openai":
		return NewOpenAIProvider(name, pCfg, decider)
	case "openai-compatible":
		return NewOpenAICompatibleProvider(name, pCfg, decider)
	case "deepseek":
		return NewDeepSeekProvider(name, pCfg, decider)
	case "openrouter":
		return NewOpenRouterProvider(name, pCfg, decider)
	case "cohere":
		return NewCohereProvider(name, pCfg, decider)
	case "replicate":
		return NewReplicateProvider(name, pCfg, decider)
	case "dashscope", "dashscope-cn", "dashscope-coding", "dashscope-coding-cn":
		return NewDashScopeProvider(name, pCfg, decider)
	case "xai":
		return NewXAIProvider(name, pCfg, decider)
	case "mistral":
		return NewMistralProvider(name, pCfg, decider)
	case "perplexity":
		return NewPerplexityProvider(name, pCfg, decider)
	case "openai-responses-compatible":
		return NewOpenAIResponsesCompatibleProvider(name, pCfg, decider)
	case "anthropic":
		return NewAnthropicProvider(name, pCfg, decider)
	case "anthropic-compatible":
		return NewAnthropicCompatibleProvider(name, pCfg, decider)
	case "gemini":
		return NewGeminiProvider(name, pCfg, decider)
	case "google-ai-studio":
		return NewGeminiProvider(name, pCfg, decider)
	case "gemini-compatible":
		return NewGeminiCompatibleProvider(name, pCfg, decider)
	case "ollama":
		return NewOllamaProvider(name, pCfg, decider)
	case "bedrock":
		return NewBedrockProvider(name, pCfg, decider)
	case "vertex-ai":
		return NewVertexAIProvider(name, pCfg, decider)
	case "azure":
		return NewAzureProvider(name, pCfg, decider)
	default:
		return nil, fmt.Errorf("unknown provider type: %s", pCfg.Type)
	}
}

// getProvider 内部无锁读取（调用方需持有至少 RLock）
func (r *Registry) getProvider(name string) (Provider, error) {
	if provider, ok := r.dynamicProviders[name]; ok {
		return provider, nil
	}
	pCfg, ok := r.providerConfigs[name]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", name)
	}
	return r.newProvider(name, pCfg)
}

// Get 按名称获取 provider 实例（线程安全）
func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.getProvider(name)
}

func (r *Registry) RegisterDynamic(name string, provider Provider) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name == "" {
		return fmt.Errorf("dynamic provider name cannot be empty")
	}
	if provider == nil {
		return fmt.Errorf("dynamic provider cannot be nil")
	}
	if _, ok := r.providerConfigs[name]; ok {
		return fmt.Errorf("provider name conflict with static provider: %s", name)
	}
	if _, ok := r.dynamicProviders[name]; ok {
		return fmt.Errorf("dynamic provider already registered: %s", name)
	}
	r.dynamicProviders[name] = provider
	return nil
}

func (r *Registry) Default() (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.defaultProvider == "" {
		return nil, fmt.Errorf("no default provider configured")
	}
	return r.getProvider(r.defaultProvider)
}

func (r *Registry) DefaultProviderName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultProvider
}

func (r *Registry) DefaultModelName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultModel
}

func (r *Registry) ListProviderNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providerConfigs)+len(r.dynamicProviders))
	for name := range r.providerConfigs {
		names = append(names, name)
	}
	for name := range r.dynamicProviders {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func (r *Registry) ListModels(ctx context.Context, providerName string) ([]ProviderModel, error) {
	r.mu.RLock()
	provider, err := r.getProvider(providerName)
	r.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	return provider.ListModels(ctx)
}

func ListModelsForProviderConfig(ctx context.Context, providerName string, providerCfg config.ProviderConfig) ([]ProviderModel, error) {
	r := &Registry{
		providerConfigs: map[string]config.ProviderConfig{
			providerName: providerCfg,
		},
		defaultProvider: providerName,
		defaultModel:    providerCfg.DefaultModel,
	}
	return r.ListModels(ctx, providerName)
}

func (r *Registry) ResolveProviderModel(providerName, model string) (string, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	resolvedProvider := providerName
	if resolvedProvider == "" {
		resolvedProvider = r.defaultProvider
	}
	if resolvedProvider == "" {
		return "", "", fmt.Errorf("no provider configured")
	}

	pCfg, ok := r.providerConfigs[resolvedProvider]
	if !ok {
		provider, dynamicOK := r.dynamicProviders[resolvedProvider]
		if !dynamicOK {
			return "", "", fmt.Errorf("provider not found: %s", resolvedProvider)
		}
		resolvedModel := model
		if resolvedModel == "" {
			models := provider.BuiltinModels()
			if len(models) > 0 {
				resolvedModel = models[0].ID
			}
		}
		if resolvedModel == "" && resolvedProvider == r.defaultProvider {
			resolvedModel = r.defaultModel
		}
		if resolvedModel == "" {
			return "", "", fmt.Errorf("no model configured for provider: %s", resolvedProvider)
		}
		return resolvedProvider, resolvedModel, nil
	}

	resolvedModel := model
	if resolvedModel == "" {
		resolvedModel = pCfg.DefaultModel
	}
	if resolvedModel == "" && resolvedProvider == r.defaultProvider {
		resolvedModel = r.defaultModel
	}
	if resolvedModel == "" && len(pCfg.Models) > 0 {
		resolvedModel = strings.TrimSpace(pCfg.Models[0].ID)
	}
	if resolvedModel == "" {
		return "", "", fmt.Errorf("no model configured for provider: %s", resolvedProvider)
	}
	if len(pCfg.Models) > 0 && !containsModel(pCfg.Models, resolvedModel) {
		return "", "", fmt.Errorf("model '%s' is not declared under provider '%s'", resolvedModel, resolvedProvider)
	}

	return resolvedProvider, resolvedModel, nil
}

func containsModel(models []config.ProviderModelConfig, target string) bool {
	target = strings.TrimSpace(target)
	for _, model := range models {
		if strings.TrimSpace(model.ID) == target {
			return true
		}
	}
	return false
}
