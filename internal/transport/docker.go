package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/amir-the-h/mcp-hub/internal/mcp"
)

// DockerTransport implements stdio-based MCP transport via Docker containers
type DockerTransport struct {
	image        string
	args         []string
	env          map[string]string
	volumes      map[string]string // host:container path mappings
	network      string
	removeOnExit bool
	timeout      time.Duration

	cmd         *exec.Cmd
	containerID string
	stdin       io.WriteCloser
	stdout      io.ReadCloser
	reader      *bufio.Reader
	mu          sync.Mutex
	requestID   int
	connected   bool
}

// NewDockerTransport creates a new Docker-based transport
func NewDockerTransport(image string, args []string, env, volumes map[string]string, network string, timeout time.Duration) *DockerTransport {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &DockerTransport{
		image:        image,
		args:         args,
		env:          env,
		volumes:      volumes,
		network:      network,
		removeOnExit: true,
		timeout:      timeout,
	}
}

// Start launches the Docker container and initializes stdio communication
func (t *DockerTransport) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.connected {
		return fmt.Errorf("transport already started")
	}

	// Build docker run command
	dockerArgs := []string{"run", "-i"}

	// Add --rm for automatic cleanup
	if t.removeOnExit {
		dockerArgs = append(dockerArgs, "--rm")
	}

	// Add environment variables
	for k, v := range t.env {
		dockerArgs = append(dockerArgs, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Add volume mounts
	for hostPath, containerPath := range t.volumes {
		dockerArgs = append(dockerArgs, "-v", fmt.Sprintf("%s:%s", hostPath, containerPath))
	}

	// Add network if specified
	if t.network != "" {
		dockerArgs = append(dockerArgs, "--network", t.network)
	}

	// Add image
	dockerArgs = append(dockerArgs, t.image)

	// Add command args
	dockerArgs = append(dockerArgs, t.args...)

	t.cmd = exec.CommandContext(ctx, "docker", dockerArgs...)

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
	stderr, err := t.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the container
	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Docker container: %w", err)
	}

	t.connected = true
	t.requestID = 0

	// Log stderr in background
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			// Log stderr output (can be improved with proper logging)
			fmt.Printf("[docker:%s] %s\n", t.image, scanner.Text())
		}
	}()

	// Monitor process in background
	go func() {
		_ = t.cmd.Wait()
		t.mu.Lock()
		t.connected = false
		t.mu.Unlock()
	}()

	return nil
}

// SendRequest sends a JSON-RPC request and waits for response
func (t *DockerTransport) SendRequest(ctx context.Context, req interface{}) (json.RawMessage, error) {
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
func (t *DockerTransport) SendNotification(ctx context.Context, notification interface{}) error {
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

// Close terminates the Docker container
func (t *DockerTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.connected {
		return nil
	}

	t.connected = false

	// Close stdin to signal container to terminate
	if t.stdin != nil {
		_ = t.stdin.Close()
	}

	// Give the container time to terminate gracefully
	done := make(chan error, 1)
	go func() {
		done <- t.cmd.Wait()
	}()

	select {
	case <-time.After(5 * time.Second):
		// Force kill if it doesn't terminate
		if t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
		// Also try docker stop if we have container ID
		if t.containerID != "" {
			exec.Command("docker", "stop", t.containerID).Run()
		}
	case <-done:
		// Container terminated gracefully
	}

	return nil
}

// IsConnected returns connection status
func (t *DockerTransport) IsConnected() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.connected
}

// NextRequestID generates a unique request ID
func (t *DockerTransport) NextRequestID() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.requestID++
	return t.requestID
}

// Initialize performs MCP initialization handshake
func (t *DockerTransport) Initialize(ctx context.Context) (*mcp.InitializeResult, error) {
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

// GetContainerInfo returns information about the running container
func (t *DockerTransport) GetContainerInfo() map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()

	info := map[string]string{
		"image":     t.image,
		"connected": fmt.Sprintf("%v", t.connected),
	}

	if t.containerID != "" {
		info["container_id"] = t.containerID
	}

	if len(t.args) > 0 {
		info["args"] = strings.Join(t.args, " ")
	}

	return info
}
