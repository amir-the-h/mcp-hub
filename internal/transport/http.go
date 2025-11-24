package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/amir-the-h/mcp-hub/internal/mcp"
)

// HTTPTransport implements HTTP-based MCP transport (SSE/Streamable HTTP)
type HTTPTransport struct {
	url     string
	headers map[string]string
	timeout time.Duration

	client    *http.Client
	mu        sync.Mutex
	requestID int
	connected bool
}

// NewHTTPTransport creates a new HTTP transport
func NewHTTPTransport(url string, headers map[string]string, timeout time.Duration) *HTTPTransport {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &HTTPTransport{
		url:     url,
		headers: headers,
		timeout: timeout,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Start initializes the HTTP transport
func (t *HTTPTransport) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.connected {
		return fmt.Errorf("transport already started")
	}

	// For HTTP transport, we just mark as connected
	// Actual connection happens per-request
	t.connected = true
	t.requestID = 0

	return nil
}

// SendRequest sends a JSON-RPC request via HTTP POST
func (t *HTTPTransport) SendRequest(ctx context.Context, req interface{}) (json.RawMessage, error) {
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

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	// Send request
	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	// Read response
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return json.RawMessage(respBytes), nil
}

// sendRequestWithMethod sends a request with a specific HTTP method
func (t *HTTPTransport) sendRequestWithMethod(ctx context.Context, method string, req interface{}) (json.RawMessage, error) {
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

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, method, t.url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	// Send request
	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	// Read response
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return json.RawMessage(respBytes), nil
}

// SendNotification sends a JSON-RPC notification via HTTP POST
func (t *HTTPTransport) SendNotification(ctx context.Context, notification interface{}) error {
	// For HTTP, notifications are sent the same way as requests
	_, err := t.SendRequest(ctx, notification)
	return err
}

// Close closes the HTTP transport
func (t *HTTPTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.connected = false
	t.client.CloseIdleConnections()
	return nil
}

// IsConnected returns connection status
func (t *HTTPTransport) IsConnected() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.connected
}

// NextRequestID generates a unique request ID
func (t *HTTPTransport) NextRequestID() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.requestID++
	return t.requestID
}

// Initialize performs MCP initialization handshake
// Tries POST first, then GET if POST fails with 405 Method Not Allowed
func (t *HTTPTransport) Initialize(ctx context.Context) (*mcp.InitializeResult, error) {
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

	// Try POST first
	respBytes, err := t.sendRequestWithMethod(ctx, "POST", req)
	if err != nil {
		// Check if it's a 405 Method Not Allowed error
		if containsHTTPError(err, 405) {
			// Try GET instead
			respBytes, err = t.sendRequestWithMethod(ctx, "GET", req)
			if err != nil {
				return nil, fmt.Errorf("initialize request failed (tried POST and GET): %w", err)
			}
		} else {
			return nil, fmt.Errorf("initialize request failed: %w", err)
		}
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
		// Log but don't fail - some servers may not require this
		fmt.Printf("warning: failed to send initialized notification: %v\n", err)
	}

	return &result, nil
}

// containsHTTPError checks if an error message contains a specific HTTP status code
func containsHTTPError(err error, statusCode int) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	targetMsg := fmt.Sprintf("HTTP error %d", statusCode)
	return len(errMsg) >= len(targetMsg) && errMsg[:len(targetMsg)] == targetMsg
}
