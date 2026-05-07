package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/proxy"

	"github.com/chawuciren/evoduck/pkg/config"
)

// ProxyClient 管理代理客户端
type ProxyClient struct {
	httpProxy   *url.URL
	socks5Proxy proxy.Dialer
	noProxy     []string
	httpConfig  config.HTTPProxyConfig
	socks5Config config.SOCKS5ProxyConfig
}

// NewProxyClient 创建代理客户端
func NewProxyClient(cfg config.ProxyConfig) *ProxyClient {
	pc := &ProxyClient{
		noProxy:     cfg.NoProxy,
		httpConfig:  cfg.HTTP,
		socks5Config: cfg.SOCKS5,
	}

	// 解析 HTTP 代理
	if cfg.HTTP.URL != "" {
		httpURL, err := url.Parse(cfg.HTTP.URL)
		if err == nil {
			// 嵌入认证信息到 URL
			if cfg.HTTP.Username != "" && cfg.HTTP.Password != "" {
				httpURL.User = url.UserPassword(cfg.HTTP.Username, cfg.HTTP.Password)
			}
			pc.httpProxy = httpURL
		}
	}

	// 解析 SOCKS5 代理
	if cfg.SOCKS5.URL != "" {
		addr := parseSocks5Addr(cfg.SOCKS5.URL)
		if addr != "" {
			var auth *proxy.Auth
			if cfg.SOCKS5.Username != "" || cfg.SOCKS5.Password != "" {
				auth = &proxy.Auth{
					User:     cfg.SOCKS5.Username,
					Password: cfg.SOCKS5.Password,
				}
			}
			dialer, err := proxy.SOCKS5("tcp", addr, auth, proxy.Direct)
			if err == nil {
				pc.socks5Proxy = dialer
			}
		}
	}

	return pc
}

// parseSocks5Addr 从 URL 解析 SOCKS5 地址
// 支持 socks5://host:port 格式
func parseSocks5Addr(socksURL string) string {
	// 移除协议前缀
	addr := socksURL
	if strings.HasPrefix(addr, "socks5://") {
		addr = strings.TrimPrefix(addr, "socks5://")
	} else if strings.HasPrefix(addr, "socks4://") {
		addr = strings.TrimPrefix(addr, "socks4://")
	}

	// 移除用户信息（如果有）
	if idx := strings.Index(addr, "@"); idx >= 0 {
		addr = addr[idx+1:]
	}

	return addr
}

// GetHTTPClient 获取配置了代理的 HTTP 客户端
func (pc *ProxyClient) GetHTTPClient(proxyType string) *http.Client {
	transport := &http.Transport{}

	switch proxyType {
	case "socks5":
		if pc.socks5Proxy != nil {
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return pc.socks5Proxy.Dial(network, addr)
			}
		}
	case "http":
		if pc.httpProxy != nil {
			transport.Proxy = http.ProxyURL(pc.httpProxy)
		}
	default:
		// 使用 HTTP 代理作为默认
		if pc.httpProxy != nil {
			transport.Proxy = http.ProxyURL(pc.httpProxy)
		}
	}

	return &http.Client{Transport: transport}
}

// ShouldProxy 判断是否需要走代理（检查 no_proxy 列表）
func (pc *ProxyClient) ShouldProxy(targetURL string) bool {
	if len(pc.noProxy) == 0 {
		return true
	}

	// 解析目标 URL
	u, err := url.Parse(targetURL)
	if err != nil {
		return true
	}

	// 获取主机名
	host := u.Host
	if host == "" {
		host = targetURL
	}

	// 检查是否匹配 no_proxy 模式
	for _, pattern := range pc.noProxy {
		if matchNoProxyPattern(host, pattern) {
			return false
		}
	}

	return true
}

// matchNoProxyPattern 匹配 no_proxy 模式
// 支持: localhost, 127.0.0.1, *.internal, .example.com, 192.168.*
func matchNoProxyPattern(host, pattern string) bool {
	// 完全匹配
	if host == pattern {
		return true
	}

	// 移除端口号进行比较
	hostWithoutPort := host
	if idx := strings.LastIndex(host, ":"); idx > strings.LastIndex(host, "]") {
		hostWithoutPort = host[:idx]
	}

	// 通配符匹配 (*.internal)
	if strings.HasPrefix(pattern, "*.") {
		domain := pattern[2:]
		// host 以 .domain 结尾，或者 host == domain
		if strings.HasSuffix(hostWithoutPort, "."+domain) || hostWithoutPort == domain {
			return true
		}
	}

	// 前缀通配符 (192.168.*)
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		if strings.HasPrefix(hostWithoutPort, prefix) {
			return true
		}
	}

	// 子域名匹配 (.example.com 匹配 sub.example.com 和 example.com)
	if strings.HasPrefix(pattern, ".") {
		domain := pattern[1:]
		if strings.HasSuffix(hostWithoutPort, pattern) || hostWithoutPort == domain {
			return true
		}
	}

	return false
}

// GetProxyURL 获取代理 URL（用于环境变量设置）
func (pc *ProxyClient) GetProxyURL(proxyType string) string {
	switch proxyType {
	case "socks5":
		if pc.socks5Config.URL != "" {
			// 构建带认证的 URL
			if pc.socks5Config.Username != "" && pc.socks5Config.Password != "" {
				return fmt.Sprintf("socks5://%s:%s@%s",
					pc.socks5Config.Username,
					pc.socks5Config.Password,
					parseSocks5Addr(pc.socks5Config.URL))
			}
			return pc.socks5Config.URL
		}
	case "http":
		if pc.httpConfig.URL != "" {
			// 构建带认证的 URL
			if pc.httpConfig.Username != "" && pc.httpConfig.Password != "" {
				if strings.HasPrefix(pc.httpConfig.URL, "http://") {
					return strings.Replace(pc.httpConfig.URL, "http://",
						fmt.Sprintf("http://%s:%s@", pc.httpConfig.Username, pc.httpConfig.Password), 1)
				} else if strings.HasPrefix(pc.httpConfig.URL, "https://") {
					return strings.Replace(pc.httpConfig.URL, "https://",
						fmt.Sprintf("https://%s:%s@", pc.httpConfig.Username, pc.httpConfig.Password), 1)
				}
			}
			return pc.httpConfig.URL
		}
	}
	return ""
}

// HasHTTPProxy 是否配置了 HTTP 代理
func (pc *ProxyClient) HasHTTPProxy() bool {
	return pc.httpProxy != nil
}

// HasSOCKS5Proxy 是否配置了 SOCKS5 代理
func (pc *ProxyClient) HasSOCKS5Proxy() bool {
	return pc.socks5Proxy != nil
}

// HasAnyProxy 是否配置了任何代理
func (pc *ProxyClient) HasAnyProxy() bool {
	return pc.HasHTTPProxy() || pc.HasSOCKS5Proxy()
}

// GetHTTPProxyURL 获取 HTTP 代理 URL（用于 WebSocket Proxy 配置）
func (pc *ProxyClient) GetHTTPProxyURL() *url.URL {
	return pc.httpProxy
}

// GetSOCKS5Dialer 获取 SOCKS5 Dialer（用于 WebSocket NetDialContext 配置）
func (pc *ProxyClient) GetSOCKS5Dialer() proxy.Dialer {
	return pc.socks5Proxy
}