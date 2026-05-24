package daemon

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/AbandonwareDev/umbra/internal/config"
	"github.com/AbandonwareDev/umbra/internal/daemon"
	"github.com/AbandonwareDev/umbra/internal/tray"
	"os/user"
)

type Options struct {
	LogFile         string
	NoTray          bool
	NoConfig        bool     // When true, only use built-in command defaults — no config.yaml read or written
	VPNDir          string   // Custom directory for VPN config files; empty uses ~/.umbra/configs/
	ConfigFile      string   // Custom path to extension-mapping config; empty uses ~/.umbra/config.yaml
	AllowUser       string   // Root mode: username allowed to connect via IPC
	TrustedPrefixes []string // Trusted directory prefixes for VPN command validation
}

func Run(opt Options) error {
	paths, err := config.ResolvePaths()
	if err != nil {
		return fmt.Errorf("resolving paths: %w", err)
	}

	if opt.VPNDir != "" {
		paths.ConfigDir = opt.VPNDir
	}
	if opt.ConfigFile != "" {
		paths.ConfigPath = opt.ConfigFile
	}

	isRoot := os.Geteuid() == 0

	// --- Root mode setup ---
	var allowedUID int
	var allowedGIDs map[int]bool

	if isRoot {
		// Root mode: use /etc/umbra/ for configs so non-root users cannot
		// create or modify VPN configs (the directory is root-owned).
		// User-specified -vpn-dir is preserved; only the default is redirected.
		if opt.VPNDir == "" {
			paths.ConfigDir = config.RootConfigDir
		}
		// Ensure the parent /etc/umbra/ exists and is traversable.
		os.MkdirAll(filepath.Dir(config.RootConfigDir), 0755)

		if opt.AllowUser != "" {
			uid, err := lookupUserUID(opt.AllowUser)
			if err != nil {
				return fmt.Errorf("looking up user %q: %w", opt.AllowUser, err)
			}
			allowedUID = uid
			if gid, ok := lookupNetworkManagerGID(); ok {
				allowedGIDs = map[int]bool{gid: true}
			}
			// Use a world-accessible runtime dir so the allowed user can
			// reach the socket. Socket-level auth via SO_PEERCRED below.
			paths.RuntimeDir = "/tmp/umbra"
			paths.SocketPath = filepath.Join("/tmp/umbra", config.DefaultSocketName)
			paths.PIDPath = filepath.Join("/tmp/umbra", config.DefaultPIDFileName)
		}
		// Lock down the config directory — world-readable with allow-user.
		config.SecureConfigDir(paths, opt.AllowUser != "")
	}

	// Create runtime directory with appropriate permissions.
	runtimePerm := os.FileMode(0700)
	if isRoot && opt.AllowUser != "" {
		runtimePerm = 0755
	}
	if err := os.MkdirAll(paths.RuntimeDir, runtimePerm); err != nil {
		return fmt.Errorf("creating runtime directory: %w", err)
	}

	// Application directories and config (skipped in -no-config mode).
	if !opt.NoConfig {
		if err := config.EnsureDirs(paths); err != nil {
			return fmt.Errorf("ensuring directories: %w", err)
		}
		if err := config.SaveDefaultConfig(paths); err != nil {
			return fmt.Errorf("saving default config: %w", err)
		}
	}

	cmdMapping, err := config.LoadMapping(paths, opt.NoConfig)
	if err != nil {
		return fmt.Errorf("loading command mapping: %w", err)
	}

	// Set up logging
	var logWriter io.Writer
	if opt.LogFile != "" {
		logDir := filepath.Dir(opt.LogFile)
		if err := os.MkdirAll(logDir, 0755); err == nil {
			f, err := os.OpenFile(opt.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err == nil {
				defer f.Close()
				logWriter = f
			}
		}
	}
	if logWriter == nil {
		logWriter = os.Stdout
	}

	log.SetOutput(logWriter)

	// Create and start the daemon server with authorization info.
	srv := daemon.NewServer(paths, cmdMapping, logWriter, allowedUID, allowedGIDs, opt.AllowUser, opt.TrustedPrefixes)

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start tray icon (only in user mode — root has no display session).
	// Disable with -no-tray or build with -tags notray.
	if !opt.NoTray && !isRoot {
		go tray.Run(paths)
	}

	go func() {
		<-sigCh
		log.Println("Shutting down...")
		srv.Stop()
		os.Exit(0)
	}()

	mode := "user"
	if isRoot {
		mode = "root"
	}
	log.Printf("Umbra daemon starting (%s mode, socket: %s, configs: %s)", mode, paths.SocketPath, paths.ConfigDir)
	return srv.Run()
}

func lookupUserUID(name string) (int, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(u.Uid)
}

func lookupNetworkManagerGID() (int, bool) {
	g, err := user.LookupGroup("networkmanager")
	if err != nil {
		return 0, false
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, false
	}
	return gid, true
}
