package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime"
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
	mux.HandleFunc("/metrics", s.handleMetrics)

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

func (s *StatsServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	// Go runtime metrics
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	w.Write([]byte("# HELP go_goroutines Number of goroutines that currently exist\n"))
	w.Write([]byte("# TYPE go_goroutines gauge\n"))
	w.Write([]byte("go_goroutines "))
	w.Write([]byte(formatInt(runtime.NumGoroutine())))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP go_threads Number of OS threads created\n"))
	w.Write([]byte("# TYPE go_threads gauge\n"))
	w.Write([]byte("go_threads "))
	w.Write([]byte(formatInt(runtime.NumCPU())))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP go_info Information about the Go environment\n"))
	w.Write([]byte("# TYPE go_info gauge\n"))
	w.Write([]byte("go_info{version=\""))
	w.Write([]byte(runtime.Version()))
	w.Write([]byte("\"} 1\n\n"))

	// Memory metrics
	w.Write([]byte("# HELP go_memstats_alloc_bytes Number of bytes allocated and still in use\n"))
	w.Write([]byte("# TYPE go_memstats_alloc_bytes gauge\n"))
	w.Write([]byte("go_memstats_alloc_bytes "))
	w.Write([]byte(formatUint64(m.Alloc)))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP go_memstats_alloc_bytes_total Total number of bytes allocated, even if freed\n"))
	w.Write([]byte("# TYPE go_memstats_alloc_bytes_total counter\n"))
	w.Write([]byte("go_memstats_alloc_bytes_total "))
	w.Write([]byte(formatUint64(m.TotalAlloc)))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP go_memstats_sys_bytes Number of bytes obtained from system\n"))
	w.Write([]byte("# TYPE go_memstats_sys_bytes gauge\n"))
	w.Write([]byte("go_memstats_sys_bytes "))
	w.Write([]byte(formatUint64(m.Sys)))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP go_memstats_heap_alloc_bytes Number of heap bytes allocated and still in use\n"))
	w.Write([]byte("# TYPE go_memstats_heap_alloc_bytes gauge\n"))
	w.Write([]byte("go_memstats_heap_alloc_bytes "))
	w.Write([]byte(formatUint64(m.HeapAlloc)))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP go_memstats_heap_sys_bytes Number of heap bytes obtained from system\n"))
	w.Write([]byte("# TYPE go_memstats_heap_sys_bytes gauge\n"))
	w.Write([]byte("go_memstats_heap_sys_bytes "))
	w.Write([]byte(formatUint64(m.HeapSys)))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP go_memstats_heap_idle_bytes Number of heap bytes waiting to be used\n"))
	w.Write([]byte("# TYPE go_memstats_heap_idle_bytes gauge\n"))
	w.Write([]byte("go_memstats_heap_idle_bytes "))
	w.Write([]byte(formatUint64(m.HeapIdle)))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP go_memstats_heap_inuse_bytes Number of heap bytes that are in use\n"))
	w.Write([]byte("# TYPE go_memstats_heap_inuse_bytes gauge\n"))
	w.Write([]byte("go_memstats_heap_inuse_bytes "))
	w.Write([]byte(formatUint64(m.HeapInuse)))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP go_memstats_heap_released_bytes Number of heap bytes released to OS\n"))
	w.Write([]byte("# TYPE go_memstats_heap_released_bytes gauge\n"))
	w.Write([]byte("go_memstats_heap_released_bytes "))
	w.Write([]byte(formatUint64(m.HeapReleased)))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP go_memstats_heap_objects Number of allocated objects\n"))
	w.Write([]byte("# TYPE go_memstats_heap_objects gauge\n"))
	w.Write([]byte("go_memstats_heap_objects "))
	w.Write([]byte(formatUint64(m.HeapObjects)))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP go_memstats_stack_inuse_bytes Number of bytes in use by the stack allocator\n"))
	w.Write([]byte("# TYPE go_memstats_stack_inuse_bytes gauge\n"))
	w.Write([]byte("go_memstats_stack_inuse_bytes "))
	w.Write([]byte(formatUint64(m.StackInuse)))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP go_memstats_stack_sys_bytes Number of bytes obtained from system for stack allocator\n"))
	w.Write([]byte("# TYPE go_memstats_stack_sys_bytes gauge\n"))
	w.Write([]byte("go_memstats_stack_sys_bytes "))
	w.Write([]byte(formatUint64(m.StackSys)))
	w.Write([]byte("\n\n"))

	// GC metrics
	w.Write([]byte("# HELP go_gc_duration_seconds A summary of the pause duration of garbage collection cycles\n"))
	w.Write([]byte("# TYPE go_gc_duration_seconds summary\n"))
	w.Write([]byte("go_gc_duration_seconds{quantile=\"0\"} "))
	w.Write([]byte(formatFloat64(float64(m.PauseNs[(m.NumGC+255)%256])/1e9)))
	w.Write([]byte("\n"))
	w.Write([]byte("go_gc_duration_seconds{quantile=\"1\"} "))
	w.Write([]byte(formatFloat64(float64(m.PauseNs[(m.NumGC+255)%256])/1e9)))
	w.Write([]byte("\n"))
	w.Write([]byte("go_gc_duration_seconds_sum "))
	w.Write([]byte(formatFloat64(float64(m.PauseTotalNs)/1e9)))
	w.Write([]byte("\n"))
	w.Write([]byte("go_gc_duration_seconds_count "))
	w.Write([]byte(formatUint64(uint64(m.NumGC))))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP go_memstats_gc_sys_bytes Number of bytes used for garbage collection system metadata\n"))
	w.Write([]byte("# TYPE go_memstats_gc_sys_bytes gauge\n"))
	w.Write([]byte("go_memstats_gc_sys_bytes "))
	w.Write([]byte(formatUint64(m.GCSys)))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP go_memstats_next_gc_bytes Number of heap bytes when next garbage collection will take place\n"))
	w.Write([]byte("# TYPE go_memstats_next_gc_bytes gauge\n"))
	w.Write([]byte("go_memstats_next_gc_bytes "))
	w.Write([]byte(formatUint64(m.NextGC)))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP go_memstats_last_gc_time_seconds Number of seconds since 1970 of last garbage collection\n"))
	w.Write([]byte("# TYPE go_memstats_last_gc_time_seconds gauge\n"))
	w.Write([]byte("go_memstats_last_gc_time_seconds "))
	w.Write([]byte(formatFloat64(float64(m.LastGC)/1e9)))
	w.Write([]byte("\n\n"))

	// Global metrics
	w.Write([]byte("# HELP dockerlb_operations_total Total number of operations\n"))
	w.Write([]byte("# TYPE dockerlb_operations_total counter\n"))
	w.Write([]byte("dockerlb_operations_total "))
	w.Write([]byte(formatUint64(ops.Load())))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP dockerlb_connections_open Currently open connections\n"))
	w.Write([]byte("# TYPE dockerlb_connections_open gauge\n"))
	w.Write([]byte("dockerlb_connections_open "))
	w.Write([]byte(formatInt64(opened.Load())))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP dockerlb_bytes_sent_total Total bytes sent\n"))
	w.Write([]byte("# TYPE dockerlb_bytes_sent_total counter\n"))
	w.Write([]byte("dockerlb_bytes_sent_total "))
	w.Write([]byte(formatInt64(cumSent.Load())))
	w.Write([]byte("\n\n"))

	w.Write([]byte("# HELP dockerlb_bytes_received_total Total bytes received\n"))
	w.Write([]byte("# TYPE dockerlb_bytes_received_total counter\n"))
	w.Write([]byte("dockerlb_bytes_received_total "))
	w.Write([]byte(formatInt64(cumReceived.Load())))
	w.Write([]byte("\n\n"))

	// Backend metrics
	w.Write([]byte("# HELP dockerlb_backend_active_connections Current active connections per backend\n"))
	w.Write([]byte("# TYPE dockerlb_backend_active_connections gauge\n"))
	for key, pool := range s.backendPools {
		backends := pool.GetBackends()
		for _, b := range backends {
			w.Write([]byte("dockerlb_backend_active_connections{host=\""))
			w.Write([]byte(pool.host))
			w.Write([]byte("\",port=\""))
			w.Write([]byte(pool.port))
			w.Write([]byte("\",backend_ip=\""))
			w.Write([]byte(b.IP))
			w.Write([]byte("\",backend_port=\""))
			w.Write([]byte(b.Port))
			w.Write([]byte("\"} "))
			w.Write([]byte(formatInt64(b.ActiveConns.Load())))
			w.Write([]byte("\n"))
		}
		_ = key
	}
	w.Write([]byte("\n"))

	w.Write([]byte("# HELP dockerlb_backend_connections_total Total connections per backend\n"))
	w.Write([]byte("# TYPE dockerlb_backend_connections_total counter\n"))
	for key, pool := range s.backendPools {
		backends := pool.GetBackends()
		for _, b := range backends {
			w.Write([]byte("dockerlb_backend_connections_total{host=\""))
			w.Write([]byte(pool.host))
			w.Write([]byte("\",port=\""))
			w.Write([]byte(pool.port))
			w.Write([]byte("\",backend_ip=\""))
			w.Write([]byte(b.IP))
			w.Write([]byte("\",backend_port=\""))
			w.Write([]byte(b.Port))
			w.Write([]byte("\"} "))
			w.Write([]byte(formatUint64(b.TotalConns.Load())))
			w.Write([]byte("\n"))
		}
		_ = key
	}
	w.Write([]byte("\n"))

	w.Write([]byte("# HELP dockerlb_backend_bytes_total Total bytes transferred per backend\n"))
	w.Write([]byte("# TYPE dockerlb_backend_bytes_total counter\n"))
	for key, pool := range s.backendPools {
		backends := pool.GetBackends()
		for _, b := range backends {
			w.Write([]byte("dockerlb_backend_bytes_total{host=\""))
			w.Write([]byte(pool.host))
			w.Write([]byte("\",port=\""))
			w.Write([]byte(pool.port))
			w.Write([]byte("\",backend_ip=\""))
			w.Write([]byte(b.IP))
			w.Write([]byte("\",backend_port=\""))
			w.Write([]byte(b.Port))
			w.Write([]byte("\"} "))
			w.Write([]byte(formatUint64(b.TotalBytes.Load())))
			w.Write([]byte("\n"))
		}
		_ = key
	}
	w.Write([]byte("\n"))

	w.Write([]byte("# HELP dockerlb_backend_weight Backend weight for load balancing\n"))
	w.Write([]byte("# TYPE dockerlb_backend_weight gauge\n"))
	for key, pool := range s.backendPools {
		backends := pool.GetBackends()
		for _, b := range backends {
			w.Write([]byte("dockerlb_backend_weight{host=\""))
			w.Write([]byte(pool.host))
			w.Write([]byte("\",port=\""))
			w.Write([]byte(pool.port))
			w.Write([]byte("\",backend_ip=\""))
			w.Write([]byte(b.IP))
			w.Write([]byte("\",backend_port=\""))
			w.Write([]byte(b.Port))
			w.Write([]byte("\"} "))
			w.Write([]byte(formatInt(b.Weight)))
			w.Write([]byte("\n"))
		}
		_ = key
	}
	w.Write([]byte("\n"))

	// Backend pool info
	w.Write([]byte("# HELP dockerlb_pool_backends Number of backends in pool\n"))
	w.Write([]byte("# TYPE dockerlb_pool_backends gauge\n"))
	for key, pool := range s.backendPools {
		backends := pool.GetBackends()
		w.Write([]byte("dockerlb_pool_backends{host=\""))
		w.Write([]byte(pool.host))
		w.Write([]byte("\",port=\""))
		w.Write([]byte(pool.port))
		w.Write([]byte("\"} "))
		w.Write([]byte(formatInt(len(backends))))
		w.Write([]byte("\n"))
		_ = key
	}
	w.Write([]byte("\n"))

	// Affinity metrics
	w.Write([]byte("# HELP dockerlb_affinity_entries Number of active affinity entries\n"))
	w.Write([]byte("# TYPE dockerlb_affinity_entries gauge\n"))
	for _, affinity := range s.affinityMaps {
		if affinity == nil {
			continue
		}
		affinity.mu.RLock()
		count := len(affinity.entries)
		affinity.mu.RUnlock()

		w.Write([]byte("dockerlb_affinity_entries{host=\""))
		w.Write([]byte(affinity.host))
		w.Write([]byte("\"} "))
		w.Write([]byte(formatInt(count)))
		w.Write([]byte("\n"))
	}
}

// Helper functions to format numbers without dependencies
func formatInt64(n int64) string {
	if n < 0 {
		return "-" + formatUint64(uint64(-n))
	}
	return formatUint64(uint64(n))
}

func formatUint64(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf) - 1
	for n > 0 {
		buf[i] = byte('0' + n%10)
		n /= 10
		i--
	}
	return string(buf[i+1:])
}

func formatInt(n int) string {
	return formatInt64(int64(n))
}

func formatFloat64(f float64) string {
	// Simple float formatting without dependencies
	// Handle special cases
	if f == 0 {
		return "0"
	}

	// Convert to integer part and fractional part
	negative := f < 0
	if negative {
		f = -f
	}

	intPart := uint64(f)
	fracPart := f - float64(intPart)

	// Format integer part
	result := formatUint64(intPart)

	// Add decimal part (6 digits precision)
	if fracPart > 0 {
		result += "."
		for i := 0; i < 6; i++ {
			fracPart *= 10
			digit := uint64(fracPart)
			result += string(byte('0' + digit))
			fracPart -= float64(digit)
		}
		// Trim trailing zeros
		for len(result) > 0 && result[len(result)-1] == '0' {
			result = result[:len(result)-1]
		}
		if result[len(result)-1] == '.' {
			result = result[:len(result)-1]
		}
	}

	if negative {
		result = "-" + result
	}

	return result
}
