package logger

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"
)

// ANSI 颜色代码
const (
	colorReset  = "\033[0m"
	colorGray   = "\033[90m" // DEBUG
	colorGreen  = "\033[32m" // INFO
	colorYellow = "\033[33m" // WARN
	colorRed    = "\033[31m" // ERROR
	colorBold   = "\033[1m"
)

// Level 日志级别
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
	FATAL
)

func (l Level) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	case FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// ParseLevel 从字符串解析日志级别
func ParseLevel(s string) Level {
	switch s {
	case "DEBUG", "debug":
		return DEBUG
	case "INFO", "info":
		return INFO
	case "WARN", "warn", "WARNING", "warning":
		return WARN
	case "ERROR", "error":
		return ERROR
	case "FATAL", "fatal":
		return FATAL
	default:
		return INFO // 默认 INFO
	}
}

// Logger 日志器
type Logger struct {
	mu       sync.Mutex
	output   io.Writer
	level    Level
	service  string
	version  string
	jsonMode bool
	color    bool // 是否启用彩色输出（仅文本模式有效）

	// 文件日志相关
	fileOutput io.Writer     // 文件写入器（与 stdout 多路复用）
	fileBuffer *bufio.Writer // 文件缓冲区（减少磁盘写压力，满 4KB 自动落盘）
	logDir     string        // 日志目录
	currentDay string        // 当前日志文件对应的日期
}

// Fields 日志字段
type Fields map[string]interface{}

// Entry 日志条目
type Entry struct {
	Time      time.Time              `json:"time"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Service   string                 `json:"service,omitempty"`
	Version   string                 `json:"version,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
	SessionID string                 `json:"session_id,omitempty"`
	AgentID   string                 `json:"agent_id,omitempty"`
	UserID    string                 `json:"user_id,omitempty"` // 用户ID
	Tool      string                 `json:"tool,omitempty"`
	Module    string                 `json:"module,omitempty"` // 模块标识
	Duration  string                 `json:"duration,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Stack     string                 `json:"stack,omitempty"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

var (
	defaultLogger *Logger
	once          sync.Once
)

// Init 初始化日志系统
func Init(opts ...Option) {
	once.Do(func() {
		defaultLogger = NewLogger(opts...)
	})
}

// Option 日志配置选项
type Option func(*Logger)

// WithOutput 设置输出目标
func WithOutput(w io.Writer) Option {
	return func(l *Logger) {
		l.output = w
	}
}

// WithLevel 设置日志级别
func WithLevel(level Level) Option {
	return func(l *Logger) {
		l.level = level
	}
}

// WithService 设置服务名称
func WithService(service string) Option {
	return func(l *Logger) {
		l.service = service
	}
}

// WithVersion 设置版本
func WithVersion(version string) Option {
	return func(l *Logger) {
		l.version = version
	}
}

// WithJSONMode 设置 JSON 模式
func WithJSONMode(jsonMode bool) Option {
	return func(l *Logger) {
		l.jsonMode = jsonMode
	}
}

// WithColor 设置彩色输出（仅文本模式有效）
func WithColor(color bool) Option {
	return func(l *Logger) {
		l.color = color
	}
}

// WithFileOutput 设置文件输出目录（同时输出到控制台和文件）
// 文件按日期分隔: data/logs/evoduck-YYYY-MM-DD.log
func WithFileOutput(logDir string) Option {
	return func(l *Logger) {
		l.logDir = logDir
	}
}

// shouldEnableColor 检测是否应该启用彩色输出
// Windows CMD 不支持 ANSI 颜色码，Windows Terminal 支持
func shouldEnableColor() bool {
	// 非 Windows 平台，默认支持颜色（终端环境）
	if runtime.GOOS != "windows" {
		return true
	}

	// Windows Terminal 支持 ANSI（检测 WT_SESSION 环境变量）
	if os.Getenv("WT_SESSION") != "" {
		return true
	}

	// Windows CMD/PowerShell 5.1 不支持 ANSI，禁用颜色
	return false
}

// NewLogger 创建新的日志器
func NewLogger(opts ...Option) *Logger {
	l := &Logger{
		output:   os.Stdout,
		level:    INFO,
		service:  "evoduck",
		jsonMode: false,               // 默认使用文本格式（开发友好）
		color:    shouldEnableColor(), // 自动检测是否支持颜色
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}

// GetLogger 获取默认日志器
func GetLogger() *Logger {
	if defaultLogger == nil {
		Init()
	}
	return defaultLogger
}

// log 写入日志
func (l *Logger) log(level Level, msg string, fields Fields) {
	if level < l.level {
		return
	}

	entry := Entry{
		Time:    time.Now(),
		Level:   level.String(),
		Message: msg,
		Service: l.service,
		Version: l.version,
		Fields:  fields,
	}

	// ERROR 和 FATAL 级别添加堆栈信息
	if level >= ERROR {
		entry.Stack = getStackTrace()
	}

	// 合并上下文字段
	if fields != nil {
		if reqID, ok := fields["request_id"].(string); ok {
			entry.RequestID = reqID
		}
		if sessID, ok := fields["session_id"].(string); ok {
			entry.SessionID = sessID
		}
		if agentID, ok := fields["agent_id"].(string); ok {
			entry.AgentID = agentID
		}
		if tool, ok := fields["tool"].(string); ok {
			entry.Tool = tool
		}
		if duration, ok := fields["duration"].(string); ok {
			entry.Duration = duration
		}
		if err, ok := fields["error"].(string); ok {
			entry.Error = err
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// 检查文件日志是否需要轮转（按日期分隔）
	l.checkAndRotateLogFile()

	if l.jsonMode {
		data, err := json.Marshal(entry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to marshal log entry: %v\n", err)
			return
		}
		fmt.Fprintln(l.output, string(data))
	} else {
		// 彩色对齐文本格式
		l.formatTextOutput(entry, level)
	}

	// FATAL 级别退出程序
	if level == FATAL {
		os.Exit(1)
	}
}

// formatTextOutput 格式化文本输出（彩色对齐格式）
func (l *Logger) formatTextOutput(entry Entry, level Level) {
	// 时间戳
	timeStr := entry.Time.Format("2006-01-02 15:04:05.000")

	// 获取模块名（从 Fields 中提取）
	module := ""
	if entry.Fields != nil {
		if m, ok := entry.Fields["module"].(string); ok {
			module = m
		}
	}

	// 级别颜色和对齐
	levelStr := fmt.Sprintf("%-5s", entry.Level)
	if l.color {
		levelStr = levelColor(level, levelStr)
	}

	// 模块名对齐（8字符）
	moduleStr := fmt.Sprintf("%-8s", module)
	if module != "" {
		moduleStr = "[" + module + "]"
		if len(moduleStr) < 9 {
			moduleStr = fmt.Sprintf("%-9s", moduleStr)
		} else {
			moduleStr = fmt.Sprintf("%-12s", moduleStr[:9])
		}
	} else {
		moduleStr = "         " // 9空格对齐
	}

	// 第一行：时间 级别 模块 消息
	fmt.Fprintf(l.output, "%s  %s %s %s", timeStr, levelStr, moduleStr, entry.Message)

	// 上下文标识（request_id 等放在第一行末尾）
	if entry.RequestID != "" {
		fmt.Fprintf(l.output, " req_id=%s", entry.RequestID)
	}

	fmt.Fprintln(l.output)

	// 第二行：结构化字段（缩进对齐）
	if entry.Fields != nil && len(entry.Fields) > 0 {
		indent := "                                 └─ " // 33空格 + 符号
		fieldParts := formatFields(entry.Fields, l.color)
		if len(fieldParts) > 0 {
			fmt.Fprintf(l.output, "%s%s\n", indent, fieldParts)
		}
	}

	// ERROR 级别输出堆栈（缩进）
	if entry.Error != "" {
		indent := "                                 └─ "
		if l.color {
			fmt.Fprintf(l.output, "%s%serror: %s%s\n", indent, colorRed, entry.Error, colorReset)
		} else {
			fmt.Fprintf(l.output, "%serror: %s\n", indent, entry.Error)
		}
	}

	if entry.Stack != "" && level >= ERROR {
		indent := "                                 └─ "
		stackLines := formatStacktrace(entry.Stack)
		for _, line := range stackLines {
			fmt.Fprintf(l.output, "%s%s\n", indent, line)
		}
	}
}

// levelColor 为日志级别添加颜色
func levelColor(level Level, s string) string {
	switch level {
	case DEBUG:
		return colorGray + s + colorReset
	case INFO:
		return colorGreen + s + colorReset
	case WARN:
		return colorYellow + s + colorReset
	case ERROR:
		return colorRed + s + colorReset
	case FATAL:
		return colorBold + colorRed + s + colorReset
	default:
		return s
	}
}

// formatFields 格式化字段为字符串
func formatFields(fields Fields, color bool) string {
	// 过滤掉已处理的字段
	skipKeys := map[string]bool{
		"request_id": true,
		"session_id": true,
		"agent_id":   true,
		"tool":       true,
		"module":     true,
		"error":      true,
		"stack":      true,
	}

	var parts []string
	for k, v := range fields {
		if skipKeys[k] {
			continue
		}
		// 格式化值
		var valStr string
		switch val := v.(type) {
		case string:
			valStr = val
		case int, int64, int32:
			valStr = fmt.Sprintf("%d", val)
		case float64, float32:
			valStr = fmt.Sprintf("%.2f", val)
		case bool:
			valStr = fmt.Sprintf("%v", val)
		case []string:
			valStr = fmt.Sprintf("[%s]", joinStrings(val, ", "))
		default:
			valStr = fmt.Sprintf("%v", val)
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, valStr))
	}

	if len(parts) == 0 {
		return ""
	}

	// 连接所有字段，用 ", " 分隔
	result := ""
	for i, part := range parts {
		if i > 0 {
			result += ", "
		}
		result += part
	}
	return result
}

// joinStrings 连接字符串数组
func joinStrings(arr []string, sep string) string {
	result := ""
	for i, s := range arr {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

// formatStacktrace 格式化堆栈跟踪为多行
func formatStacktrace(stack string) []string {
	// 堆栈格式已经是多行的，直接返回
	if stack == "" {
		return nil
	}
	// 将堆栈按换行分割，每行添加缩进
	lines := splitLines(stack)
	return lines
}

// splitLines 分割字符串为行
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// getStackTrace 获取堆栈跟踪
func getStackTrace() string {
	const depth = 32
	var pcs [depth]uintptr
	n := runtime.Callers(3, pcs[:])
	if n == 0 {
		return ""
	}

	var frames []string
	for _, pc := range pcs[:n] {
		fn := runtime.FuncForPC(pc)
		if fn == nil {
			continue
		}
		file, line := fn.FileLine(pc)
		frames = append(frames, fmt.Sprintf("%s:%d %s", file, line, fn.Name()))
	}

	result := ""
	for i, frame := range frames {
		if i > 0 {
			result += "\n"
		}
		result += "  " + frame
	}
	return result
}

// 上下文键
type ctxKey string

const (
	RequestIDKey ctxKey = "request_id"
	SessionIDKey ctxKey = "session_id"
	AgentIDKey   ctxKey = "agent_id"
	UserIDKey    ctxKey = "user_id"
	ModuleKey    ctxKey = "module"
)

// WithRequestID 设置请求 ID 到上下文
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

// WithSessionID 设置会话 ID 到上下文
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, SessionIDKey, sessionID)
}

// WithAgentID 设置 Agent ID 到上下文
func WithAgentID(ctx context.Context, agentID string) context.Context {
	return context.WithValue(ctx, AgentIDKey, agentID)
}

// WithUserID 设置用户 ID 到上下文
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}

// WithModule 设置模块标识到上下文
func WithModule(ctx context.Context, module string) context.Context {
	return context.WithValue(ctx, ModuleKey, module)
}

// extractFields 从上下文提取字段
func extractFields(ctx context.Context) Fields {
	fields := make(Fields)
	if ctx == nil {
		return fields
	}

	if reqID, ok := ctx.Value(RequestIDKey).(string); ok && reqID != "" {
		fields["request_id"] = reqID
	}
	if sessID, ok := ctx.Value(SessionIDKey).(string); ok && sessID != "" {
		fields["session_id"] = sessID
	}
	if agentID, ok := ctx.Value(AgentIDKey).(string); ok && agentID != "" {
		fields["agent_id"] = agentID
	}
	if userID, ok := ctx.Value(UserIDKey).(string); ok && userID != "" {
		fields["user_id"] = userID
	}
	if module, ok := ctx.Value(ModuleKey).(string); ok && module != "" {
		fields["module"] = module
	}

	return fields
}

// 日志方法

func Debug(msg string, fields ...Fields) {
	GetLogger().Debug(msg, fields...)
}

func Info(msg string, fields ...Fields) {
	GetLogger().Info(msg, fields...)
}

func Warn(msg string, fields ...Fields) {
	GetLogger().Warn(msg, fields...)
}

func Error(msg string, fields ...Fields) {
	GetLogger().Error(msg, fields...)
}

func Fatal(msg string, fields ...Fields) {
	GetLogger().Fatal(msg, fields...)
}

// WithContext 从上下文创建带字段的日志
func WithContext(ctx context.Context) *ContextLogger {
	return &ContextLogger{
		logger: GetLogger(),
		ctx:    ctx,
	}
}

// ContextLogger 带上下文的日志器
type ContextLogger struct {
	logger *Logger
	ctx    context.Context
}

func (cl *ContextLogger) Debug(msg string, fields ...Fields) {
	cl.logger.log(DEBUG, msg, cl.mergeFields(fields))
}

func (cl *ContextLogger) Info(msg string, fields ...Fields) {
	cl.logger.log(INFO, msg, cl.mergeFields(fields))
}

func (cl *ContextLogger) Warn(msg string, fields ...Fields) {
	cl.logger.log(WARN, msg, cl.mergeFields(fields))
}

func (cl *ContextLogger) Error(msg string, fields ...Fields) {
	cl.logger.log(ERROR, msg, cl.mergeFields(fields))
}

func (cl *ContextLogger) Fatal(msg string, fields ...Fields) {
	cl.logger.log(FATAL, msg, cl.mergeFields(fields))
}

func (cl *ContextLogger) mergeFields(fields []Fields) Fields {
	result := extractFields(cl.ctx)
	if len(fields) > 0 {
		for k, v := range fields[0] {
			result[k] = v
		}
	}
	return result
}

// Logger 实例方法

func (l *Logger) Debug(msg string, fields ...Fields) {
	f := mergeFields(fields)
	l.log(DEBUG, msg, f)
}

func (l *Logger) Info(msg string, fields ...Fields) {
	f := mergeFields(fields)
	l.log(INFO, msg, f)
}

func (l *Logger) Warn(msg string, fields ...Fields) {
	f := mergeFields(fields)
	l.log(WARN, msg, f)
}

func (l *Logger) Error(msg string, fields ...Fields) {
	f := mergeFields(fields)
	l.log(ERROR, msg, f)
}

func (l *Logger) Fatal(msg string, fields ...Fields) {
	f := mergeFields(fields)
	l.log(FATAL, msg, f)
}

// ToolLogger 工具日志器
type ToolLogger struct {
	agentID string
	tool    string
}

// NewToolLogger 创建工具日志器
func NewToolLogger(agentID, toolName string) *ToolLogger {
	return &ToolLogger{
		agentID: agentID,
		tool:    toolName,
	}
}

func (tl *ToolLogger) log(level Level, msg string, fields Fields) {
	if fields == nil {
		fields = make(Fields)
	}
	fields["agent_id"] = tl.agentID
	fields["tool"] = tl.tool
	GetLogger().log(level, msg, fields)
}

func (tl *ToolLogger) Debug(msg string, fields ...Fields) {
	tl.log(DEBUG, msg, mergeFields(fields))
}

func (tl *ToolLogger) Info(msg string, fields ...Fields) {
	tl.log(INFO, msg, mergeFields(fields))
}

func (tl *ToolLogger) Warn(msg string, fields ...Fields) {
	tl.log(WARN, msg, mergeFields(fields))
}

func (tl *ToolLogger) Error(msg string, fields ...Fields) {
	tl.log(ERROR, msg, mergeFields(fields))
}

// ModuleLogger 模块日志器（用于统一模块标识）
type ModuleLogger struct {
	module string
}

// NewModuleLogger 创建模块日志器
func NewModuleLogger(module string) *ModuleLogger {
	return &ModuleLogger{module: module}
}

func (ml *ModuleLogger) log(level Level, msg string, fields Fields) {
	if fields == nil {
		fields = make(Fields)
	}
	fields["module"] = ml.module
	GetLogger().log(level, msg, fields)
}

func (ml *ModuleLogger) Debug(msg string, fields ...Fields) {
	ml.log(DEBUG, msg, mergeFields(fields))
}

func (ml *ModuleLogger) Info(msg string, fields ...Fields) {
	ml.log(INFO, msg, mergeFields(fields))
}

func (ml *ModuleLogger) Warn(msg string, fields ...Fields) {
	ml.log(WARN, msg, mergeFields(fields))
}

func (ml *ModuleLogger) Error(msg string, fields ...Fields) {
	ml.log(ERROR, msg, mergeFields(fields))
}

// WithModule 创建带模块标识的字段
func WithModuleField(module string) Fields {
	return Fields{"module": module}
}

func mergeFields(fields []Fields) Fields {
	if len(fields) == 0 {
		return nil
	}
	result := make(Fields)
	for k, v := range fields[0] {
		result[k] = v
	}
	return result
}

// SetLevel 设置日志级别
func SetLevel(level Level) {
	GetLogger().level = level
}

// SetOutput 设置输出目标
func SetOutput(w io.Writer) {
	GetLogger().output = w
}

// SetJSONMode 设置 JSON 模式
func SetJSONMode(jsonMode bool) {
	GetLogger().jsonMode = jsonMode
}

// SetColor 设置彩色输出
func SetColor(color bool) {
	GetLogger().color = color
}

// Configure 从配置重新配置日志系统
// color=false 时自动检测环境（Windows CMD 禁用颜色）
// 用户可通过以下方式强制指定：
//   - LOG_COLOR=true 强制启用颜色
//   - LOG_COLOR=false 强制禁用颜色（注意：当前实现下 false 会触发自动检测）
//   - 或设置 LOG_JSON_MODE=true 使用 JSON 格式（无颜色）
func Configure(level string, jsonMode bool, color bool) {
	l := GetLogger()
	l.level = ParseLevel(level)
	l.jsonMode = jsonMode

	if jsonMode {
		// JSON 模式不需要颜色
		l.color = false
	} else if color {
		// 配置明确启用颜色
		l.color = true
	} else {
		// 配置未指定（false），自动检测环境
		l.color = shouldEnableColor()
	}
}

// SetFileOutputDir 设置日志文件输出目录（同时输出到控制台和文件）
// 文件按日期分隔: {logDir}/evoduck-YYYY-MM-DD.log
func SetFileOutputDir(logDir string) {
	l := GetLogger()
	l.mu.Lock()
	defer l.mu.Unlock()

	// 如果日志目录改变了，强制重新创建日志文件
	if l.logDir != logDir {
		l.logDir = logDir
		l.currentDay = "" // 重置日期，强制 checkAndRotateLogFile 创建新文件
		l.fileOutput = nil
	}

	l.checkAndRotateLogFile()
}

// Flush 立即刷盘当前日志缓冲，确保文件日志可被其他读取方立刻看到。
func Flush() error {
	l := GetLogger()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fileBuffer == nil {
		return nil
	}
	if err := l.fileBuffer.Flush(); err != nil {
		return err
	}
	if f, ok := l.fileOutput.(*os.File); ok && f != nil {
		if err := f.Sync(); err != nil {
			return err
		}
	}
	return nil
}

// checkAndRotateLogFile 检查并旋转日志文件（调用者必须持有 l.mu 锁）
// 使用 bufio.Writer 缓冲减少磁盘写压力，每秒 flush 一次
func (l *Logger) checkAndRotateLogFile() {
	if l.logDir == "" {
		return
	}

	today := time.Now().Format("2006-01-02")

	// 日期未变化，无需轮转
	if l.currentDay == today && l.fileOutput != nil {
		return
	}

	// 关闭旧文件（如果有）
	if l.fileBuffer != nil {
		l.fileBuffer.Flush()
	}
	if oldCloser, ok := l.fileOutput.(*os.File); ok && oldCloser != nil {
		oldCloser.Close()
	}

	// 创建新日志文件
	if err := os.MkdirAll(l.logDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create log directory %s: %v\n", l.logDir, err)
		return
	}

	logFile := fmt.Sprintf("%s/evoduck-%s.log", l.logDir, today)
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open log file %s: %v\n", logFile, err)
		return
	}

	// 用 bufio.Writer 包装，缓冲 4KB 减少 syscall
	buf := bufio.NewWriterSize(f, 4096)

	// 用 MultiWriter 同时写入 stdout 和缓冲文件
	l.output = io.MultiWriter(os.Stdout, buf)
	l.fileOutput = f
	l.fileBuffer = buf
	l.currentDay = today

	// 启动定期 flush 循环（后台 goroutine，每秒 flush 一次）
	go l.flushLoop()
}

// flushLoop 每秒 flush 文件缓冲区（独立协程，不持有日志器主锁）
func (l *Logger) flushLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		l.mu.Lock()
		if l.fileBuffer != nil {
			l.fileBuffer.Flush()
		}
		l.mu.Unlock()
	}
}
