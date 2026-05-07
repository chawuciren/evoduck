package plugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type ManagedProcess struct {
	pluginID      string
	definition    config.PluginDef
	command       *exec.Cmd
	startedAt     time.Time
	restartCount  int
	lastRestartAt time.Time
	token         string
}

type ProcessManager struct {
	mu        sync.Mutex
	processes map[string]*ManagedProcess
	wsURL     string
	tokens    map[string]string
	decider   *proxy.Decider
}

func NewProcessManager(wsURL string, decider *proxy.Decider) *ProcessManager {
	return &ProcessManager{
		processes: make(map[string]*ManagedProcess),
		tokens:    make(map[string]string),
		wsURL:     wsURL,
		decider:   decider,
	}
}

func (m *ProcessManager) StartAll(ctx context.Context, defs map[string]config.PluginDef) error {
	for pluginID, def := range defs {
		if !def.Enabled || def.Type != "local" {
			continue
		}
		if err := m.Start(ctx, pluginID, def); err != nil {
			return err
		}
	}
	return nil
}

func (m *ProcessManager) Start(ctx context.Context, pluginID string, def config.PluginDef) error {
	if len(def.Command) == 0 {
		return fmt.Errorf("plugin %s command is empty", pluginID)
	}

	token := def.Token
	if token == "" {
		return fmt.Errorf("plugin %s token is empty", pluginID)
	}

	cmd := exec.CommandContext(ctx, def.Command[0], def.Command[1:]...)

	// Build environment with proxy decision for this plugin
	decision := m.decider.ForPlugin(pluginID)
	baseEnv := proxy.BuildChildEnv(false) // Start with clean env
	cmd.Env = m.decider.BuildSubprocessEnv(decision.UseProxy, decision.ProxyType, baseEnv)
	cmd.Env = append(cmd.Env,
		fmt.Sprintf("EVODUCK_PLUGIN_ID=%s", pluginID),
		fmt.Sprintf("EVODUCK_PLUGIN_TOKEN=%s", token),
		fmt.Sprintf("EVODUCK_WS_URL=%s", m.wsURL),
	)
	for key, value := range def.Environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start plugin %s: %w", pluginID, err)
	}

	mp := &ManagedProcess{
		pluginID:      pluginID,
		definition:    def,
		command:       cmd,
		startedAt:     time.Now(),
		lastRestartAt: time.Now(),
		token:         token,
	}

	m.mu.Lock()
	m.processes[pluginID] = mp
	m.tokens[pluginID] = token
	m.mu.Unlock()

	logger.Info("Plugin process started", logger.Fields{
		"plugin_id": pluginID,
		"pid":       cmd.Process.Pid,
	})

	go m.watch(pluginID)
	return nil
}

func (m *ProcessManager) Tokens() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cloned := make(map[string]string, len(m.tokens))
	for pluginID, token := range m.tokens {
		cloned[pluginID] = token
	}
	return cloned
}

func (m *ProcessManager) Shutdown(ctx context.Context) {
	m.mu.Lock()
	processes := make([]*ManagedProcess, 0, len(m.processes))
	for _, process := range m.processes {
		processes = append(processes, process)
	}
	m.mu.Unlock()

	for _, process := range processes {
		if process.command == nil || process.command.Process == nil {
			continue
		}
		_ = process.command.Process.Kill()
		_, _ = process.command.Process.Wait()
		logger.Info("Plugin process stopped", logger.Fields{"plugin_id": process.pluginID})
	}

	select {
	case <-ctx.Done():
	default:
	}
}

func (m *ProcessManager) watch(pluginID string) {
	m.mu.Lock()
	process, ok := m.processes[pluginID]
	m.mu.Unlock()
	if !ok || process.command == nil {
		return
	}

	err := process.command.Wait()
	if err != nil {
		logger.Warn("Plugin process exited", logger.Fields{
			"plugin_id": pluginID,
			"error":     err.Error(),
		})
	}
}

func randomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
