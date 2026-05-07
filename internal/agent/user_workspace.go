package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/internal/profile"
	"github.com/chawuciren/evoduck/pkg/logger"
)

// UserWorkspace 用户工作空间
// 管理用户级别的文件目录，实现用户隔离
// 新目录结构: data/users/{agentID}_user_{userID}/
type UserWorkspace struct {
	dataDir string // 全局数据目录 (data/)
	agentID string // Agent ID
	userID  string // 用户 ID
	userDir string // 用户目录路径
	enabled bool   // 是否启用用户隔离
}

// UserWorkspaceConfig 用户工作空间配置
type UserWorkspaceConfig struct {
	DataDir    string // 全局数据目录 (data/)
	AgentID    string // Agent ID
	UserID     string // 用户 ID
	Enabled    bool   // 是否启用用户隔离
	AutoCreate bool   // 自动创建用户目录
}

// NewUserWorkspace 创建用户工作空间
func NewUserWorkspace(config UserWorkspaceConfig) *UserWorkspace {
	// 未启用或用户 ID 为空，返回禁用状态
	if !config.Enabled || config.UserID == "" || config.AgentID == "" {
		return &UserWorkspace{
			dataDir: config.DataDir,
			enabled: false,
		}
	}

	safeUserID := sanitizeUserID(config.UserID)
	safeAgentID := sanitizeUserID(config.AgentID)
	// 新路径格式: data/users/{agentID}_user_{userID}
	userDir := filepath.Join(config.DataDir, "users", safeAgentID+"_user_"+safeUserID)

	uw := &UserWorkspace{
		dataDir: config.DataDir,
		agentID: config.AgentID,
		userID:  config.UserID,
		userDir: userDir,
		enabled: true,
	}

	// 自动创建目录
	if config.AutoCreate {
		if err := uw.EnsureDir(); err != nil {
			logger.Warn("Failed to create user workspace", logger.Fields{
				"agent_id": config.AgentID,
				"user_id":  config.UserID,
				"error":    err.Error(),
			})
		}
	}

	return uw
}

// EnsureDir 确保用户目录存在
func (uw *UserWorkspace) EnsureDir() error {
	if !uw.enabled {
		return nil
	}

	dirs := []string{
		uw.userDir,
		uw.GetUserMemoryDir(),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	logger.Debug("User workspace ensured", logger.Fields{
		"user_id":  uw.userID,
		"user_dir": uw.userDir,
	})

	return nil
}

// IsEnabled 返回是否启用用户隔离
func (uw *UserWorkspace) IsEnabled() bool {
	return uw.enabled
}

// GetUserID 获取用户 ID
func (uw *UserWorkspace) GetUserID() string {
	return uw.userID
}

// GetUserDir 获取用户目录路径
func (uw *UserWorkspace) GetUserDir() string {
	if !uw.enabled {
		return ""
	}
	return uw.userDir
}

// GetUserMDPath 获取用户级 USER.md 路径
func (uw *UserWorkspace) GetUserMDPath() string {
	if !uw.enabled {
		return ""
	}
	return filepath.Join(uw.userDir, "USER.md")
}

// GetUserMemoryPath 获取用户级 MEMORY.md 路径
func (uw *UserWorkspace) GetUserMemoryPath() string {
	if !uw.enabled {
		return ""
	}
	return filepath.Join(uw.userDir, "MEMORY.md")
}

// GetUserMemoryDir 获取用户级 memory/ 目录路径
func (uw *UserWorkspace) GetUserMemoryDir() string {
	if !uw.enabled {
		return ""
	}
	return filepath.Join(uw.userDir, "memory")
}

// GetUserDailyMemoryPath 获取用户级指定日期的日志路径
func (uw *UserWorkspace) GetUserDailyMemoryPath(date time.Time) string {
	if !uw.enabled {
		return ""
	}
	filename := date.Format("2006-01-02") + ".md"
	return filepath.Join(uw.GetUserMemoryDir(), filename)
}

// GetUserMD 获取或创建用户级 USER.md
// 如果文件不存在，创建默认内容
func (uw *UserWorkspace) GetUserMD() (string, error) {
	if !uw.enabled {
		return "", nil
	}

	path := uw.GetUserMDPath()

	// 文件存在，直接读取
	if content, err := os.ReadFile(path); err == nil {
		return string(content), nil
	}

	// 文件不存在，创建默认内容
	defaultContent := uw.generateDefaultUserMD()
	if err := os.WriteFile(path, []byte(defaultContent), 0644); err != nil {
		return "", fmt.Errorf("create USER.md: %w", err)
	}

	logger.Info("Created default USER.md for user", logger.Fields{
		"user_id": uw.userID,
		"path":    path,
	})

	return defaultContent, nil
}

// generateDefaultUserMD 生成默认 USER.md 内容
func (uw *UserWorkspace) generateDefaultUserMD() string {
	_ = time.Now()
	return profile.DefaultUserProfileMarkdown(uw.userID)
}

// UpdateUserMD 更新用户级 USER.md
func (uw *UserWorkspace) UpdateUserMD(content string) error {
	if !uw.enabled {
		return nil
	}

	path := uw.GetUserMDPath()

	// 确保目录存在
	if err := uw.EnsureDir(); err != nil {
		return err
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("write USER.md: %w", err)
	}

	return nil
}

// FileExists 检查用户级文件是否存在
func (uw *UserWorkspace) FileExists(filename string) bool {
	if !uw.enabled {
		return false
	}

	var path string
	switch filename {
	case "USER.md":
		path = uw.GetUserMDPath()
	case "MEMORY.md":
		path = uw.GetUserMemoryPath()
	default:
		return false
	}

	_, err := os.Stat(path)
	return err == nil
}

// sanitizeUserID 清理用户 ID，移除特殊字符
func sanitizeUserID(id string) string {
	// 只保留字母、数字、下划线、连字符
	re := regexp.MustCompile(`[^a-zA-Z0-9_\-]`)
	return re.ReplaceAllString(id, "_")
}

// GetAgentWorkspace 获取 Agent workspace 路径（用于回退到 Agent 级文件）
func (uw *UserWorkspace) GetAgentWorkspace() string {
	return filepath.Join(uw.dataDir, "agents", uw.agentID)
}

// ResolveMemoryPath 解析记忆文件路径
// 优先返回用户级路径，如果用户隔离未启用则返回 Agent 级路径
func (uw *UserWorkspace) ResolveMemoryPath(filename string) string {
	if !uw.enabled {
		// 未启用用户隔离，使用 Agent 级路径
		return filepath.Join(uw.GetAgentWorkspace(), filename)
	}

	// 启用用户隔离，使用用户级路径
	switch filename {
	case "USER.md":
		return uw.GetUserMDPath()
	case "MEMORY.md":
		return uw.GetUserMemoryPath()
	case "memory":
		return uw.GetUserMemoryDir()
	default:
		return filepath.Join(uw.userDir, filename)
	}
}

// LoadFileWithFallback 加载文件，优先用户级，回退到 Agent 级
func (uw *UserWorkspace) LoadFileWithFallback(filename string) (string, bool, error) {
	// 优先尝试用户级文件
	if uw.enabled {
		var userPath string
		switch filename {
		case "USER.md":
			userPath = uw.GetUserMDPath()
		case "MEMORY.md":
			userPath = uw.GetUserMemoryPath()
		default:
			userPath = filepath.Join(uw.userDir, filename)
		}

		if content, err := os.ReadFile(userPath); err == nil {
			return string(content), true, nil // 用户级文件
		}
	}

	// 回退到 Agent 级文件
	agentPath := filepath.Join(uw.GetAgentWorkspace(), filename)
	if content, err := os.ReadFile(agentPath); err == nil {
		return string(content), false, nil // Agent 级文件
	}

	// 文件不存在
	return "", false, nil
}

// LoadRecentUserMemory 加载用户级最近 N 天的记忆日志
func (uw *UserWorkspace) LoadRecentUserMemory(days int) string {
	if !uw.enabled || days <= 0 {
		return ""
	}

	var parts []string
	for i := 0; i < days; i++ {
		date := time.Now().AddDate(0, 0, -i)
		path := uw.GetUserDailyMemoryPath(date)
		if content, err := os.ReadFile(path); err == nil {
			parts = append(parts, fmt.Sprintf("### %s\n%s", date.Format("2006-01-02"), string(content)))
		}
	}

	return strings.Join(parts, "\n\n")
}
