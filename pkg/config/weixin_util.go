package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// AddWeixinChannel 添加或更新微信渠道配置
// 如果 channel 已存在，只更新 token、api_base_url、user_id，保留其他配置
func AddWeixinChannel(configPath, channelID, name, token, role, agent, userID, apiBaseURL string) error {
	resolvedPath, err := ResolveConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	if _, err := EnsureInitialized(resolvedPath); err != nil {
		return fmt.Errorf("initialize config: %w", err)
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	channels, ok := cfg["channels"].(map[string]interface{})
	if !ok {
		channels = make(map[string]interface{})
		cfg["channels"] = channels
	}

	channelConfig, ok := channels[channelID].(map[string]interface{})
	if !ok {
		channelConfig = map[string]interface{}{
			"type": "weixin",
		}
	}

	if token != "" {
		channelConfig["token"] = token
	}
	if apiBaseURL != "" {
		channelConfig["api_base_url"] = apiBaseURL
	}
	if userID != "" {
		channelConfig["user_id"] = userID
	}
	if name != "" {
		channelConfig["name"] = name
	}
	if role != "" {
		channelConfig["role"] = role
	}
	if agent != "" {
		channelConfig["agent"] = agent
	}
	channelConfig["type"] = "weixin"

	channels[channelID] = channelConfig

	outData, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	dir := filepath.Dir(resolvedPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	if err := os.WriteFile(resolvedPath, outData, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

// UpdateWeixinToken 更新微信渠道的 token
func UpdateWeixinToken(configPath, channelID, token string) error {
	resolvedPath, err := ResolveConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	if _, err := EnsureInitialized(resolvedPath); err != nil {
		return fmt.Errorf("initialize config: %w", err)
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	channels, ok := cfg["channels"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("channels not found in config")
	}

	channel, ok := channels[channelID].(map[string]interface{})
	if !ok {
		return fmt.Errorf("channel %s not found", channelID)
	}

	channel["token"] = token
	channels[channelID] = channel

	outData, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(resolvedPath, outData, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}
