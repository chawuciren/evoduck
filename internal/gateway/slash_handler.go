package gateway

import (
	"context"

	"github.com/gorilla/websocket"
	"github.com/chawuciren/evoduck/internal/command"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/pkg/models"
)

// SlashCommandHandler 斜杆命令处理器
type SlashCommandHandler struct {
	registry *command.Registry
	gateway  *Gateway
}

// NewSlashCommandHandler 创建斜杆命令处理器
func NewSlashCommandHandler(gateway *Gateway) *SlashCommandHandler {
	registry := command.NewRegistry()

	// 注册内置命令
	command.RegisterBuiltinCommands(registry)

	return &SlashCommandHandler{
		registry: registry,
		gateway:  gateway,
	}
}

// GetRegistry 获取命令注册表 (用于扩展)
func (h *SlashCommandHandler) GetRegistry() *command.Registry {
	return h.registry
}

// Handle 处理斜杆命令
// 返回: handled (是否处理了), result (结果), error (错误)
func (h *SlashCommandHandler) Handle(
	conn *websocket.Conn,
	connID string,
	sessionKey string,
	sess *session.Session,
	agentID string,
	role models.Role,
	userID string,
	message string,
) (handled bool, result *command.Result, err error) {
	// 解析命令
	name, args := command.ParseSlashCommand(message)
	if name == "" {
		return false, nil, nil
	}

	// 检查命令是否存在
	if !h.registry.HasCommand(name) {
		return false, nil, nil // 不是已知命令，交给 Agent 处理
	}

	// 构建命令上下文
	ctx := &command.Context{
		Conn:       conn,
		ConnID:     connID,
		SessionKey: sessionKey,
		Session:    sess,
		AgentID:    agentID,
		Role:       role,
		UserID:     userID,
		Command:    message,
		Name:       name,
		Args:       args,
		Ctx:        context.Background(),
		Gateway:    h.gateway, // Gateway 实现 GatewayAccessor 接口
	}

	// 执行命令
	result, err = h.registry.Execute(ctx)
	return true, result, err
}

// ListCommands 列出所有可用命令
func (h *SlashCommandHandler) ListCommands(role models.Role) []command.Info {
	return h.registry.List(role)
}
