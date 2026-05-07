package skill

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Manifest struct {
	SchemaVersion string   `json:"schemaVersion"`
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Description   string   `json:"description"`
	License       string   `json:"license"`
	Compatibility []string `json:"compatibility"`
	Entry         string   `json:"entry"`
	Files         []string `json:"files"`
}

type LockFile struct {
	Version int                   `json:"version"`
	Skills  map[string]LockRecord `json:"skills"`
}

type LockRecord struct {
	Version     string `json:"version,omitempty"`
	SourceType  string `json:"sourceType"`
	Source      string `json:"source"`
	Ref         string `json:"ref,omitempty"`
	Commit      string `json:"commit,omitempty"`
	Path        string `json:"path,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	InstalledAt string `json:"installedAt"`
}

type InstallOptions struct {
	Source     string
	TargetRoot string
	LockPath   string
	Force      bool
}

type GitInstallOptions struct {
	URL        string
	Ref        string
	Path       string
	TargetRoot string
	LockPath   string
	Force      bool
}

type InstallResult struct {
	Name       string
	TargetPath string
	LockRecord LockRecord
}

type PackOptions struct {
	SourceDir string
	Output    string
}

type PackResult struct {
	Name   string
	Output string
	SHA256 string
}

func InstallLocal(options InstallOptions) (*InstallResult, error) {
	if strings.TrimSpace(options.Source) == "" {
		return nil, fmt.Errorf("source is required")
	}
	if strings.TrimSpace(options.TargetRoot) == "" {
		return nil, fmt.Errorf("target root is required")
	}

	info, err := os.Stat(options.Source)
	if err != nil {
		return nil, fmt.Errorf("stat source: %w", err)
	}

	if info.IsDir() {
		return installDirectory(options, options.Source, "directory", "")
	}

	if strings.EqualFold(filepath.Ext(options.Source), ".zip") {
		return installZip(options)
	}

	return nil, fmt.Errorf("unsupported skill source: use a directory or .zip file")
}

func InstallGit(options GitInstallOptions) (*InstallResult, error) {
	if strings.TrimSpace(options.URL) == "" {
		return nil, fmt.Errorf("git URL is required")
	}
	if strings.TrimSpace(options.TargetRoot) == "" {
		return nil, fmt.Errorf("target root is required")
	}
	tmp, err := os.MkdirTemp("", "evoduck-skill-git-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	repoDir := filepath.Join(tmp, "repo")
	args := []string{"clone", "--depth", "1"}
	if strings.TrimSpace(options.Ref) != "" {
		args = append(args, "--branch", options.Ref)
	}
	args = append(args, options.URL, repoDir)
	if err := runGit(tmp, args...); err != nil {
		return nil, err
	}
	commit, err := gitCommit(repoDir)
	if err != nil {
		return nil, err
	}

	skillRoot, err := resolveGitSkillRoot(repoDir, options.Path)
	if err != nil {
		return nil, err
	}
	installOptions := InstallOptions{
		Source:     options.URL,
		TargetRoot: options.TargetRoot,
		LockPath:   "",
		Force:      options.Force,
	}
	result, err := installDirectory(installOptions, skillRoot, "git", "")
	if err != nil {
		return nil, err
	}
	record := result.LockRecord
	record.Source = options.URL
	record.Ref = options.Ref
	record.Commit = commit
	record.Path = filepath.ToSlash(strings.TrimSpace(options.Path))
	if record.Path == "" {
		rel, relErr := filepath.Rel(repoDir, skillRoot)
		if relErr == nil && rel != "." {
			record.Path = filepath.ToSlash(rel)
		}
	}
	if err := updateLock(options.LockPath, result.Name, record); err != nil {
		return nil, err
	}
	result.LockRecord = record
	return result, nil
}

func VerifyPackage(root string) (*Skill, *Manifest, error) {
	root = filepath.Clean(root)
	data, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil {
		return nil, nil, fmt.Errorf("read SKILL.md: %w", err)
	}

	fmName, err := skillNameFromFrontmatter(string(data))
	if err != nil {
		return nil, nil, err
	}
	loader := NewLoader("", "")
	s, err := loader.parseSkill(filepath.Base(root), filepath.Join(root, "SKILL.md"), string(data))
	if err != nil {
		return nil, nil, err
	}
	if fmName != s.Name {
		return nil, nil, fmt.Errorf("skill name mismatch: frontmatter=%q parsed=%q", fmName, s.Name)
	}

	manifest, err := readManifest(root)
	if err != nil {
		return nil, nil, err
	}
	if manifest != nil {
		if manifest.Name != "" && manifest.Name != s.Name {
			return nil, nil, fmt.Errorf("skill.json name %q must match skill name %q", manifest.Name, s.Name)
		}
		entry := strings.TrimSpace(manifest.Entry)
		if entry == "" {
			entry = "SKILL.md"
		}
		if filepath.IsAbs(entry) || strings.Contains(filepath.ToSlash(entry), "../") {
			return nil, nil, fmt.Errorf("skill.json entry must be a safe relative path")
		}
		if _, err := os.Stat(filepath.Join(root, entry)); err != nil {
			return nil, nil, fmt.Errorf("skill.json entry %q not found: %w", entry, err)
		}
	}

	return s, manifest, nil
}

func RemoveInstalled(name, targetRoot, lockPath string) error {
	if err := validateSkillName(name, name); err != nil {
		return err
	}
	targetPath := filepath.Join(targetRoot, name)
	if err := os.RemoveAll(targetPath); err != nil {
		return fmt.Errorf("remove skill: %w", err)
	}
	lock, err := readLock(lockPath)
	if err != nil {
		return err
	}
	delete(lock.Skills, name)
	return writeLock(lockPath, lock)
}

func Pack(options PackOptions) (*PackResult, error) {
	root := filepath.Clean(options.SourceDir)
	s, manifest, err := VerifyPackage(root)
	if err != nil {
		return nil, err
	}
	output := strings.TrimSpace(options.Output)
	if output == "" {
		version := ""
		if manifest != nil && strings.TrimSpace(manifest.Version) != "" {
			version = "-" + strings.TrimSpace(manifest.Version)
		}
		output = filepath.Join(filepath.Dir(root), s.Name+version+".zip")
	}
	files, err := packageFiles(root, manifest)
	if err != nil {
		return nil, err
	}
	if err := writePackageZip(root, s.Name, output, files); err != nil {
		return nil, err
	}
	sha, err := fileSHA256(output)
	if err != nil {
		return nil, err
	}
	return &PackResult{Name: s.Name, Output: output, SHA256: sha}, nil
}

func ListInstalled(targetRoot string) ([]*Skill, error) {
	entries, err := os.ReadDir(targetRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	loader := NewLoader("", "")
	var skills []*Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(targetRoot, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		s, err := loader.parseSkill(entry.Name(), path, string(data))
		if err != nil {
			continue
		}
		skills = append(skills, s)
	}
	return skills, nil
}

func installDirectory(options InstallOptions, root string, sourceType string, sha string) (*InstallResult, error) {
	s, manifest, err := VerifyPackage(root)
	if err != nil {
		return nil, err
	}
	targetPath := filepath.Join(options.TargetRoot, s.Name)
	if _, err := os.Stat(targetPath); err == nil && !options.Force {
		return nil, fmt.Errorf("skill %q already exists at %s; use --force to overwrite", s.Name, targetPath)
	}
	if options.Force {
		if err := os.RemoveAll(targetPath); err != nil {
			return nil, fmt.Errorf("remove existing skill: %w", err)
		}
	}
	if err := copyDir(root, targetPath); err != nil {
		return nil, err
	}

	record := LockRecord{
		SourceType:  sourceType,
		Source:      options.Source,
		SHA256:      sha,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}
	if manifest != nil {
		record.Version = manifest.Version
	}
	if err := updateLock(options.LockPath, s.Name, record); err != nil {
		return nil, err
	}
	return &InstallResult{Name: s.Name, TargetPath: targetPath, LockRecord: record}, nil
}

func installZip(options InstallOptions) (*InstallResult, error) {
	sha, err := fileSHA256(options.Source)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "evoduck-skill-zip-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	extractDir := filepath.Join(tmp, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return nil, err
	}
	if err := unzipSafe(options.Source, extractDir); err != nil {
		return nil, err
	}
	root, err := findExtractedSkillRoot(extractDir)
	if err != nil {
		return nil, err
	}
	return installDirectory(options, root, "zip", sha)
}

func skillNameFromFrontmatter(content string) (string, error) {
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return "", fmt.Errorf("SKILL.md must include YAML frontmatter")
	}
	var fm struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		return "", fmt.Errorf("parse frontmatter: %w", err)
	}
	if fm.Name == "" {
		return "", fmt.Errorf("skill name is required")
	}
	return fm.Name, nil
}

func readManifest(root string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, "skill.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse skill.json: %w", err)
	}
	return &manifest, nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == ".git" || strings.HasPrefix(filepath.ToSlash(rel), ".git/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func packageFiles(root string, manifest *Manifest) ([]string, error) {
	if manifest == nil || len(manifest.Files) == 0 {
		return allPackageFiles(root)
	}
	seen := map[string]bool{}
	var files []string
	for _, pattern := range manifest.Files {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if filepath.IsAbs(pattern) || strings.Contains(pattern, "../") || strings.HasPrefix(pattern, "../") {
			return nil, fmt.Errorf("unsafe file pattern in skill.json: %s", pattern)
		}
		matches, err := matchPackagePattern(root, pattern)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			if !seen[match] {
				seen[match] = true
				files = append(files, match)
			}
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("skill.json files did not match any files")
	}
	return files, nil
}

func allPackageFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	return files, err
}

func matchPackagePattern(root string, pattern string) ([]string, error) {
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		dir := filepath.Join(root, filepath.FromSlash(prefix))
		info, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		if !info.IsDir() {
			return nil, nil
		}
		var files []string
		err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
		return files, err
	}
	path := filepath.Join(root, filepath.FromSlash(pattern))
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if info.IsDir() {
		return nil, nil
	}
	return []string{pattern}, nil
}

func writePackageZip(root string, skillName string, output string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	out, err := os.Create(output)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	defer zw.Close()
	for _, rel := range files {
		if filepath.IsAbs(rel) || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
			return fmt.Errorf("unsafe package file path: %s", rel)
		}
		fullPath := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(fullPath)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(filepath.Join(skillName, rel))
		header.Method = zip.Deflate
		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		in, err := os.Open(fullPath)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, in)
		closeErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func unzipSafe(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	destReal, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	for _, f := range r.File {
		name := filepath.Clean(f.Name)
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(filepath.ToSlash(name), "../") || strings.Contains(filepath.ToSlash(name), "/../") {
			return fmt.Errorf("unsafe zip path: %s", f.Name)
		}
		target := filepath.Join(destReal, name)
		absTarget, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		if absTarget != destReal && !strings.HasPrefix(absTarget, destReal+string(os.PathSeparator)) {
			return fmt.Errorf("zip path escapes destination: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(absTarget, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(absTarget), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(absTarget, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.FileInfo().Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		closeErr := out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func findExtractedSkillRoot(root string) (string, error) {
	if _, err := os.Stat(filepath.Join(root, "SKILL.md")); err == nil {
		name, err := skillNameFromFile(filepath.Join(root, "SKILL.md"))
		if err != nil {
			return "", err
		}
		if filepath.Base(root) == name {
			return root, nil
		}
		normalized := filepath.Join(filepath.Dir(root), name)
		if err := os.Rename(root, normalized); err == nil {
			return normalized, nil
		}
		return root, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(candidate, "SKILL.md")); err == nil {
			matches = append(matches, candidate)
		}
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("zip must contain exactly one skill package, found %d", len(matches))
	}
	return matches[0], nil
}

func resolveGitSkillRoot(repoDir string, subPath string) (string, error) {
	if strings.TrimSpace(subPath) != "" {
		cleaned := filepath.Clean(subPath)
		if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(filepath.ToSlash(cleaned), "../") || strings.Contains(filepath.ToSlash(cleaned), "/../") {
			return "", fmt.Errorf("--path must be a safe relative path")
		}
		root := filepath.Join(repoDir, cleaned)
		absRepo, err := filepath.Abs(repoDir)
		if err != nil {
			return "", err
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return "", err
		}
		if absRoot != absRepo && !strings.HasPrefix(absRoot, absRepo+string(os.PathSeparator)) {
			return "", fmt.Errorf("--path escapes repository")
		}
		return root, nil
	}
	return findExtractedSkillRoot(repoDir)
}

func runGit(workdir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = workdir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func gitCommit(repoDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func skillNameFromFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return skillNameFromFrontmatter(string(data))
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func readLock(path string) (*LockFile, error) {
	lock := &LockFile{Version: 1, Skills: map[string]LockRecord{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return lock, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, lock); err != nil {
		return nil, err
	}
	if lock.Version == 0 {
		lock.Version = 1
	}
	if lock.Skills == nil {
		lock.Skills = map[string]LockRecord{}
	}
	return lock, nil
}

func updateLock(path, name string, record LockRecord) error {
	lock, err := readLock(path)
	if err != nil {
		return err
	}
	lock.Skills[name] = record
	return writeLock(path, lock)
}

func writeLock(path string, lock *LockFile) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
