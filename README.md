# MCP-Hub

[![Docker Hub](https://img.shields.io/docker/v/amirtheh/mcp-hub?label=Docker%20Hub&logo=docker)](https://hub.docker.com/r/amirtheh/mcp-hub)
[![Docker Pulls](https://img.shields.io/docker/pulls/amirtheh/mcp-hub?logo=docker)](https://hub.docker.com/r/amirtheh/mcp-hub)
[![Docker Image Size](https://img.shields.io/docker/image-size/amirtheh/mcp-hub/latest?logo=docker)](https://hub.docker.com/r/amirtheh/mcp-hub)
[![Build Status](https://img.shields.io/github/actions/workflow/status/amir-the-h/mcp-hub/docker-build-push.yml?branch=main&logo=github)](https://github.com/amir-the-h/mcp-hub/actions)

A Go-based hub that aggregates multiple MCP (Model Context Protocol) servers and exposes their tools through a unified HTTP API.

## Features

- **Standard MCP Protocol**: Full support for MCP 2024-11-05 specification
- **Multiple Transport Types**: 
  - Stdio: For local MCP servers (Node.js, Python, etc.)
  - HTTP/SSE: For remote MCP servers
- **Configuration-Based**: JSON configuration compatible with Cursor/VSCode format
- **Docker-Ready**: Easy deployment in containers with volume mounts
- **Tool Aggregation**: Combine tools from multiple MCP servers in one place
- **HTTP API**: RESTful endpoints for tool discovery and execution

## Quick Start

### 1. Build

```bash
cd cmd/mcp-hub
go build -o ../../mcp-hub
```

### 2. Create Configuration

Create a `config.json` file (see `config.example.json` for a template):

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    },
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_TOKEN}"
      }
    }
  }
}
```

### 3. Run

```bash
# Using default config.json
./mcp-hub

# Or specify a config file
./mcp-hub --config /path/to/config.json
```

Notes:
- The HTTP listen address can be overridden with the `MCP_HUB_PORT` or `PORT` environment variable. If the value contains a colon it is treated as a full address (e.g. `0.0.0.0:8080`), otherwise it is treated as a port and is prefixed with a colon.
- The binary accepts a `--config` flag (default: `config.json`).

### 4. Use the API

List all available tools:
```bash
curl http://localhost:8080/mcp/tools
```

Execute a tool:
```bash
curl -X POST http://localhost:8080/mcp/execute \
  -H 'Content-Type: application/json' \
  -d '{
    "plugin_id": "filesystem",
    "tool_name": "read_file",
    "arguments": {"path": "/tmp/test.txt"}
  }'
```

List connected servers:
```bash
curl http://localhost:8080/mcp/servers
```

## Configuration Format

The configuration file uses the standard `mcpServers` format compatible with Cursor, VSCode, and Claude Desktop.

### Stdio Servers (Local)

For MCP servers that run as local processes:

```json
{
  "mcpServers": {
    "server-name": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-package"],
      "env": {
        "API_KEY": "${YOUR_API_KEY}"
      },
      "timeout": 30
    }
  }
}
```

Fields:
- `command`: Executable to run (required)
- `args`: Command line arguments (optional)
- `env`: Environment variables (optional, supports `${VAR}` expansion)
- `timeout`: Request timeout in seconds (optional, default: 30)
- `disabled`: Set to `true` to disable a server (optional)

### HTTP Servers (Remote)

For MCP servers accessible via HTTP:

```json
{
  "mcpServers": {
    "remote-server": {
      "type": "http",
      "url": "http://localhost:3000/mcp",
      "headers": {
        "Authorization": "Bearer ${API_TOKEN}"
      },
      "timeout": 45
    }
  }
}
```

Fields:
- `type`: Set to `"http"` for HTTP transport (or auto-detected from `url`)
- `url`: HTTP endpoint URL (required)
- `headers`: HTTP headers to include (optional, supports `${VAR}` expansion)
- `timeout`: Request timeout in seconds (optional, default: 30)

### Environment Variables

Environment variables in the configuration are expanded using `${VAR_NAME}` syntax. For example:

```json
{
  "mcpServers": {
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_TOKEN}"
      }
    }
  }
}
```

Then run:
```bash
GITHUB_TOKEN=your_token_here ./mcp-hub
```

## Docker Deployment

### Build Docker Image
Note: this repository also includes a `Dockerfile` at the project root — see it for the exact image build used by CI/deploy.

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN cd cmd/mcp-hub && go build -o /mcp-hub

FROM node:20-alpine
RUN apk add --no-cache python3 py3-pip
COPY --from=builder /mcp-hub /usr/local/bin/mcp-hub
WORKDIR /app
CMD ["mcp-hub", "--config", "/config/config.json"]
```

### Run with Docker

```bash
# Create a config directory
mkdir -p config

# Put your config.json in the config directory
cp config.json config/

# Run the container
docker run -d \
  -p 8080:8080 \
  -v $(pwd)/config:/config:ro \
  -e GITHUB_TOKEN=${GITHUB_TOKEN} \
  mcp-hub
```

### Docker Compose

```yaml
version: '3.8'
services:
  mcp-hub:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./config:/config:ro
      - ./plugins:/plugins:ro  # Optional: mount custom plugins
    environment:
      - GITHUB_TOKEN=${GITHUB_TOKEN}
      - BRAVE_API_KEY=${BRAVE_API_KEY}
```

## HTTP API Reference

### GET /mcp/tools

List all available tools from all connected MCP servers.

**Response:**
```json
[
  {
    "id": "read_file",
    "name": "read_file",
    "description": "Read contents of a file",
    "plugin_id": "filesystem"
  }
]
```

### POST /mcp/execute

Execute a tool on a specific MCP server.

**Request:**
```json
{
  "plugin_id": "filesystem",
  "tool_name": "read_file",
  "arguments": {
    "path": "/tmp/test.txt"
  }
}
```

**Response:**
MCP tool call result (format depends on the tool)

### GET /mcp/servers

List all connected MCP servers.

**Response:**
```json
{
  "servers": ["filesystem", "github", "brave-search"]
}
```

### GET /mcp/stream

Server-Sent Events stream of tool registry updates (for real-time tool discovery).

## Examples

### Example 1: Using Official MCP Servers

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/home/user/documents"]
    },
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_TOKEN}"
      }
    },
    "brave-search": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-brave-search"],
      "env": {
        "BRAVE_API_KEY": "${BRAVE_API_KEY}"
      }
    }
  }
}
```

### Example 2: Custom Python MCP Server

```json
{
  "mcpServers": {
    "custom-tools": {
      "command": "python3",
      "args": ["/app/plugins/custom_mcp_server.py"]
    }
  }
}
```

### Example 3: Mixed Local and Remote Servers

```json
{
  "mcpServers": {
    "local-filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/data"]
    },
    "remote-api": {
      "type": "http",
      "url": "https://api.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${API_TOKEN}"
      }
    }
  }
}
```

## Development

### Project Structure

```
.
├── cmd/
│   └── mcp-hub/
│       └── main.go           # Entry point
├── internal/
│   ├── config/
│   │   └── config.go         # Configuration parsing
│   ├── mcp/
│   │   └── protocol.go       # MCP protocol structures
│   ├── plugin/
│   │   └── manager.go        # Server management
│   ├── registry/
│   │   └── registry.go       # Tool registry
│   ├── server/
│   │   └── server.go         # HTTP server
│   └── transport/
│       ├── transport.go      # Transport interface
│       ├── stdio.go          # Stdio transport
│       └── http.go           # HTTP transport
├── config.example.json       # Example configuration
└── README.md
```

### Adding New Transport Types

Implement the `Transport` interface in `internal/transport/`:

```go
type Transport interface {
    Start(ctx context.Context) error
    SendRequest(ctx context.Context, req interface{}) (json.RawMessage, error)
    SendNotification(ctx context.Context, notification interface{}) error
    Close() error
    IsConnected() bool
}
```

## Troubleshooting

### Server fails to start

Check logs for specific error messages. Common issues:
- Missing `npx` or `python3` in PATH
- Invalid MCP server package names
- Missing environment variables
- Incorrect file paths in configuration

### Tool execution fails

- Verify the MCP server is properly initialized (check `/mcp/servers`)
- Ensure tool arguments match the expected schema
- Check server logs (stderr output is visible in hub logs)

### Timeout errors

Increase the `timeout` value in server configuration:
```json
{
  "mcpServers": {
    "slow-server": {
      "command": "...",
      "timeout": 120
    }
  }
}
```

## License

MIT

### Docker Servers (Containerized)

For MCP servers running in Docker containers:

```json
{
  "mcpServers": {
    "containerized-server": {
      "type": "docker",
      "image": "my-mcp-server:latest",
      "args": ["--option", "value"],
      "env": {
        "API_KEY": "${YOUR_API_KEY}"
      },
      "volumes": {
        "/host/path": "/container/path",
        "${HOME}/data": "/data"
      },
      "network": "mcp-network",
      "timeout": 60
    }
  }
}
```

Fields:
- `type`: Set to `"docker"` for Docker transport (or auto-detected from `image`)
- `image`: Docker image name (required)
- `args`: Command arguments to pass to container entrypoint (optional)
- `env`: Environment variables (optional, supports `${VAR}` expansion)
- `volumes`: Volume mounts as `host:container` mappings (optional, supports `${VAR}` expansion)
- `network`: Docker network to connect to (optional)
- `timeout`: Request timeout in seconds (optional, default: 30)

**Benefits of Docker Transport:**
- No need to install Node.js, Python, or other runtimes on the hub host
- Isolated dependencies per MCP server
- Easy version management with Docker tags
- Consistent environment across deployments


## Building Docker Images for MCP Servers

You can containerize any MCP server to avoid installing its dependencies on the hub host.

### Example: Dockerizing a Python MCP Server

**Dockerfile:**
```dockerfile
FROM python:3.11-slim

WORKDIR /app

# Install dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy MCP server code
COPY mcp_server.py .

# Run as non-root user
RUN useradd -m -u 1000 mcpuser && chown -R mcpuser:mcpuser /app
USER mcpuser

# The server should read from stdin and write to stdout
ENTRYPOINT ["python", "-u", "mcp_server.py"]
```

**Build and use:**
```bash
# Build the image
docker build -t my-mcp-server:latest .

# Add to config.json
{
  "mcpServers": {
    "my-server": {
      "image": "my-mcp-server:latest"
    }
  }
}
```

### Example: Dockerizing a Node.js MCP Server

**Dockerfile:**
```dockerfile
FROM node:20-alpine

WORKDIR /app

# Install dependencies
COPY package*.json ./
RUN npm ci --production

# Copy server code
COPY . .

# Run as non-root user
RUN addgroup -g 1000 mcpuser && \
    adduser -D -u 1000 -G mcpuser mcpuser && \
    chown -R mcpuser:mcpuser /app
USER mcpuser

ENTRYPOINT ["node", "server.js"]
```

### Important Notes for Docker MCP Servers

1. **Use `-i` flag**: The hub runs containers with `docker run -i` for interactive stdin/stdout
2. **Unbuffered output**: Ensure your server outputs are unbuffered (use `python -u` or `flush()`)
3. **Stdin/Stdout only**: The MCP protocol uses stdin for input and stdout for output
4. **Stderr for logs**: Use stderr for logging (visible in hub logs)
5. **Cleanup**: Containers are run with `--rm` for automatic cleanup

### Pre-built MCP Server Images

Create a registry of Docker images for common MCP servers:

```bash
# Example: Build an echo server image
cd examples/plugins
cat > Dockerfile << 'DOCKERFILE'
FROM python:3.11-slim
COPY mcp_echo.py /app/server.py
WORKDIR /app
RUN chmod +x server.py
ENTRYPOINT ["python", "-u", "server.py"]
DOCKERFILE

docker build -t mcp-echo:latest .
```

Then use it:
```json
{
  "mcpServers": {
    "echo": {
      "image": "mcp-echo:latest"
    }
  }
}
```


## Transport Comparison

| Transport | Use Case | Pros | Cons |
|-----------|----------|------|------|
| **stdio** | Local MCP servers with direct access | Fast, low overhead | Requires runtime (Node.js/Python) installed |
| **Docker** | Isolated, reproducible MCP servers | No runtime dependencies, easy versioning | Slightly higher overhead, requires Docker |
| **HTTP** | Remote/cloud-hosted MCP servers | Scalable, can be load-balanced | Network latency, requires server infrastructure |

### When to Use Docker Transport

Choose Docker transport when:
- ✅ You want to avoid installing Node.js, Python, or other runtimes on your hub host
- ✅ You need consistent, reproducible environments across deployments
- ✅ You want easy version management with Docker tags
- ✅ You're running the hub in a containerized environment (Kubernetes, Docker Compose)
- ✅ You need to isolate server dependencies
- ✅ You want to use pre-built MCP server images from a registry

Choose stdio transport when:
- ✅ You're developing locally and want faster iteration
- ✅ Runtime dependencies are already installed
- ✅ You need the absolute lowest latency

Choose HTTP transport when:
- ✅ MCP servers are hosted remotely
- ✅ You need to scale servers independently
- ✅ You want to use managed MCP server services

### Example: Complete Docker Setup

Here's a complete example running multiple MCP servers in Docker:

```bash
# 1. Build your custom MCP server image
docker build -t my-mcp-server:v1.0 ./my-server

# 2. Create Docker network for inter-container communication
docker network create mcp-network

# 3. Configure servers
cat > config.json << 'JSON'
{
  "mcpServers": {
    "echo": {
      "image": "mcp-echo:latest"
    },
    "custom-tools": {
      "image": "my-mcp-server:v1.0",
      "env": {
        "API_KEY": "${MY_API_KEY}"
      },
      "volumes": {
        "/host/data": "/data"
      }
    }
  }
}
JSON

# 4. Run the hub
MY_API_KEY=secret123 ./mcp-hub --config config.json
```

This setup gives you:
- ✨ No runtime dependencies on the hub host
- ✨ Isolated environments for each MCP server
- ✨ Easy updates by changing Docker image tags
- ✨ Reproducible deployments

## Containerized Deployment

The mcp-hub itself can run in a Docker container. See [DOCKER_DEPLOYMENT.md](DOCKER_DEPLOYMENT.md) for complete deployment guide.

### Quick Docker Deploy

```bash
# Build the image
docker build -t mcp-hub:latest .

# Run with your config
docker run -d \
  --name mcp-hub \
  -p 8080:8080 \
  -v $(pwd)/config.json:/app/config.json:ro \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e GITHUB_TOKEN=${GITHUB_TOKEN} \
  mcp-hub:latest
```

### Or use Docker Compose

```bash
docker-compose up -d
```

**Image Sizes:**
- Standard (with Docker CLI): ~47MB
- Minimal (stdio/HTTP only): ~6MB

Environment variables are automatically passed through and expanded in your configuration.


## Config File Watching

The MCP Hub automatically watches the configuration file for changes and updates the registry accordingly. When you modify `config.json`, the hub will:

- **Add new servers**: Automatically start any newly added MCP servers
- **Remove servers**: Stop servers that are removed from config or disabled
- **Reload servers**: Restart servers whose configuration has changed
- **Update registry**: Keep the tool registry in sync with active servers

### How It Works

The watcher uses `fsnotify` to monitor the config file for write events. When changes are detected:

1. The new config is loaded and validated
2. Changes are compared with the previous configuration
3. Appropriate actions are taken (start/stop/reload servers)
4. The registry is automatically updated
5. Changes are logged for visibility

### Debouncing

To avoid processing rapid successive changes (e.g., when editors write multiple times), the watcher includes a 500ms debounce delay. This ensures the config is only reloaded once after you finish editing.

### Example

```bash
# Start mcp-hub
./mcp-hub --config=config.json

# In another terminal, edit config.json
vim config.json

# The hub will automatically detect changes and log:
# "config file changed, reloading..."
# "adding server: new-server"
# "loaded MCP server: new-server (stdio transport)"
```

### Error Handling

If the new config contains errors:
- Invalid JSON: Changes are rejected, hub continues with previous config
- Missing required fields: Changes are rejected with validation error
- Server startup failures: Logged as warnings, other servers continue running

