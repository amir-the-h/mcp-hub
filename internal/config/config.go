package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config represents the MCP hub configuration
type Config struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// ServerConfig represents a single MCP server configuration
type ServerConfig struct {
	// Common fields
	Disabled bool              `json:"disabled,omitempty"`
	Timeout  int               `json:"timeout,omitempty"` // in seconds
	Env      map[string]string `json:"env,omitempty"`

	// Transport type (stdio, sse, http, streamable-http, docker)
	Type string `json:"type,omitempty"` // if not specified, inferred from command/url/image

	// For stdio transport
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`

	// For HTTP transports (SSE, Streamable HTTP)
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`

	// For Docker transport
	Image   string            `json:"image,omitempty"`   // Docker image name
	Volumes map[string]string `json:"volumes,omitempty"` // host:container volume mappings
	Network string            `json:"network,omitempty"` // Docker network name

	// Legacy support - if transport not specified in type field
	Transport string `json:"transport,omitempty"` // "stdio", "sse", "docker", etc.
}

// TransportType returns the normalized transport type
func (s *ServerConfig) TransportType() string {
	// Check Type field first
	if s.Type != "" {
		return normalizeTransport(s.Type)
	}
	// Check legacy Transport field
	if s.Transport != "" {
		return normalizeTransport(s.Transport)
	}
	// Infer from configuration
	if s.Image != "" {
		return "docker"
	}
	if s.URL != "" {
		return "http"
	}
	if s.Command != "" {
		return "stdio"
	}
	return "stdio" // default
}

func normalizeTransport(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	switch t {
	case "stdio":
		return "stdio"
	case "sse":
		return "sse"
	case "http", "streamable-http", "streamablehttp":
		return "http"
	case "docker", "container":
		return "docker"
	default:
		return t
	}
}

// Load reads and parses the configuration file
func Load(path string) (*Config, error) {
	// Expand ~ to home directory
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate and process environment variables
	if err := cfg.processEnvVars(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// processEnvVars expands environment variables in configuration
func (c *Config) processEnvVars() error {
	for name, srv := range c.MCPServers {
		// Expand environment variables in env values
		if srv.Env != nil {
			for k, v := range srv.Env {
				srv.Env[k] = os.ExpandEnv(v)
			}
		}

		// Expand in command
		if srv.Command != "" {
			srv.Command = os.ExpandEnv(srv.Command)
		}

		// Expand in args
		for i, arg := range srv.Args {
			srv.Args[i] = os.ExpandEnv(arg)
		}

		// Expand in URL
		if srv.URL != "" {
			srv.URL = os.ExpandEnv(srv.URL)
		}

		// Expand in headers
		if srv.Headers != nil {
			for k, v := range srv.Headers {
				srv.Headers[k] = os.ExpandEnv(v)
			}
		}

		// Expand in Docker image
		if srv.Image != "" {
			srv.Image = os.ExpandEnv(srv.Image)
		}

		// Expand in volumes (both keys and values)
		if srv.Volumes != nil {
			newVolumes := make(map[string]string)
			for k, v := range srv.Volumes {
				newKey := os.ExpandEnv(k)
				newVal := os.ExpandEnv(v)
				newVolumes[newKey] = newVal
			}
			srv.Volumes = newVolumes
		}

		c.MCPServers[name] = srv
	}
	return nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	for name, srv := range c.MCPServers {
		if srv.Disabled {
			continue
		}

		transport := srv.TransportType()
		switch transport {
		case "stdio":
			if srv.Command == "" {
				return fmt.Errorf("server %s: command is required for stdio transport", name)
			}
		case "sse":
			if srv.URL == "" {
				return fmt.Errorf("server %s: url is required for sse transport", name)
			}
		case "http":
			if srv.URL == "" {
				return fmt.Errorf("server %s: url is required for http transport", name)
			}
		case "docker":
			if srv.Image == "" {
				return fmt.Errorf("server %s: image is required for docker transport", name)
			}
		default:
			return fmt.Errorf("server %s: unsupported transport type: %s", name, transport)
		}
	}
	return nil
}

// GetEnabledServers returns a list of enabled server configurations
func (c *Config) GetEnabledServers() map[string]ServerConfig {
	enabled := make(map[string]ServerConfig)
	for name, srv := range c.MCPServers {
		if !srv.Disabled {
			enabled[name] = srv
		}
	}
	return enabled
}
