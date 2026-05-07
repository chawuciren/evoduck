package mcp

// MCP Transport - 使用 mark3labs/mcp-go 的 transport 实现
// 不再需要自己实现 STDIO transport，直接使用官方实现

import (
	"context"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
)

// 创建 STDIO MCP Client
func NewStdioClient(command string, args []string, env []string) *mcpclient.Client {
	t := transport.NewStdioWithOptions(command, env, args)
	return mcpclient.NewClient(t)
}

// 创建 SSE HTTP MCP Client
func NewSSEClient(url string, headers map[string]string) (*mcpclient.Client, error) {
	// 暂时忽略 headers，mcp-go 的 HTTP transport 使用不同方式传递
	t, err := transport.NewStreamableHTTP(url)
	if err != nil {
		return nil, err
	}
	return mcpclient.NewClient(t), nil
}

// 启动 MCP Client (调用 Start 方法)
func StartClient(ctx context.Context, client *mcpclient.Client) error {
	return client.Start(ctx)
}
