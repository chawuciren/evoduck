package proxy

import (
	"net/http"

	"github.com/chawuciren/evoduck/pkg/config"
)

// Decider 代理决策器，根据场景决定是否使用代理及使用哪种代理
type Decider struct {
	config config.ProxyConfig
	client *ProxyClient
}

// Decision 代理决策结果
type Decision struct {
	UseProxy   bool
	ProxyType  string       // "http" 或 "socks5"
	HTTPClient *http.Client // 配置好代理的 HTTP 客户端
}

// NewDecider 创建代理决策器
func NewDecider(cfg config.ProxyConfig) *Decider {
	return &Decider{
		config: cfg,
		client: NewProxyClient(cfg),
	}
}

// GetClient 获取底层 ProxyClient
func (d *Decider) GetClient() *ProxyClient {
	return d.client
}

// GetProxyClient 获取底层 ProxyClient（别名方法）
func (d *Decider) GetProxyClient() *ProxyClient {
	return d.client
}

// ForLLM 为 LLM Provider 做代理决策
func (d *Decider) ForLLM(providerName string) Decision {
	if !d.config.Enabled || !d.config.Controls.LLM.Enabled {
		return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
	}

	// 检查是否有 Provider 特定配置
	if d.config.Controls.LLM.Providers != nil {
		if enabled, ok := d.config.Controls.LLM.Providers[providerName]; ok {
			if !*enabled {
				return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
			}
		}
	}

	// 使用 LLM 控制的代理类型，如果没有指定则使用全局默认
	proxyType := d.config.Controls.LLM.Type
	if proxyType == "" {
		proxyType = d.config.Type
	}

	return Decision{
		UseProxy:   true,
		ProxyType:  proxyType,
		HTTPClient: d.client.GetHTTPClient(proxyType),
	}
}

// ForChannel 为 Channel 连接做代理决策
func (d *Decider) ForChannel(channelType string) Decision {
	if !d.config.Enabled {
		return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
	}

	// 获取 Channel 的配置
	ctrl := d.config.Controls.Channels

	// 检查是否有 Channel 特定配置
	if ctrl.PerChannel != nil {
		if enabled, ok := ctrl.PerChannel[channelType]; ok {
			if !*enabled {
				return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
			}
			// 使用 Channel 控制的代理类型
			proxyType := ctrl.Type
			if proxyType == "" {
				proxyType = d.config.Type
			}
			return Decision{
				UseProxy:   true,
				ProxyType:  proxyType,
				HTTPClient: d.client.GetHTTPClient(proxyType),
			}
		}
	}

	// 使用 Channel 默认配置
	if !ctrl.Default {
		return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
	}

	proxyType := ctrl.Type
	if proxyType == "" {
		proxyType = d.config.Type
	}

	return Decision{
		UseProxy:   true,
		ProxyType:  proxyType,
		HTTPClient: d.client.GetHTTPClient(proxyType),
	}
}

// ForTool 为内置工具做代理决策
func (d *Decider) ForTool(toolName string, targetURL string) Decision {
	if !d.config.Enabled || !d.config.Controls.Tools.Enabled {
		return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
	}

	// 检查目标 URL 是否应该跳过代理
	if !d.client.ShouldProxy(targetURL) {
		return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
	}

	// 检查是否有工具特定配置
	if d.config.Controls.Tools.PerTool != nil {
		if enabled, ok := d.config.Controls.Tools.PerTool[toolName]; ok {
			if !*enabled {
				return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
			}
		}
	}

	// 使用工具控制指定的代理类型
	proxyType := d.config.Controls.Tools.Type
	if proxyType == "" {
		proxyType = d.config.Type
	}

	return Decision{
		UseProxy:   true,
		ProxyType:  proxyType,
		HTTPClient: d.client.GetHTTPClient(proxyType),
	}
}

// ForMCP 为 MCP Server 做代理决策（工具调用）
func (d *Decider) ForMCP(serverName string) Decision {
	if !d.config.Enabled || !d.config.Controls.MCP.Enabled {
		return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
	}

	// 检查是否有 Server 特定配置
	if d.config.Controls.MCP.PerServer != nil {
		if enabled, ok := d.config.Controls.MCP.PerServer[serverName]; ok {
			if !*enabled {
				return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
			}
		}
	}

	// 使用 MCP 控制指定的代理类型
	proxyType := d.config.Controls.MCP.Type
	if proxyType == "" {
		proxyType = d.config.Type
	}

	return Decision{
		UseProxy:   true,
		ProxyType:  proxyType,
		HTTPClient: d.client.GetHTTPClient(proxyType),
	}
}

// ForMCPSubprocess 为 MCP Server 子进程做代理决策（环境变量）
// MCP 的子进程环境变量配置与工具调用共用同一配置
func (d *Decider) ForMCPSubprocess(serverName string) bool {
	decision := d.ForMCP(serverName)
	return decision.UseProxy
}

// ForPlugin 为 Plugin 做代理决策（工具调用）
func (d *Decider) ForPlugin(pluginID string) Decision {
	if !d.config.Enabled || !d.config.Controls.Plugin.Enabled {
		return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
	}

	// 检查是否有 Plugin 特定配置
	if d.config.Controls.Plugin.PerPlugin != nil {
		if enabled, ok := d.config.Controls.Plugin.PerPlugin[pluginID]; ok {
			if !*enabled {
				return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
			}
		}
	}

	// 使用 Plugin 控制指定的代理类型
	proxyType := d.config.Controls.Plugin.Type
	if proxyType == "" {
		proxyType = d.config.Type
	}

	return Decision{
		UseProxy:   true,
		ProxyType:  proxyType,
		HTTPClient: d.client.GetHTTPClient(proxyType),
	}
}

// ForPluginSubprocess 为 Plugin 子进程做代理决策（环境变量）
// Plugin 的子进程环境变量配置与工具调用共用同一配置
func (d *Decider) ForPluginSubprocess(pluginID string) bool {
	decision := d.ForPlugin(pluginID)
	return decision.UseProxy
}

// ForExec 为 Exec 命令做代理决策
func (d *Decider) ForExec(commandName string) Decision {
	if !d.config.Enabled || !d.config.Controls.Exec.Enabled {
		return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
	}

	// 检查是否有命令特定配置
	if d.config.Controls.Exec.PerCommand != nil {
		if enabled, ok := d.config.Controls.Exec.PerCommand[commandName]; ok {
			if !*enabled {
				return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
			}
		}
	}

	// 使用 Exec 控制指定的代理类型
	proxyType := d.config.Controls.Exec.Type
	if proxyType == "" {
		proxyType = d.config.Type
	}

	return Decision{
		UseProxy:   true,
		ProxyType:  proxyType,
		HTTPClient: d.client.GetHTTPClient(proxyType),
	}
}

// ForInternalSubagent 为内部子 Agent 做代理决策
func (d *Decider) ForInternalSubagent() Decision {
	if !d.config.Enabled {
		return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
	}

	ctrl := d.config.Controls.Subagents.Internal
	if ctrl.Enabled != nil && !*ctrl.Enabled {
		return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
	}

	// 内部子 Agent 使用全局默认代理类型
	proxyType := d.config.Type

	return Decision{
		UseProxy:   true,
		ProxyType:  proxyType,
		HTTPClient: d.client.GetHTTPClient(proxyType),
	}
}

// ForExternalSubagent 为外部子 Agent 做代理决策
func (d *Decider) ForExternalSubagent(agentID string) Decision {
	if !d.config.Enabled {
		return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
	}

	ctrl := d.config.Controls.Subagents.External

	// 检查是否有 Agent 特定配置
	if ctrl.PerAgent != nil {
		if enabled, ok := ctrl.PerAgent[agentID]; ok {
			if !*enabled {
				return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
			}
		}
	}

	// 使用外部子 Agent 默认配置
	if !ctrl.Enabled {
		return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
	}

	// 使用外部子 Agent 控制指定的代理类型
	proxyType := ctrl.Type
	if proxyType == "" {
		proxyType = d.config.Type
	}

	return Decision{
		UseProxy:   true,
		ProxyType:  proxyType,
		HTTPClient: d.client.GetHTTPClient(proxyType),
	}
}

// ForUpdate 为 Update 命令做代理决策
func (d *Decider) ForUpdate() Decision {
	if !d.config.Enabled || !d.config.Controls.Update.Enabled {
		return Decision{UseProxy: false, HTTPClient: http.DefaultClient}
	}

	// 使用 Update 控制指定的代理类型
	proxyType := d.config.Controls.Update.Type
	if proxyType == "" {
		proxyType = d.config.Type
	}

	return Decision{
		UseProxy:   true,
		ProxyType:  proxyType,
		HTTPClient: d.client.GetHTTPClient(proxyType),
	}
}

// BuildExecEnv 为 Exec 命令构建环境变量
func (d *Decider) BuildExecEnv(commandName string, baseEnv []string) []string {
	decision := d.ForExec(commandName)
	return d.buildEnvFromDecision(decision, baseEnv)
}

// BuildSubprocessEnv 为子进程构建环境变量
func (d *Decider) BuildSubprocessEnv(useProxy bool, proxyType string, baseEnv []string) []string {
	decision := Decision{UseProxy: useProxy, ProxyType: proxyType}
	return d.buildEnvFromDecision(decision, baseEnv)
}

// buildEnvFromDecision 从 Decision 构建环境变量
func (d *Decider) buildEnvFromDecision(decision Decision, baseEnv []string) []string {
	if !decision.UseProxy {
		// 不使用代理，清除代理相关环境变量
		return filterProxyEnvGlobal(baseEnv)
	}

	// 使用代理，添加代理环境变量
	env := filterProxyEnvGlobal(baseEnv)
	proxyURL := d.client.GetProxyURL(decision.ProxyType)
	if proxyURL != "" {
		env = append(env, "HTTP_PROXY="+proxyURL)
		env = append(env, "HTTPS_PROXY="+proxyURL)
		env = append(env, "http_proxy="+proxyURL)
		env = append(env, "https_proxy="+proxyURL)
		if decision.ProxyType == "socks5" {
			env = append(env, "ALL_PROXY="+proxyURL)
			env = append(env, "all_proxy="+proxyURL)
		}
	}

	// 添加 no_proxy
	if len(d.config.NoProxy) > 0 {
		noProxyStr := ""
		for _, p := range d.config.NoProxy {
			if noProxyStr != "" {
				noProxyStr += ","
			}
			noProxyStr += p
		}
		env = append(env, "NO_PROXY="+noProxyStr)
		env = append(env, "no_proxy="+noProxyStr)
	}

	return env
}

