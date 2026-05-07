package logger

import (
	"bufio"
	"bytes"
	"fmt"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"time"
)

// generateRequestID 生成简单的请求 ID
func generateRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ResponseRecorder 响应记录器
type ResponseRecorder struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
}

func (r *ResponseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *ResponseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func (r *ResponseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (r *ResponseRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *ResponseRecorder) Push(target string, opts *http.PushOptions) error {
	pusher, ok := r.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (r *ResponseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// LoggingMiddleware 日志中间件
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// 生成请求 ID
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = generateRequestID()
		}

		// 设置请求 ID 到响应头
		w.Header().Set("X-Request-ID", requestID)

		// 创建带请求 ID 的上下文
		ctx := WithRequestID(r.Context(), requestID)
		r = r.WithContext(ctx)

		// 记录请求
		logRequest(r, requestID)

		// 记录响应
		recorder := &ResponseRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// 调用下一个处理器
		next.ServeHTTP(recorder, r)

		// 计算耗时
		duration := time.Since(start)

		// 记录响应
		logResponse(recorder, r, duration, requestID)
	})
}

func logRequest(r *http.Request, requestID string) {
	fields := Fields{
		"request_id": requestID,
		"method":     r.Method,
		"path":       r.URL.Path,
		"remote":     r.RemoteAddr,
	}

	// 记录 Query 参数
	if r.URL.RawQuery != "" {
		fields["query"] = r.URL.RawQuery
	}

	// 记录请求体（仅限 POST/PUT/PATCH）
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		if r.Body != nil {
			bodyBytes, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			if len(bodyBytes) > 0 && len(bodyBytes) < 1024 {
				var bodyMap map[string]interface{}
				if err := json.Unmarshal(bodyBytes, &bodyMap); err == nil {
					fields["body"] = bodyMap
				}
			}
		}
	}

	Info("Request received", fields)
}

func logResponse(recorder *ResponseRecorder, r *http.Request, duration time.Duration, requestID string) {
	fields := Fields{
		"request_id":  requestID,
		"method":      r.Method,
		"path":        r.URL.Path,
		"status_code": recorder.statusCode,
		"duration":    duration.String(),
	}

	// 根据状态码选择日志级别
	level := INFO
	if recorder.statusCode >= 400 && recorder.statusCode < 500 {
		level = WARN
	} else if recorder.statusCode >= 500 {
		level = ERROR
		fields["error"] = http.StatusText(recorder.statusCode)
	}

	GetLogger().log(level, "Request completed", fields)
}

// ToolExecutionLogger 工具执行日志器
type ToolExecutionLogger struct {
	agentID string
	tool    string
	start   time.Time
}

// StartToolExecution 开始工具执行日志
func StartToolExecution(agentID, toolName string, args map[string]interface{}) *ToolExecutionLogger {
	logger := NewToolLogger(agentID, toolName)
	logger.Debug("Tool execution started", Fields{"args": args})

	return &ToolExecutionLogger{
		agentID: agentID,
		tool:    toolName,
		start:   time.Now(),
	}
}

// EndToolExecution 结束工具执行日志
func (tel *ToolExecutionLogger) EndToolExecution(result string, err error) {
	duration := time.Since(tel.start)
	logger := NewToolLogger(tel.agentID, tel.tool)

	fields := Fields{
		"duration": duration.String(),
	}

	if err != nil {
		fields["error"] = err.Error()
		logger.Error("Tool execution failed", fields)
	} else {
		// 截断过长的结果
		if len(result) > 200 {
			result = result[:200] + "...(truncated)"
		}
		fields["result"] = result
		logger.Info("Tool execution completed", fields)
	}
}

// AgentLoopLogger Agent 循环日志器
type AgentLoopLogger struct {
	agentID   string
	sessionID string
}

// NewAgentLoopLogger 创建 Agent 循环日志器
func NewAgentLoopLogger(agentID, sessionID string) *AgentLoopLogger {
	return &AgentLoopLogger{
		agentID:   agentID,
		sessionID: sessionID,
	}
}

func (al *AgentLoopLogger) log(level Level, msg string, fields Fields) {
	if fields == nil {
		fields = make(Fields)
	}
	fields["agent_id"] = al.agentID
	fields["session_id"] = al.sessionID
	GetLogger().log(level, msg, fields)
}

func (al *AgentLoopLogger) Info(msg string, fields ...Fields) {
	al.log(INFO, msg, mergeFields(fields))
}

func (al *AgentLoopLogger) Debug(msg string, fields ...Fields) {
	al.log(DEBUG, msg, mergeFields(fields))
}

func (al *AgentLoopLogger) Warn(msg string, fields ...Fields) {
	al.log(WARN, msg, mergeFields(fields))
}

func (al *AgentLoopLogger) Error(msg string, fields ...Fields) {
	al.log(ERROR, msg, mergeFields(fields))
}

// LogAgentStart 记录 Agent 启动
func (al *AgentLoopLogger) LogAgentStart(userMessage string) {
	al.Info("Agent processing message", Fields{
		"message_length": len(userMessage),
	})
}

// LogLLMCall 记录 LLM 调用
func (al *AgentLoopLogger) LogLLMCall(duration time.Duration, tokenCount int, err error) {
	fields := Fields{
		"duration":    duration.String(),
		"token_count": tokenCount,
	}

	if err != nil {
		fields["error"] = err.Error()
		al.Error("LLM call failed", fields)
	} else {
		al.Debug("LLM call completed", fields)
	}
}

// LogToolCall 记录工具调用
func (al *AgentLoopLogger) LogToolCall(toolName string, args map[string]interface{}) {
	al.Debug("Tool called", Fields{
		"tool": toolName,
		"args": args,
	})
}

// LogToolResult 记录工具结果
func (al *AgentLoopLogger) LogToolResult(toolName string, result string, err error, duration time.Duration) {
	fields := Fields{
		"tool":     toolName,
		"duration": duration.String(),
	}

	if err != nil {
		fields["error"] = err.Error()
		al.Error("Tool execution failed", fields)
	} else {
		fields["result_length"] = len(result)
		al.Debug("Tool execution completed", fields)
	}
}

// LogAgentComplete 记录 Agent 完成
func (al *AgentLoopLogger) LogAgentComplete(duration time.Duration, toolCalls int) {
	al.Info("Agent processing completed", Fields{
		"duration":   duration.String(),
		"tool_calls": toolCalls,
	})
}
