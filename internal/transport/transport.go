package transport

import (
	"context"
	"encoding/json"
)

// Transport defines the interface for MCP communication transports
type Transport interface {
	// Start initializes the transport connection
	Start(ctx context.Context) error

	// SendRequest sends a JSON-RPC request and waits for response
	SendRequest(ctx context.Context, req interface{}) (json.RawMessage, error)

	// SendNotification sends a JSON-RPC notification (no response expected)
	SendNotification(ctx context.Context, notification interface{}) error

	// Close closes the transport
	Close() error

	// IsConnected returns whether the transport is currently connected
	IsConnected() bool
}
