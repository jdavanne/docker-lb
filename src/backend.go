package main

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Backend represents a single backend server
type Backend struct {
	IP          string
	Port        string
	Weight      int           // Explicit weight for weighted algorithms (default: 1)
	ActiveConns atomic.Int64  // Current active connections
	TotalConns  atomic.Uint64 // Total connections served
	TotalBytes  atomic.Uint64 // Total bytes transferred
	LastSeen    time.Time     // Last seen during DNS probe
}

// BackendPool manages a pool of backend servers for a host
type BackendPool struct {
	host            string
	port            string // backend port
	backends        map[string]*Backend // IP -> Backend
	backendList     []*Backend          // for iteration
	mu              sync.RWMutex
	roundRobinIndex atomic.Uint64
}

// NewBackendPool creates a new backend pool
func NewBackendPool(host, port string) *BackendPool {
	return &BackendPool{
		host:        host,
		port:        port,
		backends:    make(map[string]*Backend),
		backendList: make([]*Backend, 0),
	}
}

// GetBackend returns a backend by IP
func (p *BackendPool) GetBackend(ip string) *Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.backends[ip]
}

// GetBackends returns a snapshot of all backends
func (p *BackendPool) GetBackends() []*Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()
	// Return a copy to avoid race conditions
	result := make([]*Backend, len(p.backendList))
	copy(result, p.backendList)
	return result
}

// OnConnect increments the active connection count for a backend
func (p *BackendPool) OnConnect(backend *Backend) {
	backend.ActiveConns.Add(1)
	backend.TotalConns.Add(1)
}

// OnDisconnect decrements the active connection count for a backend
func (p *BackendPool) OnDisconnect(backend *Backend) {
	backend.ActiveConns.Add(-1)
}

// AddBytes adds transferred bytes to a backend's counter
func (p *BackendPool) AddBytes(backend *Backend, bytes int64) {
	backend.TotalBytes.Add(uint64(bytes))
}

// SetWeights sets explicit weights for backends
func (p *BackendPool) SetWeights(weights map[string]int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for ip, weight := range weights {
		if backend, ok := p.backends[ip]; ok {
			backend.Weight = weight
			slog.Info("Backend weight set", "host", p.host, "ip", ip, "weight", weight)
		}
	}
}

// GetRoundRobinIndex returns the next round-robin index
func (p *BackendPool) GetRoundRobinIndex() uint64 {
	return p.roundRobinIndex.Add(1)
}

// Resolve returns a random backend IP (for compatibility)
func (p *BackendPool) resolve() (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.backendList) == 0 {
		return "", fmt.Errorf("no backends available")
	}
	// Use random selector for backward compatibility
	selector := &RandomSelector{}
	backend, err := selector.Select(p, "", nil)
	if err != nil {
		return "", err
	}
	return backend.IP, nil
}

// CheckIP checks if an IP is in the backend pool
func (p *BackendPool) checkIp(ip string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.backends[ip]
	return ok
}

// OnDNSUpdate is called when DNS resolver updates the IP list
// Implements DNSSubscriber interface
func (p *BackendPool) OnDNSUpdate(ips []string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	changed := 0

	// Create map of new IPs for quick lookup
	newIPMap := make(map[string]bool)
	for _, ip := range ips {
		newIPMap[ip] = true
	}

	// Add or update backends from new IP list
	for _, ip := range ips {
		if p.backends[ip] == nil {
			// New backend discovered
			changed++
			slog.Info("New backend", "host", p.host, "port", p.port, "ip", ip)
			p.backends[ip] = &Backend{
				IP:       ip,
				Port:     p.port,
				Weight:   1, // default weight
				LastSeen: now,
			}
		} else {
			// Existing backend still present
			p.backends[ip].LastSeen = now
		}
	}

	// Remove backends that are no longer in DNS
	for ip := range p.backends {
		if !newIPMap[ip] {
			changed++
			slog.Info("Lost backend", "host", p.host, "port", p.port, "ip", ip)
			delete(p.backends, ip)
		}
	}

	// Rebuild backend list if changed
	if changed != 0 {
		p.backendList = make([]*Backend, 0, len(p.backends))
		for _, backend := range p.backends {
			p.backendList = append(p.backendList, backend)
		}
		slog.Info("Backend list updated", "host", p.host, "port", p.port, "count", len(p.backendList))
	}
}

// GetHost returns the host name (implements DNSSubscriber interface)
func (p *BackendPool) GetHost() string {
	return p.host
}

// GetPort returns the backend port (implements DNSSubscriber interface)
func (p *BackendPool) GetPort() string {
	return p.port
}
