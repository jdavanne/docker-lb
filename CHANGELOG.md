# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Configurable Log Format**: `--log-format` selects `logfmt` (default), `json`, or `text` output
  - `logfmt` is now the default (`time=... level=INFO msg=... key=value`)
  - `json` emits one structured JSON object per line
  - `text` keeps the previous classic log line format
- **W3C Trace Context Propagation**: every forwarded connection/request carries a
  `trace_id`/`span_id`, logged on the `Forwarding start`/`close` (and backend error) lines
  - HTTP/HTTPS: adopts an inbound `traceparent` header when valid (recording `parent_span_id`),
    otherwise mints a fresh trace; propagates `traceparent` to the backend
  - TCP: mints a fresh trace per connection; adopts an upstream trace from a PROXY v2 trace TLV
    when server-side proxy protocol is enabled
  - Emits the trace context to the backend as a PROXY v2 TLV (`PP2_TYPE_TRACE_CONTEXT = 0xE1`,
    subtype `0x1`/`0x2`) when `proxy-client=v2` is configured; v1 has no TLV facility so the
    trace roots at the backend (per RFC-01 trace-context-propagation)
  - `--trace-encoding base62` renders trace/span ids as shorter base62 strings in logs
    (display-only; the `traceparent` and PROXY TLV always use standard W3C hex)

### Changed
- `--verbose` now also lowers the log level to DEBUG, surfacing per-connection transport lifecycle logs

## [0.0.10] - 2026-05-02

### Added
- **Multi-Target Support**: Load-balance across multiple host:port backends from a single listen port
  - Syntax: `8080:service1:9000+service2:9001` routes to backends on both services
  - Same host shorthand: `8080:backend:9000+9001+9002` targets multiple ports on one host
  - Each target gets its own backend pool with independent DNS resolution
  - Works with all modes (TCP, HTTP, HTTPS) and all options (affinity, algorithms, proxy protocol)
- **Cross-Platform Distribution**: CLI binaries for Linux, macOS, and Windows
  - Homebrew tap: `brew install jdavanne/docker-lb/docker-lb`
  - Scoop bucket: `scoop install docker-lb`
  - Pre-built binaries on GitHub Releases (amd64/arm64)
- **GitHub Actions CI/CD**: Automated testing and release pipeline
  - CI workflow: runs tests on push/PR to main
  - Release workflow: GoReleaser on version tags, publishes binaries, Docker images, Homebrew formula, and Scoop manifest
- **Comprehensive Unit Tests**: 80%+ code coverage without Docker
  - TCP forwarding tests with local listeners
  - HTTP handler tests with httptest
  - DNS resolver unit tests
  - Stats server endpoint tests
  - Pool interface and multi-target parsing tests
- **Improved CLI Help**: `--help` now shows `--` for long options, per-mapping options reference, and 10 usage examples
- **`--version` Flag**: Print version and exit

### Changed
- **Pool Interface**: `BackendPool` and new `MultiPool` implement a common `Pool` interface
  - Selectors, TCP forwarder, and HTTP handler work with either pool type
  - `MultiPool` aggregates multiple `BackendPool` instances for multi-target
- **Cookie/Affinity Keying**: Now stores `ip:port` instead of just `ip` for unambiguous backend identification across multi-target pools

## [0.0.8] - 2025-12-17

### Added
- **HTTP Per-Client-Connection Transport Management**: Backend connection reuse and proper cleanup
  - One `http.Transport` per client TCP connection (not per request)
  - Backend connections are reused across HTTP requests on the same client connection
  - Automatic cleanup when client disconnects via `ConnContext`/`ConnState` hooks
  - 90-second idle timeout for backend connections within a session
  - Prevents connection leaks that occurred with per-request transports
- **HTTP Request-Level Metrics**: New Prometheus metrics for HTTP/HTTPS mode
  - `dockerlb_http_requests_total{port}`: Total HTTP requests handled
  - `dockerlb_http_requests_503_total{port}`: Requests rejected with 503 (no backend available)
  - `dockerlb_http_requests_502_total{port}`: Requests rejected with 502 (backend connection error)
- **HTTP Connection-Level Metrics**: Track client TCP connections separately from requests
  - `dockerlb_http_client_connections_current{port}`: Current client TCP connections
  - `dockerlb_http_client_connections_total{port}`: Total client TCP connections accepted
  - `dockerlb_http_client_connections_rejected_total{port}`: Total client connections rejected
- **HTTP Transport Metrics**: Monitor backend connection pool lifecycle
  - `dockerlb_http_transports_current{port}`: Current backend transport pools
  - `dockerlb_http_transports_created_total{port}`: Total transports created
  - `dockerlb_http_transports_closed_total{port}`: Total transports closed
- **502 Error Handling**: Custom `ErrorHandler` on ReverseProxy to catch and track backend failures

### Changed
- **HTTP/HTTPS Mode Error Responses**: Now returns proper HTTP status codes
  - 503 Service Unavailable when no backend is available
  - 502 Bad Gateway when backend connection fails
- **Stats Server Interface**: New `TransportStatsProvider` interface for HTTP metrics

### Fixed
- **HTTP Transport Leak**: Fixed issue where a new `http.Transport` was created for every request
  - Previously, transports were never closed, causing potential connection/memory leaks
  - Now transports are bound to client connection lifecycle

## [0.0.7] - 2025-11-27

### Added
- **Port Range Fan-in**: Map multiple listen ports to a single backend port
  - Syntax: `30000-32000:backend:9090` maps all ports 30000-32000 to backend port 9090
  - Useful for scenarios where many external ports need to route to a single service
  - Works with all options (http, https, lb algorithms, affinity, proxy protocol)
- **New Unit Test**: `TestExpandBackendPorts` for port range expansion logic

### Fixed
- **TCP Error Logging**: Fixed incorrect error variables in `tcp.go` error logging
  - First goroutine now logs `err1` instead of `err`
  - Second goroutine now correctly assigns to and checks `err2`

## [0.0.6] - 2025-10-20

### Added
- **Prometheus Metrics Endpoint**: Native `/metrics` endpoint without any dependencies
  - Application metrics: `dockerlb_operations_total`, `dockerlb_connections_open`, `dockerlb_bytes_*`
  - Per-backend metrics with labels: `dockerlb_backend_active_connections`, `dockerlb_backend_connections_total`, `dockerlb_backend_bytes_total`, `dockerlb_backend_weight`
  - Pool metrics: `dockerlb_pool_backends`, `dockerlb_affinity_entries`
  - Go runtime metrics: `go_goroutines`, `go_threads`, `go_info`, `go_memstats_*`, `go_gc_duration_seconds`
  - Custom number formatting functions (formatUint64, formatInt64, formatFloat64) for zero-dependency implementation
  - Prometheus text exposition format (version 0.0.4)
- **DNS Subscriber Interface**: New interface for components that receive DNS updates
  - `BackendPool` now implements `DNSSubscriber` interface
  - `OnDNSUpdate(ips []string)` callback method for receiving IP list updates
  - `GetHost()` and `GetPort()` methods for subscriber identification
- **Memory Monitoring Goroutine**: Independent memory monitoring when `--verbose` is enabled
  - Runs in dedicated goroutine started from `main()`
  - Uses `--probe-period` for monitoring interval
  - No longer coupled to DNS resolver logic

### Changed
- **DNS Resolver Mutualization**: Optimized DNS resolution to be shared across backend pools with the same hostname
  - One DNS probe goroutine per unique hostname (instead of one per host:port combination)
  - Subscriber pattern allows multiple `BackendPool` instances to share DNS updates
  - Significantly reduced DNS query load when using port ranges or multiple ports for the same host
  - Example: `8080-8083:service:9000-9003` now creates 1 DNS resolver instead of 4
  - New `DNSResolver` component (`src/dns_resolver.go`) manages hostname resolution with multiple subscribers
  - Improved resource efficiency with fewer goroutines and network calls
- **Backend Pool Refactoring**: Removed DNS probing logic from `BackendPool`
  - No longer runs its own `dnsProbe()` goroutine
  - Receives DNS updates passively via `OnDNSUpdate()` callback
  - Cleaner separation of concerns between DNS resolution and backend management

## [0.0.5] - 2025-10-14

### Added
- **Proxy Protocol Version Selection**: Support for both v1 (text) and v2 (binary) of the PROXY protocol
  - Client-side and server-side version selection
  - Configurable per mapping or globally
- **Per-Mapping Proxy Protocol Configuration**: Fine-grained control over proxy protocol per port
  - `proxy-server=v1|v2`: Enable server-side proxy protocol (expects headers from upstream)
  - `proxy-client=v1|v2`: Enable client-side proxy protocol (sends headers to backends)
  - Mix versions across different services (e.g., `proxy-server=v1,proxy-client=v2`)
  - Works with TCP, HTTP, and HTTPS modes
- **Comprehensive Proxy Protocol Documentation**: New dedicated section in README with:
  - Explanation of PROXY protocol and when to use it
  - Configuration examples for all scenarios
  - Backend support reference (nginx, HAProxy, Apache, Traefik)
  - Docker Compose examples showing real-world usage
  - Version selection guidance (v1 vs v2)
- **22 New Unit Tests**: Full test coverage for proxy protocol configuration parsing
  - `TestParseProxyProtocolOption`: 9 test cases for version string parsing
  - `TestParseProxyProtocolConfig`: 13 test cases for configuration logic
  - Tests cover edge cases, backward compatibility, and error conditions
- **Structured Logging Throughout**: Complete migration to slog for consistent logging
  - All error messages include contextual key-value pairs
  - Better debugging with structured fields (port, arg, err, etc.)
  - Machine-parseable log output for log aggregation tools

### Changed
- **Deprecated Global Proxy Protocol Flags**: `--server-proxy-protocol` and `--client-proxy-protocol` still work but marked as deprecated
  - Default to v1 for backward compatibility
  - Recommend using per-mapping `proxy-server` and `proxy-client` options instead
- **Enhanced Error Logging**: All errors now use slog with contextual information
  - Port numbers, argument indices, and error details in structured format
  - Replaced `log.Fatal()` with `slog.Error()` + `os.Exit(1)`
- **Memory Usage Logging**: `PrintMemUsage()` now outputs structured logs instead of printf format
  - Uses key-value pairs: goroutines, alloc_kib, total_alloc_kib, sys_kib, num_gc
- **Improved Proxy Protocol Logging**: Added version info to proxy protocol enable/send messages

### Fixed
- **Hardcoded Proxy Protocol Version**: Client-side proxy protocol now uses configurable version
  - Previously always sent v1 headers regardless of configuration
  - Now correctly sends v1 or v2 based on `ClientVersion` setting
- **Removed Unused Imports**: Cleaned up standard `log` package imports after slog migration

## [0.0.4] - 2025-10-07

### Added
- **Multiple Load Balancing Algorithms**:
  - `random`: Pure random selection (default, backward compatible)
  - `round-robin`: Sequential distribution across backends
  - `least-connection`: Routes to backend with fewest active connections with random selection among equal backends
  - `weighted-random`: Intelligent probabilistic selection using connection counts
- **IP Affinity**: Source IP-based sticky sessions with configurable TTL (default: 30s)
  - Automatically tracks source IP to backend mappings
  - TTL resets on connection close
  - Works with all load balancing algorithms
  - Only enabled when explicitly requested via `affinity` option
- **Management/Stats API**: HTTP server (default port 8080) exposing internal state
  - `/health`: Health check endpoint
  - `/backends`: All backend pools with IPs, connection counts, and weights
  - `/affinity`: Affinity maps showing source IP to backend IP mappings
  - `/ports`: Per-port configuration with algorithm and backend stats
- **Per-Backend Connection Tracking**: Active connections, total requests, bytes transferred
- **Weighted-Random Implicit Weights**: Uses inverse connection counts as default weights
- **Per-Port Algorithm Configuration**: Set different algorithms for different ports via `,lb=algo` option
- **CLI Flags**:
  - `--lb-algorithm`: Global default load balancing algorithm
  - `--affinity-ttl`: IP affinity TTL configuration
  - `--backend-weights`: Explicit weights for weighted-random algorithm
  - `--stats-port`: Management API server port (default: 8080)
- **Comprehensive Unit Tests**: 32+ test cases for selectors, affinity, and backend pool
- **Enhanced Integration Tests**:
  - Go HTTP backend service returning JSON responses with service name, hostname, port, and request count
  - Tests for all algorithms with and without affinity
  - Stats API validation in test suite

### Changed
- TCP mode now uses active load balancing with backend selection (previously relied on OS DNS)
- Backend tracking migrated from `DnsProbe` to `BackendPool` with enhanced metrics
- HTTP/HTTPS mode now checks IP affinity before cookie affinity (priority: IP affinity > cookie affinity > algorithm)
- HTTPS mode now properly terminates TLS and connects to backends using HTTP
- Improved logging with algorithm names and backend selection details
- Integration tests now use JSON parsing with jq for validation

### Fixed
- **Critical**: IP affinity was incorrectly enabled by default for all ports due to `--affinity-ttl` default value
- Least-connection algorithm now randomly selects among backends with equal connection counts
- Better error messages for backend selection failures
- Proper connection tracking across all modes (TCP, HTTP, HTTPS)

## [0.0.3] - 2025-10-07

### Added
- Port range mapping support: map multiple ports with a single command
  - Syntax: `port1-port2:host:port1-port2` (e.g., `8080-8090:service:9000-9010`)
  - Works with TCP, HTTP, and HTTPS modes
  - Both listen and backend ranges must have matching lengths

### Fixed
- Validation ensures start port ≤ end port in range syntax
- Proper error messages for invalid port range formats

## [0.0.2] - 2025-08-29

### Added
- HTTP/HTTPS cookie-based session affinity using `proxy-affinity` cookie
- Self-signed certificate generation for HTTPS
- Proxy protocol support (client and server side)
- Dynamic DNS resolution with configurable probe period
- Real-time metrics tracking (connections, data transfer, memory usage)
- Structured logging with slog
- Integration tests for TCP, HTTP, and HTTPS load balancing

### Changed
- Multi-stage Docker build with Go Alpine
- Cross-platform support via BUILDPLATFORM/TARGETPLATFORM

## [0.0.1] - Initial Release

### Added
- Basic TCP load balancing with random backend selection
- Command-line argument parsing for port mappings
- Docker Compose integration
- Makefile for build automation
- Basic forwarding functionality

[0.0.10]: https://github.com/jdavanne/docker-lb/compare/v0.0.9...v0.0.10
[0.0.8]: https://github.com/jdavanne/docker-lb/compare/v0.0.7...v0.0.8
[0.0.7]: https://github.com/jdavanne/docker-lb/compare/v0.0.6...v0.0.7
[0.0.6]: https://github.com/jdavanne/docker-lb/compare/v0.0.5...v0.0.6
[0.0.5]: https://github.com/jdavanne/docker-lb/compare/v0.0.4...v0.0.5
[0.0.4]: https://github.com/jdavanne/docker-lb/compare/v0.0.3...v0.0.4
[0.0.3]: https://github.com/jdavanne/docker-lb/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/jdavanne/docker-lb/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/jdavanne/docker-lb/releases/tag/v0.0.1
