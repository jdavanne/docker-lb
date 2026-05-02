package main

import (
	"testing"
)

func createTestPoolWithPort(host, port string, ips []string) *BackendPool {
	pool := &BackendPool{
		host:        host,
		port:        port,
		backends:    make(map[string]*Backend),
		backendList: make([]*Backend, 0),
	}
	for _, ip := range ips {
		b := &Backend{IP: ip, Port: port, Weight: 1}
		pool.backends[ip] = b
		pool.backendList = append(pool.backendList, b)
	}
	return pool
}

func TestMultiPool_GetBackends(t *testing.T) {
	pool1 := createTestPoolWithPort("svc1", "9000", []string{"10.0.0.1", "10.0.0.2"})
	pool2 := createTestPoolWithPort("svc2", "8080", []string{"10.0.1.1"})
	mp := NewMultiPool([]*BackendPool{pool1, pool2})

	backends := mp.GetBackends()
	if len(backends) != 3 {
		t.Fatalf("expected 3 backends, got %d", len(backends))
	}
}

func TestMultiPool_GetBackendByIP(t *testing.T) {
	pool1 := createTestPoolWithPort("svc1", "9000", []string{"10.0.0.1"})
	pool2 := createTestPoolWithPort("svc2", "8080", []string{"10.0.1.1"})
	mp := NewMultiPool([]*BackendPool{pool1, pool2})

	b := mp.GetBackend("10.0.0.1")
	if b == nil {
		t.Fatal("expected to find backend by IP")
	}
	if b.Port != "9000" {
		t.Errorf("expected port 9000, got %s", b.Port)
	}
}

func TestMultiPool_GetBackendByIPPort(t *testing.T) {
	pool1 := createTestPoolWithPort("svc1", "9000", []string{"10.0.0.1"})
	pool2 := createTestPoolWithPort("svc2", "8080", []string{"10.0.0.1"})
	mp := NewMultiPool([]*BackendPool{pool1, pool2})

	b := mp.GetBackend("10.0.0.1:8080")
	if b == nil {
		t.Fatal("expected to find backend by IP:port")
	}
	if b.Port != "8080" {
		t.Errorf("expected port 8080, got %s", b.Port)
	}
}

func TestMultiPool_GetBackendNotFound(t *testing.T) {
	pool1 := createTestPoolWithPort("svc1", "9000", []string{"10.0.0.1"})
	mp := NewMultiPool([]*BackendPool{pool1})

	b := mp.GetBackend("10.0.0.99")
	if b != nil {
		t.Fatal("expected nil for non-existent backend")
	}
}

func TestMultiPool_CheckIP(t *testing.T) {
	pool1 := createTestPoolWithPort("svc1", "9000", []string{"10.0.0.1"})
	pool2 := createTestPoolWithPort("svc2", "8080", []string{"10.0.1.1"})
	mp := NewMultiPool([]*BackendPool{pool1, pool2})

	if !mp.CheckIP("10.0.0.1") {
		t.Error("expected CheckIP to find 10.0.0.1")
	}
	if !mp.CheckIP("10.0.1.1:8080") {
		t.Error("expected CheckIP to find 10.0.1.1:8080")
	}
	if mp.CheckIP("10.0.0.99") {
		t.Error("expected CheckIP to not find 10.0.0.99")
	}
}

func TestMultiPool_OnConnectDisconnect(t *testing.T) {
	pool1 := createTestPoolWithPort("svc1", "9000", []string{"10.0.0.1"})
	mp := NewMultiPool([]*BackendPool{pool1})

	b := mp.GetBackend("10.0.0.1")
	mp.OnConnect(b)
	if b.ActiveConns.Load() != 1 {
		t.Errorf("expected 1 active conn, got %d", b.ActiveConns.Load())
	}
	if b.TotalConns.Load() != 1 {
		t.Errorf("expected 1 total conn, got %d", b.TotalConns.Load())
	}

	mp.OnDisconnect(b)
	if b.ActiveConns.Load() != 0 {
		t.Errorf("expected 0 active conns, got %d", b.ActiveConns.Load())
	}
}

func TestMultiPool_AddBytes(t *testing.T) {
	pool1 := createTestPoolWithPort("svc1", "9000", []string{"10.0.0.1"})
	mp := NewMultiPool([]*BackendPool{pool1})

	b := mp.GetBackend("10.0.0.1")
	mp.AddBytes(b, 1024)
	if b.TotalBytes.Load() != 1024 {
		t.Errorf("expected 1024 bytes, got %d", b.TotalBytes.Load())
	}
}

func TestMultiPool_GetRoundRobinIndex(t *testing.T) {
	pool1 := createTestPoolWithPort("svc1", "9000", []string{"10.0.0.1"})
	mp := NewMultiPool([]*BackendPool{pool1})

	idx1 := mp.GetRoundRobinIndex()
	idx2 := mp.GetRoundRobinIndex()
	if idx2 != idx1+1 {
		t.Errorf("expected sequential indices, got %d and %d", idx1, idx2)
	}
}

func TestMultiPool_SelectorIntegration(t *testing.T) {
	pool1 := createTestPoolWithPort("svc1", "9000", []string{"10.0.0.1", "10.0.0.2"})
	pool2 := createTestPoolWithPort("svc2", "8080", []string{"10.0.1.1"})
	mp := NewMultiPool([]*BackendPool{pool1, pool2})

	selector := &RandomSelector{}
	selected, err := selector.Select(mp, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected == nil {
		t.Fatal("expected a backend to be selected")
	}

	// Verify all 3 backends can be selected
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		b, _ := selector.Select(mp, "", nil)
		seen[b.IP+":"+b.Port] = true
	}
	if len(seen) != 3 {
		t.Errorf("expected 3 distinct backends, got %d: %v", len(seen), seen)
	}
}

func TestMultiPool_LeastConnectionAcrossPools(t *testing.T) {
	pool1 := createTestPoolWithPort("svc1", "9000", []string{"10.0.0.1"})
	pool2 := createTestPoolWithPort("svc2", "8080", []string{"10.0.1.1"})
	mp := NewMultiPool([]*BackendPool{pool1, pool2})

	// Set different connection counts
	pool1.backends["10.0.0.1"].ActiveConns.Store(10)
	pool2.backends["10.0.1.1"].ActiveConns.Store(1)

	selector := &LeastConnectionSelector{}
	selected, err := selector.Select(mp, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected.IP != "10.0.1.1" {
		t.Errorf("expected least-connection to pick 10.0.1.1, got %s", selected.IP)
	}
}

func TestBackendPool_ImplementsPool(t *testing.T) {
	var _ Pool = (*BackendPool)(nil)
	var _ Pool = (*MultiPool)(nil)
}

func TestBackendPool_CheckIPWithPort(t *testing.T) {
	pool := createTestPoolWithPort("svc1", "9000", []string{"10.0.0.1", "10.0.0.2"})

	if !pool.CheckIP("10.0.0.1") {
		t.Error("expected to find by IP")
	}
	if !pool.CheckIP("10.0.0.1:9000") {
		t.Error("expected to find by IP:port")
	}
	if pool.CheckIP("10.0.0.1:8080") {
		t.Error("should not find with wrong port")
	}
	if pool.CheckIP("10.0.0.99") {
		t.Error("should not find non-existent IP")
	}
}

func TestBackendPool_GetBackendWithPort(t *testing.T) {
	pool := createTestPoolWithPort("svc1", "9000", []string{"10.0.0.1"})

	b := pool.GetBackend("10.0.0.1:9000")
	if b == nil {
		t.Fatal("expected to find backend by IP:port")
	}
	if b.IP != "10.0.0.1" {
		t.Errorf("expected IP 10.0.0.1, got %s", b.IP)
	}
}
