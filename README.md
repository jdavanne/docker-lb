# docker-lb

A lightweight TCP/HTTP/HTTPS load balancer with dynamic DNS resolution, designed for Docker Compose scaled services.

## Features

- **Multiple Load Balancing Algorithms**:
  - **Random**: Simple random selection (default)
  - **Round-Robin**: Sequential distribution across backends
  - **Least-Connection**: Routes to backend with fewest active connections
  - **Weighted-Random**: Intelligent probabilistic selection using connection counts
- **IP Affinity**: Source IP-based sticky sessions with configurable TTL (default: 30s)
- **HTTP/HTTPS Cookie Affinity**: Cookie-based session persistence
- **Port Range Mapping**: Map multiple ports in a single command (e.g., `8080-8090:backend:9000-9100`)
- **Dynamic DNS Resolution**: Automatically discovers and updates backend IPs
- **Connection Tracking**: Per-backend metrics (active connections, total requests, bytes transferred)
- **Proxy Protocol Support**: v1/v2 support, per-mapping configuration, preserves original client IPs
- **TLS Support**: HTTPS with auto-generated or custom certificates
- **Real-time Metrics**: Connection tracking and data transfer monitoring

## Installation

### Docker
```bash
docker pull davinci1976/docker-lb:latest
```

### Local Build
```bash
make build
# Binary will be at ./bin/lb
```

## Usage

### Command Line Syntax
```bash
lb [options] <port-mapping> [<port-mapping>...]
```

#### Port Mapping Formats:
- **TCP**: `[listen_port:]hostname:backend_port`
- **HTTP**: `[listen_port:]hostname:backend_port,http`
- **HTTPS**: `[listen_port:]hostname:backend_port,https`

#### Port Range Syntax:
Ports can be specified as single values or as ranges:
- **Single port**: `8080`
- **Port range**: `8080-8090` (expands to 8080, 8081, ..., 8090)

**Important**: When using ranges, both listen and backend port ranges must have the same length.

**Examples**:
```bash
# Map ports 8080-8083 to backend ports 9000-9003
lb 8080-8083:backend:9000-9003

# Map the same port range on both sides
lb 8080-8090:backend:8080-8090,http

# Multiple port ranges for HTTPS
lb 8443-8445:backend:9443-9445,https
```

#### Options:
- `--verbose`: Enable detailed logging
- `--lb-algorithm <algo>`: Global load balancing algorithm: `random`, `round-robin`, `least-connection`, `weighted-random` (default: `random`)
- `--affinity-ttl <duration>`: IP affinity TTL in seconds (default: 30s, 0 to disable)
- `--backend-weights <config>`: Explicit backend weights for weighted-random (format: `host:ip1=weight1,ip2=weight2;...`)
- `--probe-period <duration>`: DNS probe interval (default: 2s)
- `--server-proxy-protocol`: *(Deprecated)* Enable proxy protocol v1 on server side globally (use per-mapping `proxy-server` instead)
- `--client-proxy-protocol`: *(Deprecated)* Enable proxy protocol v1 on client side globally (use per-mapping `proxy-client` instead)
- `--cert <file>`: TLS certificate file for HTTPS
- `--key <file>`: TLS key file for HTTPS

### Load Balancing Algorithms

#### Random (default)
- Pure random selection from available backends
- Stateless, simple, good for stateless services
- No connection awareness

```bash
lb 8080:backend:9000
# or explicitly:
lb 8080:backend:9000,lb=random
```

#### Round-Robin
- Sequential rotation through backends
- Fair distribution over time
- No connection awareness

```bash
lb 8080:backend:9000,lb=round-robin
```

#### Least-Connection
- Always selects backend with minimum active connections
- Best for long-lived connections (WebSockets, streaming)
- Deterministic selection

```bash
lb 8080:backend:9000,http,lb=least-connection
```

#### Weighted-Random (Intelligent Default)
- **By default**: Uses inverse connection counts as implicit weights
  - Backends with fewer connections get higher selection probability
  - Provides gradual, probabilistic load balancing
  - Prevents thundering herd to newly available backends
- **With explicit weights**: Uses configured weights only (ignores connections)

```bash
# Implicit weights (connection-based)
lb 8080:backend:9000,lb=weighted-random

# Explicit weights (manual control)
lb --backend-weights backend:10.0.0.1=100,10.0.0.2=50,10.0.0.3=10 \
  8080:backend:9000,lb=weighted-random
```

### IP Affinity

Source IP-based sticky sessions with configurable TTL:

```bash
# Enable affinity with default 30s TTL
lb 8080:backend:9000,affinity

# Custom TTL
lb --affinity-ttl 60s 8080:backend:9000,affinity

# Disable affinity globally
lb --affinity-ttl 0 8080:backend:9000

# Combine with algorithms
lb 8080:backend:9000,http,lb=least-connection,affinity
```

**How it works:**
1. First request from source IP → backend selected by algorithm
2. Subsequent requests from same IP → same backend (if available)
3. TTL resets on connection close (keeps sessions alive)
4. After TTL expires → new backend selection

### Proxy Protocol Support

The PROXY protocol preserves original client IP addresses and connection information when traffic passes through proxies or load balancers. docker-lb supports both v1 (text) and v2 (binary) of the protocol, configurable per-mapping.

#### What is PROXY Protocol?

When a TCP connection passes through a proxy, the backend server sees the proxy's IP instead of the original client IP. The PROXY protocol solves this by prepending connection metadata to the TCP stream.

- **Version 1 (v1)**: Human-readable text format (e.g., `PROXY TCP4 192.168.1.1 10.0.0.1 56324 443\r\n`)
- **Version 2 (v2)**: Binary format, more efficient and supports additional metadata

#### Configuration

**Per-mapping options** (recommended):
```bash
# Server-side: docker-lb expects incoming connections to have PROXY headers
lb 8080:backend:9000,proxy-server=v1

# Client-side: docker-lb sends PROXY headers to backends
lb 8080:backend:9000,proxy-client=v2

# Both sides with different versions
lb 8080:backend:9000,proxy-server=v1,proxy-client=v2

# HTTP/HTTPS with proxy protocol
lb 8080:backend:9000,http,proxy-server=v2
lb 8443:backend:9443,https,proxy-client=v1
```

**Global options** (deprecated, applies v1 to all mappings):
```bash
lb --server-proxy-protocol --client-proxy-protocol 8080:backend:9000
```

#### When to Use

**Server-side (`proxy-server`)**: Enable when docker-lb sits behind another proxy/load balancer that sends PROXY headers:
```
[Client] → [HAProxy/nginx with PROXY] → [docker-lb with proxy-server=v1] → [Backend]
```

**Client-side (`proxy-client`)**: Enable when backends support PROXY protocol and need original client IPs:
```
[Client] → [docker-lb with proxy-client=v2] → [Backend with PROXY support]
```

**Both sides**: Chain multiple proxies while preserving client IPs:
```
[Client] → [Frontend LB] → [docker-lb with proxy-server=v1,proxy-client=v2] → [Backend]
```

#### Docker Compose Examples

**With nginx upstream (server-side)**:
```yml
services:
  nginx:
    image: nginx
    command: >
      sh -c "echo 'stream { server { listen 8080; proxy_protocol on; proxy_pass lb:8081; } }' > /etc/nginx/nginx.conf && nginx -g 'daemon off;'"
    ports:
      - "8080:8080"

  lb:
    image: davinci1976/docker-lb:latest
    command: ["/bin/lb", "8081:backend:9000,proxy-server=v1"]

  backend:
    scale: 3
    # Backend application
```

**Sending PROXY headers to backends (client-side)**:
```yml
services:
  lb:
    image: davinci1976/docker-lb:latest
    ports:
      - "8080:8080"
    command: ["/bin/lb", "8080:backend:9000,proxy-client=v2"]

  backend:
    scale: 3
    # Backend that supports PROXY protocol (e.g., nginx with proxy_protocol directive)
```

**Mixed versions**:
```yml
services:
  lb:
    image: davinci1976/docker-lb:latest
    ports:
      - "8080-8082:8080-8082"
    command: [
      "/bin/lb",
      "8080:service1:9000,proxy-server=v1",           # Expects v1 from upstream
      "8081:service2:9000,proxy-client=v2",           # Sends v2 to backend
      "8082:service3:9000,proxy-server=v2,proxy-client=v1"  # Both
    ]
```

#### Backend Support

Common software supporting PROXY protocol:
- **nginx**: `proxy_protocol` directive
- **HAProxy**: `accept-proxy` / `send-proxy` / `send-proxy-v2`
- **Apache**: `mod_remoteip` with `RemoteIPProxyProtocol`
- **Traefik**: Native support
- Most modern HTTP servers and proxies

#### Version Selection

- **Use v1** when: Interoperating with older systems, need human-readable debugging
- **Use v2** when: Performance matters, need advanced features, modern infrastructure

### Docker Compose Examples

#### Basic TCP Load Balancing
```yml
version: "3"
services:
  lb:
    image: davinci1976/docker-lb:latest
    ports:
      - "8080:8080"
    command: ["/bin/lb", "--verbose", "8080:backend:8081"]

  backend:
    image: alpine
    scale: 4
    command: ["/bin/sh", "-c", "while true; do echo 'HTTP/1.1 200 OK\n\nHello from '$$HOSTNAME'!' | nc -l -p 8081; done"]
```

#### HTTP with Session Affinity
```yml
version: "3"
services:
  lb:
    image: davinci1976/docker-lb:latest
    ports:
      - "8080:8080"
    command: ["/bin/lb", "8080:backend:8081,http"]

  backend:
    scale: 3
    image: nginx
```

#### Multiple Services (TCP + HTTP + HTTPS)
```yml
version: "3"
services:
  lb:
    image: davinci1976/docker-lb:latest
    ports:
      - "8080:8080"  # TCP
      - "8090:8090"  # HTTP
      - "8443:8443"  # HTTPS
    command: [
      "/bin/lb",
      "8080:service1:8081",        # TCP load balancing
      "8090:service2:8082,http",   # HTTP with cookies
      "8443:service3:8083,https"   # HTTPS with auto-cert
    ]

  service1:
    scale: 2
    # TCP service

  service2:
    scale: 3
    # HTTP service

  service3:
    scale: 2
    # HTTPS service
```

#### With Custom TLS Certificate
```yml
version: "3"
services:
  lb:
    image: davinci1976/docker-lb:latest
    ports:
      - "443:443"
    volumes:
      - ./certs:/certs
    command: [
      "/bin/lb",
      "--cert", "/certs/cert.pem",
      "--key", "/certs/key.pem",
      "443:backend:8080,https"
    ]
```

#### Port Range Mapping
```yml
version: "3"
services:
  lb:
    image: davinci1976/docker-lb:latest
    ports:
      - "8080-8090:8080-8090"  # Expose port range
    command: [
      "/bin/lb",
      "8080-8090:backend:9000-9010,http"  # Map 11 ports
    ]

  backend:
    scale: 3
    # Services listening on ports 9000-9010
```

#### Algorithm Comparison with Affinity
```yml
version: "3"
services:
  lb:
    image: davinci1976/docker-lb:latest
    ports:
      - "8080-8083:8080-8083"
    command: [
      "/bin/lb",
      "--verbose",
      "--affinity-ttl", "60s",
      "8080:backend:9000,lb=random,affinity",
      "8081:backend:9000,lb=round-robin,affinity",
      "8082:backend:9000,http,lb=least-connection,affinity",
      "8083:backend:9000,http,lb=weighted-random,affinity"
    ]

  backend:
    scale: 5
    # Your service
```

### Scaling Services
```bash
# Scale the backend service
docker-compose up -d --scale backend=5

# Test load distribution
for i in {1..10}; do
  curl http://localhost:8080
done
```

## Load Balancing Behavior

### Algorithm Selection Priority

For each connection/request:
1. **IP Affinity** (if enabled): Check if source IP has existing binding
2. **Cookie Affinity** (HTTP/HTTPS only): Check for `proxy-affinity` cookie
3. **Load Balancing Algorithm**: Use configured algorithm

### Algorithm Comparison

| Algorithm | Selection Logic | Connection Tracking | Best For |
|-----------|----------------|---------------------|----------|
| **random** | Pure random | ❌ No | Simple setups, stateless services |
| **round-robin** | Sequential rotation | ❌ No | Fair distribution, stateless services |
| **least-connection** | Min connections (deterministic) | ✅ Yes | Long-lived connections, WebSockets |
| **weighted-random** | Probabilistic by connections | ✅ Yes | HTTP services, gradual balancing |

### TCP Mode
- Supports all load balancing algorithms
- IP affinity available via `,affinity` option
- Per-backend connection tracking

### HTTP/HTTPS Mode
- Supports all load balancing algorithms
- Cookie-based session affinity using `proxy-affinity` cookie
- IP affinity takes precedence over cookie affinity
- Automatic failover if backend becomes unavailable

## Development

### Building
```bash
# Local build
make build

# Docker build
make docker

# Run unit tests
make test

# Run integration tests
make docker-test
```

### Testing
```bash
# Run integration tests
docker compose -f docker-compose.test.yml run --rm sut

# Manual testing with curl
curl -c cookies.txt http://localhost:8080  # Save cookie
curl -b cookies.txt http://localhost:8080  # Use cookie for affinity
```

## Monitoring

When running with `--verbose`, the load balancer provides:
- Connection open/close events with source/destination
- Data transfer statistics per connection
- DNS resolution updates
- Memory usage statistics
- Cumulative metrics (total connections, data transferred)

## Limitations

- Load balancing algorithm is currently random only
- HTTP/HTTPS mode requires cookie support from clients
- No health checks (relies on DNS and connection failures)
- No connection pooling or keep-alive optimization

## Possible Future Extensions
- Additional load balancing algorithms (round-robin, least-connection, weighted)
- Active health checks
- Multiple service targets (blue/green deployments)
- Connection pooling
- Rate limiting and bandwidth controls
- Circuit breaker patterns
- WebSocket support



