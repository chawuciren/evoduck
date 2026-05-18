package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type HTTPCallTool struct {
	client  *http.Client
	decider *proxy.Decider
}

func NewHTTPCallTool(decider *proxy.Decider) *HTTPCallTool {
	client := &http.Client{}
	if decider != nil {
		client = decider.ForTool("http_call", "").HTTPClient
	}
	return &HTTPCallTool{
		client:  client,
		decider: decider,
	}
}

func (t *HTTPCallTool) Name() string {
	return "http_call"
}

func (t *HTTPCallTool) Description() string {
	return "Make a generic HTTP request to any endpoint"
}

func (t *HTTPCallTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"method": map[string]interface{}{
				"type":        "string",
				"description": "HTTP method (GET, POST, PUT, DELETE)",
			},
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to call",
			},
			"headers": map[string]interface{}{
				"type":        "object",
				"description": "Request headers as key-value pairs",
			},
			"body": map[string]interface{}{
				"type":        "string",
				"description": "Request body (JSON string)",
			},
		},
		"required": []string{"method", "url"},
	}
}

// ExecuteWithRole 带角色检查的执行方法
func (t *HTTPCallTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.ExecuteWithContext(ctx, args)
}

func (t *HTTPCallTool) Execute(args map[string]interface{}) (string, error) {
	return t.ExecuteWithContext(context.Background(), args)
}

// ExecuteWithContext 带上下文的执行，支持取消传播
func (t *HTTPCallTool) ExecuteWithContext(ctx context.Context, args map[string]interface{}) (string, error) {
	method, _ := args["method"].(string)
	targetURL, _ := args["url"].(string)
	if method == "" || targetURL == "" {
		return "", fmt.Errorf("method and url are required")
	}

	var bodyReader strings.Reader
	hasBody := false
	if body, ok := args["body"].(string); ok && body != "" {
		bodyReader = *strings.NewReader(body)
		hasBody = true
	}

	var reqBody io.Reader
	if hasBody {
		reqBody = &bodyReader
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, reqBody)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	if headers, ok := args["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if vs, ok := v.(string); ok {
				req.Header.Set(k, vs)
			}
		}
	}

	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}

	// 使用 decider 为目标 URL 决定代理
	client := t.client
	if t.decider != nil {
		decision := t.decider.ForTool("http_call", targetURL)
		client = decision.HTTPClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// 先读取响应体
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	// 构建结果
	result := map[string]interface{}{
		"status_code": resp.StatusCode,
	}

	// 尝试解析JSON，如果失败则返回原始文本
	if len(bodyBytes) > 0 {
		var jsonResult map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &jsonResult); err == nil {
			// 成功解析JSON，合并到结果
			for k, v := range jsonResult {
				result[k] = v
			}
		} else {
			// 不是JSON，返回原始文本
			result["body"] = string(bodyBytes)
		}
	} else {
		result["body"] = ""
	}

	data, _ := json.Marshal(result)
	return string(data), nil
}
