package main

import (
	"net"
	"testing"
	"time"
)

// mockConn implements net.Conn for testing
type mockConn struct {
	remoteAddr net.Addr
}

func (m *mockConn) Read(b []byte) (n int, err error)   { return 0, nil }
func (m *mockConn) Write(b []byte) (n int, err error)  { return len(b), nil }
func (m *mockConn) Close() error                       { return nil }
func (m *mockConn) LocalAddr() net.Addr                { return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080} }
func (m *mockConn) RemoteAddr() net.Addr               { return m.remoteAddr }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func newMockConn(ip string, port int) *mockConn {
	return &mockConn{
		remoteAddr: &net.TCPAddr{IP: net.ParseIP(ip), Port: port},
	}
}

func TestConnTransportManager_NewManager(t *testing.T) {
	mgr := newConnTransportManager(90 * time.Second)

	if mgr == nil {
		t.Fatal("Expected non-nil manager")
	}
	if mgr.idleTimeout != 90*time.Second {
		t.Errorf("Expected idleTimeout 90s, got %v", mgr.idleTimeout)
	}
	if mgr.transports == nil {
		t.Error("Expected transports map to be initialized")
	}
	if mgr.connToID == nil {
		t.Error("Expected connToID map to be initialized")
	}
}

func TestConnTransportManager_RegisterConn(t *testing.T) {
	mgr := newConnTransportManager(90 * time.Second)
	conn1 := newMockConn("192.168.1.1", 12345)
	conn2 := newMockConn("192.168.1.2", 12346)

	// Register first connection
	id1 := mgr.registerConn(conn1)
	if id1 != 1 {
		t.Errorf("Expected first connection ID to be 1, got %d", id1)
	}

	// Check stats
	stats := mgr.Stats()
	if stats.CurrentConns != 1 {
		t.Errorf("Expected CurrentConns=1, got %d", stats.CurrentConns)
	}
	if stats.TotalConns != 1 {
		t.Errorf("Expected TotalConns=1, got %d", stats.TotalConns)
	}

	// Register second connection
	id2 := mgr.registerConn(conn2)
	if id2 != 2 {
		t.Errorf("Expected second connection ID to be 2, got %d", id2)
	}

	stats = mgr.Stats()
	if stats.CurrentConns != 2 {
		t.Errorf("Expected CurrentConns=2, got %d", stats.CurrentConns)
	}
	if stats.TotalConns != 2 {
		t.Errorf("Expected TotalConns=2, got %d", stats.TotalConns)
	}
}

func TestConnTransportManager_CloseConn(t *testing.T) {
	mgr := newConnTransportManager(90 * time.Second)
	conn := newMockConn("192.168.1.1", 12345)

	// Register and create transport
	connID := mgr.registerConn(conn)
	_ = mgr.getOrCreateTransport(connID)

	stats := mgr.Stats()
	if stats.CurrentConns != 1 {
		t.Errorf("Expected CurrentConns=1 before close, got %d", stats.CurrentConns)
	}
	if stats.CurrentTransports != 1 {
		t.Errorf("Expected CurrentTransports=1 before close, got %d", stats.CurrentTransports)
	}

	// Close connection
	mgr.closeConn(conn)

	stats = mgr.Stats()
	if stats.CurrentConns != 0 {
		t.Errorf("Expected CurrentConns=0 after close, got %d", stats.CurrentConns)
	}
	if stats.CurrentTransports != 0 {
		t.Errorf("Expected CurrentTransports=0 after close, got %d", stats.CurrentTransports)
	}
	if stats.TransportsClosed != 1 {
		t.Errorf("Expected TransportsClosed=1, got %d", stats.TransportsClosed)
	}
}

func TestConnTransportManager_CloseConnWithoutTransport(t *testing.T) {
	mgr := newConnTransportManager(90 * time.Second)
	conn := newMockConn("192.168.1.1", 12345)

	// Register but don't create transport
	mgr.registerConn(conn)

	stats := mgr.Stats()
	if stats.CurrentConns != 1 {
		t.Errorf("Expected CurrentConns=1, got %d", stats.CurrentConns)
	}

	// Close connection (no transport was created)
	mgr.closeConn(conn)

	stats = mgr.Stats()
	if stats.CurrentConns != 0 {
		t.Errorf("Expected CurrentConns=0 after close, got %d", stats.CurrentConns)
	}
	if stats.TransportsClosed != 0 {
		t.Errorf("Expected TransportsClosed=0 (no transport created), got %d", stats.TransportsClosed)
	}
}

func TestConnTransportManager_CloseUnknownConn(t *testing.T) {
	mgr := newConnTransportManager(90 * time.Second)
	conn := newMockConn("192.168.1.1", 12345)

	// Close without registering - should not panic
	mgr.closeConn(conn)

	stats := mgr.Stats()
	if stats.CurrentConns != 0 {
		t.Errorf("Expected CurrentConns=0, got %d", stats.CurrentConns)
	}
}

func TestConnTransportManager_GetOrCreateTransport(t *testing.T) {
	mgr := newConnTransportManager(90 * time.Second)
	conn := newMockConn("192.168.1.1", 12345)

	connID := mgr.registerConn(conn)

	// First call should create transport
	transport1 := mgr.getOrCreateTransport(connID)
	if transport1 == nil {
		t.Fatal("Expected non-nil transport")
	}

	stats := mgr.Stats()
	if stats.TransportsCreated != 1 {
		t.Errorf("Expected TransportsCreated=1, got %d", stats.TransportsCreated)
	}
	if stats.CurrentTransports != 1 {
		t.Errorf("Expected CurrentTransports=1, got %d", stats.CurrentTransports)
	}

	// Second call should return same transport (reuse)
	transport2 := mgr.getOrCreateTransport(connID)
	if transport2 != transport1 {
		t.Error("Expected same transport on second call")
	}

	stats = mgr.Stats()
	if stats.TransportsCreated != 1 {
		t.Errorf("Expected TransportsCreated still 1, got %d", stats.TransportsCreated)
	}
}

func TestConnTransportManager_TransportConfig(t *testing.T) {
	idleTimeout := 60 * time.Second
	mgr := newConnTransportManager(idleTimeout)
	conn := newMockConn("192.168.1.1", 12345)

	connID := mgr.registerConn(conn)
	transport := mgr.getOrCreateTransport(connID)

	if transport.IdleConnTimeout != idleTimeout {
		t.Errorf("Expected IdleConnTimeout=%v, got %v", idleTimeout, transport.IdleConnTimeout)
	}
	if transport.MaxIdleConns != 10 {
		t.Errorf("Expected MaxIdleConns=10, got %d", transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != 5 {
		t.Errorf("Expected MaxIdleConnsPerHost=5, got %d", transport.MaxIdleConnsPerHost)
	}
	if transport.TLSClientConfig == nil {
		t.Error("Expected TLSClientConfig to be set")
	}
}

func TestConnTransportManager_MultipleConnections(t *testing.T) {
	mgr := newConnTransportManager(90 * time.Second)

	// Create multiple connections
	conns := make([]*mockConn, 5)
	connIDs := make([]uint64, 5)
	for i := 0; i < 5; i++ {
		conns[i] = newMockConn("192.168.1.1", 12345+i)
		connIDs[i] = mgr.registerConn(conns[i])
	}

	stats := mgr.Stats()
	if stats.CurrentConns != 5 {
		t.Errorf("Expected CurrentConns=5, got %d", stats.CurrentConns)
	}
	if stats.TotalConns != 5 {
		t.Errorf("Expected TotalConns=5, got %d", stats.TotalConns)
	}

	// Create transports for some
	for i := 0; i < 3; i++ {
		mgr.getOrCreateTransport(connIDs[i])
	}

	stats = mgr.Stats()
	if stats.CurrentTransports != 3 {
		t.Errorf("Expected CurrentTransports=3, got %d", stats.CurrentTransports)
	}

	// Close some connections
	mgr.closeConn(conns[0])
	mgr.closeConn(conns[1])

	stats = mgr.Stats()
	if stats.CurrentConns != 3 {
		t.Errorf("Expected CurrentConns=3, got %d", stats.CurrentConns)
	}
	if stats.CurrentTransports != 1 {
		t.Errorf("Expected CurrentTransports=1, got %d", stats.CurrentTransports)
	}
	if stats.TransportsClosed != 2 {
		t.Errorf("Expected TransportsClosed=2, got %d", stats.TransportsClosed)
	}
}

func TestConnTransportManager_RecordRequest(t *testing.T) {
	mgr := newConnTransportManager(90 * time.Second)

	// Record multiple requests
	for i := 0; i < 10; i++ {
		mgr.RecordRequest()
	}

	stats := mgr.Stats()
	if stats.RequestsTotal != 10 {
		t.Errorf("Expected RequestsTotal=10, got %d", stats.RequestsTotal)
	}
}

func TestConnTransportManager_Record503(t *testing.T) {
	mgr := newConnTransportManager(90 * time.Second)

	mgr.Record503()
	mgr.Record503()
	mgr.Record503()

	stats := mgr.Stats()
	if stats.Requests503 != 3 {
		t.Errorf("Expected Requests503=3, got %d", stats.Requests503)
	}
}

func TestConnTransportManager_Record502(t *testing.T) {
	mgr := newConnTransportManager(90 * time.Second)

	mgr.Record502()
	mgr.Record502()

	stats := mgr.Stats()
	if stats.Requests502 != 2 {
		t.Errorf("Expected Requests502=2, got %d", stats.Requests502)
	}
}

func TestConnTransportManager_RejectConn(t *testing.T) {
	mgr := newConnTransportManager(90 * time.Second)

	mgr.RejectConn()
	mgr.RejectConn()

	stats := mgr.Stats()
	if stats.RejectedConns != 2 {
		t.Errorf("Expected RejectedConns=2, got %d", stats.RejectedConns)
	}
}

func TestConnTransportManager_Stats(t *testing.T) {
	mgr := newConnTransportManager(90 * time.Second)
	conn1 := newMockConn("192.168.1.1", 12345)
	conn2 := newMockConn("192.168.1.2", 12346)

	// Perform various operations
	id1 := mgr.registerConn(conn1)
	mgr.registerConn(conn2)
	mgr.getOrCreateTransport(id1)
	mgr.RecordRequest()
	mgr.RecordRequest()
	mgr.Record503()
	mgr.Record502()
	mgr.RejectConn()
	mgr.closeConn(conn1)

	stats := mgr.Stats()

	// Verify all stats
	if stats.CurrentConns != 1 {
		t.Errorf("Expected CurrentConns=1, got %d", stats.CurrentConns)
	}
	if stats.TotalConns != 2 {
		t.Errorf("Expected TotalConns=2, got %d", stats.TotalConns)
	}
	if stats.RejectedConns != 1 {
		t.Errorf("Expected RejectedConns=1, got %d", stats.RejectedConns)
	}
	if stats.CurrentTransports != 0 {
		t.Errorf("Expected CurrentTransports=0, got %d", stats.CurrentTransports)
	}
	if stats.TransportsCreated != 1 {
		t.Errorf("Expected TransportsCreated=1, got %d", stats.TransportsCreated)
	}
	if stats.TransportsClosed != 1 {
		t.Errorf("Expected TransportsClosed=1, got %d", stats.TransportsClosed)
	}
	if stats.RequestsTotal != 2 {
		t.Errorf("Expected RequestsTotal=2, got %d", stats.RequestsTotal)
	}
	if stats.Requests503 != 1 {
		t.Errorf("Expected Requests503=1, got %d", stats.Requests503)
	}
	if stats.Requests502 != 1 {
		t.Errorf("Expected Requests502=1, got %d", stats.Requests502)
	}
}

func TestConnTransportManager_ConcurrentAccess(t *testing.T) {
	mgr := newConnTransportManager(90 * time.Second)
	done := make(chan bool)

	// Spawn multiple goroutines to test thread safety
	for i := 0; i < 10; i++ {
		go func(id int) {
			conn := newMockConn("192.168.1.1", 12345+id)
			connID := mgr.registerConn(conn)
			mgr.getOrCreateTransport(connID)
			mgr.RecordRequest()
			mgr.Record503()
			mgr.Record502()
			_ = mgr.Stats()
			mgr.closeConn(conn)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	stats := mgr.Stats()
	if stats.TotalConns != 10 {
		t.Errorf("Expected TotalConns=10, got %d", stats.TotalConns)
	}
	if stats.CurrentConns != 0 {
		t.Errorf("Expected CurrentConns=0, got %d", stats.CurrentConns)
	}
	if stats.RequestsTotal != 10 {
		t.Errorf("Expected RequestsTotal=10, got %d", stats.RequestsTotal)
	}
}
