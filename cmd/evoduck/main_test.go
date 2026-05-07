package main

import (
	"bufio"
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/chawuciren/evoduck/internal/channels/weixin"
	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/spf13/cobra"
)

func TestBrandHeaderIncludesDuckLogoAndVersion(t *testing.T) {
	header := brandHeader("1.2.3")
	for _, want := range []string{"████████╗██╗", "AI Agent Gateway | v1.2.3", "██████████████████████ ██▓▓"} {
		if !strings.Contains(header, want) {
			t.Fatalf("expected brand header to contain %q", want)
		}
	}
}

func TestBrandVersionKeepsExistingPrefix(t *testing.T) {
	if got := brandVersion("v1.2.3"); got != "v1.2.3" {
		t.Fatalf("expected existing v prefix to be preserved, got %q", got)
	}
}

func TestPrintBrandHeaderOnlyPrintsOnce(t *testing.T) {
	printBrandHeaderOnce = sync.Once{}

	var buf bytes.Buffer
	printBrandHeader(&buf, "1.2.3")
	printBrandHeader(&buf, "1.2.3")

	if got := strings.Count(buf.String(), "AI Agent Gateway"); got != 1 {
		t.Fatalf("expected brand header to print once, got %d", got)
	}
}

func TestShouldPrintBrandHeaderCanBeDisabledByEnv(t *testing.T) {
	t.Setenv("EVODUCK_NO_BRAND_HEADER", "1")
	cmd := &cobra.Command{Use: "evoduck"}
	if shouldPrintBrandHeader(cmd) {
		t.Fatal("expected brand header to be disabled by EVODUCK_NO_BRAND_HEADER")
	}
}

func TestShouldPrintBrandHeaderSkipsServiceRun(t *testing.T) {
	t.Setenv("EVODUCK_NO_BRAND_HEADER", "")
	root := &cobra.Command{Use: "evoduck"}
	serviceRun := &cobra.Command{Use: "service-run"}
	root.AddCommand(serviceRun)
	if shouldPrintBrandHeader(serviceRun) {
		t.Fatal("expected service-run to skip brand header")
	}
}

func TestShouldPrintBrandHeaderSkipsAnnotation(t *testing.T) {
	t.Setenv("EVODUCK_NO_BRAND_HEADER", "")
	cmd := &cobra.Command{
		Use:         "completion",
		Annotations: map[string]string{noBrandHeaderAnnotation: "true"},
	}
	if shouldPrintBrandHeader(cmd) {
		t.Fatal("expected annotated command to skip brand header")
	}
}

func TestMain(m *testing.M) {
	printBrandHeaderOnce = sync.Once{}
	os.Exit(m.Run())
}

func TestResolveProviderChoiceUsesFirstRunDefault(t *testing.T) {
	got, err := resolveProviderChoice("")
	if err != nil {
		t.Fatalf("resolveProviderChoice returned error: %v", err)
	}
	if got != "openai-compatible" {
		t.Fatalf("expected openai-compatible for empty input, got %q", got)
	}
}

func TestResolveProviderChoiceSupportsNumericAlias(t *testing.T) {
	got, err := resolveProviderChoice("1")
	if err != nil {
		t.Fatalf("resolveProviderChoice returned error: %v", err)
	}
	if got != "openai-compatible" {
		t.Fatalf("expected openai-compatible for alias 1, got %q", got)
	}
}

func TestResolveProviderChoiceRejectsUnknownInput(t *testing.T) {
	if _, err := resolveProviderChoice("not-a-provider"); err == nil {
		t.Fatal("expected unknown provider to return error")
	}
}

func TestResolveFirstRunDefaultModelPrefersConfiguredModel(t *testing.T) {
	models := []llm.ProviderModel{{ID: "gpt-4o-mini"}, {ID: "gpt-4o"}}
	got := resolveFirstRunDefaultModel(config.SetupOptions{Model: "gpt-4o"}, models)
	if got != "gpt-4o" {
		t.Fatalf("expected configured default model, got %q", got)
	}
}

func TestResolveFirstRunDefaultModelFallsBackToFirstListedModel(t *testing.T) {
	models := []llm.ProviderModel{{ID: "gpt-4o-mini"}, {ID: "gpt-4o"}}
	got := resolveFirstRunDefaultModel(config.SetupOptions{Model: "missing"}, models)
	if got != "gpt-4o-mini" {
		t.Fatalf("expected first listed model, got %q", got)
	}
}

func TestResolveFirstRunModelChoiceUsesDefaultForEmptyInput(t *testing.T) {
	models := []llm.ProviderModel{{ID: "gpt-4o-mini"}, {ID: "gpt-4o"}}
	got, err := resolveFirstRunModelChoice("", "gpt-4o", models)
	if err != nil {
		t.Fatalf("resolveFirstRunModelChoice returned error: %v", err)
	}
	if got != "gpt-4o" {
		t.Fatalf("expected default model, got %q", got)
	}
}

func TestResolveFirstRunModelChoiceUsesNumericSelection(t *testing.T) {
	models := []llm.ProviderModel{{ID: "gpt-4o-mini"}, {ID: "gpt-4o"}}
	got, err := resolveFirstRunModelChoice("2", "gpt-4o-mini", models)
	if err != nil {
		t.Fatalf("resolveFirstRunModelChoice returned error: %v", err)
	}
	if got != "gpt-4o" {
		t.Fatalf("expected second listed model, got %q", got)
	}
}

func TestResolveFirstRunModelChoiceRejectsUnknownModelWhenListExists(t *testing.T) {
	models := []llm.ProviderModel{{ID: "gpt-4o-mini"}, {ID: "gpt-4o"}}
	if _, err := resolveFirstRunModelChoice("custom-model", "gpt-4o-mini", models); err == nil {
		t.Fatal("expected custom model to be rejected when model list exists")
	}
}

func TestResolveFirstRunModelChoiceAcceptsListedModelID(t *testing.T) {
	models := []llm.ProviderModel{{ID: "gpt-4o-mini"}, {ID: "gpt-4o"}}
	got, err := resolveFirstRunModelChoice("gpt-4o", "gpt-4o-mini", models)
	if err != nil {
		t.Fatalf("resolveFirstRunModelChoice returned error: %v", err)
	}
	if got != "gpt-4o" {
		t.Fatalf("expected listed model id, got %q", got)
	}
}

func TestValidateBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "https url", value: "https://api.openai.com/v1"},
		{name: "http url", value: "http://127.0.0.1:11434/v1"},
		{name: "missing scheme", value: "api.openai.com/v1", wantErr: true},
		{name: "unsupported scheme", value: "ftp://api.openai.com/v1", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBaseURL(tt.value)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateHost(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "localhost", value: "localhost"},
		{name: "ipv4", value: "127.0.0.1"},
		{name: "hostname", value: "api.internal"},
		{name: "empty", value: "", wantErr: true},
		{name: "contains space", value: "bad host", wantErr: true},
		{name: "contains scheme", value: "http://127.0.0.1", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHost(tt.value)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPromptProviderChoiceRetriesUntilValid(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("not-a-provider\n1\n"))

	got, err := promptProviderChoice(reader)
	if err != nil {
		t.Fatalf("promptProviderChoice returned error: %v", err)
	}
	if got != "openai-compatible" {
		t.Fatalf("expected openai-compatible after retry, got %q", got)
	}
}

func TestBuildFirstRunProviderConfigAppliesPresetHeaders(t *testing.T) {
	provider, providerCfg := buildFirstRunProviderConfig(config.SetupOptions{Provider: "portkey", APIKey: "test-key"})
	if provider != "portkey" {
		t.Fatalf("expected provider portkey, got %q", provider)
	}
	if providerCfg.Type != "portkey" {
		t.Fatalf("expected provider type portkey, got %q", providerCfg.Type)
	}
	if providerCfg.BaseURL != "https://api.portkey.ai/v1" {
		t.Fatalf("expected preset base url, got %q", providerCfg.BaseURL)
	}
	if providerCfg.DefaultModel != "openai/gpt-4o-mini" {
		t.Fatalf("expected preset default model, got %q", providerCfg.DefaultModel)
	}
	entry, ok := config.LookupProviderCatalogEntry("portkey")
	if !ok {
		t.Fatal("expected portkey catalog entry")
	}
	if len(providerCfg.Models) != len(entry.Models) {
		t.Fatalf("expected full catalog models, got %d want %d", len(providerCfg.Models), len(entry.Models))
	}
}

func TestBuildFirstRunProviderConfigKeepsFullCatalogWhenDefaultModelChanges(t *testing.T) {
	provider, providerCfg := buildFirstRunProviderConfig(config.SetupOptions{Provider: "openai", Model: "o3"})
	if provider != "openai" {
		t.Fatalf("expected provider openai, got %q", provider)
	}
	if providerCfg.DefaultModel != "o3" {
		t.Fatalf("expected overridden default model, got %q", providerCfg.DefaultModel)
	}
	entry, ok := config.LookupProviderCatalogEntry("openai")
	if !ok {
		t.Fatal("expected openai catalog entry")
	}
	if len(providerCfg.Models) != len(entry.Models) {
		t.Fatalf("expected full catalog models, got %d want %d", len(providerCfg.Models), len(entry.Models))
	}
	found := false
	for _, model := range providerCfg.Models {
		if model.ID == "gpt-4o" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected non-default catalog models to remain present")
	}
}

func TestPromptChannelDetailsUsesQRLoginFlowForWeixin(t *testing.T) {
	original := weixinQRLoginRunner
	t.Cleanup(func() {
		weixinQRLoginRunner = original
	})

	called := false
	weixinQRLoginRunner = func(channelID, name, role, agent, userID string) (*weixin.LoginResult, error) {
		called = true
		if channelID != "" || name != "" || role != "" || agent != "" || userID != "" {
			t.Fatalf("expected setup wizard QR flow to use no manual fields, got channelID=%q name=%q role=%q agent=%q userID=%q", channelID, name, role, agent, userID)
		}
		return &weixin.LoginResult{
			Success:   true,
			Token:     "test-token",
			AccountID: "bot_123",
			UserID:    "wx-user",
			BaseURL:   "https://wx.example",
		}, nil
	}

	reader := bufio.NewReader(strings.NewReader(""))
	result, err := promptChannelDetails(reader, config.ChannelCatalogEntry{
		Type:      "weixin",
		Label:     "Weixin",
		SetupKind: config.ChannelSetupKindQRLogin,
	})
	if err != nil {
		t.Fatalf("promptChannelDetails returned error: %v", err)
	}
	if !called {
		t.Fatal("expected QR login runner to be called")
	}
	if len(result) != 1 {
		t.Fatalf("expected one channel result, got %d", len(result))
	}
	if result[0].ChannelID != "weixin-bot-123" {
		t.Fatalf("expected generated channel id, got %q", result[0].ChannelID)
	}
	if result[0].Name != "微信 wx-user" {
		t.Fatalf("expected generated channel name, got %q", result[0].Name)
	}
	if result[0].Token != "test-token" {
		t.Fatalf("expected token from QR login flow, got %q", result[0].Token)
	}
	if result[0].UserID != "wx-user" {
		t.Fatalf("expected user id from QR login flow, got %q", result[0].UserID)
	}
	if result[0].APIBaseURL != "https://wx.example" {
		t.Fatalf("expected api base url from QR login flow, got %q", result[0].APIBaseURL)
	}
}

func TestDefaultWeixinChannelID(t *testing.T) {
	if got := defaultWeixinChannelID("bot_123"); got != "weixin-bot-123" {
		t.Fatalf("expected sanitized weixin channel id, got %q", got)
	}
	if got := defaultWeixinChannelID("  "); got != "weixin-default" {
		t.Fatalf("expected default weixin channel id fallback, got %q", got)
	}
}

func TestDefaultWecomChannelID(t *testing.T) {
	if got := defaultWecomChannelID("Sales Team"); got != "wecom-sales-team" {
		t.Fatalf("expected sanitized wecom channel id, got %q", got)
	}
	if got := defaultWecomChannelID("  "); got != "wecom-default" {
		t.Fatalf("expected default wecom channel id fallback, got %q", got)
	}
}

func TestPromptChannelDetailsUsesTokenFlowForWecom(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("wecom-sales\n销售企业微信\nemployee\n\nbot-123\nsecret-abc\n\n"))
	result, err := promptChannelDetails(reader, config.ChannelCatalogEntry{
		Type:           "wecom",
		Label:          "WeCom",
		SetupKind:      config.ChannelSetupKindToken,
		RequiredParams: []config.ChannelParam{{Name: "bot-id", Description: "Bot ID"}, {Name: "secret", Description: "Secret"}},
		OptionalParams: []config.ChannelParam{
			{Name: "name", Description: "Channel display name", Default: "企业微信机器人"},
			{Name: "role", Description: "Role", Default: "employee"},
			{Name: "agent", Description: "Bound agent ID"},
		},
	})
	if err != nil {
		t.Fatalf("promptChannelDetails returned error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected one channel result, got %d", len(result))
	}
	if result[0].Type != "wecom" || result[0].ChannelID != "wecom-sales" {
		t.Fatalf("unexpected channel setup option: %+v", result[0])
	}
	if result[0].BotID != "bot-123" || result[0].Secret != "secret-abc" {
		t.Fatalf("expected wecom bot_id and secret to be captured, got %+v", result[0])
	}
}

func TestChannelAddRejectsIncompleteWecomConfig(t *testing.T) {
	originalCfgFile := cfgFile
	cfgFile = t.TempDir() + "/config.yaml"
	t.Cleanup(func() {
		cfgFile = originalCfgFile
	})

	originalFactory := channelAddReaderFactory
	channelAddReaderFactory = func() *bufio.Reader {
		return bufio.NewReader(strings.NewReader(""))
	}
	t.Cleanup(func() {
		channelAddReaderFactory = originalFactory
	})

	err := channelAdd(channelAddOptions{Type: "wecom", ChannelID: "wecom-sales"})
	if err == nil {
		t.Fatal("expected incomplete wecom config to fail")
	}
}

func TestChannelAddGeneratesDefaultWecomChannelID(t *testing.T) {
	originalCfgFile := cfgFile
	cfgFile = t.TempDir() + "/config.yaml"
	t.Cleanup(func() {
		cfgFile = originalCfgFile
	})

	originalFactory := channelAddReaderFactory
	channelAddReaderFactory = func() *bufio.Reader {
		return bufio.NewReader(strings.NewReader(""))
	}
	t.Cleanup(func() {
		channelAddReaderFactory = originalFactory
	})

	err := channelAdd(channelAddOptions{Type: "wecom", Name: "Sales Team"})
	if err == nil {
		t.Fatal("expected incomplete wecom config to fail")
	}
}

func TestChannelAddPromptsForMissingWecomFields(t *testing.T) {
	originalCfgFile := cfgFile
	cfgFile = t.TempDir() + "/config.yaml"
	t.Cleanup(func() {
		cfgFile = originalCfgFile
	})

	originalFactory := channelAddReaderFactory
	channelAddReaderFactory = func() *bufio.Reader {
		return bufio.NewReader(strings.NewReader("Sales Team\nemployee\n\nbot-123\nsecret-abc\n\n"))
	}
	t.Cleanup(func() {
		channelAddReaderFactory = originalFactory
	})

	err := channelAdd(channelAddOptions{Type: "wecom"})
	if err != nil {
		t.Fatalf("expected interactive completion to satisfy required fields, got %v", err)
	}
}

func TestChannelAddWeixinWithToken(t *testing.T) {
	originalCfgFile := cfgFile
	cfgFile = t.TempDir() + "/config.yaml"
	t.Cleanup(func() {
		cfgFile = originalCfgFile
	})

	originalFactory := channelAddReaderFactory
	channelAddReaderFactory = func() *bufio.Reader {
		return bufio.NewReader(strings.NewReader("My WeChat\n"))
	}
	t.Cleanup(func() {
		channelAddReaderFactory = originalFactory
	})

	err := channelAdd(channelAddOptions{Type: "weixin", Token: "wx-token", UserID: "wx-user"})
	if err != nil {
		t.Fatalf("expected weixin with token to succeed, got %v", err)
	}
}
