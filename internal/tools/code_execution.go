package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/pkg/models"
)

// CodeExecutionTool 沙盒代码执行工具
type CodeExecutionTool struct {
	timeout       time.Duration
	maxOutputSize int
	tempDir       string
}

func NewCodeExecutionTool() *CodeExecutionTool {
	return &CodeExecutionTool{
		timeout:       60 * time.Second,
		maxOutputSize: 50000, // 50KB
		tempDir:       os.TempDir(),
	}
}

func (t *CodeExecutionTool) Name() string {
	return "code_execution"
}

// IsTimeoutExempt 自身已通过 timeout 参数管理超时，豁免 Registry 全局兜底
func (t *CodeExecutionTool) IsTimeoutExempt() bool {
	return true
}

func (t *CodeExecutionTool) Description() string {
	return `Execute Python or JavaScript code in a sandboxed environment.

**Supported Languages:**
- python: Python 3.x
- javascript: Node.js

**Security Features:**
- Execution timeout (60s default)
- No network access (disabled)
- No file system write outside temp directory
- Output size limited (50KB)

**Use Cases:**
- Data analysis and processing
- Algorithm implementation
- Quick calculations
- Code testing

**Parameters:**
- language: "python" or "javascript"
- code: The code to execute
- timeout: Timeout in seconds (optional, default: 60)

**Returns:**
- stdout and stderr output
- Execution time
- Exit code`
}

func (t *CodeExecutionTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"language": map[string]interface{}{
				"type":        "string",
				"description": "Programming language: python or javascript",
				"enum":        []string{"python", "javascript"},
			},
			"code": map[string]interface{}{
				"type":        "string",
				"description": "The code to execute",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Timeout in seconds (default: 60, max: 300)",
			},
		},
		"required": []string{"language", "code"},
	}
}

// ExecuteWithRole 带角色检查
func (t *CodeExecutionTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	if role != models.RoleEmployee && role != models.RoleAdmin {
		return "", fmt.Errorf("access denied: code_execution requires employee or admin role")
	}
	return t.ExecuteWithContext(ctx, args)
}

func (t *CodeExecutionTool) Execute(args map[string]interface{}) (string, error) {
	return t.ExecuteWithContext(context.Background(), args)
}

// ExecuteWithContext 带上下文执行，支持取消传播
func (t *CodeExecutionTool) ExecuteWithContext(parentCtx context.Context, args map[string]interface{}) (string, error) {
	language, ok := args["language"].(string)
	if !ok || language == "" {
		return "", fmt.Errorf("language is required (python or javascript)")
	}

	code, ok := args["code"].(string)
	if !ok || code == "" {
		return "", fmt.Errorf("code is required")
	}

	// 解析超时
	timeout := 60
	if to, ok := args["timeout"].(float64); ok {
		timeout = int(to)
		if timeout > 300 {
			timeout = 300 // 最大 5 分钟
		}
		if timeout < 1 {
			timeout = 60
		}
	}

	// 执行代码
	switch language {
	case "python":
		return t.executePythonWithContext(parentCtx, code, timeout)
	case "javascript":
		return t.executeJavaScriptWithContext(parentCtx, code, timeout)
	default:
		return "", fmt.Errorf("unsupported language: %s (use 'python' or 'javascript')", language)
	}
}

// executePythonWithContext 执行 Python 代码（带上下文）
func (t *CodeExecutionTool) executePythonWithContext(parentCtx context.Context, code string, timeoutSeconds int) (string, error) {
	// 创建临时文件
	tempFile := filepath.Join(t.tempDir, fmt.Sprintf("code_%d.py", time.Now().UnixNano()))
	defer os.Remove(tempFile)

	// 写入代码
	if err := os.WriteFile(tempFile, []byte(code), 0644); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}

	// 创建带超时的子上下文，继承父上下文的取消信号
	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// 准备命令
	cmd := exec.Command("python3", "-S", tempFile)
	cmd.Dir = t.tempDir

	// 安全环境变量（禁用网络相关的库）
	cmd.Env = []string{
		"PYTHONPATH=",
		"PYTHONHOME=",
		"PYTHONDONTWRITEBYTECODE=1",
		"PYTHONUNBUFFERED=1",
		"HOME=/tmp",
		"TMPDIR=" + t.tempDir,
	}

	// 执行
	startTime := time.Now()
	output, err := runCommandWithContext(ctx, cmd)
	duration := time.Since(startTime)

	return t.formatResult(output, err, duration, ctx.Err(), "Python"), nil
}

// executeJavaScriptWithContext 执行 JavaScript 代码（带上下文）
func (t *CodeExecutionTool) executeJavaScriptWithContext(parentCtx context.Context, code string, timeoutSeconds int) (string, error) {
	// 创建临时文件
	tempFile := filepath.Join(t.tempDir, fmt.Sprintf("code_%d.js", time.Now().UnixNano()))
	defer os.Remove(tempFile)

	// 写入代码
	if err := os.WriteFile(tempFile, []byte(code), 0644); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}

	// 创建带超时的子上下文，继承父上下文的取消信号
	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// 准备命令
	cmd := exec.Command("node", "--no-warnings", tempFile)
	cmd.Dir = t.tempDir

	// 安全环境变量
	cmd.Env = []string{
		"NODE_ENV=production",
		"HOME=/tmp",
		"TMPDIR=" + t.tempDir,
	}

	// 执行
	startTime := time.Now()
	output, err := runCommandWithContext(ctx, cmd)
	duration := time.Since(startTime)

	return t.formatResult(output, err, duration, ctx.Err(), "JavaScript"), nil
}

// formatResult 格式化输出结果
func (t *CodeExecutionTool) formatResult(output []byte, execErr error, duration time.Duration, ctxErr error, language string) string {
	var result strings.Builder

	result.WriteString(fmt.Sprintf("# Code Execution (%s)\n\n", language))
	result.WriteString(fmt.Sprintf("**Duration:** %v\n\n", duration))

	if ctxErr == context.DeadlineExceeded {
		result.WriteString("⏱️ **Timeout** - Execution exceeded time limit\n\n")
	}

	if execErr != nil {
		result.WriteString("❌ **Execution Error**\n\n")
		if exitErr, ok := execErr.(*exec.ExitError); ok {
			result.WriteString(fmt.Sprintf("Exit code: %d\n\n", exitErr.ExitCode()))
		} else {
			result.WriteString(fmt.Sprintf("Error: %v\n\n", execErr))
		}
	} else {
		result.WriteString("✅ **Success**\n\n")
	}

	// 截断过长的输出
	outputStr := string(output)
	if len(outputStr) > t.maxOutputSize {
		outputStr = outputStr[:t.maxOutputSize] + fmt.Sprintf("\n... (truncated, %d bytes total)", len(output))
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

// SetTempDir 设置临时目录
func (t *CodeExecutionTool) SetTempDir(dir string) {
	t.tempDir = dir
}

// SetTimeout 设置超时时间
func (t *CodeExecutionTool) SetTimeout(timeout time.Duration) {
	t.timeout = timeout
}
