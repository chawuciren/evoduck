package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultRepo = "chawuciren/evoduck"

type archiveKind string

const (
	archiveZip   archiveKind = "zip"
	archiveTarGz archiveKind = "tar.gz"
)

type Options struct {
	Repo           string
	Version        string
	InstallDir     string
	CurrentVersion string
	Force          bool
	CheckOnly      bool
	RefreshService bool
	RestartService bool
	HTTPClient     *http.Client // 可选：自定义 HTTP 客户端（用于代理）
}

type Result struct {
	CurrentVersion string
	TargetVersion  string
	AssetName      string
	InstallPath    string
	Updated        bool
	Restarted      bool
	Pending        bool
}

type latestRelease struct {
	TagName string `json:"tag_name"`
}

func Run(ctx context.Context, opts Options) (Result, error) {
	if strings.TrimSpace(opts.Repo) == "" {
		opts.Repo = defaultRepo
	}
	if strings.TrimSpace(opts.Version) == "" {
		opts.Version = strings.TrimSpace(os.Getenv("EVODUCK_VERSION"))
	}
	if strings.TrimSpace(opts.Version) == "" {
		opts.Version = "latest"
	}
	if strings.TrimSpace(opts.CurrentVersion) == "" {
		opts.CurrentVersion = "unknown"
	}

	assetName, kind, err := DetectAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return Result{}, err
	}
	installPath, err := ResolveInstallPath(opts)
	if err != nil {
		return Result{}, err
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	result := Result{
		CurrentVersion: opts.CurrentVersion,
		TargetVersion:  opts.Version,
		AssetName:      assetName,
		InstallPath:    installPath,
	}

	targetVersion := opts.Version
	if opts.CheckOnly || opts.Version == "latest" {
		latest, err := fetchLatestVersion(ctx, opts.Repo, httpClient)
		if err != nil {
			if opts.CheckOnly {
				return result, err
			}
		} else if latest != "" {
			targetVersion = latest
			result.TargetVersion = latest
		}
	}

	if !opts.Force && !isUnknownVersion(opts.CurrentVersion) && sameVersion(opts.CurrentVersion, targetVersion) {
		return result, nil
	}
	if opts.CheckOnly {
		return result, nil
	}

	tempDir, err := os.MkdirTemp("", "evoduck-update-*")
	if err != nil {
		return result, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	archivePath := filepath.Join(tempDir, assetName)
	if err := Download(ctx, downloadURL(opts.Repo, opts.Version, assetName), archivePath, httpClient); err != nil {
		return result, err
	}
	newBinary, err := ExtractBinary(archivePath, kind, binaryName(runtime.GOOS), tempDir)
	if err != nil {
		return result, err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(newBinary, 0o755); err != nil {
			return result, fmt.Errorf("chmod extracted binary: %w", err)
		}
	}

	newPath := installPath + ".new"
	if err := copyFile(newBinary, newPath, 0o755); err != nil {
		return result, fmt.Errorf("stage new binary: %w", err)
	}

	if isCurrentExecutable(installPath) {
		if err := startHelper(installPath, newPath, opts.RefreshService, opts.RestartService); err != nil {
			return result, err
		}
		result.Pending = true
		result.Updated = true
		result.Restarted = opts.RestartService
		return result, nil
	}

	if err := ReplaceBinary(newPath, installPath); err != nil {
		return result, err
	}
	result.Updated = true
	result.Restarted = opts.RestartService
	return result, nil
}

func DetectAsset(goos, goarch string) (string, archiveKind, error) {
	arch := ""
	switch goarch {
	case "amd64", "arm64":
		arch = goarch
	default:
		return "", "", fmt.Errorf("unsupported architecture: %s", goarch)
	}

	switch goos {
	case "windows":
		return fmt.Sprintf("evoduck-windows-%s.zip", arch), archiveZip, nil
	case "linux":
		return fmt.Sprintf("evoduck-linux-%s.tar.gz", arch), archiveTarGz, nil
	case "darwin":
		return fmt.Sprintf("evoduck-darwin-%s.tar.gz", arch), archiveTarGz, nil
	default:
		return "", "", fmt.Errorf("unsupported operating system: %s", goos)
	}
}

func ResolveInstallPath(opts Options) (string, error) {
	installDir := strings.TrimSpace(opts.InstallDir)
	if installDir == "" {
		installDir = strings.TrimSpace(os.Getenv("EVODUCK_INSTALL_DIR"))
	}
	if installDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		installDir = filepath.Join(home, ".local", "bin")
	}
	return filepath.Join(installDir, binaryName(runtime.GOOS)), nil
}

func Download(ctx context.Context, url string, dest string, httpClient *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download %s: unexpected HTTP status %s", url, resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create download dir: %w", err)
	}
	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}
	return nil
}

func ExtractBinary(archive string, kind archiveKind, binaryName string, destDir string) (string, error) {
	switch kind {
	case archiveZip:
		return extractZipBinary(archive, binaryName, destDir)
	case archiveTarGz:
		return extractTarGzBinary(archive, binaryName, destDir)
	default:
		return "", fmt.Errorf("unsupported archive kind: %s", kind)
	}
}

func ReplaceBinary(src string, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create install dir: %w", err)
	}
	backup := dst + ".bak"
	_ = os.Remove(backup)

	hadExisting := false
	if _, err := os.Stat(dst); err == nil {
		hadExisting = true
		if err := os.Rename(dst, backup); err != nil {
			return fmt.Errorf("move existing binary to backup: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect existing binary: %w", err)
	}

	if err := os.Rename(src, dst); err != nil {
		if hadExisting {
			_ = os.Rename(backup, dst)
		}
		return fmt.Errorf("install new binary: %w", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(dst, 0o755)
	}
	_ = os.Remove(backup)
	return nil
}

func fetchLatestVersion(ctx context.Context, repo string, httpClient *http.Client) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create latest release request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("query latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("query latest release: unexpected HTTP status %s", resp.Status)
	}
	var release latestRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("decode latest release: %w", err)
	}
	return strings.TrimSpace(release.TagName), nil
}

func downloadURL(repo string, version string, assetName string) string {
	if version == "latest" {
		return fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repo, assetName)
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, version, assetName)
}

func binaryName(goos string) string {
	if goos == "windows" {
		return "evoduck.exe"
	}
	return "evoduck"
}

func isUnknownVersion(version string) bool {
	v := strings.TrimSpace(strings.ToLower(version))
	return v == "" || v == "dev" || v == "unknown"
}

func sameVersion(a, b string) bool {
	return strings.TrimPrefix(strings.TrimSpace(a), "v") == strings.TrimPrefix(strings.TrimSpace(b), "v")
}

func extractZipBinary(archive string, binaryName string, destDir string) (string, error) {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return "", fmt.Errorf("open zip archive: %w", err)
	}
	defer r.Close()
	for _, f := range r.File {
		if filepath.Base(f.Name) != binaryName {
			continue
		}
		in, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("open %s in archive: %w", binaryName, err)
		}
		defer in.Close()
		outPath := filepath.Join(destDir, binaryName)
		if err := writeFileFromReader(outPath, in, 0o755); err != nil {
			return "", err
		}
		return outPath, nil
	}
	return "", fmt.Errorf("archive does not contain %s", binaryName)
}

func extractTarGzBinary(archive string, binaryName string, destDir string) (string, error) {
	file, err := os.Open(archive)
	if err != nil {
		return "", fmt.Errorf("open tar.gz archive: %w", err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", fmt.Errorf("open gzip stream: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read tar entry: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != binaryName {
			continue
		}
		outPath := filepath.Join(destDir, binaryName)
		if err := writeFileFromReader(outPath, tr, 0o755); err != nil {
			return "", err
		}
		return outPath, nil
	}
	return "", fmt.Errorf("archive does not contain %s", binaryName)
}

func writeFileFromReader(path string, r io.Reader, mode os.FileMode) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create extracted binary: %w", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, r); err != nil {
		return fmt.Errorf("write extracted binary: %w", err)
	}
	return nil
}

func copyFile(src string, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func isCurrentExecutable(path string) bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return canonicalPath(exe) == canonicalPath(path)
}

func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return filepath.Clean(path)
}

func startHelper(target string, staged string, refreshService bool, restartService bool) error {
	if runtime.GOOS == "windows" {
		return startWindowsHelper(target, staged, refreshService, restartService)
	}
	return startUnixHelper(target, staged)
}

func startWindowsHelper(target string, staged string, refreshService bool, restartService bool) error {
	scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("evoduck-updater-%d.ps1", os.Getpid()))
	serviceBlock := ""
	if refreshService {
		serviceBlock += "& $target install\n"
	}
	if restartService {
		serviceBlock += "& $target start\n"
	}
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$parent = %d
$target = %s
$staged = %s
$backup = "$target.bak"
while (Get-Process -Id $parent -ErrorAction SilentlyContinue) { Start-Sleep -Milliseconds 200 }
if (Test-Path $backup) { Remove-Item -Force $backup }
if (Test-Path $target) { Rename-Item -Path $target -NewName ([System.IO.Path]::GetFileName($backup)) -Force }
Move-Item -Path $staged -Destination $target -Force
if (Test-Path $backup) { Remove-Item -Force $backup }
%sRemove-Item -Force $MyInvocation.MyCommand.Path
`, os.Getpid(), psQuote(target), psQuote(staged), serviceBlock)
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return fmt.Errorf("write updater helper: %w", err)
	}
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start updater helper: %w", err)
	}
	return cmd.Process.Release()
}

func startUnixHelper(target string, staged string) error {
	scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("evoduck-updater-%d.sh", os.Getpid()))
	script := fmt.Sprintf(`#!/bin/sh
set -eu
parent=%d
target=%s
staged=%s
backup="$target.bak"
while kill -0 "$parent" 2>/dev/null; do sleep 0.2; done
rm -f "$backup"
if [ -e "$target" ]; then mv "$target" "$backup"; fi
mv "$staged" "$target"
chmod 0755 "$target"
rm -f "$backup"
rm -f "$0"
`, os.Getpid(), shQuote(target), shQuote(staged))
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return fmt.Errorf("write updater helper: %w", err)
	}
	cmd := exec.Command("sh", scriptPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start updater helper: %w", err)
	}
	return cmd.Process.Release()
}

func psQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func shQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func WaitForReplacement(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path + ".new"); errors.Is(err, os.ErrNotExist) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
