package main

import (
	"testing"
	"time"
)

func TestAffinityMap_SetAndGet(t *testing.T) {
	am := NewAffinityMap("testhost", 30*time.Second)

	sourceIP := "192.168.1.1"
	backendIP := "10.0.0.1"

	// Set affinity
	am.Set(sourceIP, backendIP)

	// Get affinity
	got, found := am.Get(sourceIP)
	if !found {
		t.Error("Expected to find affinity entry")
	}
	if got != backendIP {
		t.Errorf("Expected %s, got %s", backendIP, got)
	}
}

func TestAffinityMap_NotFound(t *testing.T) {
	am := NewAffinityMap("testhost", 30*time.Second)

	_, found := am.Get("192.168.1.1")
	if found {
		t.Error("Expected not to find non-existent entry")
	}
}

func TestAffinityMap_Update(t *testing.T) {
	am := NewAffinityMap("testhost", 30*time.Second)

	sourceIP := "192.168.1.1"
	backend1 := "10.0.0.1"
	backend2 := "10.0.0.2"

	// Set initial affinity
	am.Set(sourceIP, backend1)

	// Update affinity
	am.Set(sourceIP, backend2)

	// Should get updated value
	got, found := am.Get(sourceIP)
	if !found {
		t.Error("Expected to find affinity entry")
	}
	if got != backend2 {
		t.Errorf("Expected %s, got %s", backend2, got)
	}
}

func TestAffinityMap_Touch(t *testing.T) {
	am := NewAffinityMap("testhost", 100*time.Millisecond)

	sourceIP := "192.168.1.1"
	backendIP := "10.0.0.1"

	// Set affinity
	am.Set(sourceIP, backendIP)

	// Wait a bit
	time.Sleep(60 * time.Millisecond)

	// Touch to extend TTL
	am.Touch(sourceIP)

	// Wait past original TTL but not past new TTL
	time.Sleep(60 * time.Millisecond)

	// Should still be valid because we touched it
	got, found := am.Get(sourceIP)
	if !found {
		t.Error("Expected affinity to still be valid after touch")
	}
	if got != backendIP {
		t.Errorf("Expected %s, got %s", backendIP, got)
	}
}

func TestAffinityMap_Expiration(t *testing.T) {
	am := NewAffinityMap("testhost", 100*time.Millisecond)

	sourceIP := "192.168.1.1"
	backendIP := "10.0.0.1"

	// Set affinity
	am.Set(sourceIP, backendIP)

	// Immediately should be found
	_, found := am.Get(sourceIP)
	if !found {
		t.Error("Expected to find affinity entry immediately")
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired
	_, found = am.Get(sourceIP)
	if found {
		t.Error("Expected affinity to be expired")
	}
}

func TestAffinityMap_Cleanup(t *testing.T) {
	am := NewAffinityMap("testhost", 100*time.Millisecond)

	// Add multiple entries
	for i := 1; i <= 5; i++ {
		sourceIP := "192.168.1." + string(rune(i))
		backendIP := "10.0.0." + string(rune(i))
		am.Set(sourceIP, backendIP)
	}

	// Should have 5 entries
	if am.Size() != 5 {
		t.Errorf("Expected 5 entries, got %d", am.Size())
	}

	// Wait for cleanup to run (cleanup runs every ttl/2, plus some buffer)
	time.Sleep(200 * time.Millisecond)

	// All entries should be cleaned up
	if am.Size() > 0 {
		t.Errorf("Expected 0 entries after cleanup, got %d", am.Size())
	}
}

func TestAffinityMap_MultipleIPs(t *testing.T) {
	am := NewAffinityMap("testhost", 30*time.Second)

	entries := map[string]string{
		"192.168.1.1": "10.0.0.1",
		"192.168.1.2": "10.0.0.2",
		"192.168.1.3": "10.0.0.3",
	}

	// Set multiple affinities
	for sourceIP, backendIP := range entries {
		am.Set(sourceIP, backendIP)
	}

	// Verify all entries
	for sourceIP, expectedBackend := range entries {
		got, found := am.Get(sourceIP)
		if !found {
			t.Errorf("Expected to find affinity for %s", sourceIP)
			continue
		}
		if got != expectedBackend {
			t.Errorf("For %s: expected %s, got %s", sourceIP, expectedBackend, got)
		}
	}
}

func TestAffinityMap_Size(t *testing.T) {
	am := NewAffinityMap("testhost", 30*time.Second)

	if am.Size() != 0 {
		t.Errorf("Expected size 0 for new map, got %d", am.Size())
	}

	am.Set("192.168.1.1", "10.0.0.1")
	if am.Size() != 1 {
		t.Errorf("Expected size 1, got %d", am.Size())
	}

	am.Set("192.168.1.2", "10.0.0.2")
	if am.Size() != 2 {
		t.Errorf("Expected size 2, got %d", am.Size())
	}

	// Setting same IP again shouldn't increase size
	am.Set("192.168.1.1", "10.0.0.3")
	if am.Size() != 2 {
		t.Errorf("Expected size 2 after update, got %d", am.Size())
	}
}
