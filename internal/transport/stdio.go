package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/amir-the-h/mcp-hub/internal/mcp"
)

// StdioTransport implements stdio-based MCP transport
type StdioTransport struct {
	command string
	args    []string
	env     map[string]string
	timeout time.Duration

	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	reader    *bufio.Reader
	mu        sync.Mutex
	requestID int
	connected bool
}

// NewStdioTransport creates a new stdio transport
func NewStdioTransport(command string, args []string, env map[string]string, timeout time.Duration) *StdioTransport {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &StdioTransport{
		command: command,
		args:    args,
		env:     env,
		timeout: timeout,
	}
}

// Start launches the subprocess and initializes stdio communication
func (t *StdioTransport) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.connected {
		return fmt.Errorf("transport already started")
	}

	t.cmd = exec.CommandContext(ctx, t.command, t.args...)

	// Set up environment
	t.cmd.Env = os.Environ()
	for k, v := range t.env {
		t.cmd.Env = append(t.cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Set up pipes
	stdin, err := t.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	t.stdin = stdin

	stdout, err := t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	t.stdout = stdout
	t.reader = bufio.NewReader(stdout)

	// Capture stderr for logging
	t.cmd.Stderr = os.Stderr

	// Start the process
	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	t.connected = true
	t.requestID = 0

	log.Printf("stdio:started command=%s args=%v", t.command, t.args)

	// Monitor process in background
	go func() {
		_ = t.cmd.Wait()
		t.mu.Lock()
		t.connected = false
		t.mu.Unlock()
		log.Printf("stdio:process exited command=%s", t.command)
	}()

	return nil
}

// SendRequest sends a JSON-RPC request and waits for response
func (t *StdioTransport) SendRequest(ctx context.Context, req interface{}) (json.RawMessage, error) {
	t.mu.Lock()
	if !t.connected {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport not connected")
	}
	t.mu.Unlock()

	// Serialize request
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Log request (trimmed)
	reqSnippet := string(reqBytes)
	if len(reqSnippet) > 200 {
		reqSnippet = reqSnippet[:200] + "..."
	}
	log.Printf("stdio:send request len=%d snippet=%s", len(reqBytes), reqSnippet)

	// Send request with newline delimiter
	t.mu.Lock()
	if _, err := t.stdin.Write(append(reqBytes, '\n')); err != nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("failed to write request: %w", err)
	}
	t.mu.Unlock()

	// Read response with timeout
	responseChan := make(chan json.RawMessage, 1)
	errorChan := make(chan error, 1)

	start := time.Now()
	go func() {
		line, err := t.reader.ReadBytes('\n')
		if err != nil {
			errorChan <- fmt.Errorf("failed to read response: %w", err)
			return
		}
		responseChan <- json.RawMessage(line)
	}()

	// Apply timeout
	timeout := t.timeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}

	select {
	case resp := <-responseChan:
		dur := time.Since(start)
		log.Printf("stdio:recv response len=%d duration=%s", len(resp), dur)
		return resp, nil
	case err := <-errorChan:
		return nil, err
	case <-time.After(timeout):
		return nil, fmt.Errorf("request timeout after %v", timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// SendNotification sends a JSON-RPC notification
func (t *StdioTransport) SendNotification(ctx context.Context, notification interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.connected {
		return fmt.Errorf("transport not connected")
	}

	notifBytes, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	if _, err := t.stdin.Write(append(notifBytes, '\n')); err != nil {
		return fmt.Errorf("failed to write notification: %w", err)
	}

	return nil
}

// Close terminates the transport
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.connected {
		return nil
	}

	t.connected = false

	// Close stdin to signal process to terminate
	if t.stdin != nil {
		_ = t.stdin.Close()
	}

	// Give the process time to terminate gracefully
	done := make(chan error, 1)
	go func() {
		done <- t.cmd.Wait()
	}()

	select {
	case <-time.After(2 * time.Second):
		// Force kill if it doesn't terminate
		if t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
	case <-done:
		// Process terminated gracefully
	}

	return nil
}

// IsConnected returns connection status
func (t *StdioTransport) IsConnected() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.connected
}

// NextRequestID generates a unique request ID
func (t *StdioTransport) NextRequestID() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.requestID++
	return t.requestID
}

// Initialize performs MCP initialization handshake
func (t *StdioTransport) Initialize(ctx context.Context) (*mcp.InitializeResult, error) {
	reqID := t.NextRequestID()

	initParams := mcp.InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    mcp.ClientCapabilities{},
		ClientInfo: mcp.ClientInfo{
			Name:    "mcp-hub",
			Version: "0.1.0",
		},
	}

	req, err := mcp.NewRequest(reqID, "initialize", initParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create initialize request: %w", err)
	}

	respBytes, err := t.SendRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("initialize request failed: %w", err)
	}

	var resp mcp.JSONRPCResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse initialize response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("initialize error: %s", resp.Error.Message)
	}

	var result mcp.InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse initialize result: %w", err)
	}

	// Send initialized notification
	notif, err := mcp.NewNotification("notifications/initialized", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create initialized notification: %w", err)
	}

	if err := t.SendNotification(ctx, notif); err != nil {
		return nil, fmt.Errorf("failed to send initialized notification: %w", err)
	}

	return &result, nil
}
