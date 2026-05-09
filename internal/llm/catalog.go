package llm

import (
	"context"
	"sort"
	"strings"

	"github.com/chawuciren/evoduck/pkg/config"
)

type presetSpec struct {
	baseURL      string
	defaultModel string
	headers      map[string]string
}

var openAICompatiblePresets = buildOpenAICompatiblePresets()

func buildOpenAICompatiblePresets() map[string]presetSpec {
	presets := make(map[string]presetSpec)
	for _, preset := range config.ProviderPresets() {
		if preset.SetupKind != config.ProviderSetupKindOpenAICompatible {
			continue
		}
		switch preset.Type {
		case "openai-compatible", "openai-responses-compatible", "gemini-compatible", "anthropic-compatible", "azure":
			continue
		}
		presets[preset.Type] = presetSpec{
			baseURL:      preset.DefaultBaseURL,
			defaultModel: preset.DefaultModel,
			headers:      cloneStringMap(preset.Headers),
		}
	}
	return presets
}

func applyPreset(name string, cfg config.ProviderConfig) (config.ProviderConfig, bool) {
	preset, ok := openAICompatiblePresets[name]
	if !ok {
		return cfg, false
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = preset.baseURL
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = preset.defaultModel
	}
	if len(cfg.Models) == 0 && cfg.DefaultModel != "" {
		cfg.Models = []config.ProviderModelConfig{{ID: cfg.DefaultModel, Name: cfg.DefaultModel, Type: config.ProviderModelTypeChat}}
	}
	if len(preset.headers) > 0 && len(cfg.Headers) == 0 {
		cfg.Headers = cloneStringMap(preset.headers)
	}
	return cfg, true
}

func mergeProviderModel(primary, fallback ProviderModel) ProviderModel {
	if strings.TrimSpace(primary.Name) == "" {
		primary.Name = fallback.Name
	}
	if primary.ContextWindow == 0 {
		primary.ContextWindow = fallback.ContextWindow
	}
	if primary.MaxTokens == 0 {
		primary.MaxTokens = fallback.MaxTokens
	}
	if !primary.SupportsTools {
		primary.SupportsTools = fallback.SupportsTools
	}
	if !primary.SupportsStreaming {
		primary.SupportsStreaming = fallback.SupportsStreaming
	}
	if !primary.SupportsVision {
		primary.SupportsVision = fallback.SupportsVision
	}
	if !primary.Reasoning {
		primary.Reasoning = fallback.Reasoning
	}
	return primary
}

func MergeProviderModels(builtin, fetched []ProviderModel) []ProviderModel {
	merged := make([]ProviderModel, 0, len(builtin)+len(fetched))
	index := make(map[string]int, len(builtin)+len(fetched))

	appendModel := func(model ProviderModel) {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			return
		}
		model.ID = id
		if idx, ok := index[id]; ok {
			merged[idx] = mergeProviderModel(merged[idx], model)
			return
		}
		index[id] = len(merged)
		merged = append(merged, model)
	}

	for _, model := range builtin {
		appendModel(model)
	}
	for _, model := range fetched {
		appendModel(model)
	}

	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].ID < merged[j].ID
	})
	return merged
}

func listProviderModels(ctx context.Context, p Provider) ([]ProviderModel, error) {
	builtin := p.BuiltinModels()
	fetched, err := p.FetchModels(ctx)
	if err != nil {
		return builtin, nil
	}
	if len(fetched) == 0 {
		return builtin, nil
	}
	return MergeProviderModels(builtin, fetched), nil
}

func builtinModelsFromConfig(defaultModel string, configuredModels []config.ProviderModelConfig) []ProviderModel {
	result := make([]ProviderModel, 0, len(configuredModels)+1)
	seen := make(map[string]struct{}, len(configuredModels)+1)
	appendModel := func(model config.ProviderModelConfig) {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		name := strings.TrimSpace(model.Name)
		if name == "" {
			name = id
		}
		result = append(result, ProviderModel{
			ID:             id,
			Name:           name,
			ContextWindow:  model.ContextWindow,
			MaxTokens:      model.MaxOutputTokens,
			SupportsTools:  model.Capabilities.ToolUse,
			SupportsVision: model.Capabilities.Vision,
			Reasoning:      model.Capabilities.Reasoning,
		})
	}
	defaultModel = strings.TrimSpace(defaultModel)
	if defaultModel != "" {
		foundDefault := false
		for _, model := range configuredModels {
			if strings.TrimSpace(model.ID) == defaultModel {
				foundDefault = true
				break
			}
		}
		if !foundDefault {
			appendModel(config.ProviderModelConfig{ID: defaultModel, Name: defaultModel, Type: config.ProviderModelTypeChat})
		}
	}
	for _, model := range configuredModels {
		appendModel(model)
	}
	return result
}
