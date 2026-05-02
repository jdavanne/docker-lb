package main

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleRequestAndRedirect_BasicProxy(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello from backend"))
	}))
	defer backend.Close()

	host, port, _ := net.SplitHostPort(backend.Listener.Addr().String())
	pool := poolWithBackend(host + ":" + port)
	selector := &RandomSelector{}
	transportMgr := newConnTransportManager(90 * time.Second)

	handler := handleRequestAndRedirect(host, port, pool, selector, nil, transportMgr)

	req := httptest.NewRequest("GET", "http://localhost/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	// Add connection ID to context
	connID := transportMgr.nextID.Add(1)
	ctx := context.WithValue(req.Context(), connIDKey{}, connID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if body != "hello from backend" {
		t.Errorf("expected 'hello from backend', got %q", body)
	}
}

func TestHandleRequestAndRedirect_SetsCookie(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	host, port, _ := net.SplitHostPort(backend.Listener.Addr().String())
	pool := poolWithBackend(host + ":" + port)
	selector := &RandomSelector{}
	transportMgr := newConnTransportManager(90 * time.Second)

	handler := handleRequestAndRedirect(host, port, pool, selector, nil, transportMgr)

	req := httptest.NewRequest("GET", "http://localhost/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	connID := transportMgr.nextID.Add(1)
	ctx := context.WithValue(req.Context(), connIDKey{}, connID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler(w, req)

	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == cookieName {
			found = true
			expected := host + ":" + port
			if c.Value != expected {
				t.Errorf("expected cookie value %q, got %q", expected, c.Value)
			}
		}
	}
	if !found {
		t.Error("expected proxy-affinity cookie to be set")
	}
}

func TestHandleRequestAndRedirect_CookieAffinity(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("backend1"))
	}))
	defer backend.Close()

	host, port, _ := net.SplitHostPort(backend.Listener.Addr().String())
	pool := poolWithBackend(host + ":" + port)
	selector := &RandomSelector{}
	transportMgr := newConnTransportManager(90 * time.Second)

	handler := handleRequestAndRedirect(host, port, pool, selector, nil, transportMgr)

	// Send request with affinity cookie
	req := httptest.NewRequest("GET", "http://localhost/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	req.AddCookie(&http.Cookie{Name: cookieName, Value: host + ":" + port})
	connID := transportMgr.nextID.Add(1)
	ctx := context.WithValue(req.Context(), connIDKey{}, connID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleRequestAndRedirect_NoBackends503(t *testing.T) {
	pool := &BackendPool{
		host:        "nohost",
		port:        "9999",
		backends:    make(map[string]*Backend),
		backendList: make([]*Backend, 0),
	}
	selector := &RandomSelector{}
	transportMgr := newConnTransportManager(90 * time.Second)

	handler := handleRequestAndRedirect("nohost", "9999", pool, selector, nil, transportMgr)

	req := httptest.NewRequest("GET", "http://localhost/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	connID := transportMgr.nextID.Add(1)
	ctx := context.WithValue(req.Context(), connIDKey{}, connID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestHandleRequestAndRedirect_BackendDown502(t *testing.T) {
	// Create a backend that immediately closes
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // Close immediately so connections fail

	host, port, _ := net.SplitHostPort(addr)
	pool := poolWithBackend(host + ":" + port)
	selector := &RandomSelector{}
	transportMgr := newConnTransportManager(90 * time.Second)

	handler := handleRequestAndRedirect(host, port, pool, selector, nil, transportMgr)

	req := httptest.NewRequest("GET", "http://localhost/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	connID := transportMgr.nextID.Add(1)
	ctx := context.WithValue(req.Context(), connIDKey{}, connID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestHandleRequestAndRedirect_IPAffinity(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	host, port, _ := net.SplitHostPort(backend.Listener.Addr().String())
	pool := poolWithBackend(host + ":" + port)
	selector := &RandomSelector{}
	affinity := NewAffinityMap("test", 30*time.Second)
	transportMgr := newConnTransportManager(90 * time.Second)

	handler := handleRequestAndRedirect(host, port, pool, selector, affinity, transportMgr)

	// First request: sets affinity
	req := httptest.NewRequest("GET", "http://localhost/", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	connID := transportMgr.nextID.Add(1)
	ctx := context.WithValue(req.Context(), connIDKey{}, connID)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify affinity was set
	backendIP, found := affinity.Get("192.168.1.100")
	if !found {
		t.Fatal("expected affinity to be set")
	}
	if backendIP != host+":"+port {
		t.Errorf("expected affinity to %s:%s, got %s", host, port, backendIP)
	}
}

func TestHandleRequestAndRedirect_MultiTarget(t *testing.T) {
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("backend1"))
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("backend2"))
	}))
	defer backend2.Close()

	host1, port1, _ := net.SplitHostPort(backend1.Listener.Addr().String())
	host2, port2, _ := net.SplitHostPort(backend2.Listener.Addr().String())

	pool1 := poolWithBackend(host1 + ":" + port1)
	pool2 := poolWithBackend(host2 + ":" + port2)
	mp := NewMultiPool([]*BackendPool{pool1, pool2})

	selector := &RandomSelector{}
	transportMgr := newConnTransportManager(90 * time.Second)

	handler := handleRequestAndRedirect(host1, port1, mp, selector, nil, transportMgr)

	// Make multiple requests and check we hit both backends
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		req := httptest.NewRequest("GET", "http://localhost/", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		connID := transportMgr.nextID.Add(1)
		ctx := context.WithValue(req.Context(), connIDKey{}, connID)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code == http.StatusOK {
			body, _ := io.ReadAll(w.Result().Body)
			seen[string(body)] = true
		}
	}

	if !seen["backend1"] || !seen["backend2"] {
		t.Errorf("expected traffic to both backends, got: %v", seen)
	}
}

func TestListenerAndForwardHttp(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("http forward test"))
	}))
	defer backend.Close()

	host, port, _ := net.SplitHostPort(backend.Listener.Addr().String())
	pool := poolWithBackend(host + ":" + port)
	selector := &RandomSelector{}

	// Find a free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	_, listenPort, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()

	transportMgr := listenerAndForwardHttp(listenPort, host, port, ProxyProtocolConfig{}, false, tls.Certificate{}, pool, selector, nil)
	_ = transportMgr

	// Give the server time to start
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get("http://127.0.0.1:" + listenPort + "/")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "http forward test" {
		t.Errorf("expected 'http forward test', got %q", string(body))
	}
}
