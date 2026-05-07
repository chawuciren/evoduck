package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
)

// Tool 基础工具接口
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
	Execute(args map[string]interface{}) (string, error)
}

// ToolWithContext 支持上下文的工具接口
type ToolWithContext interface {
	Tool
	ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error)
}

// ToolWithUserContext 支持用户上下文的工具接口
type ToolWithUserContext interface {
	Tool
	ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error)
}

type contextKey string

const sessionKeyContextKey contextKey = "session_key"

type Registry struct {
	mu            sync.RWMutex
	tools         map[string]Tool
	currentUserID string // 当前用户 ID
	userIsolation bool   // 是否启用用户隔离
	workspace     string // 当前 workspace
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// SetUserContext 设置当前用户上下文
func (r *Registry) SetUserContext(userID string, userIsolation bool, workspace string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentUserID = userID
	r.userIsolation = userIsolation
	r.workspace = workspace
}

// GetCurrentUserContext 获取当前用户上下文
func (r *Registry) GetCurrentUserContext() (userID string, userIsolation bool, workspace string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentUserID, r.userIsolation, r.workspace
}

func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	return t, nil
}

func (r *Registry) List() []models.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var defs []models.ToolDefinition
	for _, t := range r.tools {
		defs = append(defs, models.ToolDefinition{
			Type: "function",
			Function: models.FunctionDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			},
		})
	}
	return defs
}

// Execute 执行工具（无上下文，用于向后兼容）
func (r *Registry) Execute(name string, args map[string]interface{}) (string, error) {
	t, err := r.Get(name)
	if err != nil {
		return "", err
	}
	return t.Execute(args)
}

// ExecuteWithRole 带角色检查的工具执行
func (r *Registry) ExecuteWithRole(ctx context.Context, name string, args map[string]interface{}, role models.Role) (string, error) {
	t, err := r.Get(name)
	if err != nil {
		return "", err
	}

	// 检查是否实现了 ToolWithUserContext 接口（最高优先级）
	if twuc, ok := t.(ToolWithUserContext); ok {
		userID, userIsolation, workspace := r.GetCurrentUserContext()
		logger.Debug("Registry ExecuteWithRole - passing user context", logger.Fields{
			"tool":          name,
			"userID":        userID,
			"userIsolation": userIsolation,
			"workspace":     workspace,
		})
		return twuc.ExecuteWithUserContext(ctx, args, role, userID, userIsolation, workspace)
	}

	// 检查是否实现了 ToolWithContext 接口
	if twc, ok := t.(ToolWithContext); ok {
		return twc.ExecuteWithRole(ctx, args, role)
	}

	// 否则使用基础 Execute 方法
	return t.Execute(args)
}

func WithSessionKey(ctx context.Context, sessionKey string) context.Context {
	if ctx == nil || sessionKey == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionKeyContextKey, sessionKey)
}

func SessionKeyFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(sessionKeyContextKey).(string)
	return value
}

// ParseToolCallArgs 解析工具调用参数
func ParseToolCallArgs(argsJSON string) (map[string]interface{}, error) {
	if argsJSON == "" {
		return make(map[string]interface{}), nil
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, fmt.Errorf("parse tool arguments: %w", err)
	}

	return args, nil
}
