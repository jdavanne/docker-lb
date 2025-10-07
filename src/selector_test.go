package main

import (
	"testing"
)

// Helper function to create a test backend pool
func createTestPool() *BackendPool {
	pool := &BackendPool{
		host:        "testhost",
		port:        "9000",
		backends:    make(map[string]*Backend),
		backendList: make([]*Backend, 0),
	}

	// Add test backends with varying connection counts
	backends := []*Backend{
		{IP: "10.0.0.1", Port: "9000", Weight: 1},
		{IP: "10.0.0.2", Port: "9000", Weight: 1},
		{IP: "10.0.0.3", Port: "9000", Weight: 1},
	}

	// Set different connection counts
	backends[0].ActiveConns.Store(5)
	backends[1].ActiveConns.Store(2)
	backends[2].ActiveConns.Store(10)

	for _, b := range backends {
		pool.backends[b.IP] = b
		pool.backendList = append(pool.backendList, b)
	}

	return pool
}

func TestRandomSelector(t *testing.T) {
	pool := createTestPool()
	selector := &RandomSelector{}

	// Test multiple selections
	results := make(map[string]int)
	for i := 0; i < 100; i++ {
		backend, err := selector.Select(pool, "", nil)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		if backend == nil {
			t.Fatal("backend is nil")
		}
		results[backend.IP]++
	}

	// All backends should have been selected at least once (with high probability)
	if len(results) < 3 {
		t.Errorf("Expected all 3 backends to be selected, got %d", len(results))
	}
}

func TestRoundRobinSelector(t *testing.T) {
	pool := createTestPool()
	selector := &RoundRobinSelector{}

	// Test sequential selection - should cycle through all backends
	seenBackends := make(map[string]bool)
	previousIP := ""

	for i := 0; i < 6; i++ {
		backend, err := selector.Select(pool, "", nil)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		seenBackends[backend.IP] = true

		// Should not select the same backend twice in a row (for 3+ backends)
		if i > 0 && backend.IP == previousIP && len(pool.backendList) > 1 {
			t.Errorf("Round-robin selected same backend twice in a row: %s", backend.IP)
		}
		previousIP = backend.IP
	}

	// Should have seen all backends
	if len(seenBackends) != 3 {
		t.Errorf("Expected to see all 3 backends, saw %d", len(seenBackends))
	}
}

func TestLeastConnectionSelector(t *testing.T) {
	pool := createTestPool()
	selector := &LeastConnectionSelector{}

	// Backend 10.0.0.2 has 2 connections (minimum)
	// When there are no ties, it should always select 10.0.0.2
	for i := 0; i < 10; i++ {
		backend, err := selector.Select(pool, "", nil)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		if backend.IP != "10.0.0.2" {
			t.Errorf("Expected backend with least connections (10.0.0.2), got %s with %d conns",
				backend.IP, backend.ActiveConns.Load())
		}
	}

	// Test with equal connection counts - should randomly distribute
	// Reset all backends to 0 connections
	for _, b := range pool.GetBackends() {
		b.ActiveConns.Store(0)
	}

	seenBackends := make(map[string]bool)
	for i := 0; i < 50; i++ {
		backend, err := selector.Select(pool, "", nil)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		seenBackends[backend.IP] = true
	}

	// With equal connections, should see multiple backends (randomization)
	if len(seenBackends) < 2 {
		t.Errorf("Expected to see at least 2 backends with equal connections, saw %d", len(seenBackends))
	}
}

func TestWeightedRandomSelector_ImplicitWeights(t *testing.T) {
	pool := createTestPool()
	selector := NewWeightedRandomSelector(false) // Use implicit weights

	// Backend 10.0.0.2 (2 conns) should have highest probability
	// Backend 10.0.0.3 (10 conns) should have lowest probability
	results := make(map[string]int)
	iterations := 10000

	for i := 0; i < iterations; i++ {
		backend, err := selector.Select(pool, "", nil)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		results[backend.IP]++
	}

	// 10.0.0.2 should be selected most often
	if results["10.0.0.2"] <= results["10.0.0.3"] {
		t.Errorf("Backend with fewer connections should have higher selection rate: "+
			"10.0.0.2=%d, 10.0.0.3=%d", results["10.0.0.2"], results["10.0.0.3"])
	}

	// All backends should be selected at least once
	if len(results) != 3 {
		t.Errorf("Expected all 3 backends to be selected, got %d", len(results))
	}
}

func TestWeightedRandomSelector_ExplicitWeights(t *testing.T) {
	pool := createTestPool()

	// Set explicit weights: 10.0.0.1=100, 10.0.0.2=50, 10.0.0.3=10
	pool.backends["10.0.0.1"].Weight = 100
	pool.backends["10.0.0.2"].Weight = 50
	pool.backends["10.0.0.3"].Weight = 10

	selector := NewWeightedRandomSelector(true) // Use explicit weights

	results := make(map[string]int)
	iterations := 10000

	for i := 0; i < iterations; i++ {
		backend, err := selector.Select(pool, "", nil)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		results[backend.IP]++
	}

	// 10.0.0.1 (weight 100) should be selected most often
	// Regardless of connection counts
	if results["10.0.0.1"] <= results["10.0.0.2"] || results["10.0.0.1"] <= results["10.0.0.3"] {
		t.Errorf("Backend with highest weight should be selected most: "+
			"10.0.0.1=%d, 10.0.0.2=%d, 10.0.0.3=%d",
			results["10.0.0.1"], results["10.0.0.2"], results["10.0.0.3"])
	}
}

func TestSelector_WithAffinity(t *testing.T) {
	pool := createTestPool()
	selector := &RandomSelector{}
	affinity := NewAffinityMap("testhost", 30000000000) // 30s

	sourceIP := "192.168.1.1"

	// First selection should create affinity
	backend1, err := selector.Select(pool, sourceIP, affinity)
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}

	// Second selection should return same backend
	backend2, err := selector.Select(pool, sourceIP, affinity)
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}

	if backend1.IP != backend2.IP {
		t.Errorf("Affinity not working: first=%s, second=%s", backend1.IP, backend2.IP)
	}

	// Different source IP should get different backend (with high probability)
	backend3, err := selector.Select(pool, "192.168.1.2", affinity)
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}

	// Note: This might occasionally fail due to randomness, but probability is low
	if backend1.IP == backend3.IP {
		t.Logf("Warning: Different source IPs got same backend (could be random chance)")
	}
}

func TestSelector_NoBackends(t *testing.T) {
	pool := &BackendPool{
		host:        "testhost",
		port:        "9000",
		backends:    make(map[string]*Backend),
		backendList: make([]*Backend, 0),
	}

	selectors := []BackendSelector{
		&RandomSelector{},
		&RoundRobinSelector{},
		&LeastConnectionSelector{},
		NewWeightedRandomSelector(false),
	}

	for _, selector := range selectors {
		_, err := selector.Select(pool, "", nil)
		if err != ErrNoBackends {
			t.Errorf("%s: expected ErrNoBackends, got %v", selector.Name(), err)
		}
	}
}

func TestNewSelector(t *testing.T) {
	tests := []struct {
		algorithm string
		valid     bool
	}{
		{"random", true},
		{"round-robin", true},
		{"least-connection", true},
		{"weighted-random", true},
		{"invalid-algo", false},
	}

	for _, tt := range tests {
		selector, err := NewSelector(tt.algorithm, false)
		if tt.valid {
			if err != nil {
				t.Errorf("Expected %s to be valid, got error: %v", tt.algorithm, err)
			}
			if selector == nil {
				t.Errorf("Expected selector for %s, got nil", tt.algorithm)
			}
		} else {
			if err == nil {
				t.Errorf("Expected error for %s, got nil", tt.algorithm)
			}
		}
	}
}
