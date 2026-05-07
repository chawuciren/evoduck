package command

import (
	"fmt"
	"strings"
	"sync"

	"github.com/chawuciren/evoduck/pkg/models"
)

// Registry 命令注册表
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewRegistry 创建命令注册表
func NewRegistry() *Registry {
	return &Registry{
		handlers: make(map[string]Handler),
	}
}

// Register 注册命令
func (r *Registry) Register(h Handler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := strings.ToLower(h.Name())
	if name == "" {
		return fmt.Errorf("command name cannot be empty")
	}

	if _, exists := r.handlers[name]; exists {
		return fmt.Errorf("command already registered: %s", name)
	}

	r.handlers[name] = h
	return nil
}

// MustRegister 注册命令 (失败时 panic)
func (r *Registry) MustRegister(h Handler) {
	if err := r.Register(h); err != nil {
		panic(err)
	}
}

// Unregister 取消注册命令
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.handlers, strings.ToLower(name))
}

// Get 获取命令处理器
func (r *Registry) Get(name string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[strings.ToLower(name)]
	return h, ok
}

// List 列出所有命令信息 (按角色过滤)
func (r *Registry) List(role models.Role) []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var list []Info
	for _, h := range r.handlers {
		// 检查角色权限
		if !r.checkRole(h.RequiredRole(), role) {
			continue
		}
		list = append(list, Info{
			Name:        h.Name(),
			Description: h.Description(),
			Usage:       h.Usage(),
			Role:        h.RequiredRole(),
		})
	}

	// 按名称排序
	sortByName(list)
	return list
}

// ListAll 列出所有命令信息 (不按角色过滤)
func (r *Registry) ListAll() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var list []Info
	for _, h := range r.handlers {
		list = append(list, Info{
			Name:        h.Name(),
			Description: h.Description(),
			Usage:       h.Usage(),
			Role:        h.RequiredRole(),
		})
	}

	sortByName(list)
	return list
}

// Execute 执行命令
func (r *Registry) Execute(ctx *Context) (*Result, error) {
	h, ok := r.Get(ctx.Name)
	if !ok {
		return nil, fmt.Errorf("unknown command: /%s", ctx.Name)
	}

	// 检查角色权限
	if !r.checkRole(h.RequiredRole(), ctx.Role) {
		return nil, fmt.Errorf("permission denied: /%s requires role %s", ctx.Name, h.RequiredRole())
	}

	return h.Execute(ctx)
}

// HasCommand 检查命令是否存在
func (r *Registry) HasCommand(name string) bool {
	_, ok := r.Get(name)
	return ok
}

// checkRole 检查角色权限
func (r *Registry) checkRole(required models.Role, actual models.Role) bool {
	// RoleAll 表示所有用户可用
	if required == RoleAll || required == "" {
		return true
	}

	// 检查角色层级: admin > employee > customer
	switch required {
	case models.RoleAdmin:
		return actual == models.RoleAdmin
	case models.RoleEmployee:
		return actual == models.RoleAdmin || actual == models.RoleEmployee
	case models.RoleCustomer:
		return true // customer 权限最低，所有角色都能访问
	default:
		return actual == required
	}
}

// ParseSlashCommand 解析斜杆命令
// 输入: "/help status" 或 "/new"
// 输出: name="help", args="status" 或 name="new", args=""
func ParseSlashCommand(text string) (name string, args string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", ""
	}

	// 移除 "/" 前缀
	text = text[1:]

	// 分割命令名和参数
	parts := strings.SplitN(text, " ", 2)
	name = strings.ToLower(parts[0])
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	return name, args
}

// IsSlashCommand 检查是否是斜杆命令
func IsSlashCommand(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "/")
}

// sortByName 按名称排序
func sortByName(list []Info) {
	// 简单冒泡排序 (列表通常很短)
	for i := 0; i < len(list)-1; i++ {
		for j := i + 1; j < len(list); j++ {
			if list[i].Name > list[j].Name {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
}

// FormatHelpText 格式化帮助文本
func FormatHelpText(commands []Info) string {
	var sb strings.Builder
	sb.WriteString("# 可用命令\n\n")
	sb.WriteString("| 命令 | 描述 | 用法 | 权限 |\n")
	sb.WriteString("|------|------|------|------|\n")

	for _, cmd := range commands {
		usage := cmd.Usage
		if usage == "" {
			usage = "/" + cmd.Name
		}
		role := "all"
		if cmd.Role != RoleAll && cmd.Role != "" {
			role = string(cmd.Role)
		}
		sb.WriteString(fmt.Sprintf("| /%s | %s | %s | %s |\n", cmd.Name, cmd.Description, usage, role))
	}

	sb.WriteString("\n💡 **提示**: 输入 `/命令名 ?` 查看详细用法\n")
	return sb.String()
}
