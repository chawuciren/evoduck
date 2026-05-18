package tools

import (
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
		bannedCommands: defaultBannedCommands(),
		decider:        decider,
	}
}

func (t *ExecTool) Name() string {
	return "exec"
}

func (t *ExecTool) Description() string {
	shellInfo := getDefaultShell()
	return fmt.Sprintf(`Execute a short-lived shell command in the workspace.

**When to use:**
- Very short, one-shot, non-interactive commands
- Commands that should finish quickly in the current turn
- Quick checks and immediate output

**Do not use when:**
- The command may take noticeable time or might timeout
- The command may ask for follow-up input later
- You may need to inspect logs across multiple turns
- You need to keep the process running in the background

If there is any realistic chance the command may run long, block, or require interaction, use the process tool instead.

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

type commandSpec struct {
	command string
	workdir string
	shell   ShellType
	env     []string
}

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
	spec, err := t.buildCommandSpec(args)
	if err != nil {
		return "", err
	}

	timeout := int(t.defaultTimeout.Seconds())
	if to, ok := args["timeout"].(float64); ok {
		timeout = int(to)
	}

	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := getShellCommand(spec.shell, spec.command)
	cmd.Dir = spec.workdir
	cmd.Env = spec.env
	setPlatformSysProcAttr(cmd)

	startTime := time.Now()
	output, err := runCommandWithContext(ctx, cmd)
	duration := time.Since(startTime)

	return t.formatResult(output, err, duration, ctx.Err()), nil
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

func defaultBannedCommands() []string {
	return []string{
		"rm -rf /",
		"mkfs",
		"dd if=/dev/zero",
		":(){ :|:& };:",
		"format c:",
		"del /f /s /q c:\\",
	}
}

// validateCommand 验证命令安全性
func validateCommand(command string, bannedCommands []string) error {
	for _, banned := range bannedCommands {
		if strings.Contains(command, banned) {
			return fmt.Errorf("blocked: dangerous command detected")
		}
	}

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

func parseShellArg(args map[string]interface{}) ShellType {
	shellType := ShellAuto
	if shell, ok := args["shell"].(string); ok && shell != "" {
		shellType = ShellType(shell)
	}
	return shellType
}

func parseWorkdirArg(args map[string]interface{}, permissions AgentPermissions) string {
	workdir := "."
	if permissions.Workspace != "" {
		workdir = permissions.Workspace
	}
	if wd, ok := args["workdir"].(string); ok && wd != "" {
		workdir = wd
	}
	return workdir
}

func buildCommandEnv(command string, baseEnv []string, envMap map[string]interface{}, decider *proxy.Decider) []string {
	env := append([]string(nil), baseEnv...)
	for k, v := range envMap {
		if vs, ok := v.(string); ok {
			env = append(env, fmt.Sprintf("%s=%s", k, vs))
		}
	}
	if decider != nil {
		commandName := extractCommandName(command)
		env = decider.BuildExecEnv(commandName, env)
	}
	return env
}

func buildCommandSpec(args map[string]interface{}, permissions AgentPermissions, decider *proxy.Decider, bannedCommands []string) (commandSpec, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return commandSpec{}, fmt.Errorf("command is required")
	}
	if err := validateCommand(command, bannedCommands); err != nil {
		return commandSpec{}, err
	}

	shellType := parseShellArg(args)
	workdir := parseWorkdirArg(args, permissions)
	fullWorkdir, err := permissions.ResolvePath(workdir)
	if err != nil {
		return commandSpec{}, err
	}
	if err := permissions.CanAccessPath(fullWorkdir); err != nil {
		return commandSpec{}, err
	}

	envMap, _ := args["env"].(map[string]interface{})
	env := buildCommandEnv(command, os.Environ(), envMap, decider)

	return commandSpec{
		command: command,
		workdir: fullWorkdir,
		shell:   shellType,
		env:     env,
	}, nil
}

func (t *ExecTool) buildCommandSpec(args map[string]interface{}) (commandSpec, error) {
	return buildCommandSpec(args, t.permissions, t.decider, t.bannedCommands)
}

// resolvePath 解析路径
func (t *ExecTool) resolvePath(path string) (string, error) {
	return t.permissions.ResolvePath(path)
}

// formatResult 格式化输出结果，截断时保留最新内容（从末尾保留）
func (t *ExecTool) formatResult(output []byte, execErr error, duration time.Duration, ctxErr error) string {
	var result strings.Builder

	outputStr := string(output)
	truncated := false
	if len(outputStr) > t.maxOutputSize {
		// 保留最新的内容（从末尾保留），截断旧的（从头部截断）
		outputStr = "... (truncated, showing latest content)\n" + outputStr[len(outputStr)-t.maxOutputSize:]
		truncated = true
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
		if truncated {
			result.WriteString("\n> ⚠️ Output was truncated (older content removed). For full output, use `process` tool with `log` action and pagination.\n")
		}
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

type ProcessTool struct {
	permissions    AgentPermissions
	sessions       map[string]*ProcessSession
	mu             sync.RWMutex
	decider        *proxy.Decider
	bannedCommands []string
}

type ProcessSession struct {
	ID         string
	Command    string
	Cmd        *exec.Cmd
	Process    *os.Process
	Stdin      io.WriteCloser
	StartTime  time.Time
	Status     string
	ExitCode   int
	Output     bytes.Buffer
	ReadOffset int
	Done       chan struct{}
	mu         sync.Mutex
}

func NewProcessTool(permissions AgentPermissions, decider *proxy.Decider) *ProcessTool {
	return &ProcessTool{
		permissions:    permissions,
		sessions:       make(map[string]*ProcessSession),
		decider:        decider,
		bannedCommands: defaultBannedCommands(),
	}
}

func (t *ProcessTool) Name() string {
	return "process"
}

func (t *ProcessTool) Description() string {
	return `Manage a long-running, blocking, background, or interactive shell process.

**When to use:**
- Commands that may block or run longer than one turn
- Commands whose output you may need to inspect while they are still running or later
- Commands that may ask for follow-up input
- Background jobs you want to wait on, poll, or terminate
- Any command that might exceed exec's short-lived, quick-return expectation

**Typical workflow:**
- start: launch the command and get a session ID
- poll: get new output since the last poll (incremental)
- log: view accumulated output with line-based pagination (from newest to oldest)
- input: send text to the running process when it prompts
- wait/kill: finish waiting or terminate the process

**Do not use when:**
- A very short one-shot command should finish immediately; use exec instead

**Actions:**
- start: Start a new background process
- list: List all tracked processes
- poll: Get new output since the last poll (incremental)
- log: View accumulated output with pagination (default: latest 100 lines)
- input: Send text to the process stdin
- kill: Terminate a process
- wait: Wait for process completion with timeout support

**Log pagination:**
- lines: Number of lines per page (default: 100, max: 500)
- offset: Number of lines to skip from the end (default: 0, means latest lines)
- max_bytes: Maximum bytes per page to prevent oversized output (default: 32768, max: 65536)
- Pagination is line-based, from newest to oldest (bottom to top in the output buffer)
- offset=0 returns the latest lines; offset=100 returns lines 101-200 from the end

**Parameters:**
- action: The action to perform
- command: Command to start (for start)
- session_id: Process session ID (for poll/log/input/kill/wait)
- text: Text to send to stdin (for input)
- append_newline: Append a trailing newline when sending input (default: true)
- close_stdin: Close stdin after sending input (default: false)
- workdir: Working directory for start (optional)
- shell: Shell for start: cmd, powershell, bash, sh, auto (optional)
- env: Environment variables for start (optional)
- timeout: Timeout in seconds for wait (default: 60, max: 300)
- lines: Number of lines per page for log (default: 100, max: 500)
- offset: Lines to skip from end for log (default: 0)
- max_bytes: Maximum bytes per page for log (default: 32768, max: 65536)`
}

func (t *ProcessTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: start, list, poll, log, input, kill, wait",
				"enum":        []string{"start", "list", "poll", "log", "input", "kill", "wait"},
			},
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Command to start (for start action)",
			},
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Process session ID",
			},
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Text to send to stdin (for input action)",
			},
			"append_newline": map[string]interface{}{
				"type":        "boolean",
				"description": "Append a trailing newline when sending input (default: true)",
			},
			"close_stdin": map[string]interface{}{
				"type":        "boolean",
				"description": "Close stdin after sending input (default: false)",
			},
			"workdir": map[string]interface{}{
				"type":        "string",
				"description": "Working directory for start (relative to workspace)",
			},
			"shell": map[string]interface{}{
				"type":        "string",
				"description": "Shell to use for start: cmd, powershell, bash, sh (default: auto-detect)",
				"enum":        []string{"cmd", "powershell", "bash", "sh", "auto"},
			},
			"env": map[string]interface{}{
				"type":        "object",
				"description": "Environment variables for start",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Timeout in seconds for wait (default: 60, max: 300)",
			},
			"lines": map[string]interface{}{
				"type":        "integer",
				"description": "Number of lines per page for log action (default: 100, max: 500)",
			},
			"offset": map[string]interface{}{
				"type":        "integer",
				"description": "Number of lines to skip from the end for log action (default: 0, means latest lines)",
			},
			"max_bytes": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum bytes per page for log action (default: 32768, max: 65536)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *ProcessTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	if role != models.RoleEmployee && role != models.RoleAdmin {
		return "", fmt.Errorf("access denied: process tool requires employee or admin role")
	}
	return t.ExecuteWithContext(ctx, args)
}

func (t *ProcessTool) Execute(args map[string]interface{}) (string, error) {
	return t.ExecuteWithContext(context.Background(), args)
}

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
	case "input":
		return t.actionInput(args)
	case "kill":
		return t.actionKill(args)
	case "wait":
		return t.actionWaitWithContext(ctx, args)
	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

func (t *ProcessTool) buildCommandSpec(args map[string]interface{}) (commandSpec, error) {
	return buildCommandSpec(args, t.permissions, t.decider, t.bannedCommands)
}

func (t *ProcessTool) actionStartWithContext(ctx context.Context, args map[string]interface{}) (string, error) {
	spec, err := t.buildCommandSpec(args)
	if err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	sessionID := fmt.Sprintf("proc_%d", time.Now().UnixNano())
	cmd := getShellCommand(spec.shell, spec.command)
	cmd.Dir = spec.workdir
	cmd.Env = spec.env
	setPlatformSysProcAttr(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("create stderr pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("create stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start process: %w", err)
	}

	session := &ProcessSession{
		ID:        sessionID,
		Command:   spec.command,
		Cmd:       cmd,
		Process:   cmd.Process,
		Stdin:     stdin,
		StartTime: time.Now(),
		Status:    "running",
		Done:      make(chan struct{}),
	}

	t.mu.Lock()
	t.sessions[sessionID] = session
	t.mu.Unlock()

	go func() {
		defer close(session.Done)
		captureDone := make(chan struct{}, 2)
		captureOutput := func(reader io.Reader) {
			defer func() { captureDone <- struct{}{} }()
			_, _ = io.Copy(session, reader)
		}
		go captureOutput(stdout)
		go captureOutput(stderr)

		err := cmd.Wait()
		<-captureDone
		<-captureDone

		session.mu.Lock()
		defer session.mu.Unlock()
		if session.Stdin != nil {
			_ = session.Stdin.Close()
			session.Stdin = nil
		}
		if ctx.Err() != nil {
			session.Status = "cancelled"
			session.ExitCode = -1
		} else if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				session.ExitCode = exitErr.ExitCode()
			} else {
				session.ExitCode = -1
			}
			if session.Status != "killed" {
				session.Status = "failed"
			}
		} else {
			session.ExitCode = 0
			if session.Status != "killed" {
				session.Status = "completed"
			}
		}
	}()

	return fmt.Sprintf("Started process: %s\nSession ID: %s\nCommand: %s", sessionID, sessionID, spec.command), nil
}

func (t *ProcessTool) actionList() (string, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.sessions) == 0 {
		return "No processes running.", nil
	}

	var output strings.Builder
	output.WriteString("# Process Sessions\n\n")

	for id, session := range t.sessions {
		session.mu.Lock()
		status := session.Status
		exitCode := session.ExitCode
		outputLen := session.Output.Len()
		startTime := session.StartTime
		command := session.Command
		session.mu.Unlock()

		output.WriteString(fmt.Sprintf("## %s\n", id))
		output.WriteString(fmt.Sprintf("- Status: %s\n", status))
		output.WriteString(fmt.Sprintf("- Command: %s\n", command))
		output.WriteString(fmt.Sprintf("- Started: %s\n", startTime.Format("15:04:05")))
		output.WriteString(fmt.Sprintf("- Output Length: %d bytes\n", outputLen))
		if status != "running" {
			output.WriteString(fmt.Sprintf("- Exit Code: %d\n", exitCode))
		}
		output.WriteString("\n")
	}

	return output.String(), nil
}

func (s *ProcessSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Output.Write(p)
}

func (s *ProcessSession) snapshotOutput(incremental bool) (status string, exitCode int, totalLen int, chunk string, startOffset int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status = s.Status
	exitCode = s.ExitCode
	totalLen = s.Output.Len()
	startOffset = 0
	data := s.Output.Bytes()
	if incremental {
		startOffset = s.ReadOffset
		if startOffset > len(data) {
			startOffset = len(data)
		}
		chunk = string(data[startOffset:])
		s.ReadOffset = len(data)
	} else {
		chunk = string(data)
	}
	return
}

func (t *ProcessTool) getSession(sessionID string) (*ProcessSession, error) {
	t.mu.RLock()
	session, exists := t.sessions[sessionID]
	t.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return session, nil
}

func (t *ProcessTool) actionPoll(args map[string]interface{}) (string, error) {
	sessionID, ok := args["session_id"].(string)
	if !ok || sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	session, err := t.getSession(sessionID)
	if err != nil {
		return "", err
	}
	status, exitCode, totalLen, chunk, startOffset := session.snapshotOutput(true)
	return fmt.Sprintf("Status: %s\nExit code: %d\nOutput length: %d bytes\nNew output offset: %d\n\n%s",
		status, exitCode, totalLen, startOffset, chunk), nil
}

func (t *ProcessTool) actionLog(args map[string]interface{}) (string, error) {
	sessionID, ok := args["session_id"].(string)
	if !ok || sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	session, err := t.getSession(sessionID)
	if err != nil {
		return "", err
	}

	// Parse pagination parameters
	lines := 100
	if l, ok := args["lines"].(float64); ok {
		lines = int(l)
		if lines > 500 {
			lines = 500
		}
		if lines < 1 {
			lines = 100
		}
	}

	offset := 0
	if o, ok := args["offset"].(float64); ok {
		offset = int(o)
		if offset < 0 {
			offset = 0
		}
	}

	maxBytes := 32768
	if mb, ok := args["max_bytes"].(float64); ok {
		maxBytes = int(mb)
		if maxBytes > 65536 {
			maxBytes = 65536
		}
		if maxBytes < 1024 {
			maxBytes = 32768
		}
	}

	// Get the full output buffer
	session.mu.Lock()
	status := session.Status
	exitCode := session.ExitCode
	data := session.Output.Bytes()
	session.mu.Unlock()

	// Count total lines by finding all newline positions from the end
	lineEnds := findLineEndsFromEnd(data)
	totalLines := len(lineEnds)

	// Calculate page boundaries
	// offset=0 means we start from the latest lines (at the end)
	// We skip 'offset' lines from the end, then take 'lines' lines
	skipFromEnd := offset
	linesToTake := lines

	// Determine which lines to include
	startLineIdx := totalLines - skipFromEnd - linesToTake
	if startLineIdx < 0 {
		startLineIdx = 0
		linesToTake = totalLines - skipFromEnd
	}
	endLineIdx := totalLines - skipFromEnd
	if endLineIdx > totalLines {
		endLineIdx = totalLines
	}

	// Extract the content for this page
	var pageContent []byte
	if totalLines == 0 {
		// No newline in buffer, treat entire buffer as one potential "line"
		pageContent = data
	} else {
		// Find byte positions for the line range
		// startLineIdx is the index of the first line to include (0-indexed)
		// endLineIdx is exclusive - we include lines up to endLineIdx-1
		startBytePos := 0
		if startLineIdx > 0 {
			// Start after the newline of the line before our first line
			startBytePos = lineEnds[startLineIdx-1] + 1
		}
		endBytePos := len(data)
		if endLineIdx > 0 && endLineIdx <= len(lineEnds) {
			// End after the newline of the last line we include (endLineIdx-1)
			endBytePos = lineEnds[endLineIdx-1] + 1
		} else if endLineIdx == 0 {
			endBytePos = 0
		}
		pageContent = data[startBytePos:endBytePos]

		// Apply max_bytes ceiling with line alignment
		if len(pageContent) > maxBytes {
			// Truncate to max_bytes, but align to line boundary
			// Since we're going from newest to oldest, truncate from the beginning (older content)
			truncatePoint := len(pageContent) - maxBytes
			// Find the next newline after truncate point to align
			newlinePos := bytes.IndexByte(pageContent[truncatePoint:], '\n')
			if newlinePos >= 0 {
				truncatePoint += newlinePos + 1
				pageContent = pageContent[truncatePoint:]
				// Recalculate lines taken after truncation
				linesInPage := countLines(pageContent)
				linesToTake = linesInPage
			} else {
				// No newline found, just truncate
				pageContent = pageContent[len(pageContent)-maxBytes:]
			}
		}
	}

	pageBytes := len(pageContent)
	hasOlder := startLineIdx > 0
	hasNewer := skipFromEnd > 0

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Status: %s\nExit code: %d\n", status, exitCode))
	result.WriteString(fmt.Sprintf("Total lines: %d\n", totalLines))
	result.WriteString(fmt.Sprintf("Offset: %d (skipped from end)\n", offset))
	result.WriteString(fmt.Sprintf("Lines in page: %d\n", linesToTake))
	result.WriteString(fmt.Sprintf("Page bytes: %d\n", pageBytes))
	if hasOlder {
		result.WriteString("Has older: yes (use offset + lines to view)\n")
	} else {
		result.WriteString("Has older: no\n")
	}
	if hasNewer {
		result.WriteString("Has newer: yes (use offset - lines to view, min offset=0)\n")
	} else {
		result.WriteString("Has newer: no\n")
	}
	result.WriteString("\n")
	result.WriteString(string(pageContent))

	return result.String(), nil
}

// findLineEndsFromEnd finds all newline positions in the data, indexed from the end
// Returns positions of newlines, where lineEnds[0] is the first newline from the start
// and lineEnds[len-1] is the last newline (closest to end)
func findLineEndsFromEnd(data []byte) []int {
	if len(data) == 0 {
		return nil
	}
	var ends []int
	for i, b := range data {
		if b == '\n' {
			ends = append(ends, i)
		}
	}
	return ends
}

// countLines counts the number of lines in the given data
func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	count := bytes.Count(data, []byte("\n"))
	// If the data doesn't end with a newline, add one more line
	if data[len(data)-1] != '\n' {
		count++
	}
	return count
}

func (t *ProcessTool) actionInput(args map[string]interface{}) (string, error) {
	sessionID, ok := args["session_id"].(string)
	if !ok || sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}
	text, ok := args["text"].(string)
	if !ok {
		return "", fmt.Errorf("text is required")
	}
	appendNewline := true
	if raw, ok := args["append_newline"].(bool); ok {
		appendNewline = raw
	}
	closeStdin := false
	if raw, ok := args["close_stdin"].(bool); ok {
		closeStdin = raw
	}

	session, err := t.getSession(sessionID)
	if err != nil {
		return "", err
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	if session.Status != "running" {
		return "", fmt.Errorf("process is not running: %s", session.Status)
	}
	if session.Stdin == nil {
		return "", fmt.Errorf("stdin is unavailable for session: %s", sessionID)
	}
	if appendNewline {
		text += "\n"
	}
	if _, err := io.WriteString(session.Stdin, text); err != nil {
		return "", fmt.Errorf("write stdin: %w", err)
	}
	if closeStdin {
		if err := session.Stdin.Close(); err != nil {
			return "", fmt.Errorf("close stdin: %w", err)
		}
		session.Stdin = nil
	}
	return fmt.Sprintf("Sent %d bytes to process %s", len(text), sessionID), nil
}

func (t *ProcessTool) actionKill(args map[string]interface{}) (string, error) {
	sessionID, ok := args["session_id"].(string)
	if !ok || sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	session, err := t.getSession(sessionID)
	if err != nil {
		return "", err
	}

	session.mu.Lock()
	if session.Status != "running" {
		status := session.Status
		session.mu.Unlock()
		return fmt.Sprintf("Process already %s", status), nil
	}
	session.Status = "killed"
	if session.Stdin != nil {
		_ = session.Stdin.Close()
		session.Stdin = nil
	}
	session.mu.Unlock()

	if err := killProcessTree(session.Cmd); err != nil {
		return "", fmt.Errorf("kill process tree: %w", err)
	}
	return fmt.Sprintf("Process %s killed", sessionID), nil
}

func (t *ProcessTool) actionWaitWithContext(ctx context.Context, args map[string]interface{}) (string, error) {
	sessionID, ok := args["session_id"].(string)
	if !ok || sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

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

	session, err := t.getSession(sessionID)
	if err != nil {
		return "", err
	}

	select {
	case <-session.Done:
		status, exitCode, _, chunk, _ := session.snapshotOutput(false)
		return fmt.Sprintf("Process %s completed with status %s and exit code %d\n\nOutput:\n%s",
			sessionID, status, exitCode, chunk), nil
	case <-time.After(time.Duration(timeout) * time.Second):
		return "", fmt.Errorf("wait timed out after %d seconds (process may still be running)", timeout)
	case <-ctx.Done():
		return "", fmt.Errorf("wait cancelled: %v", ctx.Err())
	}
}

func (t *ProcessTool) SetWorkspace(workspace string) {
	t.permissions.Workspace = workspace
}

// extractCommandName 从命令字符串中提取命令名称（第一个词）
func extractCommandName(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return ""
	}
	cmdName := parts[0]
	if idx := strings.LastIndex(cmdName, "/"); idx >= 0 {
		cmdName = cmdName[idx+1:]
	}
	if idx := strings.LastIndex(cmdName, "\\"); idx >= 0 {
		cmdName = cmdName[idx+1:]
	}
	return cmdName
}
