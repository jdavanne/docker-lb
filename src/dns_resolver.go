package main

import (
	"log/slog"
	"net"
	"sync"
	"time"
)

// DNSSubscriber is the interface for components that want DNS updates
type DNSSubscriber interface {
	OnDNSUpdate(ips []string)
	GetHost() string
	GetPort() string
}

// DNSResolver manages DNS resolution for a single hostname
// Multiple BackendPools can subscribe to receive IP updates
type DNSResolver struct {
	host        string
	ips         []string
	mu          sync.RWMutex
	subscribers []DNSSubscriber
	probePeriod time.Duration
}

// NewDNSResolver creates a new DNS resolver for a hostname
func NewDNSResolver(host string, probePeriod time.Duration) *DNSResolver {
	return &DNSResolver{
		host:        host,
		ips:         make([]string, 0),
		subscribers: make([]DNSSubscriber, 0),
		probePeriod: probePeriod,
	}
}

// Subscribe adds a subscriber to receive DNS updates
func (r *DNSResolver) Subscribe(sub DNSSubscriber) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subscribers = append(r.subscribers, sub)

	// Immediately notify with current IPs if we have any
	if len(r.ips) > 0 {
		sub.OnDNSUpdate(r.ips)
	}
}

// GetIPs returns a copy of the current IP list
func (r *DNSResolver) GetIPs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]string, len(r.ips))
	copy(result, r.ips)
	return result
}

// start begins the DNS probing loop
func (r *DNSResolver) start() {
	slog.Info("DNS resolver started", "host", r.host, "probePeriod", r.probePeriod)
	round := 0

	for {
		if round != 0 {
			time.Sleep(r.probePeriod)
		}
		round++

		if *verbose {
			slog.Info("DNS probing", "host", r.host, "round", round)
		}

		// Perform DNS lookup
		ips, err := net.LookupIP(r.host)
		if err != nil {
			slog.Error("DNS lookup failed", "host", r.host, "err", err)
			continue
		}

		// Convert IPs to strings
		newIPs := make([]string, 0, len(ips))
		for _, ip := range ips {
			newIPs = append(newIPs, ip.String())
		}

		// Check if IPs changed
		changed := r.updateIPs(newIPs)

		if changed {
			slog.Info("DNS resolved", "host", r.host, "ips", len(newIPs), "subscribers", len(r.subscribers))
			r.notifySubscribers(newIPs)
		}
	}
}

// updateIPs updates the internal IP list and returns true if changed
func (r *DNSResolver) updateIPs(newIPs []string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Compare with existing IPs
	if len(r.ips) != len(newIPs) {
		r.ips = newIPs
		return true
	}

	// Create a map for quick lookup
	oldMap := make(map[string]bool)
	for _, ip := range r.ips {
		oldMap[ip] = true
	}

	// Check if any new IP is different
	for _, ip := range newIPs {
		if !oldMap[ip] {
			r.ips = newIPs
			return true
		}
	}

	return false
}

// notifySubscribers sends DNS updates to all subscribers
func (r *DNSResolver) notifySubscribers(ips []string) {
	r.mu.RLock()
	subscribers := make([]DNSSubscriber, len(r.subscribers))
	copy(subscribers, r.subscribers)
	r.mu.RUnlock()

	for _, sub := range subscribers {
		sub.OnDNSUpdate(ips)
	}
}
