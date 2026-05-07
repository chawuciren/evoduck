package builtin

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed */SKILL.md */skill.json
var builtinSkillsFS embed.FS

// EnsureBuiltinSkills copies built-in skill files to the shared skills directory.
// Existing files are NOT overwritten so user modifications are preserved.
func EnsureBuiltinSkills(sharedSkillsDir string) error {
	return copyEmbeddedDir(builtinSkillsFS, ".", sharedSkillsDir)
}

func copyEmbeddedDir(efs embed.FS, root, destDir string) error {
	return fs.WalkDir(efs, root, func(path string, d fs.DirEntry, err error) error {
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

		destPath := filepath.Join(destDir, rel)

		if _, statErr := os.Stat(destPath); statErr == nil {
			return nil
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}

		data, err := efs.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(destPath, data, 0o644)
	})
}
