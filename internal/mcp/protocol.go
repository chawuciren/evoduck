package mcp

// MCP 协议类型 - 直接使用 mark3labs/mcp-go 的定义
// 参考: https://github.com/mark3labs/mcp-go

import (
	"github.com/mark3labs/mcp-go/mcp"
)

// 导出 mcp-go 的类型供 EvoDuck 使用
type (
	// JSON-RPC 消息类型
	JSONRPCRequest      = mcp.JSONRPCRequest
	JSONRPCResponse     = mcp.JSONRPCResponse
	JSONRPCNotification = mcp.JSONRPCNotification

	// MCP 协议类型
	Tool           = mcp.Tool
	CallToolResult = mcp.CallToolResult

	// 初始化相关
	ClientCapabilities = mcp.ClientCapabilities
	ServerCapabilities = mcp.ServerCapabilities
	ServerInfo         = mcp.Implementation
	InitializeResult   = mcp.InitializeResult
)

// 错误码常量
const (
	ParseError     = mcp.PARSE_ERROR
	InvalidRequest = mcp.INVALID_REQUEST
	MethodNotFound = mcp.METHOD_NOT_FOUND
	InvalidParams  = mcp.INVALID_PARAMS
	InternalError  = mcp.INTERNAL_ERROR
)

// 协议版本
const ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
