package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/AbandonwareDev/umbra/internal/types"
)

const (
	// SocketTimeout is the default timeout for IPC operations.
	SocketTimeout = 5 * time.Second

	// DaemonNetwork is the network type for the IPC socket.
	DaemonNetwork = "unix"
)

// Action represents an IPC action the TUI can request from the daemon.
type Action string

const (
	ActionList   Action = "list"
	ActionStart  Action = "start"
	ActionStop   Action = "stop"
	ActionStatus Action = "status"
)

// Request is sent from the TUI to the daemon.
type Request struct {
	Action Action `json:"action"`
	Config string `json:"config,omitempty"` // config name for start/stop
}

// Response is sent from the daemon back to the TUI.
type Response struct {
	Success bool              `json:"success"`
	Error   string            `json:"error,omitempty"`
	Configs []types.VPNConfig `json:"configs,omitempty"`
}

// SendRequest sends a request to the daemon over a Unix socket
// and returns the response.
func SendRequest(socketPath string, req *Request) (*Response, error) {
	conn, err := net.DialTimeout(DaemonNetwork, socketPath, SocketTimeout)
	if err != nil {
		return nil, fmt.Errorf("connecting to daemon: %w", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(SocketTimeout)); err != nil {
		return nil, fmt.Errorf("setting deadline: %w", err)
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	var resp Response
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	return &resp, nil
}
