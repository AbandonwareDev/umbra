package tui

import (
	"fmt"

	"github.com/user/umbra/internal/config"
	"github.com/user/umbra/internal/tui"
)

func Run() error {
	paths, err := config.ResolvePaths()
	if err != nil {
		return fmt.Errorf("resolving paths: %w", err)
	}

	return tui.Run(paths)
}
