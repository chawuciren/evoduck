package weixin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	ILinkBaseURL        = "https://ilinkai.weixin.qq.com"
	ILinkLiteAppBaseURL = "https://liteapp.weixin.qq.com"
	DefaultBotType      = "3"
)

// QRCodeResponse 二维码响应
type QRCodeResponse struct {
	QRCode           string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content"`
}

// QRCodeStatusResponse 二维码状态响应
type QRCodeStatusResponse struct {
	Status       string `json:"status"` // wait, scaned, confirmed, expired, scaned_but_redirect
	BotToken     string `json:"bot_token,omitempty"`
	ILinkBotID   string `json:"ilink_bot_id,omitempty"`
	ILinkUserID  string `json:"ilink_user_id,omitempty"`
	BaseURL      string `json:"baseurl,omitempty"`
	RedirectHost string `json:"redirect_host,omitempty"`
}

// LoginResult 登录结果
type LoginResult struct {
	Success   bool
	Token     string
	AccountID string
	UserID    string
	BaseURL   string
	Message   string
}

// FetchQRCode 获取登录二维码
func FetchQRCode(ctx context.Context, botType string) (*QRCodeResponse, error) {
	if botType == "" {
		botType = DefaultBotType
	}

	url := fmt.Sprintf("%s/ilink/bot/get_bot_qrcode?bot_type=%s", ILinkBaseURL, botType)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch qrcode: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error: status=%d body=%s", resp.StatusCode, string(body))
	}

	var qrResp QRCodeResponse
	if err := json.Unmarshal(body, &qrResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &qrResp, nil
}

// PollQRStatus 轮询二维码状态
func PollQRStatus(ctx context.Context, qrcode string, pollBaseURL string) (*QRCodeStatusResponse, error) {
	if pollBaseURL == "" {
		pollBaseURL = ILinkBaseURL
	}

	url := fmt.Sprintf("%s/ilink/bot/get_qrcode_status?qrcode=%s", pollBaseURL, qrcode)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	client := &http.Client{Timeout: 40 * time.Second} // 长轮询超时
	resp, err := client.Do(req)
	if err != nil {
		// 超时返回 wait 状态
		return &QRCodeStatusResponse{Status: "wait"}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var statusResp QRCodeStatusResponse
	if err := json.Unmarshal(body, &statusResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &statusResp, nil
}

// WaitForLogin 等待扫码登录
func WaitForLogin(ctx context.Context, qrcodeURL string, qrcode string, onStatus func(status string)) (*LoginResult, error) {
	const maxRefresh = 3
	const timeout = 8 * time.Minute

	deadline := time.Now().Add(timeout)
	refreshCount := 0
	currentBaseURL := ILinkBaseURL
	scanned := false

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return &LoginResult{Success: false, Message: "用户取消"}, nil
		default:
		}

		statusResp, err := PollQRStatus(ctx, qrcode, currentBaseURL)
		if err != nil {
			return &LoginResult{Success: false, Message: fmt.Sprintf("轮询状态失败: %v", err)}, nil
		}

		switch statusResp.Status {
		case "wait":
			if onStatus != nil {
				onStatus("wait")
			}

		case "scaned":
			if !scanned {
				scanned = true
				if onStatus != nil {
					onStatus("scaned")
				}
			}

		case "scaned_but_redirect":
			if statusResp.RedirectHost != "" {
				currentBaseURL = fmt.Sprintf("https://%s", statusResp.RedirectHost)
			}

		case "expired":
			refreshCount++
			if refreshCount > maxRefresh {
				return &LoginResult{Success: false, Message: "二维码多次过期，请重新登录"}, nil
			}

			if onStatus != nil {
				onStatus("expired")
			}

			// 重新获取二维码
			newQR, err := FetchQRCode(ctx, DefaultBotType)
			if err != nil {
				return &LoginResult{Success: false, Message: fmt.Sprintf("刷新二维码失败: %v", err)}, nil
			}

			qrcode = newQR.QRCode
			qrcodeURL = newQR.QRCodeImgContent
			scanned = false

			if onStatus != nil {
				onStatus("refreshed:" + qrcodeURL)
			}

		case "confirmed":
			if statusResp.ILinkBotID == "" {
				return &LoginResult{Success: false, Message: "登录失败：服务器未返回账号ID"}, nil
			}

			return &LoginResult{
				Success:   true,
				Token:     statusResp.BotToken,
				AccountID: statusResp.ILinkBotID,
				UserID:    statusResp.ILinkUserID,
				BaseURL:   statusResp.BaseURL,
				Message:   "登录成功",
			}, nil
		}

		time.Sleep(1 * time.Second)
	}

	return &LoginResult{Success: false, Message: "登录超时"}, nil
}
