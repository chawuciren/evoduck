package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

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

// ToolWithTimeout 声明自身单次调用超时的工具
// 返回值 > 0 时，覆盖 Registry 的全局默认兜底超时；返回 0 表示走全局默认
// 用于 MCP/Plugin 等每个 server/tool 需要独立配置调用超时的场景
type ToolWithTimeout interface {
	Tool
	CallTimeout() time.Duration
}

// TimeoutExempt 声明豁免 Registry 全局兜底超时的工具
// 用于自身已管理超时（exec/code_execution/web_*）或语义上需长跑（sleep/subagent_start/session_run）的工具
// 实现 IsTimeoutExempt() 返回 true 时，Registry 不会为其叠加 context.WithTimeout
type TimeoutExempt interface {
	Tool
	IsTimeoutExempt() bool
}

type contextKey string

const sessionKeyContextKey contextKey = "session_key"

type Registry struct {
	mu             sync.RWMutex
	tools          map[string]Tool
	currentUserID  string        // 当前用户 ID
	userIsolation  bool          // 是否启用用户隔离
	workspace      string        // 当前 workspace
	defaultTimeout time.Duration // 工具调用兜底超时；0 表示禁用
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// SetDefaultTimeout 设置工具调用兜底超时
// 所有未实现 TimeoutExempt 的工具调用都会被限制在此超时内
// 设为 0 表示禁用兜底超时
func (r *Registry) SetDefaultTimeout(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultTimeout = d
}

// GetDefaultTimeout 获取当前兜底超时
func (r *Registry) GetDefaultTimeout() time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultTimeout
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
// 注意：基础 Tool 接口不接受 context，因此此路径无法施加兜底超时；
// 需要超时保护的调用方应使用 ExecuteWithRole。
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

	// 叠加兜底超时（只会缩短，不会延长父 ctx 的截止时间）
	ctx, cancel := r.applyTimeoutWithCancel(ctx, t)
	if cancel != nil {
		defer cancel()
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

// applyTimeoutWithCancel 计算工具调用应使用的超时上下文
// 优先级：ToolWithTimeout.CallTimeout() > 全局 defaultTimeout
// ToolWithTimeout 返回 0 或未实现 → 用全局默认
// TimeoutExempt 返回 true 或全局默认为 0 → 不叠加超时，返回原 ctx 和 nil cancel
// context.WithTimeout 只会缩短父 ctx 的截止时间，不会延长，因此对已有更短超时的调用是安全的
func (r *Registry) applyTimeoutWithCancel(ctx context.Context, t Tool) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if t == nil {
		return ctx, nil
	}
	// 豁免：自身已管理超时或语义上需长跑
	if te, ok := t.(TimeoutExempt); ok && te.IsTimeoutExempt() {
		return ctx, nil
	}
	// 工具自声明超时优先
	if twt, ok := t.(ToolWithTimeout); ok {
		if ct := twt.CallTimeout(); ct > 0 {
			return context.WithTimeout(ctx, ct)
		}
	}
	// 全局兜底
	if r.defaultTimeout > 0 {
		return context.WithTimeout(ctx, r.defaultTimeout)
	}
	return ctx, nil
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
