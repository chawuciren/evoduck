package weixin

import "time"

type WeixinConfig struct {
	APIBaseURL        string        `json:"api_base_url"`
	Token             string        `json:"token"`
	AccountID         string        `json:"account_id"`
	GetUpdatesTimeout time.Duration `json:"get_updates_timeout"`
	SendTimeout       time.Duration `json:"send_timeout"`
}

// 默认 API 基础 URL
const DefaultAPIBaseURL = "https://ilinkai.weixin.qq.com"

func DefaultConfig() WeixinConfig {
	return WeixinConfig{
		APIBaseURL:        DefaultAPIBaseURL,
		GetUpdatesTimeout: 35 * time.Second,
		SendTimeout:       10 * time.Second,
	}
}
