package daemon

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AbandonwareDev/umbra/internal/config"
	"github.com/AbandonwareDev/umbra/internal/types"
)

// maxPkexecRetries limits pkexec re-launch attempts to prevent infinite
// loops when pkexec is unavailable or the user cancels the dialog.
const maxPkexecRetries = 2

const pkexecRetryDelay = 1 * time.Second

// VPNManager manages VPN process lifecycles.
// It supports running multiple VPNs in parallel and tracks their state.
type VPNManager struct {
	mu           sync.RWMutex
	configs      map[string]*types.VPNConfig // keyed by config name
	commands     map[string]string           // extension -> start command template
	stopCommands map[string]string           // extension -> stop command template (service-type)
	processes    map[string]*exec.Cmd        // keyed by config name
	cancels      map[string]func()           // cancel functions for running commands
	configDir    string
	allowUser    string
	logWriter    io.Writer

	pkexecAttempts  map[string]int
	trustedPrefixes []string
}

// NewVPNManager creates a new Umbra VPN manager.
func NewVPNManager(cfgDir string, cmdMapping *config.CommandMapping, allowUser string, logWriter io.Writer, trustedPrefixes []string) *VPNManager {
	return &VPNManager{
		configs:         make(map[string]*types.VPNConfig),
		commands:        cmdMapping.Extensions,
		stopCommands:    cmdMapping.StopCommands,
		processes:       make(map[string]*exec.Cmd),
		cancels:         make(map[string]func()),
		pkexecAttempts:  make(map[string]int),
		configDir:       cfgDir,
		allowUser:       allowUser,
		logWriter:       logWriter,
		trustedPrefixes: trustedPrefixes,
	}
}

// ScanConfigs scans the config directory for VPN configuration files
// and updates the internal list. It detects file extensions and matches
// them to known commands.
func (m *VPNManager) ScanConfigs() error {
	entries, err := os.ReadDir(m.configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading config directory: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Track seen names to detect removed configs
	seen := make(map[string]bool)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := filepath.Ext(entry.Name())
		if _, ok := m.commands[ext]; !ok {
			continue // no command defined for this extension
		}

		fullPath := filepath.Join(m.configDir, entry.Name())
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))

		seen[name] = true

		_, isService := m.stopCommands[ext]

		if existing, ok := m.configs[name]; ok {
			existing.Path = fullPath
			if isService {
				// Refresh actual systemd state on every scan.
				existing.Status = m.checkServiceStatus(name, fullPath, ext)
				existing.StartedAt = time.Time{}
			}
			continue
		}

		status := types.StatusStopped
		if isService {
			status = m.checkServiceStatus(name, fullPath, ext)
		}

		m.configs[name] = &types.VPNConfig{
			Name:      name,
			Path:      fullPath,
			Extension: ext,
			Status:    status,
		}
	}

	// Remove configs that no longer exist on disk
	for name := range m.configs {
		if !seen[name] {
			// If running, stop it first
			if m.configs[name].Status == types.StatusRunning {
				m.stopLocked(name)
			}
			delete(m.configs, name)
		}
	}

	return nil
}

// vpnGroupOrder maps file extensions to sort priority.
// Lower values appear first; Tor is pushed to the end.
func vpnGroupOrder(ext string) int {
	switch ext {
	case ".sgb", ".json":
		return 0 // sing-box first
	case ".wg", ".conf":
		return 1 // WireGuard second
	case ".torrc":
		return 3 // Tor before systemd
	case ".systemd", ".systemd-user":
		return 4 // systemd last
	default:
		return 2 // other (OpenVPN, NetworkManager, custom) in the middle
	}
}

// ListConfigs returns a snapshot of all known VPN configs and their states,
// sorted by VPN app group then by name. Tor configs always appear last.
func (m *VPNManager) ListConfigs() []types.VPNConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]types.VPNConfig, 0, len(m.configs))
	for _, c := range m.configs {
		result = append(result, *c)
	}

	sort.Slice(result, func(i, j int) bool {
		oi := vpnGroupOrder(result[i].Extension)
		oj := vpnGroupOrder(result[j].Extension)
		if oi != oj {
			return oi < oj
		}
		return result[i].Name < result[j].Name
	})

	return result
}

// GetConfig returns a specific config by name.
func (m *VPNManager) GetConfig(name string) (*types.VPNConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.configs[name]
	if !ok {
		return nil, false
	}
	cp := *c
	return &cp, true
}

// Start starts a VPN by name. Returns an error if the config is unknown,
// already running, or the command fails to start.
func (m *VPNManager) Start(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, ok := m.configs[name]
	if !ok {
		return fmt.Errorf("unknown config: %s", name)
	}

	if cfg.Status == types.StatusRunning {
		return fmt.Errorf("%s is already running", name)
	}

	template, ok := m.commands[cfg.Extension]
	if !ok {
		return fmt.Errorf("no command defined for extension %s", cfg.Extension)
	}

	cmdLine := config.BuildCommand(template, cfg.Path, m.allowUser)

	m.log(fmt.Sprintf("Starting VPN %s: %s", name, cmdLine))

	parts := strings.Fields(cmdLine)
	if len(parts) == 0 {
		return fmt.Errorf("empty command for %s", name)
	}

	resolved, err := exec.LookPath(parts[0])
	if err != nil {
		return fmt.Errorf("resolving command %q: %w", parts[0], err)
	}
	if err := CheckTrustedPrefix(resolved, m.trustedPrefixes); err != nil {
		return fmt.Errorf("untrusted command %q: %w", resolved, err)
	}

	// Service-type extensions (those with a stop command) use a different
	// lifecycle: run the start command and wait for it, then mark as running
	// without tracking a process. The stop command handles stopping.
	if _, isService := m.stopCommands[cfg.Extension]; isService {
		cmd := exec.Command(parts[0], parts[1:]...)
		cmd.Dir = "/tmp"
		output, err := cmd.CombinedOutput()
		if err != nil {
			m.log(fmt.Sprintf("Service %s start failed: %s\n%s", name, err, string(output)))
			return fmt.Errorf("starting service %s: %w\n%s", name, err, string(output))
		}
		m.log(fmt.Sprintf("Service %s started", name))
		cfg.Status = types.StatusRunning
		cfg.PID = 0
		cfg.StartedAt = time.Now()
		cfg.ErrorMsg = ""
		return nil
	}

	// Regular process-type: track and monitor the subprocess
	var cmd *exec.Cmd
	if len(parts) == 1 {
		cmd = exec.Command(parts[0])
	} else {
		cmd = exec.Command(parts[0], parts[1:]...)
	}
	cmd.Dir = "/tmp"

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting VPN command: %w", err)
	}

	go m.streamOutput(name, "stdout", stdout)
	go m.streamOutput(name, "stderr", stderr)

	cfg.Status = types.StatusRunning
	cfg.PID = cmd.Process.Pid
	cfg.StartedAt = time.Now()
	cfg.ErrorMsg = ""

	m.processes[name] = cmd
	go m.monitorProcess(name, cmd)

	m.log(fmt.Sprintf("VPN %s started (PID %d)", name, cmd.Process.Pid))
	return nil
}

// Stop stops a running VPN by name.
func (m *VPNManager) Stop(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopLocked(name)
}

func (m *VPNManager) stopLocked(name string) error {
	cfg, ok := m.configs[name]
	if !ok {
		return fmt.Errorf("unknown config: %s", name)
	}

	if cfg.Status != types.StatusRunning {
		return nil
	}

	// Service-type extensions: run the stop command instead of killing a PID.
	if stopCmd, ok := m.stopCommands[cfg.Extension]; ok {
		cmdLine := config.BuildCommand(stopCmd, cfg.Path, m.allowUser)
		m.log(fmt.Sprintf("Stopping service %s: %s", name, cmdLine))
		parts := strings.Fields(cmdLine)
		if len(parts) > 0 {
			resolved, err := exec.LookPath(parts[0])
			if err != nil {
				return fmt.Errorf("resolving command %q: %w", parts[0], err)
			}
			if err := CheckTrustedPrefix(resolved, m.trustedPrefixes); err != nil {
				return fmt.Errorf("untrusted command %q: %w", resolved, err)
			}
			cmd := exec.Command(parts[0], parts[1:]...)
			cmd.Dir = "/tmp"
			if output, err := cmd.CombinedOutput(); err != nil {
				m.log(fmt.Sprintf("Service %s stop failed: %s\n%s", name, err, string(output)))
				return fmt.Errorf("stopping service %s: %w\n%s", name, err, string(output))
			}
		}
		cfg.Status = types.StatusStopped
		cfg.StartedAt = time.Time{}
		m.log(fmt.Sprintf("Service %s stopped", name))
		return nil
	}

	// Regular process-type: signal the tracked process.
	cmd, ok := m.processes[name]
	if !ok {
		cfg.Status = types.StatusStopped
		return nil
	}

	m.log(fmt.Sprintf("Stopping VPN %s (PID %d)", name, cmd.Process.Pid))

	// Try graceful termination first, then kill
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		if err := cmd.Process.Kill(); err != nil {
			return fmt.Errorf("killing VPN process %s: %w", name, err)
		}
	}

	// Wait for process to exit (with timeout)
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		<-done
	}

	cfg.Status = types.StatusStopped
	cfg.PID = 0
	cfg.StartedAt = time.Time{}
	delete(m.processes, name)

	m.log(fmt.Sprintf("VPN %s stopped", name))
	return nil
}

// StopAll stops all running VPNs.
func (m *VPNManager) StopAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []string
	for name, cfg := range m.configs {
		if cfg.Status == types.StatusRunning {
			if err := m.stopLocked(name); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors stopping VPNs: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (m *VPNManager) monitorProcess(name string, cmd *exec.Cmd) {
	processErr := cmd.Wait()

	m.mu.Lock()

	cfg, ok := m.configs[name]
	if !ok {
		m.mu.Unlock()
		return
	}

	// Only update if this is still the tracked process
	if m.processes[name] != cmd {
		m.mu.Unlock()
		return
	}

	// User mode early-exit detection: if the VPN process exits within 10
	// seconds with an error, it probably needs root. Re-launch via pkexec.
	if os.Geteuid() != 0 && processErr != nil && time.Since(cfg.StartedAt) < 10*time.Second {
		attempts := m.pkexecAttempts[name]
		if attempts >= maxPkexecRetries {
			delete(m.pkexecAttempts, name)
			cfg.Status = types.StatusError
			cfg.ErrorMsg = "needs root privileges — install pkexec or re-run daemon as root"
			cfg.PID = 0
			cfg.StartedAt = time.Time{}
			delete(m.processes, name)
			delete(m.cancels, name)
			m.mu.Unlock()
			m.log(fmt.Sprintf("VPN %s needs root, gave up after %d pkexec attempts", name, attempts))
			return
		}
		m.pkexecAttempts[name] = attempts + 1
		template := m.commands[cfg.Extension]
		delete(m.processes, name)
		delete(m.cancels, name)
		cfg.PID = 0
		cfg.StartedAt = time.Time{}
		m.mu.Unlock()

		time.Sleep(pkexecRetryDelay)
		m.log(fmt.Sprintf("VPN %s exited early (likely needs root), trying pkexec (attempt %d/%d)...",
			name, attempts+1, maxPkexecRetries))
		if err := m.startWithPkexec(name, template); err != nil {
			m.mu.Lock()
			cfg.Status = types.StatusError
			cfg.ErrorMsg = fmt.Sprintf("needs root (pkexec failed: %s)", err)
			m.mu.Unlock()
			m.log(fmt.Sprintf("VPN %s pkexec failed: %s", name, err))
		}
		return
	}

	// Normal cleanup
	if processErr != nil {
		cfg.Status = types.StatusError
		cfg.ErrorMsg = processErr.Error()
		m.log(fmt.Sprintf("VPN %s exited with error: %s", name, processErr))
	} else {
		cfg.Status = types.StatusStopped
		m.log(fmt.Sprintf("VPN %s exited cleanly", name))
	}

	cfg.PID = 0
	cfg.StartedAt = time.Time{}
	delete(m.processes, name)
	delete(m.cancels, name)
	delete(m.pkexecAttempts, name)
	m.mu.Unlock()
}

// startWithPkexec re-launches a VPN command via pkexec (or kdesu as fallback).
// This is used in user mode when the VPN process exits early, likely because
// it requires root privileges. A graphical sudo prompt is shown.
func (m *VPNManager) startWithPkexec(name, template string) error {
	cfg := m.configs[name]
	cmdLine := config.BuildCommand(template, cfg.Path, m.allowUser)
	parts := strings.Fields(cmdLine)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	resolvedParts0, err := exec.LookPath(parts[0])
	if err != nil {
		return fmt.Errorf("resolving command %q: %w", parts[0], err)
	}
	if err := CheckTrustedPrefix(resolvedParts0, m.trustedPrefixes); err != nil {
		return fmt.Errorf("untrusted command %q: %w", resolvedParts0, err)
	}

	// Try pkexec first, then fall back to kdesu.
	elevators := []string{"pkexec", "kdesu"}
	var elevPath string
	for _, e := range elevators {
		if p, err := exec.LookPath(e); err == nil {
			elevPath = p
			if err := CheckTrustedPrefix(elevPath, m.trustedPrefixes); err != nil {
				return fmt.Errorf("untrusted elevator %q: %w", elevPath, err)
			}
			break
		}
	}
	if elevPath == "" {
		return fmt.Errorf("no graphical sudo tool found (try pkexec, kdesu)")
	}

	var cmd *exec.Cmd
	if filepath.Base(elevPath) == "kdesu" {
		// kdesu -c "full command line"
		cmd = exec.Command(elevPath, "-c", cmdLine)
	} else {
		// pkexec command arg1 arg2 ...
		cmd = exec.Command(elevPath, parts...)
	}
	cmd.Dir = "/tmp"

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	go m.streamOutput(name, "stdout", stdout)
	go m.streamOutput(name, "stderr", stderr)

	m.mu.Lock()
	m.processes[name] = cmd
	cfg.Status = types.StatusRunning
	cfg.PID = cmd.Process.Pid
	cfg.StartedAt = time.Now()
	cfg.ErrorMsg = ""
	m.mu.Unlock()

	go m.monitorProcess(name, cmd)
	m.log(fmt.Sprintf("VPN %s started via %s (PID %d)", name, elevPath, cmd.Process.Pid))
	return nil
}

// checkServiceStatus queries systemd's actual state for service-type configs.
// Derives an "is-active" command by replacing " start " → " is-active "
// in the start command template. Returns StatusRunning, StatusStopped, or StatusError.
func (m *VPNManager) checkServiceStatus(name, cfgPath, ext string) types.VPNStatus {
	template, ok := m.commands[ext]
	if !ok {
		return types.StatusStopped
	}

	statusTemplate := strings.Replace(template, " start ", " is-active ", 1)
	if statusTemplate == template {
		return types.StatusStopped // can't derive — not a standard systemctl start command
	}

	cmdLine := config.BuildCommand(statusTemplate, cfgPath, m.allowUser)
	parts := strings.Fields(cmdLine)
	if len(parts) == 0 {
		return types.StatusStopped
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = "/tmp"
	output, err := cmd.Output()
	if err != nil {
		// systemctl is-active exit codes: 0=active, 3=inactive, 4=unknown
		if exitErr, ok := err.(*exec.ExitError); ok {
			switch exitErr.ExitCode() {
			case 3:
				return types.StatusStopped
			case 4:
				return types.StatusError
			}
		}
		return types.StatusStopped
	}

	if strings.TrimSpace(string(output)) == "active" {
		return types.StatusRunning
	}
	return types.StatusStopped
}

func (m *VPNManager) streamOutput(name, stream string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		m.log(fmt.Sprintf("[%s][%s] %s", name, stream, scanner.Text()))
	}
}

func (m *VPNManager) log(msg string) {
	if m.logWriter == nil {
		return
	}
	ts := time.Now().Format(time.RFC3339)
	fmt.Fprintf(m.logWriter, "%s %s\n", ts, msg)
}
