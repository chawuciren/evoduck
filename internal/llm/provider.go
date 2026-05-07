package llm

import (
	"context"

	"github.com/chawuciren/evoduck/pkg/models"
)

// ChatOptions LLM 调用选项
type ChatOptions struct {
	Model       string   // 本次调用使用的模型
	Temperature *float64 // 温度参数 (0.0-2.0)
	MaxTokens   int      // 最大生成 token 数
	TopP        *float64 // Top-p 采样
}

// Provider LLM 提供商接口
type ProviderModel struct {
	ID                string
	Name              string
	ContextWindow     int
	MaxTokens         int
	SupportsTools     bool
	SupportsStreaming bool
	SupportsVision    bool
	Reasoning         bool
}

// Provider LLM 提供商接口
type Provider interface {
	Name() string
	Chat(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error)
	ChatStream(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (<-chan models.StreamEvent, error)
	ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (*models.Response, error)
	SetDefaultOptions(opts ChatOptions)
	GetMaxContextTokens() int
	BuiltinModels() []ProviderModel
	FetchModels(ctx context.Context) ([]ProviderModel, error)
	ListModels(ctx context.Context) ([]ProviderModel, error)
}

type deferredToolImageReplayProvider interface {
	RequiresDeferredToolImageReplay() bool
}

func RequiresDeferredToolImageReplay(provider Provider) bool {
	if provider == nil {
		return false
	}
	if deferred, ok := provider.(deferredToolImageReplayProvider); ok {
		return deferred.RequiresDeferredToolImageReplay()
	}
	switch provider.(type) {
	case *OpenAIProvider, *OpenAICompatibleProvider:
		return true
	default:
		return false
	}
}
