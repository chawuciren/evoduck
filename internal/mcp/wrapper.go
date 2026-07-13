package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/chawuciren/evoduck/pkg/models"
)

// MCPToolWrapper MCP 工具包装器（适配 EvoDuck Registry）
type MCPToolWrapper struct {
	client      *Client
	tool        *mcp.Tool
	serverName  string
	callTimeout time.Duration // 单次工具调用兜底超时；0 表示走 Registry 全局默认
	mu          sync.RWMutex
}

// NewMCPToolWrapper 创建 MCP 工具包装器
func NewMCPToolWrapper(client *Client, tool *mcp.Tool) *MCPToolWrapper {
	return &MCPToolWrapper{
		client:     client,
		tool:       tool,
		serverName: client.GetName(),
	}
}

// NewMCPToolWrapperWithTimeout 创建带调用超时的 MCP 工具包装器
func NewMCPToolWrapperWithTimeout(client *Client, tool *mcp.Tool, callTimeout time.Duration) *MCPToolWrapper {
	return &MCPToolWrapper{
		client:      client,
		tool:        tool,
		serverName:  client.GetName(),
		callTimeout: callTimeout,
	}
}

// CallTimeout 实现 tools.ToolWithTimeout 接口
// 返回 > 0 时覆盖 Registry 全局默认；返回 0 表示使用全局默认
func (w *MCPToolWrapper) CallTimeout() time.Duration {
	return w.callTimeout
}

// Name 返回工具名称（带服务器前缀）
func (w *MCPToolWrapper) Name() string {
	// MCP 工具命名规范: servername_toolname
	if w.serverName != "" {
		return fmt.Sprintf("%s_%s", w.serverName, w.tool.Name)
	}
	return w.tool.Name
}

// OriginalName 返回原始工具名称（不带前缀）
func (w *MCPToolWrapper) OriginalName() string {
	return w.tool.Name
}

// Description 返回工具描述
func (w *MCPToolWrapper) Description() string {
	var desc strings.Builder

	// 添加服务器来源标识
	desc.WriteString(fmt.Sprintf("[MCP/%s] ", w.serverName))

	// 主描述
	desc.WriteString(w.tool.Description)

	return desc.String()
}

// Parameters 返回工具参数定义
func (w *MCPToolWrapper) Parameters() map[string]interface{} {
	// mcp-go 的 InputSchema 是 struct，需要转换
	// 将 ToolInputSchema 转换为 map
	schema := w.tool.InputSchema
	result := make(map[string]interface{})
	if schema.Type != "" {
		result["type"] = schema.Type
	}
	if len(schema.Properties) > 0 {
		result["properties"] = schema.Properties
	}
	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}
	return result
}

// Execute 执行工具（无上下文）
func (w *MCPToolWrapper) Execute(args map[string]interface{}) (string, error) {
	return w.ExecuteWithRole(context.Background(), args, models.RoleAdmin)
}

// ExecuteWithRole 带角色执行（实现 ToolWithContext 接口）
func (w *MCPToolWrapper) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	// MCP 工具调用
	result, err := w.client.CallTool(ctx, w.tool.Name, args)
	if err != nil {
		return "", fmt.Errorf("call MCP tool: %w", err)
	}

	// 构建结果文本
	var output strings.Builder

	if result.IsError {
		output.WriteString("**Error:**\n")
	}

	// 处理内容 - mcp-go 的 Content 是 interface{}
	// 使用 JSON 序列化/反序列化来处理
	for _, content := range result.Content {
		// 先序列化再尝试解析
		jsonBytes, err := json.Marshal(content)
		if err != nil {
			continue
		}

		// 尝试解析为通用结构
		var contentMap map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &contentMap); err != nil {
			output.WriteString(string(jsonBytes))
			output.WriteString("\n")
			continue
		}

		// 根据 type 字段处理
		contentType, _ := contentMap["type"].(string)
		switch contentType {
		case "text":
			if text, ok := contentMap["text"].(string); ok {
				output.WriteString(text)
				output.WriteString("\n")
			}
		case "image":
			if mimeType, ok := contentMap["mimeType"].(string); ok {
				output.WriteString(fmt.Sprintf("[Image: %s]\n", mimeType))
			}
		case "audio":
			if mimeType, ok := contentMap["mimeType"].(string); ok {
				output.WriteString(fmt.Sprintf("[Audio: %s]\n", mimeType))
			}
		case "resource":
			if uri, ok := contentMap["uri"].(string); ok {
				output.WriteString(fmt.Sprintf("[Resource: %s]\n", uri))
			}
		default:
			// 输出原始 JSON
			output.WriteString(string(jsonBytes))
			output.WriteString("\n")
		}
	}

	return strings.TrimSpace(output.String()), nil
}

// GetServerName 获取所属服务器名称
func (w *MCPToolWrapper) GetServerName() string {
	return w.serverName
}

// GetOriginalTool 获取原始 MCP Tool 定义
func (w *MCPToolWrapper) GetOriginalTool() *mcp.Tool {
	return w.tool
}

// MCPToolRegistry MCP 工具注册表（批量管理）
type MCPToolRegistry struct {
	client      *Client
	callTimeout time.Duration
	wrappers    map[string]*MCPToolWrapper // key: prefixed name
	mu          sync.RWMutex
}

// NewMCPToolRegistry 创建 MCP 工具注册表
func NewMCPToolRegistry(client *Client) *MCPToolRegistry {
	return NewMCPToolRegistryWithTimeout(client, 0)
}

// NewMCPToolRegistryWithTimeout 创建带调用超时的 MCP 工具注册表
func NewMCPToolRegistryWithTimeout(client *Client, callTimeout time.Duration) *MCPToolRegistry {
	return &MCPToolRegistry{
		client:      client,
		callTimeout: callTimeout,
		wrappers:    make(map[string]*MCPToolWrapper),
	}
}

// LoadTools 加载工具
func (r *MCPToolRegistry) LoadTools(ctx context.Context) error {
	tools, err := r.client.ListTools(ctx)
	if err != nil {
		return err
	}

	r.mu.Lock()
	for _, tool := range tools {
		wrapper := NewMCPToolWrapperWithTimeout(r.client, tool, r.callTimeout)
		r.wrappers[wrapper.Name()] = wrapper
	}
	r.mu.Unlock()

	return nil
}

// GetAllWrappers 获取所有包装器
func (r *MCPToolRegistry) GetAllWrappers() []*MCPToolWrapper {
	r.mu.RLock()
	defer r.mu.RUnlock()

	wrappers := make([]*MCPToolWrapper, 0, len(r.wrappers))
	for _, w := range r.wrappers {
		wrappers = append(wrappers, w)
	}
	return wrappers
}

// GetWrapper 获取指定工具的包装器
func (r *MCPToolRegistry) GetWrapper(name string) (*MCPToolWrapper, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	wrapper, ok := r.wrappers[name]
	return wrapper, ok
}

// GetClient 获取客户端
func (r *MCPToolRegistry) GetClient() *Client {
	return r.client
}

// Refresh 刷新工具列表
func (r *MCPToolRegistry) Refresh(ctx context.Context) error {
	// 清空
	r.mu.Lock()
	r.wrappers = make(map[string]*MCPToolWrapper)
	r.mu.Unlock()

	// 重新加载
	return r.LoadTools(ctx)
}

// ToToolDefinitions 转换为 EvoDuck ToolDefinition 格式
func (r *MCPToolRegistry) ToToolDefinitions() []models.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]models.ToolDefinition, 0, len(r.wrappers))
	for _, w := range r.wrappers {
		defs = append(defs, models.ToolDefinition{
			Type: "function",
			Function: models.FunctionDef{
				Name:        w.Name(),
				Description: w.Description(),
				Parameters:  w.Parameters(),
			},
		})
	}
	return defs
}
