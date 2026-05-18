package agent

import (
	"testing"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

func TestEmployeeAgentRegistersExecProcessAndSleepTools(t *testing.T) {
	workspace := t.TempDir()
	llmReg, err := llm.NewRegistry(config.LLMConfig{
		DefaultProvider: "stub",
		DefaultModel:    "stub-model",
		Providers:       map[string]config.ProviderConfig{},
	}, nil)
	if err != nil {
		t.Fatalf("new llm registry: %v", err)
	}
	if err := llmReg.RegisterDynamic("stub", &scheduleTestProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	mgr := NewManager(llmReg, workspace, workspace, config.BackendCallConfig{}, config.SessionToolConfig{}, config.MemoryConfig{}, nil, nil, nil)
	if err := mgr.Register("agent-employee-tools", config.AgentConfig{
		Workspace: workspace,
		Provider:  "stub",
		Model:     "stub-model",
		Role:      string(models.RoleEmployee),
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	ag, err := mgr.Get("agent-employee-tools")
	if err != nil {
		t.Fatalf("get employee agent: %v", err)
	}

	for _, name := range []string{"exec", "process", "sleep"} {
		if _, err := ag.Tools.Get(name); err != nil {
			t.Fatalf("expected tool %s for employee role: %v", name, err)
		}
	}
}
