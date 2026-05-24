package tui

import (
	"fmt"

	"github.com/AbandonwareDev/umbra/internal/config"
	"github.com/AbandonwareDev/umbra/internal/tui"
)

func Run() error {
	paths, err := config.ResolvePaths()
	if err != nil {
		return fmt.Errorf("resolving paths: %w", err)
	}

	return tui.Run(paths)
}
