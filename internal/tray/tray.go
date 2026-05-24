//go:build !notray

package tray

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AbandonwareDev/umbra/internal/config"
	"github.com/AbandonwareDev/umbra/internal/ipc"
	"github.com/AbandonwareDev/umbra/internal/types"
	"github.com/gogpu/systray"
)

// iconSize is the tray icon dimension (square).
const iconSize = 22

var (
	white       = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	transparent = color.RGBA{A: 0}
)

func Run(appPaths *config.AppPaths) {
	paths := appPaths

	icon := generateIcon()
	t := systray.New()
	t.SetIcon(icon)
	t.SetTemplateIcon(icon)
	t.SetTooltip("Umbra — starting...")

	// socketPath starts as the user-mode socket. updateMenu will fall back
	// to the root-mode socket if connecting fails.
	socketPath := paths.SocketPath

	var updateMenu func()
	updateMenu = func() {
		resp, err := ipc.SendRequest(socketPath, &ipc.Request{
			Action: ipc.ActionList,
		})
		if err != nil {
			// User-mode socket failed — try root-mode.
			if socketPath != config.RootSocketPath {
				resp, err = ipc.SendRequest(config.RootSocketPath, &ipc.Request{
					Action: ipc.ActionList,
				})
				if err == nil {
					socketPath = config.RootSocketPath
				}
			}
		}
		if err != nil {
			t.SetTooltip("Umbra — disconnected")
			t.SetMenu(buildMenu(nil, socketPath, nil))
			return
		}
		if !resp.Success {
			t.SetTooltip("Umbra — error")
			t.SetMenu(buildMenu(nil, socketPath, nil))
			return
		}

		running := 0
		for _, c := range resp.Configs {
			if c.Status == types.StatusRunning {
				running++
			}
		}
		t.SetTooltip(fmt.Sprintf("Umbra — %d/%d running", running, len(resp.Configs)))
		t.SetMenu(buildMenu(resp.Configs, socketPath, updateMenu))
	}

	// Start with a basic menu before the first refresh.
	t.SetMenu(buildMenu(nil, socketPath, nil))
	t.Show()

	// Refresh at boot so the menu is populated immediately.
	go func() {
		time.Sleep(500 * time.Millisecond)
		updateMenu()
	}()

	// Periodic refresh every 60s as a fallback for DEs that don't call
	// Activate/ContextMenu before showing the tray menu.
	go func() {
		for {
			time.Sleep(60 * time.Second)
			updateMenu()
		}
	}()

	t.OnClick(updateMenu)
	t.OnRightClick(updateMenu)

	// Handle SIGINT/SIGTERM for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		t.Remove()
		os.Exit(0)
	}()

	// Block on the tray event loop.
	t.Run()
}

// buildMenu creates a menu with config items and a Quit button.
// onToggle is called after each start/stop action so the menu refreshes.
func buildMenu(configs []types.VPNConfig, socketPath string, onToggle func()) *systray.Menu {
	menu := systray.NewMenu()

	if len(configs) == 0 {
		menu.Add("No VPN configs", nil).AddSeparator()
	} else {
		for _, c := range configs {
			cfg := c // capture
			label := fmt.Sprintf("  %s  %s", cfg.Name, statusLabel(cfg.Status))
			menu.AddCheckbox(label, cfg.Status == types.StatusRunning, func() {
				action := ipc.ActionStart
				if cfg.Status == types.StatusRunning {
					action = ipc.ActionStop
				}
				_, _ = ipc.SendRequest(socketPath, &ipc.Request{
					Action: action,
					Config: cfg.Name,
				})
				if onToggle != nil {
					onToggle()
				}
			})
		}
		menu.AddSeparator()
	}

	menu.Add("Quit", func() {
		os.Exit(0)
	})

	return menu
}

func statusLabel(s types.VPNStatus) string {
	switch s {
	case types.StatusRunning:
		return "●"
	case types.StatusStopped:
		return "○"
	case types.StatusError:
		return "✗"
	default:
		return "○"
	}
}

func generateIcon() []byte {
	img := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))
	draw.Draw(img, img.Bounds(), image.NewUniform(transparent), image.Point{}, draw.Src)

	widths := []int{
		0, 1, 3, 5, 7, 9,
		11, 13, 13, 13, 13, 13,
		13, 13, 13, 13,
		11, 9, 7, 5, 3, 0,
	}

	for y, w := range widths {
		if w == 0 {
			continue
		}
		startX := (iconSize - w) / 2
		for x := startX; x < startX+w; x++ {
			img.SetRGBA(x, y, white)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}
