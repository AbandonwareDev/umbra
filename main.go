package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/user/umbra/cmd/daemon"
	"github.com/user/umbra/cmd/tray"
	"github.com/user/umbra/cmd/tui"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "daemon", "d":
		runDaemon(os.Args[2:])
	case "tui", "t":
		runTUI()
	case "tray":
		runTray()
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func runDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	logFile := fs.String("log", "", "Path to log file (default: stdout)")
	noTray := fs.Bool("no-tray", false, "Disable system tray icon")
	noConfig := fs.Bool("no-config", false, "Skip config.yaml — built-in defaults only (more secure)")
	vpnDir := fs.String("vpn-dir", "", "Directory with VPN config files (default: ~/.umbra/configs/)")
	configFile := fs.String("config", "", "Path to extension-mapping config (default: ~/.umbra/config.yaml)")
	allowUser := fs.String("allow-user", "", "Root mode: username allowed to control the daemon via IPC")
	fs.Parse(args)

	opt := daemon.Options{
		LogFile:    *logFile,
		NoTray:     *noTray,
		NoConfig:   *noConfig,
		VPNDir:     *vpnDir,
		ConfigFile: *configFile,
		AllowUser:  *allowUser,
	}

	if err := daemon.Run(opt); err != nil {
		fmt.Fprintf(os.Stderr, "Daemon error: %s\n", err)
		os.Exit(1)
	}
}

func runTUI() {
	if err := tui.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %s\n", err)
		os.Exit(1)
	}
}

func runTray() {
	if err := tray.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Tray error: %s\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`Umbra — VPN controller daemon + TUI

Usage:
  umbra daemon [options]    Start the background daemon
  umbra tray                Start standalone tray icon (connects to running daemon)
  umbra tui                 Start the terminal UI
  umbra help                Show this help

Daemon options:
  -vpn-dir <path>     Directory with VPN config files (default: ~/.umbra/configs/)
  -config <path>      Path to extension-mapping config (default: ~/.umbra/config.yaml)
  -log <file>         Write logs to file instead of stdout
  -no-tray            Disable system tray icon
  -no-config          Skip config file — built-in defaults only (more secure)
  -allow-user <user>  Root mode: grant IPC access to this user (also allows networkmanager group).
                      Required for .systemd-user to resolve the {{allow_user}} placeholder.

The daemon monitors a directory for VPN configuration files and runs
the appropriate VPN command based on file extension.

Built-in VPN types:
  .ovpn          OpenVPN
  .conf          WireGuard (service lifecycle)
  .torrc         Tor anonymity network
  .sgb           sing-box universal proxy
  .json          sing-box universal proxy
  .systemd       systemd system-wide services (service lifecycle)
  .systemd-user  systemd per-user services (service lifecycle, requires -allow-user)

Extensions with a matching stop_command (service lifecycle) run their
start command and wait for it, then run the stop command on shutdown
instead of killing a tracked process. Built-in service types:
.conf (WireGuard), .systemd and .systemd-user.

To add custom VPN types, create ~/.umbra/config.yaml:
  extensions:
    .ext: "command --flag {{path}}"

Placeholders available in commands:
  {{path}}        Absolute path to the config file
  {{name}}        Config file name without extension
  {{allow_user}}  Value of -allow-user flag (empty if not set)

Examples:
  umbra daemon                                    # defaults
  umbra daemon -vpn-dir /etc/openvpn              # custom VPN configs
  umbra daemon -config ./my-mappings.yaml         # custom command mapping
  umbra daemon -no-config                         # built-in defaults only
  umbra daemon -no-tray                           # headless server
  umbra daemon -log /var/log/umbra.log
  umbra daemon -allow-user alice                  # root mode, grant access to alice
  umbra tui
`)
}
