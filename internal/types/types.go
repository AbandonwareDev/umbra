package types

import "time"

// VPNStatus represents the current state of a VPN connection.
type VPNStatus int

const (
	StatusStopped VPNStatus = iota
	StatusRunning
	StatusError
)

func (s VPNStatus) String() string {
	switch s {
	case StatusStopped:
		return "stopped"
	case StatusRunning:
		return "running"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}

// VPNConfig represents a VPN configuration file and its current state.
type VPNConfig struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Extension string    `json:"extension"`
	Status    VPNStatus `json:"status"`
	PID       int       `json:"pid,omitempty"`
	ErrorMsg  string    `json:"error_msg,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
}

// TrayMode controls whether the tray icon is active.
type TrayMode bool

const (
	TrayDisabled TrayMode = false
	TrayEnabled  TrayMode = true
)
