# Docker Deployment Guide

This guide covers deploying mcp-hub in Docker containers.

## Image Variants

### 1. Standard Image (`Dockerfile`)
- **Size**: ~47MB
- **Includes**: Alpine Linux + Docker CLI + mcp-hub binary
- **Use when**: You need Docker transport support for running MCP servers in containers
- **Best for**: Production deployments with mixed transports

```bash
docker build -t mcp-hub:latest .
```

### 2. Local MCP Servers Image (`Dockerfile.local`)
- **Size**: ~800MB
- **Includes**: Node.js 20, Python 3, uv, curl, git, and mcp-hub binary
- **Use when**: You want to run local stdio-based MCP servers in the same container
- **Best for**: Self-contained deployments with Node.js/Python MCP servers

```bash
docker build -f Dockerfile.local -t mcp-hub:local .
```

## Quick Start

### Option 1: Docker Run

```bash
# Create config file
cat > config.json << 'JSON'
{
  "mcpServers": {
    "echo": {
      "image": "mcp-echo:latest"
    }
  }
}
JSON

# Run the hub
docker run -d \
  --name mcp-hub \
  -p 8080:8080 \
  -v $(pwd)/config.json:/app/config.json:ro \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --restart unless-stopped \
  mcp-hub:latest
```

Notes:
- The container/binary respects the `--config` flag (default path: `/app/config.json`).
- You can override the listen address/port with the `MCP_HUB_PORT` or `PORT` environment variable (same semantics as running locally).

### Option 2: Docker Compose

```bash
# Using the provided docker-compose.yml
docker-compose up -d
```

## Environment Variables

Pass environment variables to enable config expansion:

```bash
docker run -d \
  --name mcp-hub \
  -p 8080:8080 \
  -v $(pwd)/config.json:/app/config.json:ro \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e GITHUB_TOKEN=${GITHUB_TOKEN} \
  -e BRAVE_API_KEY=${BRAVE_API_KEY} \
  -e API_TOKEN=${API_TOKEN} \
  mcp-hub:latest
```

Or use an env file:

```bash
# .env file
GITHUB_TOKEN=ghp_xxxxx
BRAVE_API_KEY=BSA_xxxxx
API_TOKEN=secret123

# Run with env file
docker run -d \
  --name mcp-hub \
  -p 8080:8080 \
  -v $(pwd)/config.json:/app/config.json:ro \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --env-file .env \
  mcp-hub:latest
```

## Volume Mounts

### Required Volumes

1. **Configuration file**:
   ```bash
   -v $(pwd)/config.json:/app/config.json:ro
   ```

2. **Docker socket** (if using Docker transport):
   ```bash
   -v /var/run/docker.sock:/var/run/docker.sock
   ```

### Optional Volumes

3. **Data directories** (for stdio servers that need file access):
   ```bash
   -v /host/data:/data:ro
   ```

4. **Plugin directories** (for custom MCP server scripts):
   ```bash
   -v $(pwd)/plugins:/plugins:ro
   ```

## Docker Compose Example

```yaml
version: '3.8'

services:
  mcp-hub:
    image: mcp-hub:latest
    container_name: mcp-hub
    ports:
      - "8080:8080"
    volumes:
      - ./config.json:/app/config.json:ro
      - /var/run/docker.sock:/var/run/docker.sock
      - ./data:/data:ro
    environment:
      - GITHUB_TOKEN=${GITHUB_TOKEN}
      - BRAVE_API_KEY=${BRAVE_API_KEY}
    restart: unless-stopped
    networks:
      - mcp-network

networks:
  mcp-network:
    driver: bridge
```

## Configuration Examples

### Self-Contained Deployment with Local MCP Servers

Use the `mcp-hub:local` image when you want to run everything in a single container without external services:

```json
{
  "mcpServers": {
    "node-server": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/data"]
    },
    "python-script": {
      "command": "python3",
      "args": ["/plugins/my_mcp_server.py"]
    },
    "custom-binary": {
      "command": "/plugins/custom-mcp-tool"
    },
    "uv-project": {
      "command": "uv",
      "args": ["run", "python", "-m", "my_mcp_module"],
      "env": {
        "PYTHONPATH": "/plugins"
      }
    }
  }
}
```

Run with volume mounts for custom scripts:

```bash
docker run -d \
  --name mcp-hub \
  -p 8080:8080 \
  -v $(pwd)/config.json:/app/config.json:ro \
  -v $(pwd)/plugins:/plugins:ro \
  -v /data:/data \
  --restart unless-stopped \
  mcp-hub:local
```

Or with Docker Compose:

```yaml
version: '3.8'

services:
  mcp-hub:
    image: mcp-hub:local
    container_name: mcp-hub
    ports:
      - "8080:8080"
    volumes:
      - ./config.json:/app/config.json:ro
      - ./plugins:/plugins:ro
      - ./data:/data
    environment:
      - NODE_ENV=production
    restart: unless-stopped
```

### Mixed Transport Configuration

```json
{
  "mcpServers": {
    "stdio-local": {
      "command": "node",
      "args": ["/plugins/my-server.js"]
    },
    "docker-containerized": {
      "image": "my-mcp-server:latest",
      "volumes": {
        "/data": "/data"
      }
    },
    "http-remote": {
      "type": "http",
      "url": "https://api.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${API_TOKEN}"
      }
    }
  }
}
```

## Security Considerations

### 1. Docker Socket Access

Mounting the Docker socket gives the container full control over Docker on the host:

```bash
# Run as non-root when possible (default in our image)
# The mcpuser (UID 1000) is created in the image

# If you need root access for Docker socket:
docker run --user root ...
```

### 2. Read-Only Configuration

Always mount config as read-only:
```bash
-v $(pwd)/config.json:/app/config.json:ro
```

### 3. Network Isolation

Use Docker networks to isolate MCP servers:
```yaml
networks:
  mcp-network:
    driver: bridge
    internal: true  # No external access
```

### 4. Resource Limits

Set resource limits:
```yaml
services:
  mcp-hub:
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 1G
        reservations:
          cpus: '0.5'
          memory: 256M
```

## Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-hub
spec:
  replicas: 1
  selector:
    matchLabels:
      app: mcp-hub
  template:
    metadata:
      labels:
        app: mcp-hub
    spec:
      containers:
      - name: mcp-hub
        image: mcp-hub:latest
        ports:
        - containerPort: 8080
        volumeMounts:
        - name: config
          mountPath: /app/config.json
          subPath: config.json
          readOnly: true
        - name: docker-sock
          mountPath: /var/run/docker.sock
        env:
        - name: GITHUB_TOKEN
          valueFrom:
            secretKeyRef:
              name: mcp-secrets
              key: github-token
      volumes:
      - name: config
        configMap:
          name: mcp-config
      - name: docker-sock
        hostPath:
          path: /var/run/docker.sock
---
apiVersion: v1
kind: Service
metadata:
  name: mcp-hub
spec:
  selector:
    app: mcp-hub
  ports:
  - port: 8080
    targetPort: 8080
```

## Health Checks

Add health checks to your deployment:

```yaml
services:
  mcp-hub:
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/mcp/servers"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s
```

## Logging

View logs:
```bash
# Docker
docker logs mcp-hub

# Docker Compose
docker-compose logs -f mcp-hub

# Kubernetes
kubectl logs -f deployment/mcp-hub
```

## Troubleshooting

### Issue: Docker transport not working

**Solution**: Ensure Docker socket is mounted and user has access:
```bash
docker run --user root -v /var/run/docker.sock:/var/run/docker.sock ...
```

### Issue: Config file not found

**Solution**: Check volume mount path:
```bash
docker exec mcp-hub ls -la /app/
```

### Issue: Environment variables not expanded

**Solution**: Pass env vars to container:
```bash
docker run -e GITHUB_TOKEN=${GITHUB_TOKEN} ...
```

### Issue: MCP servers can't communicate

**Solution**: Use shared Docker network:
```yaml
networks:
  mcp-network:
    external: false
```

## Production Checklist

- [ ] Use specific image tags (not `latest`)
- [ ] Mount config as read-only
- [ ] Set resource limits
- [ ] Configure health checks
- [ ] Set up log aggregation
- [ ] Use secrets for sensitive env vars
- [ ] Enable restart policy
- [ ] Set up monitoring
- [ ] Test backup/restore procedures
- [ ] Document your configuration
