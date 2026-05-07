package proxy

import (
	"os"
	"strings"

	"github.com/chawuciren/evoduck/pkg/config"
)

// Setup 从配置设置代理环境变量（全局默认模式）
// 这是一个可选函数，仅当需要设置全局环境变量时调用
// 新方案优先使用 Decider 进行精细化控制，全局环境变量仅作为备用
func Setup(cfg config.ProxyConfig) {
	if !cfg.Enabled {
		return
	}

	decider := NewDecider(cfg)

	// 使用默认代理类型设置全局环境变量
	proxyURL := decider.GetClient().GetProxyURL(cfg.Type)
	if proxyURL != "" {
		os.Setenv("HTTP_PROXY", proxyURL)
		os.Setenv("HTTPS_PROXY", proxyURL)
		os.Setenv("http_proxy", proxyURL)
		os.Setenv("https_proxy", proxyURL)
		if cfg.Type == "socks5" {
			os.Setenv("ALL_PROXY", proxyURL)
			os.Setenv("all_proxy", proxyURL)
		}
	}

	// 设置跳过代理的域名列表
	if len(cfg.NoProxy) > 0 {
		noProxyStr := strings.Join(cfg.NoProxy, ",")
		os.Setenv("NO_PROXY", noProxyStr)
		os.Setenv("no_proxy", noProxyStr)
	}
}

// BuildChildEnv 构建子进程环境变量
// 根据 inheritProxy 参数决定是否保留代理相关环境变量
// 注意：这是一个简化版本，精细化场景应使用 Decider.BuildSubprocessEnv
func BuildChildEnv(inheritProxy bool) []string {
	env := os.Environ()

	if !inheritProxy {
		// 清空代理相关环境变量
		env = filterProxyEnvGlobal(env)
	}

	return env
}

// filterProxyEnvGlobal 过滤掉代理相关的环境变量（全局版本）
func filterProxyEnvGlobal(env []string) []string {
	var result []string
	proxyEnvKeys := []string{
		"HTTP_PROXY", "HTTPS_PROXY", "SOCKS_PROXY", "ALL_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "socks_proxy", "all_proxy", "no_proxy",
	}

	for _, e := range env {
		idx := strings.Index(e, "=")
		if idx > 0 {
			key := e[:idx]
			isProxyEnv := false
			for _, proxyKey := range proxyEnvKeys {
				if key == proxyKey {
					isProxyEnv = true
					break
				}
			}
			if !isProxyEnv {
				result = append(result, e)
			}
		}
	}

	return result
}