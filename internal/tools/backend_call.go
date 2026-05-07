package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

// BackendCallTool 调用企业后台 API，自带 RBAC 鉴权
type BackendCallTool struct {
	client    *http.Client
	decider   *proxy.Decider
	endpoints map[string]*BackendEndpoint
}

type BackendEndpoint struct {
	Name         string
	URL          string
	Method       string
	Auth         *BackendAuth
	AllowedRoles []models.Role
	RateLimit    int
	Timeout      time.Duration
}

type BackendAuth struct {
	Type   string // "bearer" | "api_key" | "basic"
	Token  string
	Header string // custom header name (for api_key type)
	User   string // for basic auth
	Pass   string // for basic auth
}

type BackendCallParams struct {
	Endpoint    string            `json:"endpoint"`
	PathParams  map[string]string `json:"path_params"`  // 替换 URL 中的 {param}
	QueryParams map[string]string `json:"query_params"` // 添加 query 参数
	Headers     map[string]string `json:"headers"`      // 额外的 headers
	Body        interface{}       `json:"body"`         // request body
}

func NewBackendCallTool(cfg config.BackendCallConfig, decider *proxy.Decider) *BackendCallTool {
	endpoints := make(map[string]*BackendEndpoint)

	for name, epCfg := range cfg.Endpoints {
		timeout := epCfg.Timeout
		if timeout == 0 {
			timeout = 30 * time.Second
		}

		auth := &BackendAuth{
			Type:   epCfg.Auth.Type,
			Token:  epCfg.Auth.Token,
			Header: epCfg.Auth.Header,
		}

		allowedRoles := make([]models.Role, len(epCfg.AllowedRoles))
		for i, r := range epCfg.AllowedRoles {
			allowedRoles[i] = models.Role(r)
		}

		endpoints[name] = &BackendEndpoint{
			Name:         name,
			URL:          epCfg.URL,
			Method:       epCfg.Method,
			Auth:         auth,
			AllowedRoles: allowedRoles,
			RateLimit:    epCfg.RateLimit,
			Timeout:      timeout,
		}
	}

	client := &http.Client{Timeout: 60 * time.Second}
	if decider != nil {
		client = decider.ForTool("backend_call", "").HTTPClient
	}

	return &BackendCallTool{
		client:    client,
		decider:   decider,
		endpoints: endpoints,
	}
}

func (t *BackendCallTool) Name() string {
	return "backend_call"
}

func (t *BackendCallTool) Description() string {
	return `Call enterprise backend APIs with automatic authentication.

This tool is used to call internal company APIs. It automatically injects 
authentication tokens based on endpoint configuration. Some endpoints may 
require specific roles to access.

Available endpoints can be found in the configuration file.`
}

func (t *BackendCallTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"endpoint": map[string]interface{}{
				"type":        "string",
				"description": "The endpoint name to call (e.g., 'order-query', 'user-info')",
			},
			"path_params": map[string]interface{}{
				"type":        "object",
				"description": "Parameters to replace in URL path (e.g., {id} -> '12345')",
			},
			"query_params": map[string]interface{}{
				"type":        "object",
				"description": "Query parameters to append to URL",
			},
			"headers": map[string]interface{}{
				"type":        "object",
				"description": "Additional headers (will be merged with auth headers)",
			},
			"body": map[string]interface{}{
				"type":        "object",
				"description": "Request body for POST/PUT requests",
			},
		},
		"required": []string{"endpoint"},
	}
}

// ExecuteWithRole 带角色检查的执行方法
func (t *BackendCallTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, callerRole models.Role) (string, error) {
	params, err := parseBackendCallParams(args)
	if err != nil {
		return "", err
	}

	endpoint, ok := t.endpoints[params.Endpoint]
	if !ok {
		return "", fmt.Errorf("endpoint '%s' not found", params.Endpoint)
	}

	// RBAC 检查
	if !t.checkRole(endpoint, callerRole) {
		return "", fmt.Errorf("access denied: endpoint '%s' requires one of roles %v, caller role is '%s'",
			endpoint.Name, endpoint.AllowedRoles, callerRole)
	}

	// 构建 URL
	url := t.buildURL(endpoint.URL, params)

	// 构建请求
	req, err := t.buildRequest(ctx, endpoint, url, params)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	// 注入鉴权
	t.injectAuth(req, endpoint.Auth)

	// 执行请求 - 使用 decider 为目标 URL 决定代理
	client := t.client
	if t.decider != nil {
		decision := t.decider.ForTool("backend_call", url)
		client = decision.HTTPClient
	}
	// 应用 endpoint 的 timeout
	clientWithTimeout := &http.Client{
		Transport: client.Transport,
		Timeout:   endpoint.Timeout,
	}
	resp, err := clientWithTimeout.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// 解析响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	// 构建结果
	result := map[string]interface{}{
		"status_code":   resp.StatusCode,
		"endpoint":      endpoint.Name,
		"response_body": parseResponseBody(body),
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

// Execute 默认执行方法（无角色检查，用于 backward compatibility）
func (t *BackendCallTool) Execute(args map[string]interface{}) (string, error) {
	return t.ExecuteWithRole(context.Background(), args, models.RoleEmployee)
}

func parseBackendCallParams(args map[string]interface{}) (*BackendCallParams, error) {
	params := &BackendCallParams{}

	if endpoint, ok := args["endpoint"].(string); ok {
		params.Endpoint = endpoint
	} else {
		return nil, fmt.Errorf("endpoint is required and must be string")
	}

	if pathParams, ok := args["path_params"].(map[string]interface{}); ok {
		params.PathParams = make(map[string]string)
		for k, v := range pathParams {
			if vs, ok := v.(string); ok {
				params.PathParams[k] = vs
			} else {
				params.PathParams[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	if queryParams, ok := args["query_params"].(map[string]interface{}); ok {
		params.QueryParams = make(map[string]string)
		for k, v := range queryParams {
			if vs, ok := v.(string); ok {
				params.QueryParams[k] = vs
			} else {
				params.QueryParams[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	if headers, ok := args["headers"].(map[string]interface{}); ok {
		params.Headers = make(map[string]string)
		for k, v := range headers {
			if vs, ok := v.(string); ok {
				params.Headers[k] = vs
			}
		}
	}

	params.Body = args["body"]
	return params, nil
}

func (t *BackendCallTool) checkRole(endpoint *BackendEndpoint, role models.Role) bool {
	// admin 角色拥有所有权限
	if role == models.RoleAdmin {
		return true
	}

	if len(endpoint.AllowedRoles) == 0 {
		return true // 没有配置角色限制，允许所有
	}

	for _, allowed := range endpoint.AllowedRoles {
		if role == allowed {
			return true
		}
	}
	return false
}

func (t *BackendCallTool) buildURL(template string, params *BackendCallParams) string {
	url := template

	// 替换 path 参数
	for k, v := range params.PathParams {
		url = strings.ReplaceAll(url, "{"+k+"}", v)
	}

	// 添加 query 参数
	if len(params.QueryParams) > 0 {
		queryParts := make([]string, 0, len(params.QueryParams))
		for k, v := range params.QueryParams {
			queryParts = append(queryParts, fmt.Sprintf("%s=%s", k, v))
		}
		if strings.Contains(url, "?") {
			url = url + "&" + strings.Join(queryParts, "&")
		} else {
			url = url + "?" + strings.Join(queryParts, "&")
		}
	}

	return url
}

func (t *BackendCallTool) buildRequest(ctx context.Context, endpoint *BackendEndpoint, url string, params *BackendCallParams) (*http.Request, error) {
	method := endpoint.Method
	if method == "" {
		method = "GET"
	}

	var bodyReader io.Reader
	if params.Body != nil && (method == "POST" || method == "PUT" || method == "PATCH") {
		bodyData, err := json.Marshal(params.Body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyData)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	// 添加额外的 headers
	for k, v := range params.Headers {
		req.Header.Set(k, v)
	}

	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

func (t *BackendCallTool) injectAuth(req *http.Request, auth *BackendAuth) {
	if auth == nil || auth.Token == "" {
		return
	}

	switch auth.Type {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	case "api_key":
		header := auth.Header
		if header == "" {
			header = "X-API-Key"
		}
		req.Header.Set(header, auth.Token)
	case "basic":
		if auth.User != "" && auth.Pass != "" {
			req.SetBasicAuth(auth.User, auth.Pass)
		}
	}
}

func parseResponseBody(body []byte) interface{} {
	// 尝试解析为 JSON
	var jsonObj interface{}
	if err := json.Unmarshal(body, &jsonObj); err == nil {
		return jsonObj
	}

	// 如果不是 JSON，返回字符串
	str := string(body)
	if len(str) > 500 {
		str = str[:500] + "...(truncated)"
	}
	return str
}

// ListEndpoints 返回可用的 endpoint 列表（用于调试）
func (t *BackendCallTool) ListEndpoints() []string {
	var names []string
	for name := range t.endpoints {
		names = append(names, name)
	}
	return names
}
