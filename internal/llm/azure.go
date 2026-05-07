package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/proxy"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/azure"
	"github.com/openai/openai-go/v3/option"
)

const defaultAzureAPIVersion = "2024-06-01"

type AzureProvider struct {
	*OpenAIProvider
}

func NewAzureProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (*AzureProvider, error) {
	normalized, endpoint, apiVersion, err := normalizeAzureConfig(cfg)
	if err != nil {
		return nil, err
	}

	opts := []option.RequestOption{azure.WithEndpoint(endpoint, apiVersion)}
	if strings.TrimSpace(normalized.APIKey) != "" {
		opts = append(opts, azure.WithAPIKey(normalized.APIKey))
	}
	if len(normalized.Headers) > 0 {
		httpClient := &staticHeaderHTTPClient{base: http.Client{}, headers: normalized.Headers}
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	if decider != nil {
		httpClient := decider.ForLLM(name).HTTPClient
		opts = append(opts, option.WithHTTPClient(httpClient))
	}

	client := openai.NewClient(opts...)
	provider := &OpenAIProvider{name: name, client: &client, model: normalized.DefaultModel, config: normalized, decider: decider}
	return &AzureProvider{OpenAIProvider: provider}, nil
}

func (p *AzureProvider) FetchModels(ctx context.Context) ([]ProviderModel, error) {
	pager := p.client.Models.ListAutoPaging(ctx)
	result := make([]ProviderModel, 0)
	for pager.Next() {
		model := openAIProviderModelFromSDK(pager.Current())
		if model.ID == "" {
			continue
		}
		result = append(result, model)
	}
	if err := pager.Err(); err != nil {
		return nil, fmt.Errorf("azure list models: %w", err)
	}
	return result, nil

}

func (p *AzureProvider) ListModels(ctx context.Context) ([]ProviderModel, error) {
	return listProviderModels(ctx, p)
}

func (p *AzureProvider) BuiltinModels() []ProviderModel {
	models := builtinModelsFromConfig(p.config.DefaultModel, p.config.Models)
	for i := range models {
		models[i].SupportsTools = true
		models[i].SupportsStreaming = true
		if models[i].ContextWindow == 0 {
			models[i].ContextWindow = getAzureMaxContextTokens(models[i].ID)
		}
	}
	sort.SliceStable(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})
	return models

}

func normalizeAzureConfig(cfg config.ProviderConfig) (config.ProviderConfig, string, string, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return cfg, "", "", fmt.Errorf("azure base_url is required")
	}

	parsed, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil {
		return cfg, "", "", fmt.Errorf("invalid azure base_url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return cfg, "", "", fmt.Errorf("invalid azure base_url: must include scheme and host")
	}

	apiVersion := strings.TrimSpace(parsed.Query().Get("api-version"))
	if apiVersion == "" {
		apiVersion = defaultAzureAPIVersion
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""

	endpoint := strings.TrimRight(parsed.String(), "/")
	lowerEndpoint := strings.ToLower(endpoint)
	for _, suffix := range []string{"/openai/v1", "/openai"} {
		if strings.HasSuffix(lowerEndpoint, suffix) {
			endpoint = endpoint[:len(endpoint)-len(suffix)]
			lowerEndpoint = strings.ToLower(endpoint)
			break
		}
	}

	cfg.BaseURL = endpoint
	if cfg.DefaultModel == "" && len(cfg.Models) > 0 {
		cfg.DefaultModel = strings.TrimSpace(cfg.Models[0].ID)
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "gpt-4o"
	}
	if len(cfg.Models) == 0 && cfg.DefaultModel != "" {
		cfg.Models = []config.ProviderModelConfig{{ID: cfg.DefaultModel, Name: cfg.DefaultModel, Type: config.ProviderModelTypeChat}}
	}

	return cfg, endpoint, apiVersion, nil
}

func getAzureMaxContextTokens(model string) int {
	return getOpenAICompatibleMaxContextTokens(strings.ToLower(strings.TrimSpace(model)))
}

var _ Provider = (*AzureProvider)(nil)
