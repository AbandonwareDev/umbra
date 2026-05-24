package tray

import (
	"fmt"

	"github.com/AbandonwareDev/umbra/internal/config"
	"github.com/AbandonwareDev/umbra/internal/tray"
)

func Run() error {
	paths, err := config.ResolvePaths()
	if err != nil {
		return fmt.Errorf("resolving paths: %w", err)
	}

	if err := config.EnsureRuntimeDir(paths); err != nil {
		return fmt.Errorf("creating runtime directory: %w", err)
	}

	tray.Run(paths)
	return nil
}
