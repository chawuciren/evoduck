package weixin

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type APIClient struct {
	config     WeixinConfig
	httpClient *http.Client
	decider    *proxy.Decider
	token      string
	accountID  string
}

func NewAPIClient(config WeixinConfig, decider *proxy.Decider) *APIClient {
	httpClient := http.DefaultClient
	timeout := config.GetUpdatesTimeout + 5*time.Second
	if decider != nil {
		httpClient = decider.ForChannel("weixin").HTTPClient
	}
	// Apply timeout to the client if it's not already configured
	if httpClient.Timeout == 0 {
		httpClient.Timeout = timeout
	}
	return &APIClient{
		config:     config,
		httpClient: httpClient,
		decider:    decider,
	}
}

func (c *APIClient) SetAuth(token, accountID string) {
	c.token = token
	c.accountID = accountID
}

func (c *APIClient) buildHeaders(bodyLen int) (map[string]string, error) {
	uinBytes := make([]byte, 4)
	if _, err := rand.Read(uinBytes); err != nil {
		return nil, fmt.Errorf("generate random uin: %w", err)
	}
	// X-WECHAT-UIN: random uint32 -> decimal string -> base64
	uinBase64 := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", uint32(uinBytes[0])<<24|uint32(uinBytes[1])<<16|uint32(uinBytes[2])<<8|uint32(uinBytes[3]))))

	return map[string]string{
		"Content-Type":            "application/json",
		"AuthorizationType":       "ilink_bot_token",
		"Authorization":           "Bearer " + c.token,
		"X-WECHAT-UIN":            uinBase64,
		"iLink-App-Id":            "bot",
		"iLink-App-ClientVersion": "65547", // 1.0.11
		"Content-Length":          fmt.Sprintf("%d", bodyLen),
	}, nil
}

func (c *APIClient) doRequest(ctx context.Context, endpoint string, reqBody interface{}, respBody interface{}) error {
	// 确保 URL 正确拼接
	baseURL := c.config.APIBaseURL
	if !endsWithSlash(baseURL) {
		baseURL += "/"
	}
	url := baseURL + endpoint

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	headers, err := c.buildHeaders(len(bodyBytes))
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// 打印完整请求信息
	logger.Info("Weixin API request", logger.Fields{
		"url":     url,
		"method":  "POST",
		"body":    string(bodyBytes),
		"headers": fmt.Sprintf("AuthorizationType=%s, iLink-App-Id=%s", headers["AuthorizationType"], headers["iLink-App-Id"]),
	})

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	logger.Info("Weixin API response", logger.Fields{
		"url":      url,
		"status":   resp.StatusCode,
		"response": string(respBytes),
	})

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("api error: status=%d url=%s body=%s", resp.StatusCode, url, string(respBytes))
	}

	if err := json.Unmarshal(respBytes, respBody); err != nil {
		return fmt.Errorf("unmarshal response: %w (body=%s)", err, string(respBytes))
	}

	return nil
}

func endsWithSlash(s string) bool {
	return len(s) > 0 && s[len(s)-1] == '/'
}

func (c *APIClient) GetUpdates(ctx context.Context, buf string) (*GetUpdatesResponse, error) {
	// 正确的 API 路径: ilink/bot/getupdates
	req := map[string]interface{}{
		"get_updates_buf": buf,
		"base_info": map[string]interface{}{
			"channel_version": "1.0.0",
		},
	}
	var resp GetUpdatesResponse

	logger.Debug("Weixin API GetUpdates request", logger.Fields{
		"buf_len": len(buf),
	})

	if err := c.doRequest(ctx, "ilink/bot/getupdates", &req, &resp); err != nil {
		return nil, err
	}

	if resp.Ret != 0 {
		logger.Error("Weixin API GetUpdates failed", logger.Fields{
			"ret":     resp.Ret,
			"errcode": resp.ErrCode,
			"errmsg":  resp.ErrMsg,
		})
		return nil, fmt.Errorf("getupdates failed: ret=%d errcode=%d errmsg=%s", resp.Ret, resp.ErrCode, resp.ErrMsg)
	}

	logger.Debug("Weixin API GetUpdates success", logger.Fields{
		"msg_count": len(resp.Msgs),
		"timeout":   resp.LongPollingTimeoutMS,
	})

	return &resp, nil
}

func (c *APIClient) SendMessage(ctx context.Context, toUserID, contextToken string, items []MessageItem) error {
	// 生成唯一 client_id (evoduck-weixin:timestamp-randomhex)
	randomHex := make([]byte, 4)
	if _, err := rand.Read(randomHex); err != nil {
		return fmt.Errorf("generate client_id: %w", err)
	}
	clientID := fmt.Sprintf("evoduck-weixin:%d-%x", time.Now().UnixMilli(), randomHex)

	req := map[string]interface{}{
		"msg": map[string]interface{}{
			"to_user_id":    toUserID,
			"context_token": contextToken,
			"item_list":     items,
			"message_type":  2, // BOT 发出的消息
			"message_state": 2, // FINISH 状态
			"client_id":     clientID,
		},
		"base_info": map[string]interface{}{
			"channel_version": "1.0.0",
		},
	}
	var resp map[string]interface{}

	// 正确的 API 路径: ilink/bot/sendmessage
	if err := c.doRequest(ctx, "ilink/bot/sendmessage", &req, &resp); err != nil {
		return err
	}

	if ret, ok := resp["ret"].(float64); ok && ret != 0 {
		return fmt.Errorf("sendmessage failed: ret=%d", int(ret))
	}

	logger.Debug("Weixin message sent", logger.Fields{
		"to_user_id": toUserID,
		"client_id":  clientID,
		"items":      len(items),
	})

	return nil
}

func (c *APIClient) GetConfig(ctx context.Context, userID, contextToken string) (*GetConfigResponse, error) {
	req := map[string]interface{}{
		"ilink_user_id": userID,
		"context_token": contextToken,
		"base_info": map[string]interface{}{
			"channel_version": "1.0.0",
		},
	}
	var resp GetConfigResponse

	if err := c.doRequest(ctx, "ilink/bot/getconfig", &req, &resp); err != nil {
		return nil, err
	}

	if resp.Ret != 0 {
		return nil, fmt.Errorf("getconfig failed: ret=%d", resp.Ret)
	}

	return &resp, nil
}

func (c *APIClient) SendTyping(ctx context.Context, userID, ticket string, status int) error {
	req := map[string]interface{}{
		"ilink_user_id": userID,
		"typing_ticket": ticket,
		"status":        status,
		"base_info": map[string]interface{}{
			"channel_version": "1.0.0",
		},
	}
	var resp map[string]interface{}

	if err := c.doRequest(ctx, "ilink/bot/sendtyping", &req, &resp); err != nil {
		return err
	}

	return nil
}

func (c *APIClient) GetUploadURL(ctx context.Context, req *GetUploadURLRequest) (*GetUploadURLResponse, error) {
	var resp GetUploadURLResponse

	if err := c.doRequest(ctx, "ilink/bot/getuploadurl", req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

func EncryptMediaForUpload(plain []byte, aesKey []byte) ([]byte, error) {
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}
	padded := pkcs7Pad(plain, block.BlockSize())
	encrypted := make([]byte, len(padded))
	for start := 0; start < len(padded); start += block.BlockSize() {
		block.Encrypt(encrypted[start:start+block.BlockSize()], padded[start:start+block.BlockSize()])
	}
	return encrypted, nil
}

func EncryptedMediaSize(rawSize int64) int64 {
	const blockSize int64 = aes.BlockSize
	padding := blockSize - (rawSize % blockSize)
	if padding == 0 {
		padding = blockSize
	}
	return rawSize + padding
}

func GenerateMediaAESKey() ([]byte, error) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate media aes key: %w", err)
	}
	return key, nil
}

func EncodeUploadAESKey(aesKey []byte) string {
	return hex.EncodeToString(aesKey)
}

func EncodeMediaAESKey(mediaType int, aesKey []byte) string {
	hexKey := hex.EncodeToString(aesKey)
	return base64.StdEncoding.EncodeToString([]byte(hexKey))
}

func MediaMD5Hex(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

func BuildUploadURL(baseURL, uploadParam, fileKey string) (string, error) {
	trimmed := baseURL
	if trimmed == "" {
		trimmed = "https://novac2c.cdn.weixin.qq.com/c2c"
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse upload base url: %w", err)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/upload"
	query := parsed.Query()
	query.Set("encrypted_query_param", uploadParam)
	query.Set("filekey", fileKey)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (c *APIClient) UploadEncryptedMedia(ctx context.Context, uploadURL string, encrypted []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(encrypted))
	if err != nil {
		return "", fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload encrypted media: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read upload response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upload media failed: status=%d body=%s", resp.StatusCode, string(body))
	}
	encryptedParam := strings.TrimSpace(resp.Header.Get("x-encrypted-param"))
	if encryptedParam == "" {
		return "", fmt.Errorf("upload media missing x-encrypted-param header")
	}
	return encryptedParam, nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	if padding == 0 {
		padding = blockSize
	}
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padText...)
}
