# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.0.5]: https://github.com/davinci1976/docker-lb/compare/v0.0.4...v0.0.5
[0.0.4]: https://github.com/davinci1976/docker-lb/compare/v0.0.3...v0.0.4
[0.0.3]: https://github.com/davinci1976/docker-lb/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/davinci1976/docker-lb/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/davinci1976/docker-lb/releases/tag/v0.0.1
