package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/amir-the-h/mcp-hub/internal/mcp"
)

// SSETransport implements Server-Sent Events based MCP transport
type SSETransport struct {
	baseURL string
	headers map[string]string
	timeout time.Duration

	client     *http.Client
	sseConn    *http.Response
	mu         sync.Mutex
	requestID  int
	connected  bool
	responses  map[int]chan json.RawMessage
	responseMu sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewSSETransport creates a new SSE transport
func NewSSETransport(baseURL string, headers map[string]string, timeout time.Duration) *SSETransport {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	// Remove /sse suffix if present
	baseURL = strings.TrimSuffix(baseURL, "/sse")

	return &SSETransport{
		baseURL:   baseURL,
		headers:   headers,
		timeout:   timeout,
		client:    &http.Client{Timeout: timeout},
		responses: make(map[int]chan json.RawMessage),
	}
}

// Start initializes the SSE transport and establishes the SSE connection
func (t *SSETransport) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.connected {
		return fmt.Errorf("transport already started")
	}

	t.ctx, t.cancel = context.WithCancel(ctx)

	// Establish SSE connection
	sseURL := t.baseURL + "/sse"
	req, err := http.NewRequestWithContext(t.ctx, "GET", sseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create SSE request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to SSE: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("SSE connection failed with status %d: %s", resp.StatusCode, string(body))
	}

	t.sseConn = resp
	t.connected = true
	t.requestID = 0

	// Start reading SSE events
	go t.readSSEEvents()

	return nil
}

// readSSEEvents reads and processes SSE events
func (t *SSETransport) readSSEEvents() {
	defer func() {
		t.mu.Lock()
		t.connected = false
		t.mu.Unlock()
	}()

	scanner := bufio.NewScanner(t.sseConn.Body)
	var eventData strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		// Empty line indicates end of event
		if line == "" {
			if eventData.Len() > 0 {
				t.handleSSEMessage(eventData.String())
				eventData.Reset()
			}
			continue
		}

		// Parse SSE field
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			eventData.WriteString(data)
		}
		// Ignore other SSE fields (event:, id:, retry:)
	}

	if err := scanner.Err(); err != nil && t.ctx.Err() == nil {
		fmt.Printf("SSE read error: %v\n", err)
	}
}

// handleSSEMessage processes a received SSE message
func (t *SSETransport) handleSSEMessage(data string) {
	var msg mcp.JSONRPCResponse
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		fmt.Printf("failed to parse SSE message: %v\n", err)
		return
	}

	// Route response to waiting request
	if msg.ID != nil {
		t.responseMu.Lock()
		if ch, ok := t.responses[int(msg.ID.(float64))]; ok {
			select {
			case ch <- msg.Result:
			default:
			}
		}
		t.responseMu.Unlock()
	}
}

// SendRequest sends a JSON-RPC request via POST to /messages
func (t *SSETransport) SendRequest(ctx context.Context, req interface{}) (json.RawMessage, error) {
	t.mu.Lock()
	if !t.connected {
		t.mu.Unlock()
		return nil, fmt.Errorf("transport not connected")
	}
	t.mu.Unlock()

	// Extract request ID
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	var reqMsg mcp.JSONRPCRequest
	if err := json.Unmarshal(reqBytes, &reqMsg); err != nil {
		return nil, fmt.Errorf("failed to parse request: %w", err)
	}

	// Create response channel
	respCh := make(chan json.RawMessage, 1)
	t.responseMu.Lock()
	t.responses[int(reqMsg.ID.(float64))] = respCh
	t.responseMu.Unlock()

	defer func() {
		t.responseMu.Lock()
		delete(t.responses, int(reqMsg.ID.(float64)))
		t.responseMu.Unlock()
		close(respCh)
	}()

	// Send request to /messages endpoint
	messagesURL := t.baseURL + "/messages"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", messagesURL, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// For SSE, the POST to /messages might return 202 Accepted
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	// Wait for response via SSE
	select {
	case result := <-respCh:
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(t.timeout):
		return nil, fmt.Errorf("request timeout")
	}
}

// SendNotification sends a JSON-RPC notification
func (t *SSETransport) SendNotification(ctx context.Context, notification interface{}) error {
	_, err := t.SendRequest(ctx, notification)
	return err
}

// Close closes the SSE transport
func (t *SSETransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cancel != nil {
		t.cancel()
	}

	if t.sseConn != nil {
		t.sseConn.Body.Close()
	}

	t.connected = false
	t.client.CloseIdleConnections()
	return nil
}

// IsConnected returns connection status
func (t *SSETransport) IsConnected() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.connected
}

// NextRequestID generates a unique request ID
func (t *SSETransport) NextRequestID() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.requestID++
	return t.requestID
}

// Initialize performs MCP initialization handshake
func (t *SSETransport) Initialize(ctx context.Context) (*mcp.InitializeResult, error) {
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
		// Log but don't fail - some servers may not require this
		fmt.Printf("warning: failed to send initialized notification: %v\n", err)
	}

	return &result, nil
}
