package main

import (
	"io"
	"net"
	"testing"
	"time"
)

func startEchoServer(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start echo server: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				io.Copy(conn, conn)
				conn.Close()
			}()
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func poolWithBackend(addr string) *BackendPool {
	host, port, _ := net.SplitHostPort(addr)
	pool := &BackendPool{
		host:        host,
		port:        port,
		backends:    make(map[string]*Backend),
		backendList: make([]*Backend, 0),
	}
	b := &Backend{IP: host, Port: port, Weight: 1}
	pool.backends[host] = b
	pool.backendList = append(pool.backendList, b)
	return pool
}

func TestForward_BasicTCP(t *testing.T) {
	// Start a backend that reads a line then sends a response
	backendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start backend: %v", err)
	}
	defer backendLn.Close()

	go func() {
		conn, err := backendLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		conn.Write(buf[:n])
	}()

	pool := poolWithBackend(backendLn.Addr().String())
	selector := &RandomSelector{}

	// Start the forwarder
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		forward(conn, pool, "test", selector, nil, ProxyProtocolConfig{})
	}()

	// Connect and send data
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	msg := "hello world"
	conn.Write([]byte(msg))

	// Read the echoed response
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	if string(buf[:n]) != msg {
		t.Errorf("expected %q, got %q", msg, string(buf[:n]))
	}
}

func TestForward_WithAffinity(t *testing.T) {
	backendAddr, cleanup := startEchoServer(t)
	defer cleanup()

	pool := poolWithBackend(backendAddr)
	selector := &RandomSelector{}
	affinity := NewAffinityMap("test", 30*time.Second)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go forward(conn, pool, "test", selector, affinity, ProxyProtocolConfig{})
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	conn.Write([]byte("test"))
	conn.(*net.TCPConn).CloseWrite()
	io.ReadAll(conn)
	conn.Close()

	// Verify affinity was set
	if affinity.Size() != 1 {
		t.Errorf("expected 1 affinity entry, got %d", affinity.Size())
	}
}

func TestForward_BackendUnavailable(t *testing.T) {
	pool := &BackendPool{
		host:        "127.0.0.1",
		port:        "1",
		backends:    make(map[string]*Backend),
		backendList: []*Backend{{IP: "127.0.0.1", Port: "1", Weight: 1}},
	}
	pool.backends["127.0.0.1"] = pool.backendList[0]
	selector := &RandomSelector{}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		forward(conn, pool, "test", selector, nil, ProxyProtocolConfig{})
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Connection should close quickly since backend is unreachable
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	_, err = conn.Read(buf)
	if err == nil {
		t.Error("expected error reading from connection with unavailable backend")
	}
}

func TestForward_NoBackends(t *testing.T) {
	pool := &BackendPool{
		host:        "nohost",
		port:        "9999",
		backends:    make(map[string]*Backend),
		backendList: make([]*Backend, 0),
	}
	selector := &RandomSelector{}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		forward(conn, pool, "test", selector, nil, ProxyProtocolConfig{})
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	_, err = conn.Read(buf)
	if err == nil {
		t.Error("expected connection to close when no backends available")
	}
}

func TestListenAndForward(t *testing.T) {
	// Start a backend that reads then responds
	backendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start backend: %v", err)
	}
	defer backendLn.Close()

	go func() {
		conn, err := backendLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		conn.Write(buf[:n])
	}()

	pool := poolWithBackend(backendLn.Addr().String())
	selector := &RandomSelector{}

	// Find a free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()

	listenAndForward(port, pool, selector, nil, ProxyProtocolConfig{})

	// Give the server time to start
	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatalf("failed to connect to forwarder: %v", err)
	}
	defer conn.Close()

	msg := "listen and forward test"
	conn.Write([]byte(msg))

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1024)
	n, _ := conn.Read(buf)
	if string(buf[:n]) != msg {
		t.Errorf("expected %q, got %q", msg, string(buf[:n]))
	}
}

func TestForward_ProxyProtocol(t *testing.T) {
	// Start a backend that reads the proxy protocol header
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start backend: %v", err)
	}
	defer ln.Close()

	received := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		data, _ := io.ReadAll(conn)
		received <- data
	}()

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	pool := poolWithBackend(host + ":" + port)
	selector := &RandomSelector{}
	proxyConfig := ProxyProtocolConfig{ClientEnabled: true, ClientVersion: 1}

	// Create a client connection
	clientLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer clientLn.Close()

	go func() {
		conn, err := clientLn.Accept()
		if err != nil {
			return
		}
		forward(conn, pool, "test", selector, nil, proxyConfig)
	}()

	conn, err := net.Dial("tcp", clientLn.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	conn.Write([]byte("data"))
	conn.Close()

	select {
	case data := <-received:
		// Should start with "PROXY " for v1
		if len(data) < 6 || string(data[:6]) != "PROXY " {
			t.Errorf("expected PROXY protocol v1 header, got: %q", string(data[:min(20, len(data))]))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for backend to receive data")
	}
}
