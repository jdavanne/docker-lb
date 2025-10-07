# docker-lb

A lightweight TCP/HTTP/HTTPS load balancer with dynamic DNS resolution, designed for Docker Compose scaled services.

## Features

- **TCP Load Balancing**: Random selection across available backends
- **HTTP/HTTPS Load Balancing**: Cookie-based session affinity for sticky sessions
- **Port Range Mapping**: Map multiple ports in a single command using ranges (e.g., `8080-8090`)
- **Dynamic DNS Resolution**: Automatically discovers and updates backend IPs
- **Proxy Protocol Support**: Preserves original client IPs
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
- `--probe-period <duration>`: DNS probe interval (default: 2s)
- `--server-proxy-protocol`: Enable proxy protocol on server side
- `--client-proxy-protocol`: Enable proxy protocol on client side  
- `--cert <file>`: TLS certificate file for HTTPS
- `--key <file>`: TLS key file for HTTPS

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

### TCP Mode
- Uses random selection for each new connection
- No session persistence
- Best for stateless TCP services

### HTTP/HTTPS Mode  
- Cookie-based session affinity using `proxy-affinity` cookie
- New clients randomly assigned to available backends
- Sessions stick to the same backend while it's available
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



