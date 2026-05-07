package scheduler

import (
	"path/filepath"

	"github.com/chawuciren/evoduck/pkg/config"
)

func DefaultStorePath(cfg *config.Config) string {
	base := cfg.DataDir
	if base == "" {
		defaultDataDir, err := config.DefaultDataDir()
		if err == nil {
			base = defaultDataDir
		} else {
			base = filepath.Join(".", "data")
		}
	}
	return filepath.Join(base, "scheduler", "tasks.jsonl")
}

func DefaultRunStoreDir(cfg *config.Config) string {
	base := cfg.DataDir
	if base == "" {
		defaultDataDir, err := config.DefaultDataDir()
		if err == nil {
			base = defaultDataDir
		} else {
			base = filepath.Join(".", "data")
		}
	}
	return filepath.Join(base, "scheduler")
}
