package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

func TestAgentPermissionsAdminDefaultsToUnrestrictedDirectories(t *testing.T) {
	workspace := t.TempDir()
	otherDir := t.TempDir()
	perms := NewAgentPermissions(models.RoleAdmin, workspace, config.AgentPermissionConfig{})

	if err := perms.CanAccessPath(otherDir); err != nil {
		t.Fatalf("expected admin default path access to be unrestricted: %v", err)
	}
}

func TestAgentPermissionsEmployeeDefaultsToWorkspace(t *testing.T) {
	workspace := t.TempDir()
	inside := filepath.Join(workspace, "docs", "note.txt")
	outside := t.TempDir()
	perms := NewAgentPermissions(models.RoleEmployee, workspace, config.AgentPermissionConfig{})

	if err := perms.CanAccessPath(inside); err != nil {
		t.Fatalf("expected workspace path to be allowed: %v", err)
	}
	if err := perms.CanAccessPath(outside); err == nil {
		t.Fatal("expected outside workspace path to be denied")
	}
}

func TestAgentPermissionsDirectoryOverrideReplacesDefault(t *testing.T) {
	workspace := t.TempDir()
	allowedDir := t.TempDir()
	perms := NewAgentPermissions(models.RoleEmployee, workspace, config.AgentPermissionConfig{
		AuthorizedDirectories: []string{allowedDir},
	})

	if err := perms.CanAccessPath(filepath.Join(allowedDir, "ok.txt")); err != nil {
		t.Fatalf("expected override directory to be allowed: %v", err)
	}
	if err := perms.CanAccessPath(filepath.Join(workspace, "still-denied.txt")); err == nil {
		t.Fatal("expected workspace to be denied after directory override")
	}
}

func TestAgentPermissionsToolOverrideReplacesDefaults(t *testing.T) {
	perms := NewAgentPermissions(models.RoleEmployee, t.TempDir(), config.AgentPermissionConfig{
		AuthorizedTools: []string{"file_read"},
	})

	if !perms.CanUseTool("file_read", true) {
		t.Fatal("expected explicitly authorized tool to be allowed")
	}
	if perms.CanUseTool("time", true) {
		t.Fatal("expected default-allowed tool to be denied when override is present")
	}
	if perms.CanUseTool("exec", false) {
		t.Fatal("expected non-listed tool to remain denied")
	}
}

func TestEmployeeFileToolsNeedExplicitKnowledgeDirectoryAccess(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	knowledgeDir := filepath.Join(dataDir, "knowledge")
	if err := os.MkdirAll(knowledgeDir, 0o755); err != nil {
		t.Fatalf("mkdir knowledge dir: %v", err)
	}
	target := filepath.Join(knowledgeDir, "ops", "runbook.md")

	defaultPerms := NewAgentPermissions(models.RoleEmployee, workspace, config.AgentPermissionConfig{})
	defaultWrite := NewFileWriteTool(defaultPerms)
	if _, err := defaultWrite.Execute(map[string]interface{}{"path": target, "content": "denied"}); err == nil {
		t.Fatal("expected employee without explicit knowledge directory access to be denied")
	}

	allowedPerms := NewAgentPermissions(models.RoleEmployee, workspace, config.AgentPermissionConfig{
		AuthorizedDirectories: []string{knowledgeDir},
	})
	allowedWrite := NewFileWriteTool(allowedPerms)
	if _, err := allowedWrite.Execute(map[string]interface{}{"path": target, "operation": "create", "content": "# Runbook\n"}); err != nil {
		t.Fatalf("expected explicitly authorized knowledge write to succeed: %v", err)
	}

	allowedEdit := NewFileEditTool(allowedPerms)
	if _, err := allowedEdit.Execute(map[string]interface{}{"path": target, "operation": "append", "content": "\nDetails."}); err != nil {
		t.Fatalf("expected explicitly authorized knowledge edit to succeed: %v", err)
	}
}
