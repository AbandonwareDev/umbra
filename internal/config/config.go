package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultConfigDir is the default directory for VPN config files,
	// relative to the user's home directory.
	DefaultConfigDir = ".umbra/configs"

	// RootConfigDir is the default config directory when running in root mode.
	// Uses /etc/umbra/configs/ so non-root users cannot create/modify configs.
	RootConfigDir = "/etc/umbra/configs"

	// AppDir is the application data directory relative to home.
	AppDir = ".umbra"

	// DefaultSocketName is the default Unix socket name.
	DefaultSocketName = "daemon.sock"

	// RootSocketPath is the socket path the daemon uses in root mode
	// with allow-user. The TUI and tray fall back to this when the
	// per-user socket (/tmp/umbra-$UID/) has no daemon listening.
	RootSocketPath = "/tmp/umbra/daemon.sock"

	// DefaultPIDFileName is the default PID file name.
	DefaultPIDFileName = "daemon.pid"

	// ConfigFileName is the extension-to-command mapping config.
	ConfigFileName = "config.yaml"
)

// CommandMapping maps file extensions to shell commands.
// The {{path}} placeholder is replaced with the full path to the config file.
// The {{name}} placeholder is replaced with the config file name (without extension).
// The {{allow_user}} placeholder is replaced with the -allow-user value.
//
// Extensions with a matching stop_command use a service lifecycle:
// the start command is run and waited on, and the stop command is run on stop
// instead of killing a tracked process. This is useful for systemd services.
//
// To add a new VPN type, simply add an entry here or in the config file:
//
//	extensions:
//	  .ovpn: "openvpn --config {{path}}"
//	  .conf: "wg-quick up {{path}}"
//	  .systemd: "systemctl start {{name}}"
//	stop_commands:
//	  .systemd: "systemctl stop {{name}}"
type CommandMapping struct {
	Extensions   map[string]string `yaml:"extensions"`
	StopCommands map[string]string `yaml:"stop_commands"`
}

// DefaultCommands returns the built-in default command mappings.
// These are used when no config file exists and serve as the base
// that user overrides extend.
//
// To add support for a new VPN type, add an entry here:
//
//	".ext": "command --flag {{path}}"
//
// The following placeholders are available:
//   - {{path}} — absolute path to the config file
//   - {{name}} — config file name without extension
//   - {{allow_user}} — value of the -allow-user flag (empty string if not set)
func DefaultCommands() map[string]string {
	return map[string]string{
		// OpenVPN
		".ovpn": "openvpn --config {{path}}",
		// WireGuard
		".conf": "wg-quick up {{path}}",
		// Tor anonymity network
		".torrc": "tor -f {{path}}",
		// sing-box universal proxy platform
		".sgb":  "sing-box run -c {{path}}",
		".json": "sing-box run -c {{path}}",
		// systemd system-wide services
		".systemd": "systemctl start {{name}}",
		// systemd user services (requires -allow-user)
		".systemd-user": "systemctl --user -M {{allow_user}}@ start {{name}}",
	}
}

// DefaultStopCommands returns the built-in default stop command mappings.
// Extensions listed here use a service lifecycle: start runs the start
// command and waits for it, stop runs the stop command instead of killing
// a tracked process.
func DefaultStopCommands() map[string]string {
	return map[string]string{
		// WireGuard
		".conf":         "wg-quick down {{path}}",
		".systemd":      "systemctl stop {{name}}",
		".systemd-user": "systemctl --user -M {{allow_user}}@ stop {{name}}",
	}
}

// runtimeDir returns the path for runtime files (socket, PID).
// Uses /tmp/umbra-$UID/ so it's always writable, per-user, and
// cleaned up on reboot — no stale sockets.
func runtimeDir() string {
	return filepath.Join("/tmp", fmt.Sprintf("umbra-%d", os.Getuid()))
}

// AppPaths holds all resolved application paths.
type AppPaths struct {
	HomeDir    string
	AppDir     string
	ConfigDir  string
	SocketPath string
	PIDPath    string
	ConfigPath string
	RuntimeDir string
}

// ResolvePaths resolves all application paths for the current user.
func ResolvePaths() (*AppPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot find home directory: %w", err)
	}

	appDir := filepath.Join(home, AppDir)
	configDir := filepath.Join(home, DefaultConfigDir)
	runDir := runtimeDir()

	return &AppPaths{
		HomeDir:    home,
		AppDir:     appDir,
		ConfigDir:  configDir,
		SocketPath: filepath.Join(runDir, DefaultSocketName),
		PIDPath:    filepath.Join(runDir, DefaultPIDFileName),
		ConfigPath: filepath.Join(appDir, ConfigFileName),
		RuntimeDir: runDir,
	}, nil
}

// EnsureRuntimeDir creates the runtime directory with user-only permissions.
func EnsureRuntimeDir(paths *AppPaths) error {
	return os.MkdirAll(paths.RuntimeDir, 0700)
}

// SecureConfigDir locks down the config directory and all files inside
// to root ownership. In allowUser mode the directory is 0755 and files 0644
// (readable by the allowed user). Otherwise 0700/0600 (root-only).
// The caller must ensure the directory is not inside a user-writable tree.
func SecureConfigDir(paths *AppPaths, allowUser bool) error {
	dirPerm := os.FileMode(0700)
	if allowUser {
		dirPerm = 0755
	}
	if err := os.MkdirAll(paths.ConfigDir, dirPerm); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	// chown the directory itself to root
	os.Chown(paths.ConfigDir, 0, 0)
	os.Chmod(paths.ConfigDir, dirPerm)

	entries, err := os.ReadDir(paths.ConfigDir)
	if err != nil {
		return nil
	}
	filePerm := os.FileMode(0600)
	if allowUser {
		filePerm = 0644
	}
	for _, entry := range entries {
		fullPath := filepath.Join(paths.ConfigDir, entry.Name())
		os.Chown(fullPath, 0, 0)
		os.Chmod(fullPath, filePerm)
	}
	return nil
}

// LoadMapping reads the extension-to-command mapping from the config file.
// It merges the user's config with built-in defaults — user entries override
// defaults for the same extension, and new extensions are added.
// When noConfig is true, only built-in defaults are returned and the config
// file on disk is never read. This provides a secure baseline with no
// external configuration dependency.
func LoadMapping(paths *AppPaths, noConfig bool) (*CommandMapping, error) {
	// Start with built-in defaults
	mapping := &CommandMapping{
		Extensions:   DefaultCommands(),
		StopCommands: DefaultStopCommands(),
	}

	if noConfig {
		return mapping, nil
	}

	data, err := os.ReadFile(paths.ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No user config file — return defaults
			return mapping, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var userMapping CommandMapping
	if err := yaml.Unmarshal(data, &userMapping); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Merge user mappings over defaults
	for ext, cmd := range userMapping.Extensions {
		mapping.Extensions[ext] = cmd
	}
	for ext, cmd := range userMapping.StopCommands {
		mapping.StopCommands[ext] = cmd
	}

	return mapping, nil
}

// EnsureDirs creates all necessary application directories.
func EnsureDirs(paths *AppPaths) error {
	for _, dir := range []string{paths.AppDir, paths.ConfigDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}
	return nil
}

// SaveDefaultConfig writes the default config file if none exists.
func SaveDefaultConfig(paths *AppPaths) error {
	if _, err := os.Stat(paths.ConfigPath); err == nil {
		return nil // already exists
	}

	mapping := CommandMapping{
		Extensions:   DefaultCommands(),
		StopCommands: DefaultStopCommands(),
	}
	data, err := yaml.Marshal(&mapping)
	if err != nil {
		return fmt.Errorf("marshalling default config: %w", err)
	}

	comment := `# Umbra Configuration
# Maps file extensions to shell commands.
# The {{path}} placeholder is replaced with the absolute path to the config file.
# The {{name}} placeholder is replaced with the config file name (without extension).
# The {{allow_user}} placeholder is replaced with the -allow-user flag value.
#
# Extensions with a matching stop_command use a service lifecycle:
# the start command is run and waited on, and the stop command is run on stop
# instead of killing a tracked process. This is useful for systemd services.
#
# To add a new VPN type, add an entry here:
#   .ext: "command --flag {{path}}"
#   stop_commands:
#     .ext: "stop command {{name}}"
#
` + string(data)

	if err := os.WriteFile(paths.ConfigPath, []byte(comment), 0644); err != nil {
		return fmt.Errorf("writing default config: %w", err)
	}

	return nil
}

// BuildCommand replaces placeholders in the command template with actual values.
// allowUser can be empty; {{allow_user}} is replaced with the empty string.
func BuildCommand(template, configPath, allowUser string) string {
	name := strings.TrimSuffix(filepath.Base(configPath), filepath.Ext(configPath))
	cmd := strings.ReplaceAll(template, "{{path}}", configPath)
	cmd = strings.ReplaceAll(cmd, "{{name}}", name)
	cmd = strings.ReplaceAll(cmd, "{{allow_user}}", allowUser)
	return cmd
}
