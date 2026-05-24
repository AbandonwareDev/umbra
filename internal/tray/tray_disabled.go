//go:build notray

package tray

import "github.com/AbandonwareDev/umbra/internal/config"

func Run(paths *config.AppPaths) {
	// Tray disabled. Build without tags (default) to enable tray.
	// go build .                  → tray enabled
	// go build -tags notray       → tray disabled
}
