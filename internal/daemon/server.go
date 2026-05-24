package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"syscall"

	"github.com/AbandonwareDev/umbra/internal/config"
	"github.com/AbandonwareDev/umbra/internal/ipc"
	"github.com/AbandonwareDev/umbra/internal/types"
)

// Server is the daemon's IPC server. It listens on a Unix socket
// and handles commands from the TUI or tray app.
type Server struct {
	socketPath      string
	manager         *VPNManager
	listener        net.Listener
	wg              sync.WaitGroup
	quit            chan struct{}
	logWriter       io.Writer
	allowedUID      int          // non-zero = restrict IPC to this UID (root mode)
	allowedGIDs     map[int]bool // additionally authorize users in these groups
	trustedPrefixes []string
}

// NewServer creates a new IPC server.
// allowedUID and allowedGIDs control peer credential authorization:
// zero allowedUID means no restriction (user mode / root-only socket).
func NewServer(paths *config.AppPaths, cmdMapping *config.CommandMapping, logWriter io.Writer, allowedUID int, allowedGIDs map[int]bool, allowUser string, trustedPrefixes []string) *Server {
	return &Server{
		socketPath:      paths.SocketPath,
		manager:         NewVPNManager(paths.ConfigDir, cmdMapping, allowUser, logWriter, trustedPrefixes),
		quit:            make(chan struct{}),
		logWriter:       logWriter,
		allowedUID:      allowedUID,
		allowedGIDs:     allowedGIDs,
		trustedPrefixes: trustedPrefixes,
	}
}

// Run starts the IPC server and listens for connections.
// It blocks until Stop is called or a fatal error occurs.
func (s *Server) Run() error {
	// Remove stale socket
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing stale socket: %w", err)
	}

	var err error
	s.listener, err = net.Listen(ipc.DaemonNetwork, s.socketPath)
	if err != nil {
		return fmt.Errorf("listening on socket: %w", err)
	}

	// Set socket permissions: 0600 for user mode, 0666 for root mode
	// with allow-user (peer credential check handles authorization).
	socketPerm := os.FileMode(0600)
	if s.allowedUID != 0 {
		socketPerm = 0666
	}
	if err := os.Chmod(s.socketPath, socketPerm); err != nil {
		return fmt.Errorf("setting socket permissions: %w", err)
	}

	s.log("Umbra daemon started, listening on " + s.socketPath)

	// Initial scan of configs
	if err := s.manager.ScanConfigs(); err != nil {
		s.log(fmt.Sprintf("Warning: initial config scan: %s", err))
	}

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return nil
			default:
				s.log(fmt.Sprintf("Accept error: %s", err))
				continue
			}
		}

		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// Stop gracefully shuts down the server, stopping all VPNs.
func (s *Server) Stop() {
	s.log("Shutting down daemon...")

	close(s.quit)

	if s.listener != nil {
		s.listener.Close()
	}

	// Stop all running VPNs
	if err := s.manager.StopAll(); err != nil {
		s.log(fmt.Sprintf("Error stopping VPNs: %s", err))
	}

	s.wg.Wait()

	os.Remove(s.socketPath)
	s.log("Daemon stopped")
}

func (s *Server) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	// Peer credential authorization for root mode with allow-user.
	if s.allowedUID != 0 {
		if err := s.authorizePeer(conn); err != nil {
			s.log(fmt.Sprintf("Unauthorized connection attempt: %s", err))
			s.sendError(conn, "unauthorized")
			return
		}
	}

	var req ipc.Request
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&req); err != nil {
		s.sendError(conn, fmt.Sprintf("invalid request: %s", err))
		return
	}

	s.log(fmt.Sprintf("Received action: %s (config: %s)", req.Action, req.Config))

	var resp ipc.Response

	switch req.Action {
	case ipc.ActionList:
		resp = s.handleList()
	case ipc.ActionStatus:
		resp = s.handleStatus(req.Config)
	case ipc.ActionStart:
		resp = s.handleStart(req.Config)
	case ipc.ActionStop:
		resp = s.handleStop(req.Config)
	default:
		resp = ipc.Response{
			Success: false,
			Error:   fmt.Sprintf("unknown action: %s", req.Action),
		}
	}

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(resp); err != nil {
		s.log(fmt.Sprintf("Error sending response: %s", err))
	}
}

func (s *Server) handleList() ipc.Response {
	if err := s.manager.ScanConfigs(); err != nil {
		s.log(fmt.Sprintf("Warning: config scan: %s", err))
	}
	return ipc.Response{
		Success: true,
		Configs: s.manager.ListConfigs(),
	}
}

func (s *Server) handleStatus(name string) ipc.Response {
	if name == "" {
		return s.handleList()
	}

	cfg, ok := s.manager.GetConfig(name)
	if !ok {
		return ipc.Response{
			Success: false,
			Error:   fmt.Sprintf("config not found: %s", name),
		}
	}

	return ipc.Response{
		Success: true,
		Configs: []types.VPNConfig{*cfg},
	}
}

func (s *Server) handleStart(name string) ipc.Response {
	if name == "" {
		return ipc.Response{
			Success: false,
			Error:   "config name is required",
		}
	}

	// Re-scan configs to pick up any changes
	if err := s.manager.ScanConfigs(); err != nil {
		s.log(fmt.Sprintf("Warning: config scan: %s", err))
	}

	if err := s.manager.Start(name); err != nil {
		return ipc.Response{
			Success: false,
			Error:   err.Error(),
		}
	}

	cfg, _ := s.manager.GetConfig(name)
	return ipc.Response{
		Success: true,
		Configs: []types.VPNConfig{*cfg},
	}
}

func (s *Server) handleStop(name string) ipc.Response {
	if name == "" {
		return ipc.Response{
			Success: false,
			Error:   "config name is required",
		}
	}

	if err := s.manager.Stop(name); err != nil {
		return ipc.Response{
			Success: false,
			Error:   err.Error(),
		}
	}

	cfg, _ := s.manager.GetConfig(name)
	return ipc.Response{
		Success: true,
		Configs: []types.VPNConfig{*cfg},
	}
}

func (s *Server) sendError(conn net.Conn, msg string) {
	resp := ipc.Response{
		Success: false,
		Error:   msg,
	}
	encoder := json.NewEncoder(conn)
	encoder.Encode(resp)
}

func (s *Server) log(msg string) {
	if s.logWriter == nil {
		return
	}
	fmt.Fprintf(s.logWriter, "[daemon] %s\n", msg)
}

// authorizePeer checks whether the connecting Unix socket peer is authorized.
// Returns nil if the peer's UID matches allowedUID, or the peer's GID is in
// allowedGIDs (e.g. the networkmanager group).
func (s *Server) authorizePeer(conn net.Conn) error {
	uid, gid, err := getPeerUIDGID(conn)
	if err != nil {
		return fmt.Errorf("getting peer credentials: %w", err)
	}
	if s.allowedUID != 0 && uid == s.allowedUID {
		return nil
	}
	if s.allowedGIDs != nil && s.allowedGIDs[gid] {
		return nil
	}
	return fmt.Errorf("peer uid=%d not in allowed set", uid)
}

// getPeerUIDGID extracts the UID and GID of the peer on the other end of a
// Unix socket connection using SO_PEERCRED.
func getPeerUIDGID(conn net.Conn) (int, int, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, 0, fmt.Errorf("not a unix socket connection")
	}
	rawConn, err := unixConn.SyscallConn()
	if err != nil {
		return 0, 0, fmt.Errorf("syscall connection: %w", err)
	}
	var uid, gid int
	ctrlErr := rawConn.Control(func(fd uintptr) {
		cred, err := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		if err != nil {
			return
		}
		uid = int(cred.Uid)
		gid = int(cred.Gid)
	})
	if ctrlErr != nil {
		return 0, 0, fmt.Errorf("control: %w", ctrlErr)
	}
	return uid, gid, nil
}
