package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/amir-the-h/mcp-hub/internal/config"
	"github.com/amir-the-h/mcp-hub/internal/registry"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPServer represents a connected MCP server using the official SDK
type MCPServer struct {
	name    string
	client  *mcp.Client
	session *mcp.ClientSession
	mu      sync.Mutex
}

// Manager manages MCP servers using the official SDK
type Manager struct {
	reg     *registry.Registry
	mu      sync.Mutex
	servers map[string]*MCPServer
}

// NewManager creates a new plugin manager
func NewManager(reg *registry.Registry) *Manager {
	return &Manager{
		reg:     reg,
		servers: make(map[string]*MCPServer),
	}
}

// LoadFromConfig loads and starts servers from configuration
func (m *Manager) LoadFromConfig(ctx context.Context, cfg *config.Config) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	enabledServers := cfg.GetEnabledServers()

	for name, srvCfg := range enabledServers {
		if err := m.StartServer(ctx, name, srvCfg); err != nil {
			log.Printf("warning: failed to start server %s: %v", name, err)
		} else {
			log.Printf("loaded MCP server: %s (%s transport)", name, srvCfg.TransportType())
		}
	}

	return nil
}

// StartServer starts a single MCP server based on configuration
func (m *Manager) StartServer(ctx context.Context, name string, cfg config.ServerConfig) error {
	m.mu.Lock()
	if _, exists := m.servers[name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("server %s already started", name)
	}
	m.mu.Unlock()

	// Create MCP client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "mcp-hub",
		Version: "0.1.0",
	}, nil)

	// Create appropriate transport
	var transport mcp.Transport

	switch cfg.TransportType() {
	case "stdio":
		// For stdio, use CommandTransport
		cmd := exec.Command(cfg.Command, cfg.Args...)
		if cfg.Env != nil {
			cmd.Env = append(cmd.Env, envMapToSlice(cfg.Env)...)
		}
		transport = &mcp.CommandTransport{Command: cmd}

	case "docker":
		// For Docker, build docker run command
		args := buildDockerArgs(cfg)
		cmd := exec.Command("docker", args...)
		transport = &mcp.CommandTransport{Command: cmd}

	case "http":
		// For HTTP/Streamable HTTP, use StreamableClientTransport
		transport = &mcp.StreamableClientTransport{
			Endpoint:   cfg.URL,
			HTTPClient: &http.Client{},
		}

	case "sse":
		// For legacy SSE, use SSEClientTransport
		transport = &mcp.SSEClientTransport{
			Endpoint:   cfg.URL,
			HTTPClient: &http.Client{},
		}

	default:
		return fmt.Errorf("unsupported transport type: %s", cfg.TransportType())
	}

	// Attempt to connect to the server
	// WORKAROUND: For HTTP and Streamable HTTP transports, the SDK (v1.1.0) automatically tries to subscribe
	// to listChanged notifications when a server reports listChanged: true in capabilities.
	// This causes "rejected by transport: undelivered message" errors because HTTP/Streamable HTTP
	// transports don't support bidirectional notifications. The SDK retries in a loop,
	// causing an infinite loop of errors.
	//
	// The issue is in the SDK's internal handling of server capabilities. Until the SDK
	// is updated to handle this properly, we work around it by:
	// 1. Connecting normally (the error happens in background goroutines)
	// 2. The errors are logged but don't prevent the connection from working
	// 3. Tool listing and execution still work correctly
	//
	// Note: "streamable-http" is normalized to "http" in config, so it uses the same code path
	//
	// TODO: Update to newer SDK version when available that fixes this issue
	log.Printf("connect:attempt server=%s transport=%s", name, cfg.TransportType())
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		log.Printf("connect:fail server=%s transport=%s err=%v", name, cfg.TransportType(), err)
		return fmt.Errorf("failed to connect: %w", err)
	}

	log.Printf("connect:ok server=%s transport=%s", name, cfg.TransportType())
	
	// For HTTP and Streamable HTTP transports, log a warning about potential notification errors
	// These errors are harmless and don't affect functionality
	// Note: "streamable-http" is normalized to "http" in config, so it's covered by this check
	if cfg.TransportType() == "http" || cfg.TransportType() == "sse" {
		log.Printf("warning: HTTP/Streamable HTTP transport detected for server %s. If the server reports listChanged: true, "+
			"you may see 'rejected by transport: undelivered message' errors in logs. "+
			"This is a known SDK limitation and doesn't affect functionality.", name)
	}

	// Create server instance
	server := &MCPServer{
		name:    name,
		client:  client,
		session: session,
	}

	// List tools
	toolsResult, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		session.Close()
		return fmt.Errorf("failed to list tools: %w", err)
	}

	log.Printf("MCP server %s: discovered %d tools", name, len(toolsResult.Tools))

	// Register tools in registry
	registryTools := make([]registry.Tool, len(toolsResult.Tools))
	for i, tool := range toolsResult.Tools {
		registryTools[i] = registry.Tool{
			ID:          tool.Name,
			Name:        tool.Name,
			Description: tool.Description,
			PluginID:    name,
		}
	}
	m.reg.RegisterTools(name, registryTools)

	// Store server
	m.mu.Lock()
	m.servers[name] = server
	m.mu.Unlock()

	return nil
}

// Execute executes a tool on an MCP server
func (m *Manager) Execute(ctx context.Context, pluginID string, toolName string, arguments json.RawMessage) (json.RawMessage, error) {
	m.mu.Lock()
	server, ok := m.servers[pluginID]
	m.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("server not found: %s", pluginID)
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	// Parse arguments
	var args map[string]any
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &args); err != nil {
			return nil, fmt.Errorf("failed to parse arguments: %w", err)
		}
	}

	// Call tool (log start/end with duration and sizes)
	reqID := time.Now().UnixNano()
	argStr := ""
	if len(arguments) > 0 {
		if len(arguments) > 200 {
			argStr = string(arguments[:200]) + "..."
		} else {
			argStr = string(arguments)
		}
	}
	log.Printf("exec:start id=%d plugin=%s tool=%s args=%s", reqID, pluginID, toolName, argStr)
	start := time.Now()

	result, err := server.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
	dur := time.Since(start)
	if err != nil {
		log.Printf("exec:fail id=%d plugin=%s tool=%s duration=%s err=%v", reqID, pluginID, toolName, dur, err)
		return nil, fmt.Errorf("tool call failed: %w", err)
	}

	// Marshal result for returning and for logging
	respBytes, merr := json.Marshal(result)
	if merr != nil {
		log.Printf("exec:fail id=%d plugin=%s tool=%s duration=%s err=%v", reqID, pluginID, toolName, dur, merr)
		return nil, fmt.Errorf("failed to marshal tool result: %w", merr)
	}

	log.Printf("exec:done id=%d plugin=%s tool=%s duration=%s resultBytes=%d isError=%v", reqID, pluginID, toolName, dur, len(respBytes), result.IsError)

	if result.IsError {
		return nil, fmt.Errorf("tool returned error")
	}

	return respBytes, nil
}

// StopServer stops a single MCP server
func (m *Manager) StopServer(name string) error {
	m.mu.Lock()
	server, ok := m.servers[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("server not found: %s", name)
	}
	delete(m.servers, name)
	m.mu.Unlock()

	// Unregister tools from registry
	m.reg.UnregisterTools(name)

	// Close session
	if err := server.session.Close(); err != nil {
		return fmt.Errorf("failed to close server %s: %w", name, err)
	}

	log.Printf("stopped MCP server: %s", name)
	return nil
}

// ReloadServer stops and restarts a server with new configuration
func (m *Manager) ReloadServer(ctx context.Context, name string, cfg config.ServerConfig) error {
	// Stop existing server if it exists
	if _, exists := m.GetServer(name); exists {
		if err := m.StopServer(name); err != nil {
			return fmt.Errorf("failed to stop server for reload: %w", err)
		}
	}

	// Start with new configuration
	return m.StartServer(ctx, name, cfg)
}

// StopAll stops all running servers
func (m *Manager) StopAll(ctx context.Context) {
	m.mu.Lock()
	servers := make([]*MCPServer, 0, len(m.servers))
	for _, s := range m.servers {
		servers = append(servers, s)
	}
	m.mu.Unlock()

	for _, s := range servers {
		if err := s.session.Close(); err != nil {
			log.Printf("error closing server %s: %v", s.name, err)
		}
	}
}

// GetServer returns server information
func (m *Manager) GetServer(name string) (*MCPServer, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	server, ok := m.servers[name]
	return server, ok
}

// ListServers returns list of running servers
func (m *Manager) ListServers() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	return names
}

// Helper functions

func envMapToSlice(m map[string]string) []string {
	result := make([]string, 0, len(m))
	for k, v := range m {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}
	return result
}

func buildDockerArgs(cfg config.ServerConfig) []string {
	args := []string{"run", "--rm", "-i"}

	// Add environment variables
	for k, v := range cfg.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Add volume mounts
	for host, container := range cfg.Volumes {
		args = append(args, "-v", fmt.Sprintf("%s:%s", host, container))
	}

	// Add network
	if cfg.Network != "" {
		args = append(args, "--network", cfg.Network)
	}

	// Add image
	args = append(args, cfg.Image)

	// Add args if any
	args = append(args, cfg.Args...)

	return args
}
