package config

import "strings"

type ProviderSetupKind string

const (
	ProviderSetupKindOpenAICompatible ProviderSetupKind = "openai-compatible"
	ProviderSetupKindOpenAI           ProviderSetupKind = "openai"
	ProviderSetupKindGemini           ProviderSetupKind = "gemini"
	ProviderSetupKindAnthropic        ProviderSetupKind = "anthropic"
	ProviderSetupKindOllama           ProviderSetupKind = "ollama"
	ProviderSetupKindBedrock          ProviderSetupKind = "bedrock"
	ProviderSetupKindVertexAI         ProviderSetupKind = "vertex-ai"
)

type ProviderCatalogEntry struct {
	Type             string
	Label            string
	Group            string
	DefaultBaseURL   string
	DefaultModel     string
	Models           []ProviderModelConfig
	SupportsFirstRun bool
	SetupKind        ProviderSetupKind
	Aliases          []string
}

type ProviderPreset struct {
	Type             string
	Label            string
	Group            string
	DefaultBaseURL   string
	DefaultModel     string
	Models           []ProviderModelConfig
	SupportsFirstRun bool
	SetupKind        ProviderSetupKind
	Aliases          []string
	Headers          map[string]string
}

func catalogChatModel(id string, vision, reasoning, toolUse bool, contextWindow, maxOutputTokens int) ProviderModelConfig {
	return ProviderModelConfig{
		ID:   id,
		Name: id,
		Type: ProviderModelTypeChat,
		Capabilities: ProviderModelCapabilities{
			Vision:    vision,
			Reasoning: reasoning,
			ToolUse:   toolUse,
		},
		ContextWindow:   contextWindow,
		MaxOutputTokens: maxOutputTokens,
	}
}

func catalogEmbeddingModel(id string, contextWindow int) ProviderModelConfig {
	return ProviderModelConfig{
		ID:            id,
		Name:          id,
		Type:          ProviderModelTypeEmbedding,
		ContextWindow: contextWindow,
	}
}

func catalogRerankModel(id string, contextWindow int) ProviderModelConfig {
	return ProviderModelConfig{
		ID:            id,
		Name:          id,
		Type:          ProviderModelTypeRerank,
		ContextWindow: contextWindow,
	}
}

func cloneProviderModelConfigs(models []ProviderModelConfig) []ProviderModelConfig {
	if len(models) == 0 {
		return nil
	}
	cloned := make([]ProviderModelConfig, len(models))
	copy(cloned, models)
	return cloned
}

func defaultCatalogModels(defaultModel string) []ProviderModelConfig {
	defaultModel = strings.TrimSpace(defaultModel)
	if defaultModel == "" {
		return nil
	}
	return []ProviderModelConfig{catalogChatModel(defaultModel, false, false, true, 32768, 4096)}
}

var providerPresets = []ProviderPreset{
	{Type: "openai-compatible", Label: "OpenAI-Compatible API", Group: "Custom", DefaultBaseURL: defaultOpenAIBaseURL, DefaultModel: defaultOpenAIModel, Models: []ProviderModelConfig{
		catalogChatModel("gpt-4o", true, false, true, 128000, 16384),
		catalogChatModel("gpt-4o-mini", true, false, true, 128000, 16384),
		catalogChatModel("o3", true, true, true, 200000, 100000),
		catalogChatModel("o4-mini", true, true, true, 200000, 100000),
		catalogEmbeddingModel("text-embedding-3-small", 8192),
		catalogEmbeddingModel("text-embedding-3-large", 8192),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"1"}},
	{Type: "openai-responses-compatible", Label: "OpenAI Responses-Compatible API", Group: "Custom", DefaultBaseURL: defaultOpenAIBaseURL, DefaultModel: defaultOpenAIModel, Models: []ProviderModelConfig{
		catalogChatModel("gpt-4o", true, false, true, 128000, 16384),
		catalogChatModel("gpt-4o-mini", true, false, true, 128000, 16384),
		catalogChatModel("o3", true, true, true, 200000, 100000),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"2"}},
	{Type: "gemini-compatible", Label: "Gemini-Compatible API", Group: "Custom", DefaultBaseURL: "", DefaultModel: "gemini-2.5-flash", Models: []ProviderModelConfig{
		catalogChatModel("gemini-2.5-flash", true, true, true, 1048576, 65536),
		catalogChatModel("gemini-2.5-pro", true, true, true, 1048576, 65536),
		catalogEmbeddingModel("gemini-embedding-001", 2048),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"3"}},
	{Type: "anthropic-compatible", Label: "Anthropic-Compatible API", Group: "Custom", DefaultBaseURL: defaultAnthropicBaseURL, DefaultModel: defaultAnthropicModel, Models: []ProviderModelConfig{
		catalogChatModel("claude-sonnet-4-5", true, true, true, 200000, 64000),
		catalogChatModel("claude-opus-4-1", true, true, true, 200000, 32000),
		catalogChatModel("claude-3-5-haiku-latest", true, false, true, 200000, 8192),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"4"}},
	{Type: "ollama", Label: "Ollama", Group: "Local", DefaultBaseURL: "http://localhost:11434/v1", DefaultModel: "qwen2.5", Models: []ProviderModelConfig{
		catalogChatModel("qwen2.5", true, true, true, 128000, 8192),
		catalogChatModel("qwen2.5-coder", false, true, true, 128000, 8192),
		catalogChatModel("llama3.2", true, false, true, 128000, 8192),
		catalogEmbeddingModel("nomic-embed-text", 8192),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOllama, Aliases: []string{"5"}},
	{Type: "lmstudio", Label: "LM Studio", Group: "Local", DefaultBaseURL: "http://localhost:1234/v1", DefaultModel: "local-model", Models: defaultCatalogModels("local-model"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"6"}},
	{Type: "vllm", Label: "vLLM", Group: "Local", DefaultBaseURL: "http://localhost:8000/v1", DefaultModel: "default", Models: defaultCatalogModels("default"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"7"}},
	{Type: "litellm", Label: "LiteLLM", Group: "Local", DefaultBaseURL: "http://localhost:4000/v1", DefaultModel: "gpt-4o-mini", Models: []ProviderModelConfig{
		catalogChatModel("gpt-4o-mini", true, false, true, 128000, 16384),
		catalogChatModel("claude-sonnet-4-5", true, true, true, 200000, 64000),
		catalogChatModel("gemini-2.5-flash", true, true, true, 1048576, 65536),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"8"}},
	{Type: "openai", Label: "OpenAI", Group: "Vendors", DefaultBaseURL: defaultOpenAIBaseURL, DefaultModel: defaultOpenAIModel, Models: []ProviderModelConfig{
		catalogChatModel("gpt-4o", true, false, true, 128000, 16384),
		catalogChatModel("gpt-4o-mini", true, false, true, 128000, 16384),
		catalogChatModel("o3", true, true, true, 200000, 100000),
		catalogChatModel("o4-mini", true, true, true, 200000, 100000),
		catalogEmbeddingModel("text-embedding-3-small", 8192),
		catalogEmbeddingModel("text-embedding-3-large", 8192),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAI, Aliases: []string{"9"}},
	{Type: "gemini", Label: "Gemini", Group: "Vendors", DefaultBaseURL: "", DefaultModel: "gemini-2.5-flash", Models: []ProviderModelConfig{
		catalogChatModel("gemini-2.5-flash", true, true, true, 1048576, 65536),
		catalogChatModel("gemini-2.5-pro", true, true, true, 1048576, 65536),
		catalogEmbeddingModel("gemini-embedding-001", 2048),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindGemini, Aliases: []string{"10"}},
	{Type: "anthropic", Label: "Anthropic", Group: "Vendors", DefaultBaseURL: defaultAnthropicBaseURL, DefaultModel: defaultAnthropicModel, Models: []ProviderModelConfig{
		catalogChatModel("claude-sonnet-4-5", true, true, true, 200000, 64000),
		catalogChatModel("claude-opus-4-1", true, true, true, 200000, 32000),
		catalogChatModel("claude-3-5-haiku-latest", true, false, true, 200000, 8192),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindAnthropic, Aliases: []string{"11"}},
	{Type: "deepseek", Label: "DeepSeek", Group: "Vendors", DefaultBaseURL: "https://api.deepseek.com", DefaultModel: "deepseek-v4-pro", Models: []ProviderModelConfig{
		catalogChatModel("deepseek-v4-pro", false, true, true, 128000, 16384),
		catalogChatModel("deepseek-v4-flash", false, true, true, 128000, 16384),
		catalogEmbeddingModel("deepseek-embedding", 8192),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"12"}},
	{Type: "minimax", Label: "MiniMax", Group: "Vendors", DefaultBaseURL: "https://api.minimaxi.chat/v1", DefaultModel: "abab5-chat", Models: defaultCatalogModels("abab5-chat"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"13"}},
	{Type: "minimax-cn", Label: "MiniMax (China)", Group: "Vendors", DefaultBaseURL: "https://api.minimax.chat", DefaultModel: "abab5-chat", Models: defaultCatalogModels("abab5-chat"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"14"}},

	{Type: "openrouter", Label: "OpenRouter", Group: "Vendors", DefaultBaseURL: "https://openrouter.ai/api/v1", DefaultModel: "openai/gpt-4o-mini", Models: []ProviderModelConfig{
		catalogChatModel("openai/gpt-4o-mini", true, false, true, 128000, 16384),
		catalogChatModel("anthropic/claude-sonnet-4.5", true, true, true, 200000, 64000),
		catalogChatModel("google/gemini-2.5-flash", true, true, true, 1048576, 65536),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"15"}},
	{Type: "dashscope", Label: "DashScope", Group: "Vendors", DefaultBaseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", DefaultModel: "qwen-plus", Models: []ProviderModelConfig{
		catalogChatModel("qwen-plus", true, true, true, 131072, 8192),
		catalogChatModel("qwen-max", true, true, true, 131072, 8192),
		catalogEmbeddingModel("text-embedding-v4", 2048),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"16"}},
	{Type: "dashscope-cn", Label: "DashScope (China)", Group: "Vendors", DefaultBaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", DefaultModel: "qwen-plus", Models: []ProviderModelConfig{
		catalogChatModel("qwen-plus", true, true, true, 131072, 8192),
		catalogChatModel("qwen-max", true, true, true, 131072, 8192),
		catalogEmbeddingModel("text-embedding-v4", 2048),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"17"}},
	{Type: "dashscope-coding", Label: "DashScope Coding", Group: "Vendors", DefaultBaseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", DefaultModel: "qwen3-coder-plus", Models: []ProviderModelConfig{
		catalogChatModel("qwen3-coder-plus", false, true, true, 262144, 16384),
		catalogChatModel("qwen-coder-plus", false, true, true, 262144, 16384),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"18"}},
	{Type: "dashscope-coding-cn", Label: "DashScope Coding (China)", Group: "Vendors", DefaultBaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", DefaultModel: "qwen-coder-plus", Models: []ProviderModelConfig{
		catalogChatModel("qwen-coder-plus", false, true, true, 262144, 16384),
		catalogChatModel("qwen3-coder-plus", false, true, true, 262144, 16384),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"19"}},
	{Type: "xai", Label: "xAI", Group: "Vendors", DefaultBaseURL: "https://api.x.ai/v1", DefaultModel: "grok-3-mini", Models: []ProviderModelConfig{
		catalogChatModel("grok-3-mini", true, true, true, 131072, 16384),
		catalogChatModel("grok-3", true, true, true, 131072, 16384),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"20"}},
	{Type: "groq", Label: "Groq", Group: "Vendors", DefaultBaseURL: "https://api.groq.com/openai/v1", DefaultModel: "llama-3.3-70b-versatile", Models: []ProviderModelConfig{
		catalogChatModel("llama-3.3-70b-versatile", false, false, true, 131072, 8192),
		catalogChatModel("qwen/qwen3-32b", false, true, true, 131072, 8192),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"21"}},
	{Type: "mistral", Label: "Mistral", Group: "Vendors", DefaultBaseURL: "https://api.mistral.ai/v1", DefaultModel: "mistral-small-latest", Models: []ProviderModelConfig{
		catalogChatModel("mistral-small-latest", false, false, true, 131072, 8192),
		catalogChatModel("pixtral-large-latest", true, false, true, 131072, 8192),
		catalogEmbeddingModel("mistral-embed", 8192),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"22"}},
	{Type: "together", Label: "Together", Group: "Vendors", DefaultBaseURL: "https://api.together.xyz/v1", DefaultModel: "meta-llama/Llama-3.3-70B-Instruct-Turbo", Models: defaultCatalogModels("meta-llama/Llama-3.3-70B-Instruct-Turbo"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"23"}},
	{Type: "fireworks", Label: "Fireworks", Group: "Vendors", DefaultBaseURL: "https://api.fireworks.ai/inference/v1", DefaultModel: "accounts/fireworks/models/llama-v3p1-70b-instruct", Models: defaultCatalogModels("accounts/fireworks/models/llama-v3p1-70b-instruct"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"24"}},

	{Type: "perplexity", Label: "Perplexity", Group: "Vendors", DefaultBaseURL: "https://api.perplexity.ai", DefaultModel: "sonar-pro", Models: []ProviderModelConfig{
		catalogChatModel("sonar-pro", false, true, true, 128000, 8192),
		catalogChatModel("sonar", false, false, true, 128000, 8192),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"25"}},
	{Type: "moonshot", Label: "Moonshot", Group: "Vendors", DefaultBaseURL: "https://api.moonshot.ai/v1", DefaultModel: "moonshot-v1-8k", Models: defaultCatalogModels("moonshot-v1-8k"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"26"}},
	{Type: "nvidia", Label: "NVIDIA", Group: "Vendors", DefaultBaseURL: "https://integrate.api.nvidia.com/v1", DefaultModel: "meta/llama-3.1-70b-instruct", Models: defaultCatalogModels("meta/llama-3.1-70b-instruct"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"27"}},
	{Type: "cloudflare-ai-gateway", Label: "Cloudflare AI Gateway", Group: "Vendors", DefaultBaseURL: "https://gateway.ai.cloudflare.com/v1/<account_id>/<gateway_id>/openai", DefaultModel: "gpt-4o-mini", Models: defaultCatalogModels("gpt-4o-mini"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"28"}},
	{Type: "vercel-ai-gateway", Label: "Vercel AI Gateway", Group: "Vendors", DefaultBaseURL: "https://ai-gateway.vercel.sh/v1", DefaultModel: "openai/gpt-4o-mini", Models: defaultCatalogModels("openai/gpt-4o-mini"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"29"}},
	{Type: "helicone", Label: "Helicone", Group: "Vendors", DefaultBaseURL: "https://gateway.helicone.ai/v1", DefaultModel: "gpt-4o-mini", Models: defaultCatalogModels("gpt-4o-mini"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"30"}},
	{Type: "portkey", Label: "Portkey", DefaultBaseURL: "https://api.portkey.ai/v1", DefaultModel: "openai/gpt-4o-mini", Models: defaultCatalogModels("openai/gpt-4o-mini"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"31"}},

	{Type: "cohere", Label: "Cohere", DefaultBaseURL: "https://api.cohere.ai/compatibility/v1", DefaultModel: "command-a-03-2025", Models: []ProviderModelConfig{
		catalogChatModel("command-a-03-2025", false, false, true, 256000, 8192),
		catalogEmbeddingModel("embed-v4.0", 131072),
		catalogRerankModel("rerank-v3.5", 4096),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"32"}},
	{Type: "novita", Label: "Novita", DefaultBaseURL: "https://api.novita.ai/openai", DefaultModel: "deepseek/deepseek-v3-0324", Models: defaultCatalogModels("deepseek/deepseek-v3-0324"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"33"}},
	{Type: "bedrock", Label: "Amazon Bedrock", DefaultBaseURL: "", DefaultModel: "anthropic.claude-3-5-sonnet-20240620-v1:0", Models: []ProviderModelConfig{
		catalogChatModel("anthropic.claude-3-5-sonnet-20240620-v1:0", true, true, true, 200000, 8192),
		catalogChatModel("us.anthropic.claude-3-7-sonnet-20250219-v1:0", true, true, true, 200000, 8192),
		catalogEmbeddingModel("amazon.titan-embed-text-v2:0", 8192),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindBedrock, Aliases: []string{"34"}},
	{Type: "vertex-ai", Label: "Vertex AI", DefaultBaseURL: "", DefaultModel: "gemini-2.5-flash", Models: []ProviderModelConfig{
		catalogChatModel("gemini-2.5-flash", true, true, true, 1048576, 65536),
		catalogChatModel("gemini-2.5-pro", true, true, true, 1048576, 65536),
		catalogEmbeddingModel("text-embedding-005", 2048),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindVertexAI, Aliases: []string{"35"}},
	{Type: "azure", Label: "Azure OpenAI", DefaultBaseURL: "https://<resource>.openai.azure.com/?api-version=2024-06-01", DefaultModel: "gpt-4o", Models: []ProviderModelConfig{
		catalogChatModel("gpt-4o", true, false, true, 128000, 16384),
		catalogChatModel("gpt-4o-mini", true, false, true, 128000, 16384),
		catalogEmbeddingModel("text-embedding-3-large", 8192),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"36"}},
	{Type: "google-ai-studio", Label: "Google AI Studio", DefaultBaseURL: "https://generativelanguage.googleapis.com/v1beta", DefaultModel: "gemini-2.0-flash", Models: []ProviderModelConfig{
		catalogChatModel("gemini-2.0-flash", true, true, true, 1048576, 65536),
		catalogChatModel("gemini-2.5-flash", true, true, true, 1048576, 65536),
		catalogEmbeddingModel("gemini-embedding-001", 2048),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"37"}},
	{Type: "siliconflow", Label: "SiliconFlow", DefaultBaseURL: "https://api.siliconflow.cn/v1", DefaultModel: "Qwen/Qwen2.5-7B-Instruct", Models: defaultCatalogModels("Qwen/Qwen2.5-7B-Instruct"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"38"}},
	{Type: "zhipu", Label: "Zhipu", DefaultBaseURL: "https://open.bigmodel.cn/api/paas-international/v4", DefaultModel: "glm-4-flash", Models: []ProviderModelConfig{
		catalogChatModel("glm-4-flash", true, false, true, 128000, 8192),
		catalogChatModel("glm-4.5", true, true, true, 128000, 8192),
		catalogEmbeddingModel("embedding-3", 8192),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"39"}},
	{Type: "zhipu-cn", Label: "Zhipu (China)", DefaultBaseURL: "https://open.bigmodel.cn/api/paas/v4", DefaultModel: "glm-4-flash", Models: []ProviderModelConfig{
		catalogChatModel("glm-4-flash", true, false, true, 128000, 8192),
		catalogChatModel("glm-4.5", true, true, true, 128000, 8192),
		catalogEmbeddingModel("embedding-3", 8192),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"40"}},
	{Type: "zhipu-coding", Label: "Zhipu Coding", DefaultBaseURL: "https://open.bigmodel.cn/api/paas-international/v4", DefaultModel: "glm-4-codegemma", Models: defaultCatalogModels("glm-4-codegemma"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"41"}},
	{Type: "zhipu-coding-cn", Label: "Zhipu Coding (China)", DefaultBaseURL: "https://open.bigmodel.cn/api/paas/v4", DefaultModel: "glm-4-codegemma", Models: defaultCatalogModels("glm-4-codegemma"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"42"}},

	{Type: "baidu-qianfan", Label: "Baidu Qianfan", DefaultBaseURL: "https://qianfan.baidubce.com/v2", DefaultModel: "ernie-4.0-8k", Models: defaultCatalogModels("ernie-4.0-8k"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"43"}},
	{Type: "tencent-hunyuan", Label: "Tencent Hunyuan", DefaultBaseURL: "https://api.hunyuan.cloud.tencent.com/v1", DefaultModel: "hunyuan-lite", Models: defaultCatalogModels("hunyuan-lite"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"44"}},
	{Type: "bytedance", Label: "ByteDance", DefaultBaseURL: "https://ark.ap-southeast.bytepluses.com/api/v3", DefaultModel: "doubao-pro-32k", Models: defaultCatalogModels("doubao-pro-32k"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"45"}},
	{Type: "bytedance-cn", Label: "ByteDance (China)", DefaultBaseURL: "https://ark.cn-beijing.volces.com/api/v3", DefaultModel: "doubao-pro-32k", Models: defaultCatalogModels("doubao-pro-32k"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"46"}},
	{Type: "iflytek-spark", Label: "iFlytek Spark", DefaultBaseURL: "https://spark-api-open.xf-yun.com/v1", DefaultModel: "generalv3.5", Models: defaultCatalogModels("generalv3.5"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"47"}},
	{Type: "cerebras", Label: "Cerebras", DefaultBaseURL: "https://api.cerebras.ai/v1", DefaultModel: "llama-3.3-70b", Models: defaultCatalogModels("llama-3.3-70b"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"48"}},
	{Type: "replicate", Label: "Replicate", DefaultBaseURL: "https://api.replicate.com/v1", DefaultModel: "meta/llama-3.1-70b-instruct", Models: defaultCatalogModels("meta/llama-3.1-70b-instruct"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"49"}},
	{Type: "sambanova", Label: "SambaNova", DefaultBaseURL: "https://api.sambanova.ai/v1", DefaultModel: "Meta-Llama-3.1-70B-Instruct", Models: defaultCatalogModels("Meta-Llama-3.1-70B-Instruct"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"50"}},
	{Type: "akle", Label: "Akle", DefaultBaseURL: "https://api.akle.ai/v1", DefaultModel: "gpt-4o-mini", Models: defaultCatalogModels("gpt-4o-mini"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"51"}},
	{Type: "kilo", Label: "Kilo", Group: "Vendors", DefaultBaseURL: "https://api.kilo.ai/api/gateway", DefaultModel: "anthropic/claude-sonnet-4.5", Models: defaultCatalogModels("anthropic/claude-sonnet-4.5"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"52"}},
	{Type: "opencode", Label: "OpenCode", Group: "Vendors", DefaultBaseURL: "https://opencode.ai/zen/go/v1", DefaultModel: "opencode-go/kimi-k2.6", Models: defaultCatalogModels("opencode-go/kimi-k2.6"), SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"53"}},
	{Type: "xiaomi-mimo", Label: "xiaomi-mimo", Group: "Vendors", DefaultBaseURL: "https://token-plan-cn.xiaomimimo.com/v1", DefaultModel: "mimo-v2.5", Models: []ProviderModelConfig{
		catalogChatModel("mimo-v2.5", false, true, true, 128000, 8192),
		catalogChatModel("mimo-v2.5-pro", false, true, true, 128000, 8192),
	}, SupportsFirstRun: true, SetupKind: ProviderSetupKindOpenAICompatible, Aliases: []string{"54"}},
}

var providerCatalog = buildProviderCatalog()

func ProviderPresets() []ProviderPreset {
	result := make([]ProviderPreset, len(providerPresets))
	for i, preset := range providerPresets {
		result[i] = preset
		result[i].Models = cloneProviderModelConfigs(preset.Models)
		if len(preset.Headers) > 0 {
			result[i].Headers = make(map[string]string, len(preset.Headers))
			for key, value := range preset.Headers {
				result[i].Headers[key] = value
			}
		}
	}
	return result
}

func buildProviderCatalog() []ProviderCatalogEntry {
	presets := ProviderPresets()
	result := make([]ProviderCatalogEntry, 0, len(presets))
	for _, preset := range presets {
		models := cloneProviderModelConfigs(preset.Models)
		if len(models) == 0 {
			models = defaultCatalogModels(preset.DefaultModel)
		}
		result = append(result, ProviderCatalogEntry{
			Type:             preset.Type,
			Label:            preset.Label,
			Group:            preset.Group,
			DefaultBaseURL:   preset.DefaultBaseURL,
			DefaultModel:     preset.DefaultModel,
			Models:           models,
			SupportsFirstRun: preset.SupportsFirstRun,
			SetupKind:        preset.SetupKind,
			Aliases:          append([]string(nil), preset.Aliases...),
		})
	}
	return result
}

func ProviderCatalog() []ProviderCatalogEntry {
	result := make([]ProviderCatalogEntry, len(providerCatalog))
	for i, entry := range providerCatalog {
		result[i] = entry
		result[i].Models = cloneProviderModelConfigs(entry.Models)
	}
	return result
}

func FirstRunProviderCatalog() []ProviderCatalogEntry {
	var result []ProviderCatalogEntry
	for _, entry := range providerCatalog {
		if entry.SupportsFirstRun {
			item := entry
			item.Models = cloneProviderModelConfigs(entry.Models)
			result = append(result, item)
		}
	}
	return result
}

func LookupProviderCatalogEntry(provider string) (ProviderCatalogEntry, bool) {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	for _, entry := range providerCatalog {
		if entry.Type == normalized {
			item := entry
			item.Models = cloneProviderModelConfigs(entry.Models)
			return item, true
		}
		for _, alias := range entry.Aliases {
			if strings.ToLower(strings.TrimSpace(alias)) == normalized {
				item := entry
				item.Models = cloneProviderModelConfigs(entry.Models)
				return item, true
			}
		}
	}
	return ProviderCatalogEntry{}, false
}

func NormalizeProviderName(provider string) string {
	if entry, ok := LookupProviderCatalogEntry(provider); ok {
		return entry.Type
	}
	return "openai"
}

func NormalizeFirstRunProviderName(provider string) string {
	normalized := strings.TrimSpace(provider)
	if normalized == "" {
		return defaultFirstRunProvider
	}
	if entry, ok := LookupProviderCatalogEntry(normalized); ok {
		return entry.Type
	}
	return defaultFirstRunProvider
}
