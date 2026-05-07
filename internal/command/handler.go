package command

import (
	"github.com/chawuciren/evoduck/pkg/models"
)

// Handler 命令处理器接口
type Handler interface {
	// 基础信息
	Name() string        // 命令名称 (不含 "/"，如 "help")
	Description() string // 命令描述
	Usage() string       // 用法说明 (可选，显示参数格式)

	// 权限要求
	RequiredRole() models.Role // 所需角色，models.RoleAll 表示所有用户可用

	// 执行命令
	Execute(ctx *Context) (*Result, error)
}

// HandlerFunc 函数式处理器 (简化实现)
type HandlerFunc func(ctx *Context) (*Result, error)

// FuncHandler 函数式处理器包装
type FuncHandler struct {
	name        string
	description string
	usage       string
	role        models.Role
	handler     HandlerFunc
}

// NewFuncHandler 创建函数式处理器
func NewFuncHandler(name, description, usage string, role models.Role, handler HandlerFunc) *FuncHandler {
	return &FuncHandler{
		name:        name,
		description: description,
		usage:       usage,
		role:        role,
		handler:     handler,
	}
}

func (h *FuncHandler) Name() string              { return h.name }
func (h *FuncHandler) Description() string       { return h.description }
func (h *FuncHandler) Usage() string             { return h.usage }
func (h *FuncHandler) RequiredRole() models.Role { return h.role }
func (h *FuncHandler) Execute(ctx *Context) (*Result, error) {
	return h.handler(ctx)
}

// Info 命令信息 (用于列表显示)
type Info struct {
	Name        string
	Description string
	Usage       string
	Role        models.Role
}

// RoleAll 特殊角色，表示所有用户可用
const RoleAll models.Role = ""
