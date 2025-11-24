package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/amir-the-h/mcp-hub/internal/plugin"
	"github.com/amir-the-h/mcp-hub/internal/registry"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// New creates an HTTP server that serves MCP Streamable HTTP using the SDK.
// It builds a single SDK Server instance and keeps it synchronized with the
// hub registry (tools aggregated and namespaced as <plugin>:<tool>).
func New(reg *registry.Registry, pm *plugin.Manager) *http.Server {
	impl := &mcp.Implementation{Name: "mcp-hub", Version: "0.1.0"}
	sdkServer := mcp.NewServer(impl, &mcp.ServerOptions{HasTools: true})

	// Track registered tools so we can remove stale ones
	registered := make(map[string]bool)

	// Synchronize registry snapshots to SDK server tools
	ch := reg.Subscribe()
	go func() {
		defer reg.Unsubscribe(ch)
		for snapshot := range ch {
			desired := make(map[string]struct{})
			for _, t := range snapshot {
				namespaced := t.PluginID + ":" + t.Name
				desired[namespaced] = struct{}{}
				if !registered[namespaced] {
					// add tool with simple object input schema
					tool := &mcp.Tool{
						Name:        namespaced,
						Description: t.Description,
						InputSchema: map[string]any{"type": "object"},
					}
					// handler forwards to plugin.Manager
					handler := func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						// parse namespaced name
						name := req.Params.Name
						idx := strings.Index(name, ":")
						var pluginID, toolName string
						if idx >= 0 {
							pluginID = name[:idx]
							toolName = name[idx+1:]
						} else {
							// fallback: if only one server, use it
							servers := pm.ListServers()
							if len(servers) == 1 {
								pluginID = servers[0]
								toolName = name
							} else {
								return nil, fmt.Errorf("tool name must be namespaced as <plugin>:<tool>")
							}
						}

						respBytes, err := pm.Execute(ctx, pluginID, toolName, req.Params.Arguments)
						if err != nil {
							return nil, err
						}

						var result mcp.CallToolResult
						if err := json.Unmarshal(respBytes, &result); err != nil {
							// Return raw text content if unmarshal fails
							res := &mcp.CallToolResult{}
							res.Content = []mcp.Content{&mcp.TextContent{Text: string(respBytes)}}
							return res, nil
						}
						return &result, nil
					}

					sdkServer.AddTool(tool, handler)
					registered[namespaced] = true
				}
			}
			// remove tools that are no longer present
			var toRemove []string
			for name := range registered {
				if _, ok := desired[name]; !ok {
					toRemove = append(toRemove, name)
				}
			}
			if len(toRemove) > 0 {
				sdkServer.RemoveTools(toRemove...)
				for _, n := range toRemove {
					delete(registered, n)
				}
			}
		}
	}()

	// Create streamable HTTP handler using SDK helper
	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server { return sdkServer }, nil)

	return &http.Server{Addr: ":8080", Handler: handler, ReadTimeout: 15 * time.Second}
}
