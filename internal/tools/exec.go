package tools

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

// ExecTool 执行 Shell 命令
type ExecTool struct {
	permissions    AgentPermissions
	defaultTimeout time.Duration
	maxOutputSize  int
	bannedCommands []string
	decider        *proxy.Decider
}

func NewExecTool(permissions AgentPermissions, decider *proxy.Decider) *ExecTool {
	return &ExecTool{
		permissions:    permissions,
		defaultTimeout: 30 * time.Second,
		maxOutputSize:  50000, // 50KB
		bannedCommands: []string{
			"rm -rf /",
			"mkfs",
			"dd if=/dev/zero",
			":(){ :|:& };:", // Fork bomb
			"format c:",
			"del /f /s /q c:\\",
		},
		decider: decider,
	}
}

func (t *ExecTool) Name() string {
	return "exec"
}

func (t *ExecTool) Description() string {
	shellInfo := getDefaultShell()
	return fmt.Sprintf(`Execute a shell command in the workspace.

**Features:**
- Runs commands in a sandboxed environment
- Timeout protection (default: 30s)
- Output truncation for large outputs
- Working directory support
- Cross-platform support (Windows/Linux/macOS)

**Current System:**
- OS: %s
- Default Shell: %s
- Workspace: %s

**Security:**
- Commands run within workspace only
- Dangerous commands are blocked
- Environment variables can be customized

**Parameters:**
- command: Shell command to execute
- workdir: Working directory (optional, default: workspace)
- timeout: Timeout in seconds (optional, default: 30)
- shell: Shell to use (optional: "cmd", "powershell", "bash", "sh")
- env: Environment variables (optional)

**Returns:**
- stdout + stderr combined
- Exit code
- Duration`, runtime.GOOS, shellInfo, t.permissions.Workspace)
}

func (t *ExecTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Shell command to execute",
			},
			"workdir": map[string]interface{}{
				"type":        "string",
				"description": "Working directory (relative to workspace)",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Timeout in seconds (default: 30)",
			},
			"shell": map[string]interface{}{
				"type":        "string",
				"description": "Shell to use: cmd, powershell, bash, sh (default: auto-detect)",
				"enum":        []string{"cmd", "powershell", "bash", "sh", "auto"},
			},
			"env": map[string]interface{}{
				"type":        "object",
				"description": "Environment variables",
			},
		},
		"required": []string{"command"},
	}
}

// ShellType shell 类型
type ShellType string

const (
	ShellAuto       ShellType = "auto"
	ShellCmd        ShellType = "cmd"
	ShellPowerShell ShellType = "powershell"
	ShellBash       ShellType = "bash"
	ShellSh         ShellType = "sh"
)

// getDefaultShell 获取默认 shell
func getDefaultShell() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "sh"
}

// getShellCommand 根据类型获取 shell 命令
func getShellCommand(shellType ShellType, command string) *exec.Cmd {
	if shellType == ShellAuto {
		if runtime.GOOS == "windows" {
			shellType = ShellCmd
		} else {
			shellType = ShellSh
		}
	}

	switch shellType {
	case ShellCmd:
		return exec.Command("cmd", "/c", command)
	case ShellPowerShell:
		return exec.Command("powershell", "-Command", command)
	case ShellBash:
		return exec.Command("bash", "-c", command)
	case ShellSh:
		return exec.Command("sh", "-c", command)
	default:
		if runtime.GOOS == "windows" {
			return exec.Command("cmd", "/c", command)
		}
		return exec.Command("sh", "-c", command)
	}
}

// ExecuteWithRole 带角色检查的执行
func (t *ExecTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	// 只有 employee 和 admin 可以执行命令
	if role != models.RoleEmployee && role != models.RoleAdmin {
		return "", fmt.Errorf("access denied: exec tool requires employee or admin role")
	}

	return t.ExecuteWithContext(ctx, args)
}

func (t *ExecTool) Execute(args map[string]interface{}) (string, error) {
	return t.ExecuteWithContext(context.Background(), args)
}

// ExecuteWithContext 带上下文的执行，支持取消传播
func (t *ExecTool) ExecuteWithContext(parentCtx context.Context, args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command is required")
	}

	// 安全检查
	if err := t.validateCommand(command); err != nil {
		return "", err
	}

	// 解析 shell 参数
	shellType := ShellAuto
	if shell, ok := args["shell"].(string); ok && shell != "" {
		shellType = ShellType(shell)
	}

	// 解析参数
	workdir := "."
	if wd, ok := args["workdir"].(string); ok && wd != "" {
		workdir = wd
	}

	timeout := int(t.defaultTimeout.Seconds())
	if to, ok := args["timeout"].(float64); ok {
		timeout = int(to)
	}

	// 解析环境变量
	env := os.Environ()
	if envMap, ok := args["env"].(map[string]interface{}); ok {
		for k, v := range envMap {
			if vs, ok := v.(string); ok {
				env = append(env, fmt.Sprintf("%s=%s", k, vs))
			}
		}
	}

	// 使用 decider 为 exec 命令构建代理环境变量
	if t.decider != nil {
		// 提取命令名称（第一个词）
		commandName := extractCommandName(command)
		env = t.decider.BuildExecEnv(commandName, env)
	}

	// 解析工作目录
	fullWorkdir, err := t.resolvePath(workdir)
	if err != nil {
		return "", err
	}
	if err := t.permissions.CanAccessPath(fullWorkdir); err != nil {
		return "", err
	}

	// 创建带超时的子上下文，继承父上下文的取消信号
	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(timeout)*time.Second)
	defer cancel()

	// 根据平台和参数选择 shell，由 runCommandWithContext 统一处理取消和超时
	cmd := getShellCommand(shellType, command)
	cmd.Dir = fullWorkdir
	cmd.Env = env

	// 设置平台特定的进程属性
	setPlatformSysProcAttr(cmd)

	// 执行命令
	startTime := time.Now()
	output, err := runCommandWithContext(ctx, cmd)
	duration := time.Since(startTime)

	// 处理输出
	result := t.formatResult(output, err, duration, ctx.Err())

	return result, nil
}

func runCommandWithContext(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case err := <-waitCh:
		return output.Bytes(), err
	case <-ctx.Done():
		_ = killProcessTree(cmd)
		err := <-waitCh
		return output.Bytes(), err
	}
}

// validateCommand 验证命令安全性
func (t *ExecTool) validateCommand(command string) error {
	// 检查禁止的命令
	for _, banned := range t.bannedCommands {
		if strings.Contains(command, banned) {
			return fmt.Errorf("blocked: dangerous command detected")
		}
	}

	// 检查危险的 shell 语法
	dangerous := []string{
		"sudo",
		"su -",
		"chmod 777",
		"chown root",
		"> /dev/sd",
		"mkfs",
	}
	for _, d := range dangerous {
		if strings.Contains(command, d) {
			return fmt.Errorf("blocked: potentially dangerous operation: %s", d)
		}
	}

	return nil
}

// resolvePath 解析路径
func (t *ExecTool) resolvePath(path string) (string, error) {
	return t.permissions.ResolvePath(path)
}

// formatResult 格式化输出结果
func (t *ExecTool) formatResult(output []byte, execErr error, duration time.Duration, ctxErr error) string {
	var result strings.Builder

	// 截断过长的输出
	outputStr := string(output)
	if len(outputStr) > t.maxOutputSize {
		outputStr = outputStr[:t.maxOutputSize] + fmt.Sprintf("\n... (truncated, %d bytes total)", len(output))
	}

	result.WriteString(fmt.Sprintf("Duration: %v\n\n", duration))

	if ctxErr == context.DeadlineExceeded {
		result.WriteString("⏱️ **Timeout**\n\n")
	}

	if execErr != nil {
		result.WriteString("❌ **Error**\n\n")
		if exitErr, ok := execErr.(*exec.ExitError); ok {
			result.WriteString(fmt.Sprintf("Exit code: %d\n\n", exitErr.ExitCode()))
		} else {
			result.WriteString(fmt.Sprintf("Error: %v\n\n", execErr))
		}
	} else {
		result.WriteString("✅ **Success**\n\n")
	}

	if outputStr != "" {
		result.WriteString("### Output\n```\n")
		result.WriteString(outputStr)
		result.WriteString("\n```\n")
	} else {
		result.WriteString("_(no output)_\n")
	}

	return result.String()
}

// SetWorkspace 设置工作目录
func (t *ExecTool) SetWorkspace(workspace string) {
	t.permissions.Workspace = workspace
}

// ============================================================================
// ProcessTool - 后台进程管理
// ============================================================================

// ProcessTool 管理后台进程
type ProcessTool struct {
	permissions AgentPermissions
	sessions    map[string]*ProcessSession
	mu          sync.RWMutex
	decider     *proxy.Decider
}

// ProcessSession 进程会话
type ProcessSession struct {
	ID        string
	Command   string
	Cmd       *exec.Cmd
	Process   *os.Process
	StartTime time.Time
	Status    string // running, completed, failed
	ExitCode  int
	Output    *strings.Builder
	Done      chan struct{}
}

func NewProcessTool(permissions AgentPermissions, decider *proxy.Decider) *ProcessTool {
	return &ProcessTool{
		permissions: permissions,
		sessions:    make(map[string]*ProcessSession),
		decider:     decider,
	}
}

func (t *ProcessTool) Name() string {
	return "process"
}

func (t *ProcessTool) Description() string {
	return `Manage background processes.

**Actions:**
- start: Start a new background process
- list: List all running processes
- poll: Get output from a process (waits for new output)
- log: Get accumulated output
- kill: Terminate a process
- wait: Wait for process to complete (with timeout support)

**Parameters:**
- action: The action to perform
- command: Command to start (for 'start')
- session_id: Process session ID (for poll/log/kill/wait)
- timeout: Timeout in seconds for 'wait' action (default: 60, max: 300)`
}

func (t *ProcessTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: start, list, poll, log, kill, wait",
				"enum":        []string{"start", "list", "poll", "log", "kill", "wait"},
			},
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Command to start (for 'start' action)",
			},
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Process session ID",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Timeout in seconds for 'wait' action (default: 60, max: 300)",
			},
		},
		"required": []string{"action"},
	}
}

// ExecuteWithRole 带角色检查
func (t *ProcessTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	if role != models.RoleEmployee && role != models.RoleAdmin {
		return "", fmt.Errorf("access denied: process tool requires employee or admin role")
	}
	return t.ExecuteWithContext(ctx, args)
}

func (t *ProcessTool) Execute(args map[string]interface{}) (string, error) {
	return t.ExecuteWithContext(context.Background(), args)
}

// ExecuteWithContext 带上下文的执行，支持取消传播
func (t *ProcessTool) ExecuteWithContext(ctx context.Context, args map[string]interface{}) (string, error) {
	action, ok := args["action"].(string)
	if !ok || action == "" {
		return "", fmt.Errorf("action is required")
	}

	switch action {
	case "start":
		return t.actionStartWithContext(ctx, args)
	case "list":
		return t.actionList()
	case "poll":
		return t.actionPoll(args)
	case "log":
		return t.actionLog(args)
	case "kill":
		return t.actionKill(args)
	case "wait":
		return t.actionWaitWithContext(ctx, args)
	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

// actionStartWithContext 启动后台进程（带上下文，支持取消）
func (t *ProcessTool) actionStartWithContext(ctx context.Context, args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command is required for start action")
	}

	// 检查上下文是否已取消
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// 生成 session ID
	sessionID := fmt.Sprintf("proc_%d", time.Now().UnixNano())

	// 解析工作目录
	workdir := t.permissions.Workspace
	if workdir == "" {
		workdir = "."
	}
	if err := t.permissions.CanAccessPath(workdir); err != nil {
		return "", err
	}

	// 根据平台选择 shell，使用 CommandContext 以支持取消
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Dir = workdir

	// 使用 decider 为 process 命令构建代理环境变量
	env := os.Environ()
	if t.decider != nil {
		commandName := extractCommandName(command)
		env = t.decider.BuildExecEnv(commandName, env)
	}
	cmd.Env = env

	// 设置平台特定的进程属性
	setPlatformSysProcAttr(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start process: %w", err)
	}

	// 创建会话
	session := &ProcessSession{
		ID:        sessionID,
		Command:   command,
		Cmd:       cmd,
		Process:   cmd.Process,
		StartTime: time.Now(),
		Status:    "running",
		Output:    &strings.Builder{},
		Done:      make(chan struct{}),
	}

	// 保存会话
	t.mu.Lock()
	t.sessions[sessionID] = session
	t.mu.Unlock()

	// 收集输出的 goroutine
	go func() {
		defer close(session.Done)

		// 合并 stdout 和 stderr
		reader := io.MultiReader(stdout, stderr)
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			t.mu.Lock()
			session.Output.WriteString(line + "\n")
			t.mu.Unlock()
		}

		// 等待进程结束
		err := cmd.Wait()
		t.mu.Lock()
		if ctx.Err() != nil {
			// 进程被取消或超时
			session.Status = "cancelled"
			session.ExitCode = -1
		} else if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				session.ExitCode = exitErr.ExitCode()
				session.Status = "failed"
			}
		} else {
			session.ExitCode = 0
			session.Status = "completed"
		}
		t.mu.Unlock()
	}()

	return fmt.Sprintf("Started process: %s\nSession ID: %s\nCommand: %s", sessionID, sessionID, command), nil
}

// actionList 列出所有进程
func (t *ProcessTool) actionList() (string, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.sessions) == 0 {
		return "No processes running.", nil
	}

	var output strings.Builder
	output.WriteString("# Process Sessions\n\n")

	for id, session := range t.sessions {
		output.WriteString(fmt.Sprintf("## %s\n", id))
		output.WriteString(fmt.Sprintf("- Status: %s\n", session.Status))
		output.WriteString(fmt.Sprintf("- Command: %s\n", session.Command))
		output.WriteString(fmt.Sprintf("- Started: %s\n", session.StartTime.Format("15:04:05")))
		if session.Status != "running" {
			output.WriteString(fmt.Sprintf("- Exit Code: %d\n", session.ExitCode))
		}
		output.WriteString("\n")
	}

	return output.String(), nil
}

// actionPoll 获取进程输出
func (t *ProcessTool) actionPoll(args map[string]interface{}) (string, error) {
	sessionID, ok := args["session_id"].(string)
	if !ok || sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	t.mu.RLock()
	session, exists := t.sessions[sessionID]
	t.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	return fmt.Sprintf("Status: %s\nOutput length: %d bytes\n\n%s",
		session.Status, session.Output.Len(), session.Output.String()), nil
}

// actionLog 获取进程日志
func (t *ProcessTool) actionLog(args map[string]interface{}) (string, error) {
	return t.actionPoll(args)
}

// actionKill 终止进程
func (t *ProcessTool) actionKill(args map[string]interface{}) (string, error) {
	sessionID, ok := args["session_id"].(string)
	if !ok || sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	t.mu.RLock()
	session, exists := t.sessions[sessionID]
	t.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	if session.Status != "running" {
		return fmt.Sprintf("Process already %s", session.Status), nil
	}

	if err := killProcessTree(session.Cmd); err != nil {
		return "", fmt.Errorf("kill process tree: %w", err)
	}

	t.mu.Lock()
	session.Status = "killed"
	t.mu.Unlock()

	return fmt.Sprintf("Process %s killed", sessionID), nil
}

// actionWaitWithContext 等待进程完成（带上下文，支持取消）
func (t *ProcessTool) actionWaitWithContext(ctx context.Context, args map[string]interface{}) (string, error) {
	sessionID, ok := args["session_id"].(string)
	if !ok || sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	// 解析超时参数，默认 60 秒，最大 300 秒
	timeout := 60
	if to, ok := args["timeout"].(float64); ok {
		timeout = int(to)
		if timeout > 300 {
			timeout = 300
		}
		if timeout < 1 {
			timeout = 60
		}
	}

	t.mu.RLock()
	session, exists := t.sessions[sessionID]
	t.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	// 等待进程完成，支持取消和超时
	select {
	case <-session.Done:
		return fmt.Sprintf("Process %s completed with exit code %d\n\nOutput:\n%s",
			sessionID, session.ExitCode, session.Output.String()), nil
	case <-time.After(time.Duration(timeout) * time.Second):
		return "", fmt.Errorf("wait timed out after %d seconds (process may still be running)", timeout)
	case <-ctx.Done():
		return "", fmt.Errorf("wait cancelled: %v", ctx.Err())
	}
}

// SetWorkspace 设置工作目录
func (t *ProcessTool) SetWorkspace(workspace string) {
	t.permissions.Workspace = workspace
}

// extractCommandName 从命令字符串中提取命令名称（第一个词）
func extractCommandName(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	// 处理管道、重定向等复杂情况，只取第一个命令
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return ""
	}
	// 返回第一个词（去掉路径前缀）
	cmdName := parts[0]
	// 如果包含路径分隔符，只取最后一部分
	if idx := strings.LastIndex(cmdName, "/"); idx >= 0 {
		cmdName = cmdName[idx+1:]
	}
	if idx := strings.LastIndex(cmdName, "\\"); idx >= 0 {
		cmdName = cmdName[idx+1:]
	}
	return cmdName
}
