package mcp

// MCP Client - 使用 mark3labs/mcp-go 的 Client 实现
// 包装官方 client，提供 EvoDuck 需要的接口

import (
	"context"
	"fmt"
	"sync"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/chawuciren/evoduck/pkg/logger"
)

// Client MCP 客户端 (包装 mcp-go Client)
type Client struct {
	name         string
	mcpClient    *mcpclient.Client
	tools        map[string]*mcp.Tool
	serverInfo   mcp.Implementation
	capabilities mcp.ServerCapabilities
	mu           sync.RWMutex
	initialized  bool
}

// NewClientFromMCPGo 从 mcp-go client 创建包装器
func NewClientFromMCPGo(name string, mc *mcpclient.Client) *Client {
	return &Client{
		name:      name,
		mcpClient: mc,
		tools:     make(map[string]*mcp.Tool),
	}
}

// NewFakeClient 构造一个仅含 name 和已缓存工具的 Client（无真实 transport）。
// 仅供测试使用：当需要在不启动真实 MCP server 的情况下模拟一个"已连接"的 client 时。
func NewFakeClient(name string, initialized bool, toolNames ...string) *Client {
	c := NewClientFromMCPGo(name, nil)
	c.initialized = initialized
	for _, tn := range toolNames {
		c.tools[tn] = &mcp.Tool{Name: tn}
	}
	return c
}

// Initialize 初始化连接
func (c *Client) Initialize(ctx context.Context) error {
	// 先启动 transport
	if err := c.mcpClient.Start(ctx); err != nil {
		return fmt.Errorf("start transport: %w", err)
	}

	// 使用 mcp-go 的 Initialize 进行初始化
	request := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			Capabilities: mcp.ClientCapabilities{
				Roots: &struct {
					ListChanged bool `json:"listChanged,omitempty"`
				}{
					ListChanged: true,
				},
			},
			ClientInfo: mcp.Implementation{
				Name:    "EvoDuck",
				Version: "1.0.0",
			},
		},
	}

	result, err := c.mcpClient.Initialize(ctx, request)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	c.capabilities = result.Capabilities
	c.serverInfo = result.ServerInfo
	c.initialized = true

	logger.Info("MCP client initialized", logger.Fields{
		"server":      c.name,
		"server_name": result.ServerInfo.Name,
		"version":     result.ServerInfo.Version,
		"has_tools":   result.Capabilities.Tools != nil,
	})

	return nil
}

// ListTools 获取工具列表
func (c *Client) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	if c.capabilities.Tools == nil {
		return nil, fmt.Errorf("server does not support tools")
	}

	result, err := c.mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	// 缓存工具
	c.mu.Lock()
	for i := range result.Tools {
		c.tools[result.Tools[i].Name] = &result.Tools[i]
	}
	c.mu.Unlock()

	logger.Info("MCP tools loaded", logger.Fields{
		"server": c.name,
		"count":  len(result.Tools),
	})

	// 转换为指针数组
	tools := make([]*mcp.Tool, len(result.Tools))
	for i := range result.Tools {
		tools[i] = &result.Tools[i]
	}

	return tools, nil
}

// CallTool 调用工具
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      name,
			Arguments: args,
		},
	}

	result, err := c.mcpClient.CallTool(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("call tool: %w", err)
	}

	return result, nil
}

// GetTool 获取工具定义
func (c *Client) GetTool(name string) (*mcp.Tool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tool, ok := c.tools[name]
	return tool, ok
}

// GetAllTools 获取所有工具
func (c *Client) GetAllTools() []*mcp.Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tools := make([]*mcp.Tool, 0, len(c.tools))
	for _, tool := range c.tools {
		tools = append(tools, tool)
	}
	return tools
}

// GetName 获取服务器名称
func (c *Client) GetName() string {
	return c.name
}

// GetServerInfo 获取服务器信息
func (c *Client) GetServerInfo() mcp.Implementation {
	return c.serverInfo
}

// Close 关闭客户端
func (c *Client) Close() error {
	if c == nil || c.mcpClient == nil {
		return nil
	}
	return c.mcpClient.Close()
}

// IsConnected 检查连接状态
func (c *Client) IsConnected() bool {
	return c.initialized
}

// RefreshTools 刷新工具列表
func (c *Client) RefreshTools(ctx context.Context) error {
	// 清空缓存
	c.mu.Lock()
	c.tools = make(map[string]*mcp.Tool)
	c.mu.Unlock()

	// 重新获取
	_, err := c.ListTools(ctx)
	return err
}

// GetCapabilities 获取服务器能力
func (c *Client) GetCapabilities() mcp.ServerCapabilities {
	return c.capabilities
}
