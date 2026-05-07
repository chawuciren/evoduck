package config

import "strings"

type ChannelSetupKind string

const (
	ChannelSetupKindBuiltin  ChannelSetupKind = "builtin"
	ChannelSetupKindToken    ChannelSetupKind = "token"
	ChannelSetupKindQRLogin  ChannelSetupKind = "qr-login"
)

type ChannelParam struct {
	Name        string
	Description string
	Required    bool
	Default     string
}

type ChannelCatalogEntry struct {
	Type             string
	Label            string
	Description      string
	SetupKind        ChannelSetupKind
	Required         bool
	RequiredParams   []ChannelParam
	OptionalParams   []ChannelParam
	Aliases          []string
	SupportsFirstRun bool
}

var channelCatalog = []ChannelCatalogEntry{
	{
		Type:        "webchat",
		Label:       "WebChat",
		Description: "Gateway web interface entry used by the built-in web UI. This is not a regular channel bridge and requires no extra setup.",
		SetupKind:   ChannelSetupKindBuiltin,
		Required:    true,
		Aliases:     []string{"1"},
		SupportsFirstRun: true,
	},
	{
		Type:        "weixin",
		Label:       "Weixin",
		Description: "Personal WeChat, scan QR code to login and obtain token",
		SetupKind:   ChannelSetupKindQRLogin,
		Aliases:     []string{"2"},
		SupportsFirstRun: true,
		RequiredParams: []ChannelParam{
			{Name: "token", Description: "Channel token (obtained via QR login)", Required: true},
		},
		OptionalParams: []ChannelParam{
			{Name: "name", Description: "Channel display name", Default: "我的微信"},
			{Name: "role", Description: "Role: admin, employee, customer", Default: "employee"},
			{Name: "agent", Description: "Bound agent ID", Default: "config.default_agent"},
			{Name: "user-id", Description: "User ID for this WeChat account"},
			{Name: "api-base-url", Description: "API base URL (uses default if empty)"},
		},
	},
	{
		Type:        "wecom",
		Label:       "WeCom",
		Description: "WeCom AI Bot (WebSocket long connection, no public IP required)",
		SetupKind:   ChannelSetupKindToken,
		Aliases:     []string{"3", "wecom-bot", "wecom-aibot"},
		SupportsFirstRun: true,
		RequiredParams: []ChannelParam{
			{Name: "bot-id", Description: "AI Bot ID from WeCom admin console", Required: true},
			{Name: "secret", Description: "AI Bot Secret for WebSocket authentication", Required: true},
		},
		OptionalParams: []ChannelParam{
			{Name: "name", Description: "Channel display name", Default: "企业微信机器人"},
			{Name: "role", Description: "Role: admin, employee, customer", Default: "employee"},
			{Name: "agent", Description: "Bound agent ID", Default: "config.default_agent"},
		},
	},
}

func ChannelCatalog() []ChannelCatalogEntry {
	result := make([]ChannelCatalogEntry, len(channelCatalog))
	copy(result, channelCatalog)
	return result
}

func FirstRunChannelCatalog() []ChannelCatalogEntry {
	var result []ChannelCatalogEntry
	for _, entry := range channelCatalog {
		if entry.SupportsFirstRun {
			result = append(result, entry)
		}
	}
	return result
}

func LookupChannelCatalogEntry(channel string) (ChannelCatalogEntry, bool) {
	normalized := strings.ToLower(strings.TrimSpace(channel))
	for _, entry := range channelCatalog {
		if entry.Type == normalized {
			return entry, true
		}
		for _, alias := range entry.Aliases {
			if strings.ToLower(strings.TrimSpace(alias)) == normalized {
				return entry, true
			}
		}
	}
	return ChannelCatalogEntry{}, false
}

func NormalizeChannelName(channel string) string {
	if entry, ok := LookupChannelCatalogEntry(channel); ok {
		return entry.Type
	}
	return ""
}

func OptionalChannelCatalog() []ChannelCatalogEntry {
	var result []ChannelCatalogEntry
	for _, entry := range channelCatalog {
		if !entry.Required {
			result = append(result, entry)
		}
	}
	return result
}

func DescribeSetupKind(kind ChannelSetupKind) string {
	switch kind {
	case ChannelSetupKindBuiltin:
		return "built-in"
	case ChannelSetupKindToken:
		return "token required"
	case ChannelSetupKindQRLogin:
		return "QR login"
	default:
		return "unknown"
	}
}
