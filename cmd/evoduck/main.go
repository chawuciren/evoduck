package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/chawuciren/evoduck/internal/agent"
	"github.com/chawuciren/evoduck/internal/channels/weixin"
	"github.com/chawuciren/evoduck/internal/daemon"
	"github.com/chawuciren/evoduck/internal/gateway"
	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/plugin"
	"github.com/chawuciren/evoduck/internal/skill"
	selfupdate "github.com/chawuciren/evoduck/internal/update"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/proxy"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var cfgFile string

// Build-time version info injected via ldflags
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

var weixinQRLoginRunner = runWeixinQRLoginFlow
var channelAddReaderFactory = func() *bufio.Reader { return bufio.NewReader(os.Stdin) }

const noBrandHeaderAnnotation = "evoduck:no-brand-header"

var printBrandHeaderOnce sync.Once

func printBrandHeader(w io.Writer, version string) {
	printBrandHeaderOnce.Do(func() {
		fmt.Fprint(w, brandHeader(version))
	})
}

func brandHeader(version string) string {
	return fmt.Sprintf(`
                    ░░░░
                ██████████░░
              ██████████████░
            ████  ██  ████████░
            ████  ██  ██████████  ██░░
        ░░██████████████████████ ██▓▓
      ░░██████████████▓▓██████▓▓
      ███████████████▓▓▓▓██████░
      ████████████▓▓▓▓▓▓████░░
        ████████▓▓▓▓▓▓██░░
          ░░██      ██
            ██      ██

████████╗██╗   ██╗ ██████╗ ██████╗ ██╗   ██╗ ██████╗██╗  ██╗
██╔════╝██║   ██║██╔═══██╗██╔══██╗██║   ██║██╔════╝██║ ██╔╝
█████╗  ██║   ██║██║   ██║██║  ██║██║   ██║██║     █████╔╝
██╔══╝  ╚██╗ ██╔╝██║   ██║██║  ██║██║   ██║██║     ██╔═██╗
███████╗ ╚████╔╝ ╚██████╔╝██████╔╝╚██████╔╝╚██████╗██║  ██╗
╚══════╝  ╚═══╝   ╚═════╝ ╚═════╝  ╚═════╝  ╚═════╝╚═╝  ╚═╝
 ░░░░░░    ░░░     ░░░░░   ░░░░░    ░░░░░    ░░░░░  ░░   ░░

AI Agent Gateway | %s

`, brandVersion(version))
}

func brandVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "vdev"
	}
	if strings.HasPrefix(strings.ToLower(version), "v") {
		return version
	}
	return "v" + version
}

func shouldPrintBrandHeader(cmd *cobra.Command) bool {
	if brandHeaderDisabled() {
		return false
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.CommandPath() == "evoduck service-run" {
			return false
		}
		if c.Annotations != nil && c.Annotations[noBrandHeaderAnnotation] == "true" {
			return false
		}
	}
	return true
}

func brandHeaderDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("EVODUCK_NO_BRAND_HEADER"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func maybePrintBrandHeader(cmd *cobra.Command, _ []string) {
	if shouldPrintBrandHeader(cmd) {
		printBrandHeader(cmd.OutOrStdout(), Version)
	}
}

func main() {
	// 初始化日志系统（优先使用环境变量）
	initLoggerFromEnv()

	var rootCmd = &cobra.Command{
		Use:              "evoduck",
		Short:            "EvoDuck - Enterprise AI Agent Framework",
		Long:             "EvoDuck is a high-performance AI Agent framework written in Go.",
		PersistentPreRun: maybePrintBrandHeader,
	}
	originalHelpFunc := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		maybePrintBrandHeader(cmd, args)
		originalHelpFunc(cmd, args)
	})

	var runCmd = &cobra.Command{
		Use:   "run",
		Short: "Run EvoDuck in foreground mode (development)",
		Long:  "Run EvoDuck directly in foreground without daemon supervision. Ideal for development and debugging.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGatewayForeground()
		},
	}

	var setupCmd = &cobra.Command{
		Use:   "setup",
		Short: "Run first-time interactive setup",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFirstTimeSetup(false)
		},
	}

	var versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Show EvoDuck version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("evoduck %s\n", Version)
			fmt.Printf("commit: %s\n", Commit)
			fmt.Printf("build_time: %s\n", BuildTime)
		},
	}

	var updateVersion string
	var updateInstallDir string
	var updateRepo string
	var updateCheck bool
	var updateForce bool
	var updateCmd = &cobra.Command{
		Use:   "update",
		Short: "Update the installed EvoDuck binary",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd.Context(), updateOptions{
				Repo:       updateRepo,
				Version:    updateVersion,
				InstallDir: updateInstallDir,
				CheckOnly:  updateCheck,
				Force:      updateForce,
			})
		},
	}
	updateCmd.Flags().StringVar(&updateVersion, "version", "", "Version to install, defaults to latest or EVODUCK_VERSION")
	updateCmd.Flags().BoolVar(&updateCheck, "check", false, "Check for a newer release without installing")
	updateCmd.Flags().BoolVar(&updateForce, "force", false, "Reinstall even when the target version matches")
	updateCmd.Flags().StringVar(&updateInstallDir, "install-dir", "", "Install directory, defaults to ~/.local/bin or EVODUCK_INSTALL_DIR")
	updateCmd.Flags().StringVar(&updateRepo, "repo", "", "GitHub repository, defaults to chawuciren/evoduck or EVODUCK_REPO")

	var serviceCmd = &cobra.Command{
		Use:   "service",
		Short: "Manage EvoDuck as a system service",
	}

	var daemonModeCmd = &cobra.Command{
		Use:         "daemon-mode",
		Short:       "Run as daemon supervisor process (internal)",
		Hidden:      true,
		Annotations: map[string]string{noBrandHeaderAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonMode()
		},
	}

	var workerModeCmd = &cobra.Command{
		Use:         "worker-mode",
		Short:       "Run as worker process (internal)",
		Hidden:      true,
		Annotations: map[string]string{noBrandHeaderAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkerMode()
		},
	}

	var installCmd = &cobra.Command{
		Use:   "install",
		Short: "Install EvoDuck autostart configuration",
		Long:  "Configure EvoDuck to start automatically on system boot. No admin privileges required.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return autostartInstall()
		},
	}

	var startCmd = &cobra.Command{
		Use:   "start",
		Short: "Start EvoDuck daemon process",
		Long:  "Start the daemon supervisor which manages the worker process in background.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return daemonStart()
		},
	}

	var stopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop EvoDuck daemon process",
		Long:  "Stop the daemon supervisor and worker process.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return daemonStop()
		},
	}

	var restartCmd = &cobra.Command{
		Use:   "restart",
		Short: "Restart EvoDuck worker process",
		Long:  "Restart the worker process while keeping the daemon supervisor running.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return daemonRestart()
		},
	}

	var statusCmd = &cobra.Command{
		Use:   "status",
		Short: "Show EvoDuck process status",
		Long:  "Display the status of daemon and worker processes including PIDs.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return daemonStatus()
		},
	}

	var uninstallCmd = &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall EvoDuck autostart configuration",
		Long:  "Remove the autostart configuration. Does not stop running processes.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return autostartUninstall()
		},
	}

	var channelCmd = &cobra.Command{
		Use:   "channel",
		Short: "Manage channel configuration",
	}

	var skillsCmd = &cobra.Command{
		Use:   "skills",
		Short: "Manage EvoDuck skills",
	}

	var channelTypesCmd = &cobra.Command{
		Use:   "types",
		Short: "List all available channel types",
		RunE: func(cmd *cobra.Command, args []string) error {
			return channelTypes()
		},
	}

	var channelInfoCmd = &cobra.Command{
		Use:   "info <type>",
		Short: "Show detailed info for a channel type",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return channelInfo(args[0])
		},
	}

	var channelListCmd = &cobra.Command{
		Use:   "list",
		Short: "List configured channels",
		RunE: func(cmd *cobra.Command, args []string) error {
			return channelList()
		},
	}

	var addChannelID string
	var addName string
	var addRole string
	var addAgent string
	var addToken string
	var addUserID string
	var addAPIBaseURL string
	var addBotID string
	var addSecret string

	var channelAddCmd = &cobra.Command{
		Use:   "add [type]",
		Short: "Add or update a channel (interactive if no type given)",
		Long: `Add or update a channel in config.

If no type is provided, starts interactive mode to guide you through setup.
If type is provided, proceeds with that channel type's setup flow.

Examples:
  # Interactive mode (guided setup)
  evoduck channel add

  # Add Weixin with QR login
  evoduck channel add weixin

  # Add Weixin with existing token
  evoduck channel add weixin --token your-token

  # Add WeCom AI Bot
  evoduck channel add wecom --bot-id your-bot-id --secret your-secret`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var channelType string
			if len(args) > 0 {
				channelType = args[0]
			}
			return channelAdd(channelAddOptions{
				Type:       channelType,
				ChannelID:  addChannelID,
				Name:       addName,
				Role:       addRole,
				Agent:      addAgent,
				Token:      addToken,
				UserID:     addUserID,
				APIBaseURL: addAPIBaseURL,
				BotID:      addBotID,
				Secret:     addSecret,
			})
		},
	}

	channelAddCmd.Flags().StringVar(&addChannelID, "channel-id", "", "Channel ID (auto-generated if empty)")
	channelAddCmd.Flags().StringVar(&addName, "name", "", "Channel display name")
	channelAddCmd.Flags().StringVar(&addRole, "role", "employee", "Role: admin, employee, customer")
	channelAddCmd.Flags().StringVar(&addAgent, "agent", "", "Bound agent ID")
	channelAddCmd.Flags().StringVar(&addToken, "token", "", "Token (for weixin)")
	channelAddCmd.Flags().StringVar(&addUserID, "user-id", "", "User ID (for weixin)")
	channelAddCmd.Flags().StringVar(&addAPIBaseURL, "api-base-url", "", "API base URL (for weixin)")
	channelAddCmd.Flags().StringVar(&addBotID, "bot-id", "", "Bot ID (for wecom)")
	channelAddCmd.Flags().StringVar(&addSecret, "secret", "", "Bot Secret (for wecom)")

	var removeChannelID string
	var channelRemoveCmd = &cobra.Command{
		Use:   "remove",
		Short: "Remove a channel from config",
		RunE: func(cmd *cobra.Command, args []string) error {
			return channelRemove(removeChannelID)
		},
	}
	channelRemoveCmd.Flags().StringVar(&removeChannelID, "channel-id", "", "Channel ID")
	_ = channelRemoveCmd.MarkFlagRequired("channel-id")

	var reconnectChannelID string
	var channelReconnectCmd = &cobra.Command{
		Use:   "reconnect",
		Short: "Reconnect a channel by restarting EvoDuck service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return channelReconnect(reconnectChannelID)
		},
	}
	channelReconnectCmd.Flags().StringVar(&reconnectChannelID, "channel-id", "", "Channel ID")
	_ = channelReconnectCmd.MarkFlagRequired("channel-id")

	var skillScope string
	var skillAgent string
	var skillForce bool
	var skillPath string
	var skillRef string
	var skillOutput string

	var skillsInstallCmd = &cobra.Command{
		Use:   "install <directory-or-zip>",
		Short: "Install a skill from a local directory or zip file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return skillsInstall(args[0], skillScope, skillAgent, skillForce, skillPath, skillRef)
		},
	}
	skillsInstallCmd.Flags().StringVar(&skillScope, "scope", "", "Install scope: agent or shared")
	skillsInstallCmd.Flags().StringVar(&skillAgent, "agent", "", "Agent ID for --scope agent")
	skillsInstallCmd.Flags().BoolVar(&skillForce, "force", false, "Overwrite an existing installed skill")
	skillsInstallCmd.Flags().StringVar(&skillPath, "path", "", "Skill subdirectory path when installing from a git repository")
	skillsInstallCmd.Flags().StringVar(&skillRef, "ref", "", "Git ref to install when source is a git repository")

	var skillsListCmd = &cobra.Command{
		Use:   "list",
		Short: "List installed skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			return skillsList(skillScope, skillAgent)
		},
	}
	skillsListCmd.Flags().StringVar(&skillScope, "scope", "shared", "Skill scope: agent or shared")
	skillsListCmd.Flags().StringVar(&skillAgent, "agent", "", "Agent ID for --scope agent")

	var skillsDetailCmd = &cobra.Command{
		Use:   "detail <name>",
		Short: "Show installed skill details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return skillsDetail(args[0], skillScope, skillAgent)
		},
	}
	skillsDetailCmd.Flags().StringVar(&skillScope, "scope", "shared", "Skill scope: agent or shared")
	skillsDetailCmd.Flags().StringVar(&skillAgent, "agent", "", "Agent ID for --scope agent")

	var skillsRemoveCmd = &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an installed skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return skillsRemove(args[0], skillScope, skillAgent)
		},
	}
	skillsRemoveCmd.Flags().StringVar(&skillScope, "scope", "shared", "Skill scope: agent or shared")
	skillsRemoveCmd.Flags().StringVar(&skillAgent, "agent", "", "Agent ID for --scope agent")

	var skillsVerifyCmd = &cobra.Command{
		Use:   "verify <name>",
		Short: "Verify an installed skill package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return skillsVerify(args[0], skillScope, skillAgent)
		},
	}
	skillsVerifyCmd.Flags().StringVar(&skillScope, "scope", "shared", "Skill scope: agent or shared")
	skillsVerifyCmd.Flags().StringVar(&skillAgent, "agent", "", "Agent ID for --scope agent")

	var skillsPackCmd = &cobra.Command{
		Use:   "pack <skill-dir>",
		Short: "Pack a skill directory into a zip archive",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return skillsPack(args[0], skillOutput)
		},
	}
	skillsPackCmd.Flags().StringVar(&skillOutput, "output", "", "Output zip path")

	skillsCmd.AddCommand(skillsInstallCmd, skillsListCmd, skillsDetailCmd, skillsRemoveCmd, skillsVerifyCmd, skillsPackCmd)
	channelCmd.AddCommand(channelTypesCmd, channelInfoCmd, channelListCmd, channelAddCmd, channelRemoveCmd, channelReconnectCmd)
	serviceCmd.AddCommand(startCmd, stopCmd, restartCmd, statusCmd)
	rootCmd.AddCommand(runCmd, setupCmd, versionCmd, updateCmd, serviceCmd, daemonModeCmd, workerModeCmd, channelCmd, skillsCmd, installCmd, uninstallCmd)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path")

	if err := rootCmd.Execute(); err != nil {
		logger.Error("Command execution failed", logger.Fields{
			"error": err.Error(),
		})
		fmt.Println(err)
		os.Exit(1)
	}
}

func runGateway() error {
	gw, pluginMgr, cfg, err := buildGatewayRuntime()
	if err != nil {
		return err
	}

	// 创建守护进程
	d := daemon.New()
	d.OnShutdown(func(ctx context.Context) error {
		logger.Info("Shutting down gateway")
		if err := gw.Stop(); err != nil {
			return err
		}
		return pluginMgr.Shutdown(ctx)
	})

	logger.Info("Starting EvoDuck", logger.Fields{
		"host": cfg.Gateway.Host,
		"port": cfg.Gateway.Port,
	})

	// 运行
	return d.Run(func() error {
		return gw.Start()
	})
}

func buildGatewayRuntime() (*gateway.Gateway, *plugin.Manager, *config.Config, error) {
	if err := ensureStartupConfigReady(); err != nil {
		return nil, nil, nil, err
	}

	// 加载配置
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load config: %w", err)
	}

	// 设置代理环境变量（程序启动时设置，所有 HTTP client 和子进程自动继承）
	proxy.Setup(cfg.Proxy)

	// 验证配置
	if err := cfg.ValidateWithEnv(); err != nil {
		return nil, nil, nil, fmt.Errorf("config validation: %w", err)
	}

	// 根据配置重新设置日志（配置文件或环境变量可能覆盖默认值）
	logger.Configure(cfg.Logging.Level, cfg.Logging.JSONMode, cfg.Logging.Color)

	// 启用文件日志输出（按日期分隔，落到 data/logs/ 目录）
	if cfg.DataDir != "" {
		logger.SetFileOutputDir(cfg.DataDir + "/logs")
	}

	logger.Info("Configuration loaded and validated")

	// Create proxy decider for LLM and other components
	proxyDecider := proxy.NewDecider(cfg.Proxy)

	// 初始化 LLM Registry
	llmReg, err := llm.NewRegistry(cfg.LLM, proxyDecider)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("init LLM registry: %w", err)
	}

	logger.Info("LLM registry initialized", logger.Fields{
		"default_provider": cfg.LLM.DefaultProvider,
		"default_model":    cfg.LLM.DefaultModel,
	})
	pluginMgr := plugin.NewManager(cfg.Plugins, proxyDecider)
	if err := pluginMgr.Start(context.Background()); err != nil {
		return nil, nil, nil, fmt.Errorf("start plugin manager: %w", err)
	}
	if err := pluginMgr.WaitReady(context.Background(), 30*time.Second); err != nil {
		logger.Warn("Plugin manager ready wait failed", logger.Fields{
			"error": err.Error(),
		})
	}

	for _, providerAdapter := range pluginMgr.ListProviderAdapters() {
		if err := llmReg.RegisterDynamic(providerAdapter.Name(), providerAdapter); err != nil {
			return nil, nil, nil, fmt.Errorf("register plugin provider %s: %w", providerAdapter.Name(), err)
		}
		logger.Info("Plugin provider registered", logger.Fields{"provider": providerAdapter.Name()})
	}

	// 初始化 Agent Manager
	agentMgr := agent.NewManager(llmReg, cfg.DataDir, cfg.Shared.SkillsDir, cfg.Tools.BackendCall, cfg.Tools.Session, cfg.Memory, &cfg.MCP, proxyDecider, pluginMgr)
	for id, agentCfg := range cfg.Agents {
		if err := agentMgr.Register(id, agentCfg); err != nil {
			return nil, nil, nil, fmt.Errorf("register agent %s: %w", id, err)
		}
		logger.Info("Agent registered", logger.Fields{
			"agent_id":  id,
			"role":      agentCfg.Role,
			"workspace": agentCfg.Workspace,
		})
	}
	curatorBaseCfg := config.AgentConfig{
		Provider: cfg.LLM.DefaultProvider,
		Model:    cfg.LLM.DefaultModel,
	}
	if defaultCfg, ok := cfg.Agents[cfg.DefaultAgent]; ok {
		curatorBaseCfg.Provider = defaultCfg.Provider
		curatorBaseCfg.Model = defaultCfg.Model
		curatorBaseCfg.Temperature = defaultCfg.Temperature
		curatorBaseCfg.MaxTokens = defaultCfg.MaxTokens
		curatorBaseCfg.TopP = defaultCfg.TopP
		curatorBaseCfg.MaxIterations = defaultCfg.MaxIterations
	}
	curatorCfg := agent.ExperienceCuratorConfig(cfg.DataDir, curatorBaseCfg)
	if err := agentMgr.Register(agent.ExperienceCuratorID, curatorCfg); err != nil {
		return nil, nil, nil, fmt.Errorf("register system agent %s: %w", agent.ExperienceCuratorID, err)
	}
	logger.Info("System agent registered", logger.Fields{
		"agent_id":  agent.ExperienceCuratorID,
		"workspace": curatorCfg.Workspace,
	})

	// 创建 Gateway
	gateway.SetRuntimeVersion(Version)
	gw := gateway.New(cfg, cfgFile, llmReg, agentMgr, pluginMgr, proxyDecider)
	agentMgr.SetScheduleManager(gw)
	agentMgr.SetSessionGateway(gw)
	agentMgr.SetSubagentGateway(gw)
	return gw, pluginMgr, cfg, nil
}

type skillTarget struct {
	Scope      string
	AgentID    string
	SkillsRoot string
	LockPath   string
}

func resolveSkillTarget(scope string, agentID string) (skillTarget, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return skillTarget{}, fmt.Errorf("--scope is required: use agent or shared")
	}
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return skillTarget{}, fmt.Errorf("load config: %w", err)
	}
	switch scope {
	case "shared":
		return skillTarget{
			Scope:      scope,
			SkillsRoot: cfg.Shared.SkillsDir,
			LockPath:   filepath.Join(filepath.Dir(cfg.Shared.SkillsDir), "skills.lock.json"),
		}, nil
	case "agent":
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			return skillTarget{}, fmt.Errorf("--agent is required for --scope agent")
		}
		agentCfg, ok := cfg.Agents[agentID]
		if !ok {
			return skillTarget{}, fmt.Errorf("agent not found: %s", agentID)
		}
		return skillTarget{
			Scope:      scope,
			AgentID:    agentID,
			SkillsRoot: filepath.Join(agentCfg.Workspace, "skills"),
			LockPath:   filepath.Join(agentCfg.Workspace, "skills.lock.json"),
		}, nil
	default:
		return skillTarget{}, fmt.Errorf("invalid --scope %q: use agent or shared", scope)
	}
}

func skillsInstall(source string, scope string, agentID string, force bool, skillPath string, ref string) error {
	target, err := resolveSkillTarget(scope, agentID)
	if err != nil {
		return err
	}
	var result *skill.InstallResult
	if isGitSource(source) {
		result, err = skill.InstallGit(skill.GitInstallOptions{
			URL:        source,
			Ref:        ref,
			Path:       skillPath,
			TargetRoot: target.SkillsRoot,
			LockPath:   target.LockPath,
			Force:      force,
		})
	} else {
		if strings.TrimSpace(skillPath) != "" || strings.TrimSpace(ref) != "" {
			return fmt.Errorf("--path and --ref are only supported for git repository installs")
		}
		result, err = skill.InstallLocal(skill.InstallOptions{
			Source:     source,
			TargetRoot: target.SkillsRoot,
			LockPath:   target.LockPath,
			Force:      force,
		})
	}
	if err != nil {
		return err
	}
	fmt.Printf("Installed skill %q to %s\n", result.Name, result.TargetPath)
	fmt.Printf("Updated lockfile: %s\n", target.LockPath)
	return nil
}

func isGitSource(source string) bool {
	source = strings.TrimSpace(source)
	return strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "git@") || strings.HasSuffix(source, ".git")
}

func skillsList(scope string, agentID string) error {
	target, err := resolveSkillTarget(scope, agentID)
	if err != nil {
		return err
	}
	skills, err := skill.ListInstalled(target.SkillsRoot)
	if err != nil {
		return err
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	if len(skills) == 0 {
		fmt.Println("No skills installed.")
		return nil
	}
	for _, s := range skills {
		line := fmt.Sprintf("- %s: %s", s.Name, s.Description)
		if s.License != "" {
			line += fmt.Sprintf(" [license: %s]", s.License)
		}
		fmt.Println(line)
	}
	return nil
}

func skillsDetail(name string, scope string, agentID string) error {
	target, err := resolveSkillTarget(scope, agentID)
	if err != nil {
		return err
	}
	root := filepath.Join(target.SkillsRoot, name)
	s, manifest, err := skill.VerifyPackage(root)
	if err != nil {
		return err
	}
	fmt.Printf("Name: %s\n", s.Name)
	fmt.Printf("Description: %s\n", s.Description)
	if s.License != "" {
		fmt.Printf("License: %s\n", s.License)
	}
	if len(s.Compatibility) > 0 {
		fmt.Printf("Compatibility: %s\n", strings.Join(s.Compatibility, ", "))
	}
	if s.Role != "" {
		fmt.Printf("Role: %s\n", s.Role)
	}
	if len(s.Tags) > 0 {
		fmt.Printf("Tags: %s\n", strings.Join(s.Tags, ", "))
	}
	fmt.Printf("Location: %s\n", s.Location)
	fmt.Printf("BaseDir: %s\n", s.BaseDir())
	if manifest != nil {
		fmt.Printf("Version: %s\n", manifest.Version)
		fmt.Printf("Entry: %s\n", manifest.Entry)
	}
	if deprecated := s.DeprecatedSummary(); deprecated != "" {
		fmt.Printf("Deprecated fields: %s\n", deprecated)
	}
	return nil
}

func skillsRemove(name string, scope string, agentID string) error {
	target, err := resolveSkillTarget(scope, agentID)
	if err != nil {
		return err
	}
	if err := skill.RemoveInstalled(name, target.SkillsRoot, target.LockPath); err != nil {
		return err
	}
	fmt.Printf("Removed skill %q from %s\n", name, target.SkillsRoot)
	return nil
}

func skillsVerify(name string, scope string, agentID string) error {
	target, err := resolveSkillTarget(scope, agentID)
	if err != nil {
		return err
	}
	root := filepath.Join(target.SkillsRoot, name)
	s, manifest, err := skill.VerifyPackage(root)
	if err != nil {
		return err
	}
	if manifest != nil {
		fmt.Printf("Skill %q verified (version %s).\n", s.Name, manifest.Version)
	} else {
		fmt.Printf("Skill %q verified.\n", s.Name)
	}
	return nil
}

func skillsPack(sourceDir string, output string) error {
	result, err := skill.Pack(skill.PackOptions{SourceDir: sourceDir, Output: output})
	if err != nil {
		return err
	}
	fmt.Printf("Packed skill %q to %s\n", result.Name, result.Output)
	fmt.Printf("SHA256: %s\n", result.SHA256)
	return nil
}

func runGatewayForeground() error {
	return runGateway()
}

func runDaemonMode() error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	resolvedConfigPath, err := config.ResolveConfigPath(cfgFile)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	// Load config to get gateway host/port and daemon control port
	cfg, err := config.Load(cfgFile)
	if err != nil {
		// Continue without config - will use defaults
		cfg = nil
	}

	gatewayHost := ""
	gatewayPort := 0
	ctrlPort := 0
	if cfg != nil {
		gatewayHost = cfg.Gateway.Host
		gatewayPort = cfg.Gateway.Port
		ctrlPort = cfg.Daemon.ControlPort
	}

	supervisor := daemon.NewSupervisor(daemon.SupervisorConfig{
		Executable:  execPath,
		ConfigPath:  resolvedConfigPath,
		GatewayHost: gatewayHost,
		GatewayPort: gatewayPort,
		CtrlPort:    ctrlPort,
	})
	return supervisor.RunDaemonMode()
}

func runWorkerMode() error {
	return runGateway()
}

func autostartInstall() error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	resolvedConfigPath, err := config.ResolveConfigPath(cfgFile)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	return daemon.InstallAutostart(execPath, resolvedConfigPath)
}

func daemonStart() error {
	if err := ensureStartupConfigReady(); err != nil {
		return err
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	resolvedConfigPath, err := config.ResolveConfigPath(cfgFile)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	// Load config to get gateway host/port and daemon control port
	cfg, err := config.Load(cfgFile)
	if err != nil {
		// Continue without config - will use defaults
		cfg = nil
	}

	gatewayHost := ""
	gatewayPort := 0
	ctrlPort := 0
	if cfg != nil {
		gatewayHost = cfg.Gateway.Host
		gatewayPort = cfg.Gateway.Port
		ctrlPort = cfg.Daemon.ControlPort
	}

	supervisor := daemon.NewSupervisor(daemon.SupervisorConfig{
		Executable:  execPath,
		ConfigPath:  resolvedConfigPath,
		GatewayHost: gatewayHost,
		GatewayPort: gatewayPort,
		CtrlPort:    ctrlPort,
	})

	// Check if daemon is already running
	status, err := supervisor.Status()
	if err != nil {
		return fmt.Errorf("check daemon status: %w", err)
	}

	if status.DaemonRunning {
		fmt.Println("Daemon is already running")
		fmt.Printf("  Daemon PID: %d\n", status.DaemonPID)
		if status.WorkerRunning {
			fmt.Printf("  Worker PID: %d\n", status.WorkerPID)
		}
		return nil
	}

	fmt.Println("Starting EvoDuck daemon...")
	if err := supervisor.StartDaemon(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	fmt.Println("✓ Daemon started successfully")
	return nil
}

func daemonStop() error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	resolvedConfigPath, err := config.ResolveConfigPath(cfgFile)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	// Load config to get gateway host/port and daemon control port for graceful shutdown
	cfg, err := config.Load(cfgFile)
	if err != nil {
		// Continue without config - will use defaults
		cfg = nil
	}

	gatewayHost := ""
	gatewayPort := 0
	ctrlPort := 0
	if cfg != nil {
		gatewayHost = cfg.Gateway.Host
		gatewayPort = cfg.Gateway.Port
		ctrlPort = cfg.Daemon.ControlPort
	}

	supervisor := daemon.NewSupervisor(daemon.SupervisorConfig{
		Executable:  execPath,
		ConfigPath:  resolvedConfigPath,
		GatewayHost: gatewayHost,
		GatewayPort: gatewayPort,
		CtrlPort:    ctrlPort,
	})

	// Check if daemon is running
	status, err := supervisor.Status()
	if err != nil {
		return fmt.Errorf("check daemon status: %w", err)
	}

	if !status.DaemonRunning {
		fmt.Println("Daemon is not running")
		return nil
	}

	fmt.Println("Stopping EvoDuck daemon...")
	fmt.Printf("  Daemon PID: %d\n", status.DaemonPID)
	if status.WorkerRunning {
		fmt.Printf("  Worker PID: %d\n", status.WorkerPID)
	}

	if err := supervisor.StopDaemon(); err != nil {
		return fmt.Errorf("stop daemon: %w", err)
	}

	fmt.Println("✓ Daemon stopped successfully")
	return nil
}

func daemonRestart() error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	resolvedConfigPath, err := config.ResolveConfigPath(cfgFile)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	supervisor := daemon.NewSupervisor(daemon.SupervisorConfig{
		Executable: execPath,
		ConfigPath: resolvedConfigPath,
	})

	// Check if daemon is running
	status, err := supervisor.Status()
	if err != nil {
		return fmt.Errorf("check daemon status: %w", err)
	}

	if !status.DaemonRunning {
		fmt.Println("Daemon is not running. Use 'evoduck start' to start it.")
		return nil
	}

	fmt.Println("Restarting EvoDuck worker...")
	fmt.Printf("  Daemon PID: %d\n", status.DaemonPID)
	if status.WorkerRunning {
		fmt.Printf("  Worker PID: %d (stopping)\n", status.WorkerPID)
	}

	if err := supervisor.RestartWorker(); err != nil {
		return fmt.Errorf("restart worker: %w", err)
	}

	fmt.Println("✓ Worker restarted successfully")
	return nil
}

func daemonStatus() error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	resolvedConfigPath, err := config.ResolveConfigPath(cfgFile)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	supervisor := daemon.NewSupervisor(daemon.SupervisorConfig{
		Executable: execPath,
		ConfigPath: resolvedConfigPath,
	})

	status, err := supervisor.Status()
	if err != nil {
		return fmt.Errorf("check daemon status: %w", err)
	}

	fmt.Println("EvoDuck Process Status:")
	fmt.Println()

	if status.DaemonRunning {
		fmt.Println("Daemon: running")
		fmt.Printf("  PID: %d\n", status.DaemonPID)
	} else {
		fmt.Println("Daemon: not running")
	}

	if status.WorkerRunning {
		fmt.Println("Worker: running")
		fmt.Printf("  PID: %d\n", status.WorkerPID)
		if status.Uptime > 0 {
			fmt.Printf("  Uptime: %s\n", status.Uptime.Round(time.Second))
		}
		if status.RestartCount > 0 {
			fmt.Printf("  Restart count: %d\n", status.RestartCount)
		}
	} else if status.DaemonRunning {
		fmt.Println("Worker: stopped (daemon waiting)")
	} else {
		fmt.Println("Worker: not running")
	}

	// Check autostart status
	autostartEnabled, err := daemon.CheckAutostartStatus()
	if err != nil {
		fmt.Printf("\nAutostart: error checking (%v)\n", err)
	} else if autostartEnabled {
		manager := daemon.GetAutostartManager()
		fmt.Printf("\nAutostart: enabled\n")
		fmt.Printf("  Location: %s\n", manager.GetPath())
	} else {
		fmt.Println("\nAutostart: disabled")
	}

	return nil
}

func autostartUninstall() error {
	return daemon.UninstallAutostart()
}

type updateOptions struct {
	Repo       string
	Version    string
	InstallDir string
	CheckOnly  bool
	Force      bool
}

func runUpdate(ctx context.Context, opts updateOptions) error {
	repo := strings.TrimSpace(opts.Repo)
	if repo == "" {
		repo = strings.TrimSpace(os.Getenv("EVODUCK_REPO"))
	}

	// Check daemon status before update
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	resolvedConfigPath, err := config.ResolveConfigPath(cfgFile)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	// Load config to get proxy settings for update
	cfg, err := config.Load(resolvedConfigPath)
	if err != nil {
		// Non-critical: update can proceed without proxy config
		cfg = &config.Config{}
	}

	// Create proxy decider and get HTTP client for update
	proxyDecider := proxy.NewDecider(cfg.Proxy)
	updateDecision := proxyDecider.ForUpdate()
	var httpClient *http.Client
	if updateDecision.UseProxy {
		httpClient = updateDecision.HTTPClient
	}

	supervisor := daemon.NewSupervisor(daemon.SupervisorConfig{
		Executable: execPath,
		ConfigPath: resolvedConfigPath,
	})

	wasRunning := false
	daemonStopped := false
	status, err := supervisor.Status()
	if err == nil && status.DaemonRunning {
		wasRunning = status.WorkerRunning
		if wasRunning && !opts.CheckOnly {
			fmt.Println("Stopping EvoDuck daemon...")
			if err := supervisor.StopDaemon(); err != nil {
				fmt.Printf("Failed to stop daemon: %v\n", err)
				return err
			}
			daemonStopped = true
			fmt.Println("Daemon stopped.")
		}
	}

	fmt.Println("Checking for updates...")
	result, err := selfupdate.Run(ctx, selfupdate.Options{
		Repo:           repo,
		Version:        opts.Version,
		InstallDir:     opts.InstallDir,
		CurrentVersion: Version,
		Force:          opts.Force,
		CheckOnly:      opts.CheckOnly,
		RefreshService: false,
		RestartService: false,
		HTTPClient:     httpClient,
	})
	if err != nil {
		fmt.Printf("Update failed: %v\n", err)
		if daemonStopped {
			fmt.Println("Restarting daemon...")
			_ = daemonStart()
		}
		return err
	}

	if opts.CheckOnly {
		if result.TargetVersion == result.CurrentVersion {
			fmt.Printf("Already up to date: %s\n", result.CurrentVersion)
			return nil
		}
		fmt.Printf("Update available: %s -> %s\n", result.CurrentVersion, result.TargetVersion)
		return nil
	}

	if !result.Updated {
		fmt.Printf("Already up to date: %s\n", result.CurrentVersion)
		fmt.Println("Use --force to reinstall.")
		return nil
	}

	// Refresh autostart if configured
	autostartEnabled, _ := daemon.CheckAutostartStatus()
	if autostartEnabled && !result.Pending {
		fmt.Println("Refreshing autostart configuration...")
		_ = autostartInstall()
		if wasRunning {
			fmt.Println("Starting daemon...")
			if err := daemonStart(); err != nil {
				fmt.Printf("Failed to start daemon: %v\n", err)
				return err
			}
		}
	}

	if result.Pending {
		fmt.Printf("Update staged: %s\n", result.TargetVersion)
		fmt.Printf("Binary will be replaced after exit: %s\n", result.InstallPath)
		if wasRunning {
			fmt.Println("Daemon will be restarted automatically after update.")
		}
		fmt.Println("Update completed successfully.")
		return nil
	}

	fmt.Printf("Updated to %s\n", result.TargetVersion)
	fmt.Printf("Installed at: %s\n", result.InstallPath)
	fmt.Println("Update completed successfully.")
	return nil
}

func channelReconnect(channelID string) error {
	if strings.TrimSpace(channelID) == "" {
		return fmt.Errorf("channel-id is required")
	}
	resolvedPath, err := config.ResolveConfigPath(cfgFile)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := config.Load(resolvedPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if _, ok := cfg.Channels[channelID]; !ok {
		return fmt.Errorf("channel not found: %s", channelID)
	}
	if err := daemonRestart(); err != nil {
		return fmt.Errorf("restart daemon for reconnect: %w", err)
	}
	fmt.Printf("✓ Reconnected channel via daemon restart: %s\n", channelID)
	return nil
}

func daemonRestartForUpdate() error {
	return daemonRestart()
}

func ensureStartupConfigReady() error {
	_, err := maybeRunFirstTimeSetup()
	return err
}

func maybeRunFirstTimeSetup() (bool, error) {
	state, err := config.DetectFirstRunSetup(cfgFile)
	if err != nil {
		return false, err
	}
	if state == nil {
		return false, nil
	}
	fmt.Println("First launch detected. The current config is still using the default template.")
	if err := runFirstTimeSetup(true); err != nil {
		return false, err
	}
	return true, nil
}

func runFirstTimeSetup(autoTriggered bool) error {
	state, err := config.DetectFirstRunSetup(cfgFile)
	if err != nil {
		return err
	}
	if state == nil && autoTriggered {
		return nil
	}

	configPath := cfgFile
	if state != nil {
		configPath = state.ConfigPath
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("========================================")
	fmt.Println("     EvoDuck First-Time Setup Wizard")
	fmt.Println("========================================")
	fmt.Printf("Config file: %s\n", configPath)
	fmt.Println()
	setupProviders := config.FirstRunProviderCatalog()
	fmt.Println("Choose the default LLM provider:")
	for i, provider := range setupProviders {
		fmt.Printf("  %d) %s\n", i+1, provider.Label)
	}
	fmt.Println()

	providerName, err := promptProviderChoice(reader)
	if err != nil {
		return err
	}

	opts := config.DefaultSetupOptions(providerName)
	modelRequired := false

	switch opts.Provider {
	case "openai":
		apiKey, err := promptRequired(reader, "OpenAI API Key: ")
		if err != nil {
			return err
		}
		opts.APIKey = apiKey
		baseURL, err := promptBaseURLWithDefault(reader, "OpenAI Base URL", opts.BaseURL)
		if err != nil {
			return err
		}
		opts.BaseURL = baseURL
	case "gemini":
		apiKey, err := promptRequired(reader, "Gemini API Key: ")
		if err != nil {
			return err
		}
		opts.APIKey = apiKey
	case "anthropic":
		apiKey, err := promptRequired(reader, "Anthropic API Key: ")
		if err != nil {
			return err
		}
		opts.APIKey = apiKey
		baseURL, err := promptBaseURLWithDefault(reader, "Anthropic Base URL", opts.BaseURL)
		if err != nil {
			return err
		}
		opts.BaseURL = baseURL
	case "ollama":
		baseURL, err := promptBaseURLWithDefault(reader, "Ollama Base URL", opts.BaseURL)
		if err != nil {
			return err
		}
		opts.BaseURL = baseURL
	case "deepseek", "openrouter", "dashscope", "groq", "mistral", "together", "fireworks", "perplexity", "moonshot", "nvidia", "litellm", "lmstudio", "vllm", "cloudflare-ai-gateway", "vercel-ai-gateway", "helicone", "xai", "azure", "google-ai-studio", "siliconflow", "zhipu", "zhipu-cn", "zhipu-coding", "zhipu-coding-cn", "baidu-qianfan", "tencent-hunyuan", "bytedance", "bytedance-cn", "iflytek-spark", "cerebras", "replicate", "sambanova", "akle", "kilo", "opencode", "cohere", "novita", "dashscope-cn", "dashscope-coding", "dashscope-coding-cn", "portkey":
		baseURL, err := promptBaseURLWithDefault(reader, fmt.Sprintf("%s Base URL", opts.Provider), opts.BaseURL)
		if err != nil {
			return err
		}
		opts.BaseURL = baseURL
		apiKey, err := promptOptional(reader, fmt.Sprintf("%s API Key (optional): ", opts.Provider))
		if err != nil {
			return err
		}
		opts.APIKey = apiKey
	case "bedrock":
		modelRequired = false
	case "vertex-ai":
		modelRequired = false
	case "minimax", "minimax-cn":
		baseURL, err := promptBaseURLWithDefault(reader, "MiniMax Base URL", opts.BaseURL)
		if err != nil {
			return err
		}
		opts.BaseURL = baseURL
		apiKey, err := promptOptional(reader, "MiniMax API Key (optional): ")
		if err != nil {
			return err
		}
		opts.APIKey = apiKey
	case "openai-compatible":
		baseURL, err := promptRequiredValidated(reader, "OpenAI-Compatible Base URL: ", validateBaseURL)
		if err != nil {
			return err
		}
		opts.BaseURL = baseURL
		apiKey, err := promptOptional(reader, "OpenAI-Compatible API Key (optional): ")
		if err != nil {
			return err
		}
		opts.APIKey = apiKey
		modelRequired = true
	case "openai-responses-compatible":
		baseURL, err := promptRequiredValidated(reader, "OpenAI Responses-Compatible Base URL: ", validateBaseURL)
		if err != nil {
			return err
		}
		opts.BaseURL = baseURL
		apiKey, err := promptOptional(reader, "OpenAI Responses-Compatible API Key (optional): ")
		if err != nil {
			return err
		}
		opts.APIKey = apiKey
		modelRequired = true
	case "gemini-compatible":
		baseURL, err := promptRequiredValidated(reader, "Gemini-Compatible Base URL: ", validateBaseURL)
		if err != nil {
			return err
		}
		opts.BaseURL = baseURL
		apiKey, err := promptOptional(reader, "Gemini-Compatible API Key (optional): ")
		if err != nil {
			return err
		}
		opts.APIKey = apiKey
		modelRequired = true
	case "anthropic-compatible":
		baseURL, err := promptRequiredValidated(reader, "Anthropic-Compatible Base URL: ", validateBaseURL)
		if err != nil {
			return err
		}
		opts.BaseURL = baseURL
		apiKey, err := promptOptional(reader, "Anthropic-Compatible API Key (optional): ")
		if err != nil {
			return err
		}
		opts.APIKey = apiKey
		modelRequired = true
	}

	model, err := promptFirstRunModel(reader, opts, modelRequired)
	if err != nil {
		return err
	}
	opts.Model = model

	host, err := promptHostWithDefault(reader, "Gateway Host", opts.Host)
	if err != nil {
		return err
	}
	opts.Host = host

	port, err := promptGatewayPort(reader, opts.Port)
	if err != nil {
		return err
	}
	opts.Port = port

	// Channel configuration step (skippable)
	opts.AdditionalChannels, err = promptChannelSetup(reader)
	if err != nil {
		return err
	}

	if err := config.SaveFirstRunSetup(cfgFile, opts); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("✓ First-time setup has been saved")
	fmt.Printf("✓ Provider: %s\n", opts.Provider)
	fmt.Printf("✓ Config: %s\n", configPath)
	fmt.Println()
	return nil
}

func promptLine(reader *bufio.Reader, label string) (string, error) {
	fmt.Print(label)
	text, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			trimmed := strings.TrimSpace(text)
			if trimmed == "" {
				return "", io.EOF
			}
			return trimmed, nil
		}
		return "", err
	}
	return strings.TrimSpace(text), nil
}

func promptWithDefault(reader *bufio.Reader, label, defaultValue string) (string, error) {
	value, err := promptLine(reader, fmt.Sprintf("%s [%s]: ", label, defaultValue))
	if err != nil {
		return "", err
	}
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
}

func promptProviderChoice(reader *bufio.Reader) (string, error) {
	return promptUntilValid(func() (string, error) {
		providerChoice, err := promptLine(reader, "Provider [1/openai-compatible]: ")
		if err != nil {
			return "", err
		}
		return resolveProviderChoice(providerChoice)
	})
}

func promptRequired(reader *bufio.Reader, label string) (string, error) {
	return promptRequiredValidated(reader, label, nil)
}

func promptOptional(reader *bufio.Reader, label string) (string, error) {
	return promptLine(reader, label)
}

func promptRequiredValidated(reader *bufio.Reader, label string, validator func(string) error) (string, error) {
	return promptUntilValid(func() (string, error) {
		value, err := promptLine(reader, label)
		if err != nil {
			return "", err
		}
		if value == "" {
			return "", fmt.Errorf("%s cannot be empty", strings.TrimSuffix(label, ": "))
		}
		if validator != nil {
			if err := validator(value); err != nil {
				return "", err
			}
		}
		return value, nil
	})
}

func promptBaseURLWithDefault(reader *bufio.Reader, label, defaultValue string) (string, error) {
	return promptUntilValid(func() (string, error) {
		value, err := promptWithDefault(reader, label, defaultValue)
		if err != nil {
			return "", err
		}
		if err := validateBaseURL(value); err != nil {
			return "", err
		}
		return value, nil
	})
}

func promptHostWithDefault(reader *bufio.Reader, label, defaultValue string) (string, error) {
	return promptUntilValid(func() (string, error) {
		value, err := promptWithDefault(reader, label, defaultValue)
		if err != nil {
			return "", err
		}
		if err := validateHost(value); err != nil {
			return "", err
		}
		return value, nil
	})
}

func promptGatewayPort(reader *bufio.Reader, defaultPort int) (int, error) {
	value, err := promptUntilValid(func() (string, error) {
		portText, err := promptWithDefault(reader, "Gateway Port", strconv.Itoa(defaultPort))
		if err != nil {
			return "", err
		}
		port, err := strconv.Atoi(strings.TrimSpace(portText))
		if err != nil || port < 1 || port > 65535 {
			return "", fmt.Errorf("gateway port must be a number between 1 and 65535")
		}
		return strconv.Itoa(port), nil
	})
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(value)
}

func promptChannelSetup(reader *bufio.Reader) ([]config.ChannelSetupOption, error) {
	fmt.Println()
	fmt.Println("Configure additional channels (webchat is always included as built-in)")
	fmt.Println()

	catalog := config.FirstRunChannelCatalog()
	if len(catalog) == 0 {
		fmt.Println("No additional channels available for setup.")
		return nil, nil
	}

	// Filter out webchat (required builtin)
	var optionalChannels []config.ChannelCatalogEntry
	for _, entry := range catalog {
		if !entry.Required {
			optionalChannels = append(optionalChannels, entry)
		}
	}

	if len(optionalChannels) == 0 {
		fmt.Println("No optional channels available for setup.")
		return nil, nil
	}

	fmt.Println("Available channels:")
	for i, entry := range optionalChannels {
		setupDesc := describeSetupKind(entry.SetupKind)
		fmt.Printf("  %d) %s (%s)\n", i+1, entry.Label, setupDesc)
	}
	fmt.Println("  s) Skip channel setup for now")
	fmt.Println()

	choice, err := promptLine(reader, "Select channel to configure [s]: ")
	if err != nil {
		return nil, err
	}

	choice = strings.TrimSpace(choice)
	if choice == "" || strings.ToLower(choice) == "s" {
		fmt.Println("Skipping channel setup. You can configure channels later using 'evoduck channel add' or editing the config file.")
		return nil, nil
	}

	idx, err := strconv.Atoi(choice)
	if err != nil || idx < 1 || idx > len(optionalChannels) {
		fmt.Printf("Invalid selection, skipping channel setup.\n")
		return nil, nil
	}

	selected := optionalChannels[idx-1]
	return promptChannelDetails(reader, selected)
}

func describeSetupKind(kind config.ChannelSetupKind) string {
	switch kind {
	case config.ChannelSetupKindBuiltin:
		return "built-in, no setup needed"
	case config.ChannelSetupKindToken:
		return "requires token"
	case config.ChannelSetupKindQRLogin:
		return "QR code login"
	default:
		return "unknown"
	}
}

func promptChannelDetails(reader *bufio.Reader, entry config.ChannelCatalogEntry) ([]config.ChannelSetupOption, error) {
	option, err := collectChannelSetupOption(reader, entry)
	if err != nil {
		return nil, err
	}
	if option == nil {
		return nil, nil
	}
	return []config.ChannelSetupOption{*option}, nil
}

func collectChannelSetupOption(reader *bufio.Reader, entry config.ChannelCatalogEntry) (*config.ChannelSetupOption, error) {
	fmt.Printf("\nConfiguring %s channel:\n", entry.Label)

	switch entry.SetupKind {
	case config.ChannelSetupKindBuiltin:
		return nil, nil
	case config.ChannelSetupKindToken:
		channelID, err := promptChannelID(reader, entry.Type)
		if err != nil {
			return nil, err
		}

		var name, role, agent, botID, secret, token, userID, apiBaseURL string

		for _, param := range entry.OptionalParams {
			if param.Name == "name" {
				defaultName := entry.Label
				if param.Default != "" {
					defaultName = param.Default
				}
				name, err = promptWithDefault(reader, "Channel name", defaultName)
				if err != nil {
					return nil, err
				}
			} else if param.Name == "role" {
				role, err = promptRoleWithDefault(reader)
				if err != nil {
					return nil, err
				}
			} else if param.Name == "agent" {
				agent, err = promptOptional(reader, "Bound agent ID (optional): ")
				if err != nil {
					return nil, err
				}
			}
		}

		for _, param := range entry.RequiredParams {
			switch param.Name {
			case "bot-id":
				botID, err = promptRequired(reader, "Bot ID: ")
				if err != nil {
					return nil, err
				}
			case "secret":
				secret, err = promptRequired(reader, "Secret: ")
				if err != nil {
					return nil, err
				}
			case "token":
				token, err = promptRequired(reader, "Token: ")
				if err != nil {
					return nil, err
				}
			}
		}

		for _, param := range entry.OptionalParams {
			if param.Name == "user-id" && userID == "" {
				userID, err = promptOptional(reader, "User ID (optional): ")
				if err != nil {
					return nil, err
				}
			} else if param.Name == "api-base-url" && apiBaseURL == "" {
				apiBaseURL, err = promptOptional(reader, "API Base URL (optional, uses default if empty): ")
				if err != nil {
					return nil, err
				}
			}
		}

		return &config.ChannelSetupOption{
			Type:       entry.Type,
			ChannelID:  channelID,
			Name:       name,
			Role:       role,
			Agent:      agent,
			BotID:      botID,
			Secret:     secret,
			Token:      token,
			UserID:     userID,
			APIBaseURL: apiBaseURL,
		}, nil
	case config.ChannelSetupKindQRLogin:
		fmt.Println()
		fmt.Println("Starting QR login flow for Weixin...")
		loginResult, err := weixinQRLoginRunner("", "", "", "", "")
		if err != nil {
			return nil, err
		}
		return &config.ChannelSetupOption{Type: entry.Type, ChannelID: defaultWeixinChannelID(loginResult.AccountID), Name: defaultWeixinChannelName(loginResult), Token: loginResult.Token, UserID: loginResult.UserID, APIBaseURL: loginResult.BaseURL}, nil
	default:
		return nil, fmt.Errorf("unsupported channel setup kind: %s", entry.SetupKind)
	}
}

func promptChannelID(reader *bufio.Reader, channelType string) (string, error) {
	defaultID := channelType + "-default"
	return promptUntilValid(func() (string, error) {
		id, err := promptWithDefault(reader, "Channel ID", defaultID)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(id) == "" {
			return "", fmt.Errorf("channel ID cannot be empty")
		}
		return id, nil
	})
}

func defaultWeixinChannelID(accountID string) string {
	trimmed := strings.TrimSpace(accountID)
	if trimmed == "" {
		return "weixin-default"
	}
	value := sanitizeChannelIDSuffix(trimmed)
	if value == "" {
		return "weixin-default"
	}
	return "weixin-" + value
}

func defaultWecomChannelID(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "wecom-default"
	}
	value := sanitizeChannelIDSuffix(trimmed)
	if value == "" {
		return "wecom-default"
	}
	return "wecom-" + value
}

func sanitizeChannelIDSuffix(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	lastDash := false
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func defaultWeixinChannelName(result *weixin.LoginResult) string {
	if result == nil {
		return "已登录微信"
	}
	if userID := strings.TrimSpace(result.UserID); userID != "" {
		return "微信 " + userID
	}
	if accountID := strings.TrimSpace(result.AccountID); accountID != "" {
		return "微信 " + accountID
	}
	return "已登录微信"
}

func promptRoleWithDefault(reader *bufio.Reader) (string, error) {
	return promptUntilValid(func() (string, error) {
		role, err := promptWithDefault(reader, "Role (admin/employee/customer)", "employee")
		if err != nil {
			return "", err
		}
		role = strings.ToLower(strings.TrimSpace(role))
		if role != "admin" && role != "employee" && role != "customer" {
			return "", fmt.Errorf("role must be admin, employee, or customer")
		}
		return role, nil
	})
}

func promptUntilValid[T any](fn func() (T, error)) (T, error) {
	for {
		value, err := fn()
		if err == nil {
			return value, nil
		}
		if isPromptRetryableError(err) {
			fmt.Printf("Invalid input: %v. Please try again.\n", err)
			continue
		}
		var zero T
		return zero, err
	}
}

func isPromptRetryableError(err error) bool {
	return err != nil && !errors.Is(err, io.EOF)
}

func validateBaseURL(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("base URL cannot be empty")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("base URL must be a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("base URL must start with http:// or https://")
	}
	return nil
}

func validateHost(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("gateway host cannot be empty")
	}
	if strings.ContainsAny(trimmed, " /?#") {
		return fmt.Errorf("gateway host is not a valid hostname")
	}
	if net.ParseIP(trimmed) != nil {
		return nil
	}
	if _, err := netip.ParseAddr(trimmed); err == nil {
		return nil
	}
	labels := strings.Split(trimmed, ".")
	for _, label := range labels {
		if label == "" {
			return fmt.Errorf("gateway host is not a valid hostname")
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return fmt.Errorf("gateway host is not a valid hostname")
			}
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("gateway host is not a valid hostname")
		}
	}
	return nil
}

func promptFirstRunModel(reader *bufio.Reader, opts config.SetupOptions, required bool) (string, error) {
	models, err := listFirstRunModels(context.Background(), opts)
	if err == nil && len(models) > 0 {
		defaultModel := resolveFirstRunDefaultModel(opts, models)
		fmt.Println("Available models:")
		for i, model := range models {
			fmt.Printf("  %d) %s\n", i+1, model.ID)
		}
		fmt.Println()
		return promptUntilValid(func() (string, error) {
			input, err := promptLine(reader, fmt.Sprintf("Default model [%s]: ", defaultModel))
			if err != nil {
				return "", err
			}
			return resolveFirstRunModelChoice(input, defaultModel, models)
		})
	}
	if err != nil {
		fmt.Println("Could not fetch the online model list. Falling back to manual default model input.")
	}
	if required {
		return promptRequired(reader, "Default model: ")
	}
	return promptWithDefault(reader, "Default model", opts.Model)
}

func listFirstRunModels(ctx context.Context, opts config.SetupOptions) ([]llm.ProviderModel, error) {
	providerName, providerCfg := buildFirstRunProviderConfig(opts)
	if providerName == "" {
		return nil, fmt.Errorf("provider is required")
	}
	return llm.ListModelsForProviderConfig(ctx, providerName, providerCfg)
}

func buildFirstRunProviderConfig(opts config.SetupOptions) (string, config.ProviderConfig) {
	providerName := config.NormalizeFirstRunProviderName(opts.Provider)
	providerCfg := config.ProviderConfig{
		Type:         providerName,
		BaseURL:      strings.TrimSpace(opts.BaseURL),
		APIKey:       strings.TrimSpace(opts.APIKey),
		DefaultModel: strings.TrimSpace(opts.Model),
	}
	if entry, ok := config.LookupProviderCatalogEntry(providerName); ok {
		providerCfg.Type = entry.Type
		providerCfg.Models = append([]config.ProviderModelConfig(nil), entry.Models...)
		if providerCfg.BaseURL == "" {
			providerCfg.BaseURL = entry.DefaultBaseURL
		}
		if providerCfg.DefaultModel == "" {
			providerCfg.DefaultModel = entry.DefaultModel
		}
	}
	for _, preset := range config.ProviderPresets() {
		if preset.Type != providerName {
			continue
		}
		if len(providerCfg.Models) == 0 {
			providerCfg.Models = append([]config.ProviderModelConfig(nil), preset.Models...)
		}
		if providerCfg.BaseURL == "" {
			providerCfg.BaseURL = preset.DefaultBaseURL
		}
		if providerCfg.DefaultModel == "" {
			providerCfg.DefaultModel = preset.DefaultModel
		}
		if len(preset.Headers) > 0 {
			providerCfg.Headers = make(map[string]string, len(preset.Headers))
			for key, value := range preset.Headers {
				providerCfg.Headers[key] = value
			}
		}
		break
	}
	return providerName, providerCfg
}

func resolveFirstRunDefaultModel(opts config.SetupOptions, models []llm.ProviderModel) string {
	preferred := strings.TrimSpace(opts.Model)
	if preferred != "" {
		for _, model := range models {
			if model.ID == preferred {
				return preferred
			}
		}
	}
	if len(models) > 0 {
		return models[0].ID
	}
	return preferred
}

func resolveFirstRunModelChoice(input, defaultModel string, models []llm.ProviderModel) (string, error) {
	choice := strings.TrimSpace(input)
	if choice == "" {
		if defaultModel == "" {
			return "", fmt.Errorf("default model cannot be empty")
		}
		return defaultModel, nil
	}
	if index, err := strconv.Atoi(choice); err == nil {
		if index < 1 || index > len(models) {
			return "", fmt.Errorf("model selection must be between 1 and %d", len(models))
		}
		return models[index-1].ID, nil
	}
	for _, model := range models {
		if model.ID == choice {
			return model.ID, nil
		}
	}
	return "", fmt.Errorf("model must be a listed name or number")
}

func resolveProviderChoice(input string) (string, error) {
	choice := strings.TrimSpace(input)
	if choice == "" {
		return config.NormalizeFirstRunProviderName(choice), nil
	}
	entry, ok := config.LookupProviderCatalogEntry(choice)
	if !ok || !entry.SupportsFirstRun {
		return "", fmt.Errorf("unknown provider: %s", choice)
	}
	return entry.Type, nil
}

func channelTypes() error {
	catalog := config.ChannelCatalog()

	fmt.Println("Available channel types:")
	fmt.Println()
	fmt.Printf("%-10s %-12s %-15s %s\n", "TYPE", "LABEL", "SETUP", "DESCRIPTION")
	fmt.Printf("%-10s %-12s %-15s %s\n", "----", "-----", "-----", "-----------")

	for _, entry := range catalog {
		setupDesc := config.DescribeSetupKind(entry.SetupKind)
		fmt.Printf("%-10s %-12s %-15s %s\n", entry.Type, entry.Label, setupDesc, entry.Description)
	}

	fmt.Println()
	fmt.Println("Use 'evoduck channel info <type>' for detailed parameters")
	fmt.Println("Use 'evoduck channel add <type>' to configure a channel")

	return nil
}

func channelInfo(channelType string) error {
	entry, ok := config.LookupChannelCatalogEntry(channelType)
	if !ok {
		return fmt.Errorf("unknown channel type: %s. Run 'evoduck channel types' to see available types", channelType)
	}

	fmt.Printf("\n%s - %s Channel\n", entry.Label, entry.Type)
	fmt.Printf("%s\n\n", entry.Description)

	setupDesc := config.DescribeSetupKind(entry.SetupKind)
	fmt.Printf("Setup: %s\n\n", setupDesc)

	if entry.SetupKind == config.ChannelSetupKindBuiltin {
		fmt.Println("This channel is built-in and requires no configuration.")
		return nil
	}

	if len(entry.RequiredParams) > 0 {
		fmt.Println("Required flags:")
		for _, param := range entry.RequiredParams {
			fmt.Printf("  --%-20s %s\n", param.Name, param.Description)
		}
		fmt.Println()
	}

	if len(entry.OptionalParams) > 0 {
		fmt.Println("Optional flags:")
		for _, param := range entry.OptionalParams {
			defaultDesc := ""
			if param.Default != "" {
				defaultDesc = fmt.Sprintf(" (default: %s)", param.Default)
			}
			fmt.Printf("  --%-20s %s%s\n", param.Name, param.Description, defaultDesc)
		}
		fmt.Println()
	}

	fmt.Println("Example:")
	switch entry.Type {
	case "weixin":
		fmt.Println("  evoduck channel add weixin")
		fmt.Println("  # Or with existing token:")
		fmt.Println("  evoduck channel add weixin --token your-token --name \"My WeChat\"")
	case "wecom":
		fmt.Println("  evoduck channel add wecom --corp-id wx123 --corp-secret xxx --agent-id 100 --token abc --encoding-aes-key 123")
	}

	return nil
}

func channelList() error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	fmt.Println("Channels:")
	fmt.Println("---------")

	count := 0
	for channelID, chCfg := range cfg.Channels {
		count++
		fmt.Printf("\n[%d] Channel ID: %s\n", count, channelID)
		fmt.Printf("    Type: %s\n", chCfg.Type)
		if chCfg.Name != "" {
			fmt.Printf("    Name: %s\n", chCfg.Name)
		}
		if chCfg.Token != "" {
			fmt.Println("    Token: ✓ configured")
		} else {
			fmt.Println("    Token: ✗ not configured")
		}
		fmt.Printf("    Role: %s\n", chCfg.Role)
		fmt.Printf("    Agent: %s\n", chCfg.Agent)
		if chCfg.UserID != "" {
			fmt.Printf("    User ID: %s\n", chCfg.UserID)
		}
		if chCfg.BotID != "" {
			fmt.Printf("    Bot ID: %s\n", chCfg.BotID)
		}
	}

	if count == 0 {
		fmt.Println("\n✗ No channels configured")
		fmt.Println("\nExample configuration:")
		fmt.Println("  channels:")
		fmt.Println("    wecom-sales:")
		fmt.Println("      type: wecom")
		fmt.Println("      role: employee")
		fmt.Println("      agent: sales-bot")
		fmt.Println("      bot_id: \"your-bot-id\"")
		fmt.Println("      secret: ${WECOM_SECRET}")
	} else {
		fmt.Printf("\n✓ Total: %d channel(s)\n", count)
	}

	return nil
}

func runWeixinQRLoginFlow(channelID, name, role, agent, userID string) (*weixin.LoginResult, error) {
	_ = agent
	_ = userID

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 处理 Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n\n取消登录...")
		cancel()
	}()

	fmt.Println("========================================")
	fmt.Println("     微信扫码登录")
	fmt.Println("========================================")
	fmt.Println()

	// 如果没有提供 channelID，交互式输入
	if channelID == "" {
		fmt.Print("请输入 Channel ID (如 weixin-cs): ")
		fmt.Scanln(&channelID)
		if channelID == "" {
			channelID = "weixin-mine"
		}
	}

	if name == "" {
		name = "我的微信"
	}
	if role == "" {
		role = "employee"
	}
	if agent == "" {
		agent = "employee-bot"
	}

	fmt.Println("正在获取二维码...")

	// 获取二维码
	qrResp, err := weixin.FetchQRCode(ctx, weixin.DefaultBotType)
	if err != nil {
		return nil, fmt.Errorf("获取二维码失败: %w", err)
	}

	fmt.Println()
	fmt.Println("请用微信扫描以下二维码：")

	// 在终端打印二维码
	if err := weixin.PrintQRCodeTerminal(qrResp.QRCodeImgContent); err != nil {
		// 如果终端打印失败，显示链接
		fmt.Println()
		fmt.Println(qrResp.QRCodeImgContent)
	}

	fmt.Println("或用浏览器打开以下链接扫码：")
	fmt.Println(qrResp.QRCodeImgContent)
	fmt.Println()
	fmt.Println("等待扫码...")

	// 轮询状态
	result, err := weixin.WaitForLogin(ctx, qrResp.QRCodeImgContent, qrResp.QRCode, func(status string) {
		switch status {
		case "wait":
			fmt.Print(".")
		case "scaned":
			fmt.Println()
			fmt.Println("✓ 已扫码，请在手机上确认授权...")
		case "expired":
			fmt.Println()
			fmt.Println("⏳ 二维码已过期，正在刷新...")
		}
	})

	if err != nil {
		return nil, fmt.Errorf("登录失败: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("登录失败: %s", result.Message)
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("✓ 登录成功！")
	fmt.Println("========================================")
	fmt.Printf("Account ID: %s\n", result.AccountID)
	fmt.Printf("User ID: %s\n", result.UserID)
	fmt.Println()
	return result, nil
}

type channelAddOptions struct {
	Type       string
	ChannelID  string
	Name       string
	Role       string
	Agent      string
	Token      string // Weixin: token from QR login
	UserID     string // Weixin: user ID
	APIBaseURL string // Weixin: API base URL
	BotID      string // WeCom: AI Bot ID
	Secret     string // WeCom: AI Bot Secret
}

func channelAdd(opts channelAddOptions) error {
	if opts.Type == "" {
		return channelAddInteractive()
	}

	entry, ok := config.LookupChannelCatalogEntry(opts.Type)
	if !ok {
		return fmt.Errorf("unknown channel type: %s. Run 'evoduck channel types' to see available types", opts.Type)
	}

	opts.Type = entry.Type

	if strings.TrimSpace(opts.Role) == "" {
		opts.Role = "employee"
	}

	switch entry.SetupKind {
	case config.ChannelSetupKindBuiltin:
		fmt.Printf("Channel %s is built-in, no configuration needed.\n", entry.Label)
		return nil
	case config.ChannelSetupKindQRLogin:
		return channelAddWithQRLogin(opts, entry)
	case config.ChannelSetupKindToken:
		return channelAddWithToken(opts, entry)
	default:
		return fmt.Errorf("unsupported channel setup kind: %s", entry.SetupKind)
	}
}

func channelAddInteractive() error {
	reader := channelAddReaderFactory()

	fmt.Println()
	fmt.Println("Configure a new channel")
	fmt.Println()

	catalog := config.OptionalChannelCatalog()
	if len(catalog) == 0 {
		fmt.Println("No optional channels available for setup.")
		return nil
	}

	fmt.Println("Available channel types:")
	for i, entry := range catalog {
		setupDesc := config.DescribeSetupKind(entry.SetupKind)
		fmt.Printf("  %d) %s - %s (%s)\n", i+1, entry.Type, entry.Label, setupDesc)
	}
	fmt.Println("  q) Quit")
	fmt.Println()

	choice, err := promptLine(reader, "Select type [1-2 or q]: ")
	if err != nil {
		return err
	}

	choice = strings.TrimSpace(choice)
	if choice == "" || strings.ToLower(choice) == "q" {
		fmt.Println("Cancelled.")
		return nil
	}

	idx, err := strconv.Atoi(choice)
	if err != nil || idx < 1 || idx > len(catalog) {
		fmt.Println("Invalid selection.")
		return nil
	}

	selected := catalog[idx-1]
	fmt.Printf("\nConfiguring %s channel:\n\n", selected.Label)

	opts := channelAddOptions{Type: selected.Type, Role: "employee"}

	switch selected.SetupKind {
	case config.ChannelSetupKindQRLogin:
		return channelAddWithQRLogin(opts, selected)
	case config.ChannelSetupKindToken:
		return channelAddWithTokenInteractive(opts, selected, reader)
	default:
		return fmt.Errorf("unsupported setup kind: %s", selected.SetupKind)
	}
}

func channelAddWithQRLogin(opts channelAddOptions, entry config.ChannelCatalogEntry) error {
	reader := channelAddReaderFactory()

	if strings.TrimSpace(opts.Token) != "" {
		fmt.Printf("Configuring %s channel (with provided token):\n\n", entry.Label)

		if strings.TrimSpace(opts.Name) == "" {
			defaultName := "我的微信"
			for _, param := range entry.OptionalParams {
				if param.Name == "name" && param.Default != "" {
					defaultName = param.Default
					break
				}
			}
			name, err := promptWithDefault(reader, "Channel name", defaultName)
			if err != nil {
				return err
			}
			opts.Name = name
		}

		if strings.TrimSpace(opts.ChannelID) == "" {
			opts.ChannelID = defaultWeixinChannelID(opts.UserID)
		}

		if err := config.AddWeixinChannel(cfgFile, opts.ChannelID, opts.Name, opts.Token, opts.Role, opts.Agent, opts.UserID, opts.APIBaseURL); err != nil {
			return fmt.Errorf("save weixin channel: %w", err)
		}

		fmt.Printf("\n✓ Saved %s channel: %s\n", entry.Label, opts.ChannelID)
		return nil
	}

	fmt.Printf("Configuring %s channel (QR login):\n\n", entry.Label)

	if strings.TrimSpace(opts.Name) == "" {
		defaultName := "我的微信"
		for _, param := range entry.OptionalParams {
			if param.Name == "name" && param.Default != "" {
				defaultName = param.Default
				break
			}
		}
		name, err := promptWithDefault(reader, "Channel name", defaultName)
		if err != nil {
			return err
		}
		opts.Name = name
	}

	if strings.TrimSpace(opts.Role) == "" {
		role, err := promptRoleWithDefault(reader)
		if err != nil {
			return err
		}
		opts.Role = role
	}

	if strings.TrimSpace(opts.Agent) == "" {
		agent, err := promptOptional(reader, "Bound agent ID (optional): ")
		if err != nil {
			return err
		}
		opts.Agent = agent
	}

	fmt.Println()
	fmt.Println("Starting QR login flow...")

	result, err := weixinQRLoginRunner("", opts.Name, opts.Role, opts.Agent, opts.UserID)
	if err != nil {
		return err
	}

	opts.Token = result.Token
	opts.UserID = result.UserID
	opts.APIBaseURL = result.BaseURL

	if strings.TrimSpace(opts.ChannelID) == "" {
		opts.ChannelID = defaultWeixinChannelID(result.AccountID)
	}

	if err := config.AddWeixinChannel(cfgFile, opts.ChannelID, opts.Name, opts.Token, opts.Role, opts.Agent, opts.UserID, opts.APIBaseURL); err != nil {
		fmt.Printf("Warning: Failed to save config: %v\n", err)
		fmt.Println("Please manually add the following to config.yaml:")
		fmt.Printf(`
%s:
  type: weixin
  name: "%s"
  token: %s
  api_base_url: %s
  role: %s
  agent: %s
  user_id: "%s"
`, opts.ChannelID, opts.Name, opts.Token, opts.APIBaseURL, opts.Role, opts.Agent, opts.UserID)
	} else {
		fmt.Printf("\n✓ Saved %s channel: %s\n", entry.Label, opts.ChannelID)
		if opts.APIBaseURL != "" {
			fmt.Printf("  API Base URL: %s\n", opts.APIBaseURL)
		}
		if opts.UserID != "" {
			fmt.Printf("  User ID: %s\n", opts.UserID)
		}
	}

	return nil
}

func channelAddWithToken(opts channelAddOptions, entry config.ChannelCatalogEntry) error {
	reader := channelAddReaderFactory()

	missingParams := checkMissingRequiredParams(opts, entry.RequiredParams)
	if len(missingParams) > 0 {
		return channelAddWithTokenInteractive(opts, entry, reader)
	}

	return saveChannelConfig(opts, entry)
}

func channelAddWithTokenInteractive(opts channelAddOptions, entry config.ChannelCatalogEntry, reader *bufio.Reader) error {
	fmt.Printf("Configuring %s channel:\n\n", entry.Label)

	if strings.TrimSpace(opts.Name) == "" {
		defaultName := entry.Label
		for _, param := range entry.OptionalParams {
			if param.Name == "name" && param.Default != "" {
				defaultName = param.Default
				break
			}
		}
		name, err := promptWithDefault(reader, "Channel name", defaultName)
		if err != nil {
			return err
		}
		opts.Name = name
	}

	if strings.TrimSpace(opts.Role) == "" {
		role, err := promptRoleWithDefault(reader)
		if err != nil {
			return err
		}
		opts.Role = role
	}

	if strings.TrimSpace(opts.Agent) == "" {
		agent, err := promptOptional(reader, "Bound agent ID (optional): ")
		if err != nil {
			return err
		}
		opts.Agent = agent
	}

	for _, param := range entry.RequiredParams {
		if !hasParamValue(opts, param.Name) {
			value, err := promptRequired(reader, fmt.Sprintf("%s: ", param.Name))
			if err != nil {
				return err
			}
			setParamValue(&opts, param.Name, value)
		}
	}

	for _, param := range entry.OptionalParams {
		if !hasParamValue(opts, param.Name) && param.Name != "name" && param.Name != "role" && param.Name != "agent" {
			promptLabel := fmt.Sprintf("%s", param.Name)
			if param.Default != "" {
				promptLabel = fmt.Sprintf("%s [%s]", param.Name, param.Default)
			}
			value, err := promptOptional(reader, promptLabel+": ")
			if err != nil {
				return err
			}
			if value != "" {
				setParamValue(&opts, param.Name, value)
			} else if param.Default != "" && param.Default != "corp-id" {
				setParamValue(&opts, param.Name, param.Default)
			}
		}
	}

	if strings.TrimSpace(opts.ChannelID) == "" {
		opts.ChannelID = defaultWecomChannelID(opts.Name)
	}

	return saveChannelConfig(opts, entry)
}

func checkMissingRequiredParams(opts channelAddOptions, params []config.ChannelParam) []string {
	var missing []string
	for _, param := range params {
		if !hasParamValue(opts, param.Name) {
			missing = append(missing, param.Name)
		}
	}
	return missing
}

func hasParamValue(opts channelAddOptions, paramName string) bool {
	switch paramName {
	case "token":
		return strings.TrimSpace(opts.Token) != ""
	case "bot-id":
		return strings.TrimSpace(opts.BotID) != ""
	case "secret":
		return strings.TrimSpace(opts.Secret) != ""
	default:
		return false
	}
}

func setParamValue(opts *channelAddOptions, paramName, value string) {
	switch paramName {
	case "token":
		opts.Token = value
	case "bot-id":
		opts.BotID = value
	case "secret":
		opts.Secret = value
	case "name":
		opts.Name = value
	case "role":
		opts.Role = value
	case "agent":
		opts.Agent = value
	case "user-id":
		opts.UserID = value
	case "api-base-url":
		opts.APIBaseURL = value
	}
}

func saveChannelConfig(opts channelAddOptions, entry config.ChannelCatalogEntry) error {
	resolvedPath, err := config.ResolveConfigPath(cfgFile)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	if _, err := config.EnsureInitialized(resolvedPath); err != nil {
		return fmt.Errorf("initialize config: %w", err)
	}

	cfg, err := config.Load(resolvedPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.Channels == nil {
		cfg.Channels = make(config.ChannelsConfig)
	}

	channelCfg := config.ChannelConfig{
		Type:       entry.Type,
		Name:       opts.Name,
		Role:       opts.Role,
		Agent:      opts.Agent,
		Token:      opts.Token,
		UserID:     opts.UserID,
		APIBaseURL: opts.APIBaseURL,
		BotID:      opts.BotID,
		Secret:     opts.Secret,
	}

	if channelCfg.Agent == "" {
		channelCfg.Agent = cfg.DefaultAgent
	}

	cfg.Channels[opts.ChannelID] = channelCfg

	outData, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(resolvedPath, outData, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("\n✓ Saved %s channel: %s\n", entry.Label, opts.ChannelID)

	if entry.Type == "wecom" {
		fmt.Println()
		fmt.Println("Note: WeCom AI Bot uses WebSocket connection, no public IP required.")
		fmt.Println("      Start the gateway to activate the connection.")
	}

	return nil
}

func channelRemove(channelID string) error {
	resolvedPath, err := config.ResolveConfigPath(cfgFile)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := config.Load(resolvedPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if _, ok := cfg.Channels[channelID]; !ok {
		return fmt.Errorf("channel not found: %s", channelID)
	}
	delete(cfg.Channels, channelID)
	outData, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(resolvedPath, outData, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	fmt.Printf("✓ Removed channel: %s\n", channelID)
	return nil
}

// initLoggerFromEnv 从环境变量初始化日志配置
func initLoggerFromEnv() {
	// 解析日志级别（优先使用环境变量）
	level := logger.INFO
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		level = logger.ParseLevel(envLevel)
	}

	// JSON 模式（默认使用文本格式，除非环境变量指定）
	jsonMode := false
	if envJSON := os.Getenv("LOG_JSON_MODE"); envJSON == "true" || envJSON == "1" {
		jsonMode = true
	}

	// 彩色输出（文本格式默认启用）
	color := true
	if envColor := os.Getenv("LOG_COLOR"); envColor == "false" || envColor == "0" {
		color = false
	}

	logger.Init(
		logger.WithLevel(level),
		logger.WithJSONMode(jsonMode),
		logger.WithColor(color),
		logger.WithService("evoduck"),
	)

	// 默认启用文件日志（data/logs/ 目录）
	logDir := os.Getenv("LOG_DIR")
	if logDir == "" {
		defaultLogsDir, err := config.DefaultLogsDir()
		if err != nil {
			logDir = "data/logs"
		} else {
			logDir = defaultLogsDir
		}
	} else {
		logDir = logDir + "/logs"
	}
	logger.SetFileOutputDir(logDir)
}
