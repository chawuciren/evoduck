package mcp

// MCP Manager - 使用 mark3labs/mcp-go 管理多个 MCP Server 连接
//
// 设计要点：
//   - 所有 server 的连接/初始化/工具加载都是异步的，启动不会阻塞。
//   - 每个 server 维护独立的连接状态（pending / connecting / connected / failed / reconnecting / closed）。
//   - 通过 ToolRegistrar 回调把工具注册进上层（agent tool registries），
//     连上时注册、断开/失败时反注册，从而 MCP 工具对 agent 动态可见。
//   - 提供 Status()（查看每个 server 是否在线）和 Reconnect()（重连失败/掉线的 server）。

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

// ToolRegistrar 由上层（agent.Manager）实现，用于把 MCP 工具动态注册/反注册到各 agent 的工具注册表。
// 必须线程安全。
type ToolRegistrar interface {
	// RegisterMCPTools 注册一组 MCP 工具（server 连接成功时调用）。
	RegisterMCPTools(serverName string, wrappers []*MCPToolWrapper)
	// UnregisterMCPTools 反注册某 server 的全部工具（server 断开/失败/重连前调用）。
	UnregisterMCPTools(serverName string)
}

// Manager MCP Server 管理器
type Manager struct {
	config    *config.MCPConfig
	decider   *proxy.Decider
	registrar ToolRegistrar

	mu       sync.RWMutex
	clients  map[string]*Client           // key: server name；只包含 connected 的
	states   map[string]*serverState      // key: server name；所有 server 的状态
	wrappers map[string][]*MCPToolWrapper // key: server name；当前已注册的工具

	// connectFn 可在测试中替换以注入伪连接逻辑。
	connectFn func(ctx context.Context, name string, cfg config.MCPServerConfig) (*Client, []*MCPToolWrapper, error)

	cancel context.CancelFunc // 用于关闭时取消所有在途连接
}

// NewManager 创建 MCP 管理器。
// 不会立即连接——调用 Start() 后才异步发起连接。
func NewManager(cfg *config.MCPConfig, decider *proxy.Decider) *Manager {
	m := &Manager{
		config:   cfg,
		decider:  decider,
		clients:  make(map[string]*Client),
		states:   make(map[string]*serverState),
		wrappers: make(map[string][]*MCPToolWrapper),
	}
	// 默认连接实现闭包，捕获 decider
	m.connectFn = func(ctx context.Context, name string, sc config.MCPServerConfig) (*Client, []*MCPToolWrapper, error) {
		return defaultConnect(ctx, name, sc, decider)
	}
	if cfg != nil {
		for name, serverCfg := range cfg.Servers {
			state := &serverState{name: name}
			if serverCfg.Enabled {
				state.state = StatePending
			} else {
				state.state = StateDisabled
			}
			m.states[name] = state
		}
	}
	return m
}

// SetToolRegistrar 设置工具注册回调（由 agent.Manager 在创建 manager 后调用）。
// 必须在 Start() 之前调用。
func (m *Manager) SetToolRegistrar(r ToolRegistrar) {
	m.mu.Lock()
	m.registrar = r
	m.mu.Unlock()
}

// SetConnectFunc 替换连接实现（仅供测试）。
func (m *Manager) SetConnectFunc(f func(ctx context.Context, name string, cfg config.MCPServerConfig) (*Client, []*MCPToolWrapper, error)) {
	if f == nil {
		f = func(ctx context.Context, name string, sc config.MCPServerConfig) (*Client, []*MCPToolWrapper, error) {
			return defaultConnect(ctx, name, sc, m.decider)
		}
	}
	m.mu.Lock()
	m.connectFn = f
	m.mu.Unlock()
}

// Start 异步连接所有已启用的 server。
// 立即返回，绝不阻塞；某个 server 失败不会影响其它 server。
func (m *Manager) Start(ctx context.Context) {
	if m.config == nil || len(m.config.Servers) == 0 {
		logger.Info("No MCP servers configured")
		return
	}

	// 记录可取消的根 context，Close 时统一取消
	innerCtx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()

	logger.Info("Starting MCP servers (async)...")
	for name, serverCfg := range m.config.Servers {
		if !serverCfg.Enabled {
			continue
		}
		// 复制循环变量避免捕获问题
		n := name
		c := serverCfg
		go m.connectOne(innerCtx, n, c, false)
	}
}

// Initialize 同步初始化（保留以兼容旧调用方/测试）。
// 会阻塞直到所有 server 连接完成（成功或失败）。新代码应使用 Start()。
func (m *Manager) Initialize(ctx context.Context) error {
	if m.config == nil || len(m.config.Servers) == 0 {
		logger.Info("No MCP servers configured")
		return nil
	}

	logger.Info("Initializing MCP servers...")

	var wg sync.WaitGroup
	for name, serverCfg := range m.config.Servers {
		if !serverCfg.Enabled {
			continue
		}
		wg.Add(1)
		n := name
		c := serverCfg
		go func() {
			defer wg.Done()
			m.connectOne(ctx, n, c, false)
		}()
	}
	wg.Wait()

	logger.Info("MCP servers initialized")
	return nil
}

// connectOne 连接单个 server 并更新状态。
// reconnect=true 时表示这是一次重连（状态切换为 reconnecting）。
func (m *Manager) connectOne(ctx context.Context, name string, cfg config.MCPServerConfig, reconnect bool) {
	m.mu.Lock()
	state := m.states[name]
	if state == nil {
		state = &serverState{name: name}
		m.states[name] = state
	}
	state.attempts++
	state.err = ""
	if reconnect {
		state.state = StateReconnecting
	} else {
		state.state = StateConnecting
	}
	connectFn := m.connectFn
	m.mu.Unlock()

	logger.Info("Connecting MCP server", logger.Fields{
		"name":      name,
		"attempt":   state.attempts,
		"reconnect": reconnect,
	})

	// 连接超时：复用配置中的 timeout，默认 120s
	timeout := time.Duration(cfg.Timeout) * time.Millisecond
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	connCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, wrappers, err := connectFn(connCtx, name, cfg)
	if err != nil {
		m.mu.Lock()
		state.state = StateFailed
		state.err = err.Error()
		m.mu.Unlock()
		logger.Warn("Failed to connect MCP server", logger.Fields{
			"name":  name,
			"error": err.Error(),
		})
		return
	}

	m.mu.Lock()
	// 记录连接产物
	m.clients[name] = client
	m.wrappers[name] = wrappers
	state.state = StateConnected
	state.connectedAt = nowFunc()
	state.toolCount = len(wrappers)
	if si := client.GetServerInfo(); si.Name != "" {
		state.serverInfo = si
	}
	registrar := m.registrar
	m.mu.Unlock()

	// 注册工具到上层
	if registrar != nil && len(wrappers) > 0 {
		registrar.RegisterMCPTools(name, wrappers)
	}

	logger.Info("MCP server connected", logger.Fields{
		"name":       name,
		"tool_count": len(wrappers),
	})
}

// nowFunc 返回当前时间（便于测试替换）。默认 time.Now。
var nowFunc = time.Now

// defaultConnect 默认连接实现：创建 client → 初始化 → 加载工具。
// 返回已初始化的 client、已构造的 tool wrappers。
func defaultConnect(ctx context.Context, name string, cfg config.MCPServerConfig, decider *proxy.Decider) (*Client, []*MCPToolWrapper, error) {
	var mc *mcpclient.Client
	var err error

	switch cfg.Type {
	case "local":
		mc, err = createStdioClientWithDecider(name, cfg, decider)
	case "remote":
		mc, err = createHTTPClient(cfg)
	default:
		return nil, nil, fmt.Errorf("unknown MCP server type: %s", cfg.Type)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("create client: %w", err)
	}

	client := NewClientFromMCPGo(name, mc)
	if err := client.Initialize(ctx); err != nil {
		_ = mc.Close()
		return nil, nil, fmt.Errorf("initialize client: %w", err)
	}

	callTimeout := time.Duration(cfg.CallTimeout) * time.Millisecond
	registry := NewMCPToolRegistryWithTimeout(client, callTimeout)
	if err := registry.LoadTools(ctx); err != nil {
		_ = mc.Close()
		return nil, nil, fmt.Errorf("load tools: %w", err)
	}

	return client, registry.GetAllWrappers(), nil
}

// createHTTPClient 创建 HTTP/SSE client (远程服务)
func createHTTPClient(cfg config.MCPServerConfig) (*mcpclient.Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("url is required for remote MCP server")
	}
	t, err := transport.NewStreamableHTTP(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("create HTTP transport: %w", err)
	}
	return mcpclient.NewClient(t), nil
}

// createStdioClientWithDecider 创建 STDIO client (本地进程)
func createStdioClientWithDecider(name string, cfg config.MCPServerConfig, decider *proxy.Decider) (*mcpclient.Client, error) {
	if len(cfg.Command) == 0 {
		return nil, fmt.Errorf("command is required for local MCP server")
	}

	logger.Info("Starting MCP server process", logger.Fields{
		"command": cfg.Command,
	})

	command := cfg.Command[0]
	args := cfg.Command[1:]

	var env []string
	if decider != nil {
		baseEnv := proxy.BuildChildEnv(false) // 干净环境
		decision := decider.ForMCP(name)
		env = decider.BuildSubprocessEnv(decision.UseProxy, decision.ProxyType, baseEnv)
	}
	for k, v := range cfg.Environment {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	t := transport.NewStdioWithOptions(command, env, args)
	mc := mcpclient.NewClient(t)

	logger.Info("MCP server process started, waiting for readiness...")
	return mc, nil
}

// Reconnect 重连 server，不带回调。
//   - target == ""：只重连未连接/失败的 server（已 connected 的跳过）。
//   - target == "all"：强制重连全部（含已 connected）。
//   - 其它：强制重连指定名称的 server。
//
// 返回被触发重连的 server 名称列表（同步）。每个 server 在独立 goroutine 中重连。
func (m *Manager) Reconnect(ctx context.Context, target string) []string {
	return m.ReconnectWithFeedback(ctx, target, nil)
}

// ReconnectWithFeedback 重连 server，每个 server 完成后（成功或失败）调用 onResult 并传入最新状态。
// 返回被触发重连的 server 名称列表（同步）。
func (m *Manager) ReconnectWithFeedback(ctx context.Context, target string, onResult func(ServerStatus)) []string {
	targets := m.resolveReconnectTargets(target)
	for _, n := range targets {
		cfg, ok := m.serverCfg(n)
		if !ok {
			continue
		}
		nn := n
		cc := cfg
		go func(name string, cfg config.MCPServerConfig) {
			m.reconnectOne(ctx, name, cfg)
			if onResult != nil {
				m.mu.RLock()
				st := m.states[name]
				var snap ServerStatus
				if st != nil {
					snap = st.snapshot()
				}
				m.mu.RUnlock()
				onResult(snap)
			}
		}(nn, cc)
	}
	return targets
}

// resolveReconnectTargets 根据 target 解析需要重连的 server 列表。
func (m *Manager) resolveReconnectTargets(target string) []string {
	target = strings.TrimSpace(target)
	if target != "" && target != "all" {
		// 指定名称：仅当存在且未被禁用/关闭时才纳入
		m.mu.RLock()
		st := m.states[target]
		m.mu.RUnlock()
		if st == nil || st.state == StateDisabled || st.state == StateClosed {
			return nil
		}
		return []string{target}
	}

	force := target == "all"
	var targets []string
	m.mu.RLock()
	for n, st := range m.states {
		if st == nil || st.state == StateDisabled || st.state == StateClosed {
			continue
		}
		// 非强制模式下，跳过已连接的 server
		if !force && st.state == StateConnected {
			continue
		}
		targets = append(targets, n)
	}
	m.mu.RUnlock()
	sort.Strings(targets)
	return targets
}

func (m *Manager) reconnectOne(ctx context.Context, name string, cfg config.MCPServerConfig) {
	// 先关闭并反注册旧连接
	m.teardownServer(name)
	m.connectOne(ctx, name, cfg, true)
}

// serverCfg 返回某 server 的配置（线程安全读取）。
func (m *Manager) serverCfg(name string) (config.MCPServerConfig, bool) {
	if m.config == nil {
		return config.MCPServerConfig{}, false
	}
	c, ok := m.config.Servers[name]
	return c, ok
}

// teardownServer 关闭并清理某 server 的连接产物（client + 已注册工具），不改变状态（状态由调用方设定）。
func (m *Manager) teardownServer(name string) {
	m.mu.Lock()
	client := m.clients[name]
	wrappers := m.wrappers[name]
	delete(m.clients, name)
	delete(m.wrappers, name)
	registrar := m.registrar
	m.mu.Unlock()

	if registrar != nil {
		registrar.UnregisterMCPTools(name)
	}
	_ = wrappers
	if client != nil {
		if err := client.Close(); err != nil {
			logger.Warn("Failed to close MCP client during teardown", logger.Fields{
				"name":  name,
				"error": err.Error(),
			})
		}
	}
}

// Status 返回所有 server 的状态快照。
func (m *Manager) Status() StatusSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snap := StatusSnapshot{Servers: make([]ServerStatus, 0, len(m.states))}
	names := make([]string, 0, len(m.states))
	for n := range m.states {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		st := m.states[n]
		if st == nil {
			continue
		}
		if st.state == StateDisabled {
			continue
		}
		s := st.snapshot()
		snap.Servers = append(snap.Servers, s)
		snap.Total++
		switch st.state {
		case StateConnected:
			snap.Online++
		case StateConnecting, StateReconnecting:
			snap.Connecting++
		case StateFailed:
			snap.Failed++
		}
	}
	return snap
}

// GetClient 获取指定服务器的客户端（仅当已连接）
func (m *Manager) GetClient(name string) (*Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	client, ok := m.clients[name]
	return client, ok
}

// GetRegistry 获取指定服务器的工具注册表。
// 注意：async 重构后不再保留 MCPToolRegistry（wrapper 在连接时构造并交给 registrar）。
// 为兼容历史调用方，返回基于当前 wrappers 的临时 registry。
func (m *Manager) GetRegistry(name string) (*MCPToolRegistry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	client, ok := m.clients[name]
	if !ok {
		return nil, false
	}
	// 用当前 wrapper 重建一个 registry 视图
	reg := NewMCPToolRegistry(client)
	for _, w := range m.wrappers[name] {
		reg.addExistingWrapper(w)
	}
	return reg, true
}

// GetAllClients 获取所有已连接的客户端
func (m *Manager) GetAllClients() map[string]*Client {
	m.mu.RLock()
	defer m.mu.RUnlock()

	clients := make(map[string]*Client, len(m.clients))
	for k, v := range m.clients {
		clients[k] = v
	}
	return clients
}

// GetAllRegistries 获取所有已连接 server 的工具注册表（兼容历史调用）。
func (m *Manager) GetAllRegistries() map[string]*MCPToolRegistry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	registries := make(map[string]*MCPToolRegistry, len(m.clients))
	for name, client := range m.clients {
		reg := NewMCPToolRegistry(client)
		for _, w := range m.wrappers[name] {
			reg.addExistingWrapper(w)
		}
		registries[name] = reg
	}
	return registries
}

// GetAllTools 获取所有已连接 server 的 MCP 工具（聚合）
func (m *Manager) GetAllTools() []*MCPToolWrapper {
	m.mu.RLock()
	defer m.mu.RUnlock()

	allTools := make([]*MCPToolWrapper, 0)
	for _, ws := range m.wrappers {
		allTools = append(allTools, ws...)
	}
	return allTools
}

// RefreshServer 刷新指定服务器的工具列表（在线状态下重新 ListTools）
func (m *Manager) RefreshServer(ctx context.Context, name string) error {
	m.mu.RLock()
	client, ok := m.clients[name]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("server not connected: %s", name)
	}
	if err := client.RefreshTools(ctx); err != nil {
		return err
	}
	logger.Info("MCP server tools refreshed", logger.Fields{"name": name})
	return nil
}

// Close 关闭所有连接，取消在途连接。
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	names := make([]string, 0, len(m.states))
	for n, st := range m.states {
		if st.state != StateDisabled {
			names = append(names, n)
		}
	}
	m.mu.Unlock()

	for _, name := range names {
		m.mu.Lock()
		if st, ok := m.states[name]; ok {
			st.state = StateClosed
		}
		m.mu.Unlock()
		m.teardownServer(name)
	}
	return nil
}
