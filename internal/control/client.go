package control

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Client sends control commands to a running executor
type Client struct {
	socketPath string
	timeout    time.Duration
}

// NewClient creates a new control client
func NewClient(socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
		timeout:    10 * time.Second, // Default 10s timeout
	}
}

// SetTimeout sets the client timeout for commands
func (c *Client) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
}

// SendCommand sends a command to the executor and waits for response
func (c *Client) SendCommand(cmd Command) (*Response, error) {
	// Connect to control socket
	conn, err := net.DialTimeout("unix", c.socketPath, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to executor (is it running?): %w", err)
	}
	defer conn.Close()

	// Set overall deadline
	if err := conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
		return nil, fmt.Errorf("failed to set deadline: %w", err)
	}

	// Send command
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(cmd); err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	// Read response
	decoder := json.NewDecoder(conn)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return &resp, nil
}

// Pause sends a pause command for the specified issue
func (c *Client) Pause(issueID string, reason string) (*Response, error) {
	cmd := Command{
		Type:      "pause",
		IssueID:   issueID,
		Reason:    reason,
		Timestamp: time.Now(),
	}
	return c.SendCommand(cmd)
}

// Resume sends a resume command for the specified issue
func (c *Client) Resume(issueID string) (*Response, error) {
	cmd := Command{
		Type:      "resume",
		IssueID:   issueID,
		Timestamp: time.Now(),
	}
	return c.SendCommand(cmd)
}

// Status requests the current executor status
func (c *Client) Status() (*Response, error) {
	cmd := Command{
		Type:      "status",
		Timestamp: time.Now(),
	}
	return c.SendCommand(cmd)
}
