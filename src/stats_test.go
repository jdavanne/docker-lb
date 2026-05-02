package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatsServer_Health(t *testing.T) {
	s := NewStatsServer()
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	s.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "OK" {
		t.Errorf("expected OK, got %s", w.Body.String())
	}
}

func TestStatsServer_Backends(t *testing.T) {
	s := NewStatsServer()
	pool := createTestPoolWithPort("myhost", "9000", []string{"10.0.0.1", "10.0.0.2"})
	pool.backends["10.0.0.1"].ActiveConns.Store(3)
	pool.backends["10.0.0.1"].TotalConns.Store(100)
	pool.backends["10.0.0.1"].TotalBytes.Store(5000)
	s.RegisterBackendPool("myhost:9000", pool)

	req := httptest.NewRequest("GET", "/backends", nil)
	w := httptest.NewRecorder()

	s.handleBackends(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var pools []PoolStats
	if err := json.Unmarshal(w.Body.Bytes(), &pools); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(pools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(pools))
	}
	if pools[0].Host != "myhost" {
		t.Errorf("expected host myhost, got %s", pools[0].Host)
	}
	if pools[0].Count != 2 {
		t.Errorf("expected 2 backends, got %d", pools[0].Count)
	}
}

func TestStatsServer_Affinity(t *testing.T) {
	s := NewStatsServer()
	am := NewAffinityMap("myhost", 30*time.Second)
	am.Set("192.168.1.1", "10.0.0.1:9000")
	am.Set("192.168.1.2", "10.0.0.2:9000")
	s.RegisterAffinityMap("myhost", am)

	req := httptest.NewRequest("GET", "/affinity", nil)
	w := httptest.NewRecorder()

	s.handleAffinity(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var affinities []AffinityStats
	if err := json.Unmarshal(w.Body.Bytes(), &affinities); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(affinities) != 1 {
		t.Fatalf("expected 1 affinity map, got %d", len(affinities))
	}
	if affinities[0].Count != 2 {
		t.Errorf("expected 2 entries, got %d", affinities[0].Count)
	}
}

func TestStatsServer_Ports(t *testing.T) {
	s := NewStatsServer()
	pool := createTestPoolWithPort("myhost", "9000", []string{"10.0.0.1"})
	s.RegisterBackendPool("myhost:9000", pool)
	selector := &RandomSelector{}
	s.RegisterSelector("8080", selector)

	req := httptest.NewRequest("GET", "/ports", nil)
	w := httptest.NewRecorder()

	s.handlePorts(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var ports []PortStats
	if err := json.Unmarshal(w.Body.Bytes(), &ports); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
	if ports[0].Port != "8080" {
		t.Errorf("expected port 8080, got %s", ports[0].Port)
	}
	if ports[0].Algorithm != "random" {
		t.Errorf("expected algorithm random, got %s", ports[0].Algorithm)
	}
}

func TestStatsServer_Metrics(t *testing.T) {
	s := NewStatsServer()
	pool := createTestPoolWithPort("myhost", "9000", []string{"10.0.0.1"})
	pool.backends["10.0.0.1"].ActiveConns.Store(5)
	pool.backends["10.0.0.1"].TotalConns.Store(42)
	pool.backends["10.0.0.1"].TotalBytes.Store(1024)
	s.RegisterBackendPool("myhost:9000", pool)

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	s.handleMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()

	expectedMetrics := []string{
		"dockerlb_operations_total",
		"dockerlb_connections_open",
		"dockerlb_bytes_sent_total",
		"dockerlb_bytes_received_total",
		"dockerlb_backend_active_connections",
		"dockerlb_backend_connections_total",
		"dockerlb_backend_bytes_total",
		"dockerlb_pool_backends",
		"go_goroutines",
		"go_info",
	}

	for _, metric := range expectedMetrics {
		if !strings.Contains(body, metric) {
			t.Errorf("expected metrics to contain %s", metric)
		}
	}

	// Verify content type
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("expected text/plain content-type, got %s", ct)
	}
}

func TestStatsServer_MetricsWithTransportStats(t *testing.T) {
	s := NewStatsServer()
	mgr := newConnTransportManager(90 * time.Second)
	mgr.RecordRequest()
	mgr.RecordRequest()
	mgr.Record503()
	s.RegisterTransportManager("8080", mgr)

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	s.handleMetrics(w, req)

	body := w.Body.String()

	expectedMetrics := []string{
		"dockerlb_http_requests_total",
		"dockerlb_http_requests_503_total",
	}

	for _, metric := range expectedMetrics {
		if !strings.Contains(body, metric) {
			t.Errorf("expected metrics to contain %s", metric)
		}
	}
}
