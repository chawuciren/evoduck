package mcp

// MCP 连接状态机与状态快照类型
// 设计目标：MCP 连接全部异步进行，启动不阻塞；可随时查询每个 server 的连接状态；
// 可对失败/掉线的 server 触发重连。

import (
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// ConnState 单个 MCP server 的连接状态
type ConnState string

const (
	// StateDisabled 该 server 在配置中被禁用
	StateDisabled ConnState = "disabled"
	// StatePending 已入队但尚未开始连接
	StatePending ConnState = "pending"
	// StateConnecting 正在建立连接 + 初始化 + 加载工具
	StateConnecting ConnState = "connecting"
	// StateConnected 已成功连接并完成初始化、工具已注册
	StateConnected ConnState = "connected"
	// StateFailed 连接/初始化/加载工具失败
	StateFailed ConnState = "failed"
	// StateReconnecting 正在重连（先关闭旧连接再重试）
	StateReconnecting ConnState = "reconnecting"
	// StateClosed 已被关闭（通常发生在进程关闭时）
	StateClosed ConnState = "closed"
)

// IsOnline 返回该状态是否代表"在线可用"
func (s ConnState) IsOnline() bool {
	return s == StateConnected
}

// serverState 单个 server 的运行时状态记录（非公开）
type serverState struct {
	name        string
	state       ConnState
	err         string
	connectedAt time.Time
	attempts    int
	toolCount   int
	serverInfo  mcp.Implementation
}

// ServerStatus 对外的单个 server 状态快照
type ServerStatus struct {
	Name        string    `json:"name"`         // server 名称（配置 key）
	State       ConnState `json:"state"`        // 连接状态
	Online      bool      `json:"online"`       // 是否在线可用（== connected）
	Error       string    `json:"error"`        // 最近一次错误信息（失败/重连时）
	ConnectedAt time.Time `json:"connected_at"` // 最近一次连上的时间
	Attempts    int       `json:"attempts"`     // 累计连接尝试次数
	ToolCount   int       `json:"tool_count"`   // 已加载并注册的工具数量
	Server      string    `json:"server"`       // server 自报名称
	Version     string    `json:"version"`      // server 自报版本
}

// StatusSnapshot 全部 server 的状态快照
type StatusSnapshot struct {
	Total      int            `json:"total"`      // 已启用的 server 总数（不含 disabled）
	Online     int            `json:"online"`     // 在线 server 数
	Connecting int            `json:"connecting"` // 正在连接/重连的 server 数
	Failed     int            `json:"failed"`     // 失败的 server 数
	Servers    []ServerStatus `json:"servers"`    // 各 server 状态（按名称排序）
}

func (s *serverState) snapshot() ServerStatus {
	return ServerStatus{
		Name:        s.name,
		State:       s.state,
		Online:      s.state.IsOnline(),
		Error:       s.err,
		ConnectedAt: s.connectedAt,
		Attempts:    s.attempts,
		ToolCount:   s.toolCount,
		Server:      s.serverInfo.Name,
		Version:     s.serverInfo.Version,
	}
}
