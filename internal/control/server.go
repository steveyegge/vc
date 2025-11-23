package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Command represents a control command sent to the executor
type Command struct {
	Type      string                 `json:"type"`       // "pause", "resume", "status"
	IssueID   string                 `json:"issue_id"`   // Target issue ID (for pause)
	Reason    string                 `json:"reason"`     // Optional reason for pause
	Timestamp time.Time              `json:"timestamp"`  // When command was sent
	Metadata  map[string]interface{} `json:"metadata"`   // Additional metadata
}

// Response represents a response to a control command
type Response struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
	Error   string                 `json:"error,omitempty"`
}

// Server manages the control socket for executor communication
type Server struct {
	socketPath string
	listener   net.Listener
	mu         sync.RWMutex
	running    bool
	stopCh     chan struct{}
	doneCh     chan struct{}

	// Command handler - called when commands are received
	// Returns response data and error
	onCommand func(cmd Command) (map[string]interface{}, error)
}

// NewServer creates a new control server
// socketPath should be something like /tmp/vc-<instance-id>.sock
func NewServer(socketPath string, onCommand func(Command) (map[string]interface{}, error)) (*Server, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(socketPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Remove existing socket file if it exists (from crashed previous instance)
	if err := os.RemoveAll(socketPath); err != nil {
		return nil, fmt.Errorf("failed to remove existing socket: %w", err)
	}

	return &Server{
		socketPath: socketPath,
		onCommand:  onCommand,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}, nil
}

// Start begins listening for control commands
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("control server already running")
	}

	// Create Unix domain socket
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to create control socket: %w", err)
	}

	s.listener = listener
	s.running = true
	s.mu.Unlock()

	fmt.Printf("Control server listening on %s\n", s.socketPath)

	// Accept connections in background
	go s.acceptLoop(ctx)

	return nil
}

// acceptLoop accepts incoming connections
func (s *Server) acceptLoop(ctx context.Context) {
	defer close(s.doneCh)
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		default:
		}

		// Set accept timeout to allow checking stop channel
		if err := s.listener.(*net.UnixListener).SetDeadline(time.Now().Add(1 * time.Second)); err != nil {
			fmt.Fprintf(os.Stderr, "control: failed to set deadline: %v\n", err)
			continue
		}

		conn, err := s.listener.Accept()
		if err != nil {
			// Check if this was a timeout (expected)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			// Check if we're stopping
			select {
			case <-s.stopCh:
				return
			default:
			}
			fmt.Fprintf(os.Stderr, "control: accept error: %v\n", err)
			continue
		}

		// Handle connection in background
		go s.handleConnection(ctx, conn)
	}
}

// handleConnection processes a single control connection
func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	// Set read deadline to prevent hanging on bad clients
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		fmt.Fprintf(os.Stderr, "control: failed to set read deadline: %v\n", err)
		return
	}

	// Read command
	decoder := json.NewDecoder(conn)
	var cmd Command
	if err := decoder.Decode(&cmd); err != nil {
		s.sendError(conn, fmt.Sprintf("failed to decode command: %v", err))
		return
	}

	// Set timestamp if not provided
	if cmd.Timestamp.IsZero() {
		cmd.Timestamp = time.Now()
	}

	// Call handler
	var resp Response
	if s.onCommand != nil {
		data, err := s.onCommand(cmd)
		if err != nil {
			resp = Response{
				Success: false,
				Message: fmt.Sprintf("Command failed: %v", err),
				Error:   err.Error(),
			}
		} else {
			resp = Response{
				Success: true,
				Message: fmt.Sprintf("Command '%s' completed successfully", cmd.Type),
				Data:    data,
			}
		}
	} else {
		resp = Response{
			Success: false,
			Message: "No command handler registered",
			Error:   "server misconfiguration",
		}
	}

	// Send response
	if err := s.sendResponse(conn, resp); err != nil {
		fmt.Fprintf(os.Stderr, "control: failed to send response: %v\n", err)
	}
}

// sendError sends an error response to the client
func (s *Server) sendError(conn net.Conn, message string) {
	resp := Response{
		Success: false,
		Message: message,
		Error:   message,
	}
	_ = s.sendResponse(conn, resp) // Ignore errors on error path
}

// sendResponse sends a response to the client
func (s *Server) sendResponse(conn net.Conn, resp Response) error {
	encoder := json.NewEncoder(conn)
	return encoder.Encode(resp)
}

// Stop stops the control server
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	// Signal stop
	close(s.stopCh)

	// Close listener to unblock Accept
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "control: error closing listener: %v\n", err)
		}
	}

	// Wait for accept loop to finish
	select {
	case <-s.doneCh:
	case <-time.After(5 * time.Second):
		fmt.Fprintf(os.Stderr, "control: timeout waiting for server shutdown\n")
	}

	// Clean up socket file
	if err := os.RemoveAll(s.socketPath); err != nil {
		fmt.Fprintf(os.Stderr, "control: failed to remove socket file: %v\n", err)
	}

	fmt.Printf("Control server stopped\n")
	return nil
}

// IsRunning returns whether the server is currently running
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// SocketPath returns the path to the control socket
func (s *Server) SocketPath() string {
	return s.socketPath
}
