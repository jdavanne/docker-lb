package main

import (
	"log/slog"
	"sync"
	"time"
)

// AffinityEntry represents an IP affinity binding
type AffinityEntry struct {
	backendIP string
	lastUsed  time.Time
}

// AffinityMap tracks source IP to backend IP mappings with TTL
type AffinityMap struct {
	entries map[string]*AffinityEntry // sourceIP -> entry
	ttl     time.Duration
	mu      sync.RWMutex
	host    string // for logging
}

// NewAffinityMap creates a new affinity map
func NewAffinityMap(host string, ttl time.Duration) *AffinityMap {
	am := &AffinityMap{
		entries: make(map[string]*AffinityEntry),
		ttl:     ttl,
		host:    host,
	}

	// Start cleanup goroutine
	go am.cleanup()

	return am
}

// Get retrieves the backend IP for a source IP
func (am *AffinityMap) Get(sourceIP string) (backendIP string, found bool) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	entry, ok := am.entries[sourceIP]
	if !ok {
		return "", false
	}

	// Check if entry has expired
	if time.Since(entry.lastUsed) > am.ttl {
		return "", false
	}

	return entry.backendIP, true
}

// Set creates or updates an affinity binding
func (am *AffinityMap) Set(sourceIP, backendIP string) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if entry, ok := am.entries[sourceIP]; ok {
		// Update existing entry
		entry.backendIP = backendIP
		entry.lastUsed = time.Now()
	} else {
		// Create new entry
		am.entries[sourceIP] = &AffinityEntry{
			backendIP: backendIP,
			lastUsed:  time.Now(),
		}
		if *verbose {
			slog.Info("Affinity created", "host", am.host, "sourceIP", sourceIP, "backendIP", backendIP)
		}
	}
}

// Touch updates the last-used timestamp for a source IP
// This is called when a connection closes to extend the TTL
func (am *AffinityMap) Touch(sourceIP string) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if entry, ok := am.entries[sourceIP]; ok {
		entry.lastUsed = time.Now()
		if *verbose {
			slog.Info("Affinity touched", "host", am.host, "sourceIP", sourceIP, "backendIP", entry.backendIP, "ttl", am.ttl)
		}
	}
}

// cleanup runs periodically to remove expired entries
func (am *AffinityMap) cleanup() {
	// Run cleanup every ttl/2
	ticker := time.NewTicker(am.ttl / 2)
	defer ticker.Stop()

	for range ticker.C {
		am.mu.Lock()
		now := time.Now()
		removed := 0

		for sourceIP, entry := range am.entries {
			if now.Sub(entry.lastUsed) > am.ttl {
				delete(am.entries, sourceIP)
				removed++
				if *verbose {
					slog.Info("Affinity expired", "host", am.host, "sourceIP", sourceIP, "backendIP", entry.backendIP, "age", now.Sub(entry.lastUsed))
				}
			}
		}

		if removed > 0 {
			slog.Info("Affinity cleanup", "host", am.host, "removed", removed, "remaining", len(am.entries))
		}

		am.mu.Unlock()
	}
}

// Size returns the current number of affinity entries
func (am *AffinityMap) Size() int {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return len(am.entries)
}
