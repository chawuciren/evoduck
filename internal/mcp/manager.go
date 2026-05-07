package mcp

// MCP Manager - 使用 mark3labs/mcp-go 管理多个 MCP Server 连接

import (
	"context"
	"fmt"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

// Manager MCP Server 管理器
type Manager struct {
	config     *config.MCPConfig
	clients    map[string]*Client // key: server name
	registries map[string]*MCPToolRegistry
	mu         sync.RWMutex
	decider    *proxy.Decider // 代理决策器
}

// NewManager 创建 MCP 管理器
func NewManager(cfg *config.MCPConfig, decider *proxy.Decider) *Manager {
	return &Manager{
		config:     cfg,
		clients:    make(map[string]*Client),
		registries: make(map[string]*MCPToolRegistry),
		decider:    decider,
	}
}

// Initialize 初始化所有 MCP Server
func (m *Manager) Initialize(ctx context.Context) error {
	if m.config == nil || len(m.config.Servers) == 0 {
		logger.Info("No MCP servers configured")
		return nil
	}

	logger.Info("Initializing MCP servers...")

	for name, serverCfg := range m.config.Servers {
		if !serverCfg.Enabled {
			logger.Debug("MCP server disabled", logger.Fields{"name": name})
			continue
		}

		if err := m.connectServer(ctx, name, serverCfg); err != nil {
			logger.Warn("Failed to connect MCP server", logger.Fields{
				"name":  name,
				"error": err.Error(),
			})
			continue
		}

		logger.Info("MCP server connected", logger.Fields{"name": name})
	}

	logger.Info("MCP servers initialized successfully")

	// 统计可用工具数量
	totalTools := 0
	for _, registry := range m.registries {
		totalTools += len(registry.GetAllWrappers())
	}
	logger.Info("MCP tools available", logger.Fields{"count": totalTools})

	return nil
}

// connectServer 连接单个 MCP Server
func (m *Manager) connectServer(ctx context.Context, name string, cfg config.MCPServerConfig) error {
	var mc *mcpclient.Client
	var err error

	switch cfg.Type {
	case "local":
		mc, err = m.createStdioClient(name, cfg)
	case "remote":
		mc, err = m.createHTTPClient(cfg)
	default:
		return fmt.Errorf("unknown MCP server type: %s", cfg.Type)
	}

	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	// 创建包装 client
	client := NewClientFromMCPGo(name, mc)

	// 使用配置的超时时间
	timeout := time.Duration(cfg.Timeout) * time.Millisecond
	if timeout == 0 {
		timeout = 120 * time.Second
	}

	// 创建带超时的上下文
	initCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 初始化连接
	if err := client.Initialize(initCtx); err != nil {
		mc.Close()
		return fmt.Errorf("initialize client: %w", err)
	}

	// 创建工具注册表并加载工具
	registry := NewMCPToolRegistry(client)
	if err := registry.LoadTools(ctx); err != nil {
		mc.Close()
		return fmt.Errorf("load tools: %w", err)
	}

	// 保存到管理器
	m.mu.Lock()
	m.clients[name] = client
	m.registries[name] = registry
	m.mu.Unlock()

	return nil
}

// createStdioClient 创建 STDIO client (本地进程)
func (m *Manager) createStdioClient(name string, cfg config.MCPServerConfig) (*mcpclient.Client, error) {
	if len(cfg.Command) == 0 {
		return nil, fmt.Errorf("command is required for local MCP server")
	}

	logger.Info("Starting MCP server process", logger.Fields{
		"command": cfg.Command,
	})

	command := cfg.Command[0]
	args := cfg.Command[1:]

	// Build environment with proxy decision for this server
	baseEnv := proxy.BuildChildEnv(false) // Start with clean env
	decision := m.decider.ForMCP(name)
	env := m.decider.BuildSubprocessEnv(decision.UseProxy, decision.ProxyType, baseEnv)
	for k, v := range cfg.Environment {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// 使用 mcp-go 的 STDIO transport
	t := transport.NewStdioWithOptions(command, env, args)
	mc := mcpclient.NewClient(t)

	logger.Info("MCP server process started, waiting for readiness...")
	return mc, nil
}

// createHTTPClient 创建 HTTP/SSE client (远程服务)
func (m *Manager) createHTTPClient(cfg config.MCPServerConfig) (*mcpclient.Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("url is required for remote MCP server")
	}

	// 使用 mcp-go 的 StreamableHTTP transport
	// 注意：headers 暂时不支持，mcp-go 使用不同的方式
	t, err := transport.NewStreamableHTTP(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("create HTTP transport: %w", err)
	}

	mc := mcpclient.NewClient(t)
	return mc, nil
}

// GetClient 获取指定服务器的客户端
func (m *Manager) GetClient(name string) (*Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	client, ok := m.clients[name]
	return client, ok
}

// GetRegistry 获取指定服务器的工具注册表
func (m *Manager) GetRegistry(name string) (*MCPToolRegistry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	registry, ok := m.registries[name]
	return registry, ok
}

// GetAllClients 获取所有客户端
func (m *Manager) GetAllClients() map[string]*Client {
	m.mu.RLock()
	defer m.mu.RUnlock()

	clients := make(map[string]*Client)
	for k, v := range m.clients {
		clients[k] = v
	}
	return clients
}

// GetAllRegistries 获取所有工具注册表
func (m *Manager) GetAllRegistries() map[string]*MCPToolRegistry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	registries := make(map[string]*MCPToolRegistry)
	for k, v := range m.registries {
		registries[k] = v
	}
	return registries
}

// GetAllTools 获取所有 MCP 工具 (聚合)
func (m *Manager) GetAllTools() []*MCPToolWrapper {
	m.mu.RLock()
	defer m.mu.RUnlock()

	allTools := make([]*MCPToolWrapper, 0)
	for _, registry := range m.registries {
		allTools = append(allTools, registry.GetAllWrappers()...)
	}
	return allTools
}

// RefreshServer 刷新指定服务器的工具列表
func (m *Manager) RefreshServer(ctx context.Context, name string) error {
	m.mu.RLock()
	registry, ok := m.registries[name]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("server not found: %s", name)
	}

	// 刷新工具列表
	if err := registry.Refresh(ctx); err != nil {
		return err
	}

	logger.Info("MCP server tools refreshed", logger.Fields{"name": name})
	return nil
}

// Close 关闭所有连接
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, client := range m.clients {
		if err := client.Close(); err != nil {
			logger.Warn("Failed to close MCP client", logger.Fields{
				"name":  name,
				"error": err.Error(),
			})
		}
	}

	m.clients = make(map[string]*Client)
	m.registries = make(map[string]*MCPToolRegistry)

	return nil
}
