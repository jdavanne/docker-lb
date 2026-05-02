package main

import (
	"testing"
	"time"
)

type mockSubscriber struct {
	host    string
	port    string
	updates [][]string
}

func (m *mockSubscriber) OnDNSUpdate(ips []string) {
	m.updates = append(m.updates, ips)
}

func (m *mockSubscriber) GetHost() string { return m.host }
func (m *mockSubscriber) GetPort() string { return m.port }

func TestDNSResolver_NewResolver(t *testing.T) {
	r := NewDNSResolver("example.com", 2*time.Second)
	if r.host != "example.com" {
		t.Errorf("expected host example.com, got %s", r.host)
	}
	if r.probePeriod != 2*time.Second {
		t.Errorf("expected probe period 2s, got %v", r.probePeriod)
	}
	if len(r.ips) != 0 {
		t.Errorf("expected empty IPs, got %v", r.ips)
	}
}

func TestDNSResolver_Subscribe(t *testing.T) {
	r := NewDNSResolver("example.com", 2*time.Second)
	sub := &mockSubscriber{host: "example.com", port: "9000"}

	r.Subscribe(sub)

	if len(r.subscribers) != 1 {
		t.Errorf("expected 1 subscriber, got %d", len(r.subscribers))
	}
}

func TestDNSResolver_SubscribeWithExistingIPs(t *testing.T) {
	r := NewDNSResolver("example.com", 2*time.Second)
	r.ips = []string{"10.0.0.1", "10.0.0.2"}

	sub := &mockSubscriber{host: "example.com", port: "9000"}
	r.Subscribe(sub)

	if len(sub.updates) != 1 {
		t.Fatalf("expected 1 update on subscribe, got %d", len(sub.updates))
	}
	if len(sub.updates[0]) != 2 {
		t.Errorf("expected 2 IPs in update, got %d", len(sub.updates[0]))
	}
}

func TestDNSResolver_GetIPs(t *testing.T) {
	r := NewDNSResolver("example.com", 2*time.Second)
	r.ips = []string{"10.0.0.1", "10.0.0.2"}

	ips := r.GetIPs()
	if len(ips) != 2 {
		t.Errorf("expected 2 IPs, got %d", len(ips))
	}

	// Verify it returns a copy
	ips[0] = "modified"
	if r.ips[0] == "modified" {
		t.Error("GetIPs should return a copy, not a reference")
	}
}

func TestDNSResolver_UpdateIPs_Changed(t *testing.T) {
	r := NewDNSResolver("example.com", 2*time.Second)
	r.ips = []string{"10.0.0.1"}

	changed := r.updateIPs([]string{"10.0.0.1", "10.0.0.2"})
	if !changed {
		t.Error("expected changed=true when IPs differ in length")
	}
	if len(r.ips) != 2 {
		t.Errorf("expected 2 IPs after update, got %d", len(r.ips))
	}
}

func TestDNSResolver_UpdateIPs_NoChange(t *testing.T) {
	r := NewDNSResolver("example.com", 2*time.Second)
	r.ips = []string{"10.0.0.1", "10.0.0.2"}

	changed := r.updateIPs([]string{"10.0.0.1", "10.0.0.2"})
	if changed {
		t.Error("expected changed=false when IPs are same")
	}
}

func TestDNSResolver_UpdateIPs_DifferentIPs(t *testing.T) {
	r := NewDNSResolver("example.com", 2*time.Second)
	r.ips = []string{"10.0.0.1", "10.0.0.2"}

	changed := r.updateIPs([]string{"10.0.0.1", "10.0.0.3"})
	if !changed {
		t.Error("expected changed=true when IP content differs")
	}
}

func TestDNSResolver_NotifySubscribers(t *testing.T) {
	r := NewDNSResolver("example.com", 2*time.Second)
	sub1 := &mockSubscriber{host: "example.com", port: "9000"}
	sub2 := &mockSubscriber{host: "example.com", port: "9001"}
	r.subscribers = []DNSSubscriber{sub1, sub2}

	r.notifySubscribers([]string{"10.0.0.1", "10.0.0.2"})

	if len(sub1.updates) != 1 {
		t.Errorf("expected sub1 to receive 1 update, got %d", len(sub1.updates))
	}
	if len(sub2.updates) != 1 {
		t.Errorf("expected sub2 to receive 1 update, got %d", len(sub2.updates))
	}
}

func TestDNSResolver_StartWithLocalhost(t *testing.T) {
	// Test with "localhost" which should always resolve
	r := NewDNSResolver("localhost", 100*time.Millisecond)
	sub := &mockSubscriber{host: "localhost", port: "9000"}
	r.Subscribe(sub)

	go r.start()

	// Wait for at least one probe
	time.Sleep(300 * time.Millisecond)

	if len(sub.updates) == 0 {
		t.Error("expected at least one DNS update for localhost")
	}
	if len(sub.updates[0]) == 0 {
		t.Error("expected at least one IP for localhost")
	}
}

func TestBackendPool_OnDNSUpdate(t *testing.T) {
	pool := NewBackendPool("svc", "9000")

	pool.OnDNSUpdate([]string{"10.0.0.1", "10.0.0.2"})

	backends := pool.GetBackends()
	if len(backends) != 2 {
		t.Fatalf("expected 2 backends, got %d", len(backends))
	}

	// Add a new IP, remove one
	pool.OnDNSUpdate([]string{"10.0.0.2", "10.0.0.3"})
	backends = pool.GetBackends()
	if len(backends) != 2 {
		t.Fatalf("expected 2 backends after update, got %d", len(backends))
	}

	// Verify 10.0.0.1 is gone and 10.0.0.3 is added
	if pool.GetBackend("10.0.0.1") != nil {
		t.Error("expected 10.0.0.1 to be removed")
	}
	if pool.GetBackend("10.0.0.3") == nil {
		t.Error("expected 10.0.0.3 to be added")
	}
}

func TestBackendPool_OnDNSUpdate_EmptyList(t *testing.T) {
	pool := NewBackendPool("svc", "9000")
	pool.OnDNSUpdate([]string{"10.0.0.1"})

	pool.OnDNSUpdate([]string{})

	backends := pool.GetBackends()
	if len(backends) != 0 {
		t.Errorf("expected 0 backends after empty update, got %d", len(backends))
	}
}
