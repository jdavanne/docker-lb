package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
)

type StatsServer struct {
	backendPools map[string]*BackendPool // key: "host:port"
	affinityMaps map[string]*AffinityMap // key: "host"
	selectors    map[string]BackendSelector // key: "port"
	mu           sync.RWMutex
}

type BackendStats struct {
	IP          string `json:"ip"`
	Port        string `json:"port"`
	Weight      int    `json:"weight"`
	ActiveConns int64  `json:"active_conns"`
	TotalConns  uint64 `json:"total_conns"`
	TotalBytes  uint64 `json:"total_bytes"`
}

type PoolStats struct {
	Host     string         `json:"host"`
	Port     string         `json:"port"`
	Backends []BackendStats `json:"backends"`
	Count    int            `json:"count"`
}

type AffinityStats struct {
	Host    string            `json:"host"`
	TTL     string            `json:"ttl"`
	Entries map[string]string `json:"entries"` // sourceIP -> backendIP
	Count   int               `json:"count"`
}

type PortStats struct {
	Port      string    `json:"port"`
	Host      string    `json:"host"`
	Algorithm string    `json:"algorithm"`
	Pool      PoolStats `json:"pool"`
}

func NewStatsServer() *StatsServer {
	return &StatsServer{
		backendPools: make(map[string]*BackendPool),
		affinityMaps: make(map[string]*AffinityMap),
		selectors:    make(map[string]BackendSelector),
	}
}

func (s *StatsServer) RegisterBackendPool(key string, pool *BackendPool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backendPools[key] = pool
}

func (s *StatsServer) RegisterAffinityMap(key string, affinity *AffinityMap) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.affinityMaps[key] = affinity
}

func (s *StatsServer) RegisterSelector(port string, selector BackendSelector) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selectors[port] = selector
}

func (s *StatsServer) Start(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/backends", s.handleBackends)
	mux.HandleFunc("/affinity", s.handleAffinity)
	mux.HandleFunc("/ports", s.handlePorts)

	slog.Info("Starting stats server", "addr", addr)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			slog.Error("Stats server failed", "err", err)
		}
	}()
}

func (s *StatsServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *StatsServer) handleBackends(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pools := make([]PoolStats, 0, len(s.backendPools))
	for key, pool := range s.backendPools {
		backends := pool.GetBackends()
		backendStats := make([]BackendStats, 0, len(backends))

		for _, b := range backends {
			backendStats = append(backendStats, BackendStats{
				IP:          b.IP,
				Port:        b.Port,
				Weight:      b.Weight,
				ActiveConns: b.ActiveConns.Load(),
				TotalConns:  b.TotalConns.Load(),
				TotalBytes:  b.TotalBytes.Load(),
			})
		}

		pools = append(pools, PoolStats{
			Host:     pool.host,
			Port:     pool.port,
			Backends: backendStats,
			Count:    len(backends),
		})
		_ = key
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pools)
}

func (s *StatsServer) handleAffinity(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	affinities := make([]AffinityStats, 0, len(s.affinityMaps))
	for _, affinity := range s.affinityMaps {
		if affinity == nil {
			continue
		}

		affinity.mu.RLock()
		entries := make(map[string]string, len(affinity.entries))
		for sourceIP, entry := range affinity.entries {
			entries[sourceIP] = entry.backendIP
		}
		affinity.mu.RUnlock()

		affinities = append(affinities, AffinityStats{
			Host:    affinity.host,
			TTL:     affinity.ttl.String(),
			Entries: entries,
			Count:   len(entries),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(affinities)
}

func (s *StatsServer) handlePorts(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ports := make([]PortStats, 0, len(s.selectors))
	for port, selector := range s.selectors {
		// Find the corresponding backend pool
		var poolStats PoolStats
		for key, pool := range s.backendPools {
			backends := pool.GetBackends()
			backendStats := make([]BackendStats, 0, len(backends))

			for _, b := range backends {
				backendStats = append(backendStats, BackendStats{
					IP:          b.IP,
					Port:        b.Port,
					Weight:      b.Weight,
					ActiveConns: b.ActiveConns.Load(),
					TotalConns:  b.TotalConns.Load(),
					TotalBytes:  b.TotalBytes.Load(),
				})
			}

			poolStats = PoolStats{
				Host:     pool.host,
				Port:     pool.port,
				Backends: backendStats,
				Count:    len(backends),
			}
			_ = key
			break
		}

		ports = append(ports, PortStats{
			Port:      port,
			Algorithm: selector.Name(),
			Pool:      poolStats,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ports)
}
