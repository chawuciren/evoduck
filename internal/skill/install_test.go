package skill

import (
	"archive/zip"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeInstallSkill(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	content := `---
name: ` + name + `
description: Test skill
license: MIT
compatibility: evoduck
---
Instructions.`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	manifest := `{"schemaVersion":"1.0","name":"` + name + `","version":"0.1.0","entry":"SKILL.md"}`
	if err := os.WriteFile(filepath.Join(dir, "skill.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir
}

func TestInstallLocalDirectoryWritesSkillAndLock(t *testing.T) {
	sourceRoot := t.TempDir()
	source := writeInstallSkill(t, sourceRoot, "local-skill")
	targetRoot := filepath.Join(t.TempDir(), "skills")
	lockPath := filepath.Join(filepath.Dir(targetRoot), "skills.lock.json")

	result, err := InstallLocal(InstallOptions{Source: source, TargetRoot: targetRoot, LockPath: lockPath})
	if err != nil {
		t.Fatalf("InstallLocal returned error: %v", err)
	}
	if result.Name != "local-skill" {
		t.Fatalf("expected local-skill, got %q", result.Name)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "local-skill", "SKILL.md")); err != nil {
		t.Fatalf("expected installed SKILL.md: %v", err)
	}

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	var lock LockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatalf("parse lock: %v", err)
	}
	if got := lock.Skills["local-skill"].SourceType; got != "directory" {
		t.Fatalf("expected directory source type, got %q", got)
	}
	if got := lock.Skills["local-skill"].Version; got != "0.1.0" {
		t.Fatalf("expected manifest version, got %q", got)
	}
}

func TestInstallLocalZipWritesSkillAndSHA(t *testing.T) {
	sourceRoot := t.TempDir()
	source := writeInstallSkill(t, sourceRoot, "zip-skill")
	zipPath := filepath.Join(t.TempDir(), "zip-skill.zip")
	writeZip(t, zipPath, source, "zip-skill")
	targetRoot := filepath.Join(t.TempDir(), "skills")
	lockPath := filepath.Join(filepath.Dir(targetRoot), "skills.lock.json")

	_, err := InstallLocal(InstallOptions{Source: zipPath, TargetRoot: targetRoot, LockPath: lockPath})
	if err != nil {
		t.Fatalf("InstallLocal zip returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "zip-skill", "SKILL.md")); err != nil {
		t.Fatalf("expected installed zip SKILL.md: %v", err)
	}
	lock, err := readLock(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	record := lock.Skills["zip-skill"]
	if record.SourceType != "zip" || record.SHA256 == "" {
		t.Fatalf("expected zip lock with sha, got %#v", record)
	}
}

func TestInstallZipRejectsPathTraversal(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "bad.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../evil/SKILL.md")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := w.Write([]byte("evil")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	_, err = InstallLocal(InstallOptions{Source: zipPath, TargetRoot: filepath.Join(t.TempDir(), "skills"), LockPath: filepath.Join(t.TempDir(), "skills.lock.json")})
	if err == nil || !strings.Contains(err.Error(), "unsafe zip path") {
		t.Fatalf("expected unsafe zip path error, got %v", err)
	}
}

func TestRemoveInstalledDeletesSkillAndLockEntry(t *testing.T) {
	sourceRoot := t.TempDir()
	source := writeInstallSkill(t, sourceRoot, "remove-skill")
	targetRoot := filepath.Join(t.TempDir(), "skills")
	lockPath := filepath.Join(filepath.Dir(targetRoot), "skills.lock.json")
	if _, err := InstallLocal(InstallOptions{Source: source, TargetRoot: targetRoot, LockPath: lockPath}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := RemoveInstalled("remove-skill", targetRoot, lockPath); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "remove-skill")); !os.IsNotExist(err) {
		t.Fatalf("expected skill dir removed, got %v", err)
	}
	lock, err := readLock(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if _, ok := lock.Skills["remove-skill"]; ok {
		t.Fatal("expected lock entry removed")
	}
}

func TestInstallGitWithPathAndRefWritesCommitAndPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "config", "user.email", "test@example.com")
	runTestGit(t, repo, "config", "user.name", "Test User")
	writeInstallSkill(t, filepath.Join(repo, "skills"), "git-skill")
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "commit", "-m", "add skill")
	runTestGit(t, repo, "tag", "v0.1.0")
	commit := strings.TrimSpace(runTestGitOutput(t, repo, "rev-parse", "HEAD"))

	targetRoot := filepath.Join(t.TempDir(), "skills")
	lockPath := filepath.Join(filepath.Dir(targetRoot), "skills.lock.json")
	result, err := InstallGit(GitInstallOptions{
		URL:        repo,
		Ref:        "v0.1.0",
		Path:       "skills/git-skill",
		TargetRoot: targetRoot,
		LockPath:   lockPath,
	})
	if err != nil {
		t.Fatalf("InstallGit returned error: %v", err)
	}
	if result.Name != "git-skill" {
		t.Fatalf("expected git-skill, got %q", result.Name)
	}
	lock, err := readLock(lockPath)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	record := lock.Skills["git-skill"]
	if record.SourceType != "git" || record.Ref != "v0.1.0" || record.Commit != commit || record.Path != "skills/git-skill" {
		t.Fatalf("unexpected git lock record: %#v", record)
	}
}

func TestPackUsesManifestFilesAndOutputsSHA(t *testing.T) {
	sourceRoot := t.TempDir()
	source := writeInstallSkill(t, sourceRoot, "pack-skill")
	if err := os.MkdirAll(filepath.Join(source, "examples"), 0o755); err != nil {
		t.Fatalf("mkdir examples: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "examples", "sample.md"), []byte("sample"), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "ignored.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("write ignored: %v", err)
	}
	manifest := `{"schemaVersion":"1.0","name":"pack-skill","version":"0.1.0","entry":"SKILL.md","files":["SKILL.md","skill.json","examples/**"]}`
	if err := os.WriteFile(filepath.Join(source, "skill.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	output := filepath.Join(t.TempDir(), "pack-skill.zip")
	result, err := Pack(PackOptions{SourceDir: source, Output: output})
	if err != nil {
		t.Fatalf("Pack returned error: %v", err)
	}
	if result.Name != "pack-skill" || result.SHA256 == "" {
		t.Fatalf("unexpected pack result: %#v", result)
	}
	entries := zipEntries(t, output)
	for _, expected := range []string{"pack-skill/SKILL.md", "pack-skill/skill.json", "pack-skill/examples/sample.md"} {
		if !entries[expected] {
			t.Fatalf("expected zip entry %s, got %#v", expected, entries)
		}
	}
	if entries["pack-skill/ignored.txt"] {
		t.Fatalf("did not expect ignored file in zip")
	}
}

func TestPackDefaultOutputUsesVersion(t *testing.T) {
	sourceRoot := t.TempDir()
	source := writeInstallSkill(t, sourceRoot, "default-pack")
	result, err := Pack(PackOptions{SourceDir: source})
	if err != nil {
		t.Fatalf("Pack returned error: %v", err)
	}
	if filepath.Base(result.Output) != "default-pack-0.1.0.zip" {
		t.Fatalf("unexpected default output: %s", result.Output)
	}
	if _, err := os.Stat(result.Output); err != nil {
		t.Fatalf("expected output zip: %v", err)
	}
}

func writeZip(t *testing.T, zipPath, sourceDir, rootName string) {
	t.Helper()
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(f)
	err = filepath.WalkDir(sourceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		w, err := zw.Create(filepath.ToSlash(filepath.Join(rootName, rel)))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	if err != nil {
		t.Fatalf("walk zip source: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
}

func runTestGit(t *testing.T, workdir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = workdir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v: %s", strings.Join(args, " "), err, string(output))
	}
}

func runTestGitOutput(t *testing.T, workdir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = workdir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
	}
	return string(output)
}

func zipEntries(t *testing.T, path string) map[string]bool {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()
	entries := map[string]bool{}
	for _, f := range r.File {
		entries[f.Name] = true
	}
	return entries
}
