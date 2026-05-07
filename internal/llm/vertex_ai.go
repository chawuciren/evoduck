package llm

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

const (
	defaultVertexAIProject  = ""
	defaultVertexAILocation = "us-central1"
)

type VertexAIProvider struct {
	*GeminiProvider
}

func NewVertexAIProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (*VertexAIProvider, error) {
	project := strings.TrimSpace(cfg.Metadata["project"])
	location := strings.TrimSpace(cfg.Metadata["location"])
	if location == "" {
		location = strings.TrimSpace(cfg.Metadata["region"])
	}
	if location == "" {
		location = defaultVertexAILocation
	}
	if cfg.DefaultModel == "" && len(cfg.Models) > 0 {
		cfg.DefaultModel = strings.TrimSpace(cfg.Models[0].ID)
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "gemini-2.5-flash"
	}
	if len(cfg.Models) == 0 && cfg.DefaultModel != "" {
		cfg.Models = []config.ProviderModelConfig{{ID: cfg.DefaultModel, Name: cfg.DefaultModel, Type: config.ProviderModelTypeChat}}
	}

	clientCfg := &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  project,
		Location: location,
	}
	if cfg.BaseURL != "" {
		clientCfg.HTTPOptions = genai.HTTPOptions{BaseURL: cfg.BaseURL}
	}
	client, err := genai.NewClient(context.Background(), clientCfg)
	if err != nil {
		return nil, fmt.Errorf("init vertex ai client: %w", err)
	}

	provider := &GeminiProvider{name: name, client: client, model: cfg.DefaultModel, decider: decider}
	return &VertexAIProvider{GeminiProvider: provider}, nil
}

var _ Provider = (*VertexAIProvider)(nil)
