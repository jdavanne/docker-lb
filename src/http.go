package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pires/go-proxyproto"
)

const (
	cookieName = "proxy-affinity"
)

// Connection ID key for context
type connIDKey struct{}

// connTransportManager manages per-client-connection transports
type connTransportManager struct {
	mu                sync.RWMutex
	transports        map[uint64]*http.Transport
	connToID          map[net.Conn]uint64 // maps net.Conn to connection ID
	nextID            atomic.Uint64
	idleTimeout       time.Duration
	// Connection-level metrics
	currentConns      atomic.Int64  // number of current client connections
	totalConns        atomic.Uint64 // total client connections accepted
	rejectedConns     atomic.Uint64 // total client connections rejected (e.g., at limit)
	transportsCreated atomic.Uint64 // total transports created
	transportsClosed  atomic.Uint64 // total transports closed
	// Request-level metrics
	requestsTotal     atomic.Uint64 // total HTTP requests handled
	requests503       atomic.Uint64 // requests rejected with 503 (no backend)
	requests502       atomic.Uint64 // requests rejected with 502 (backend error)
}

func newConnTransportManager(idleTimeout time.Duration) *connTransportManager {
	return &connTransportManager{
		transports:  make(map[uint64]*http.Transport),
		connToID:    make(map[net.Conn]uint64),
		idleTimeout: idleTimeout,
	}
}

// registerConn registers a new connection and returns its ID
func (m *connTransportManager) registerConn(c net.Conn) uint64 {
	connID := m.nextID.Add(1)
	m.mu.Lock()
	m.connToID[c] = connID
	m.mu.Unlock()
	m.currentConns.Add(1)
	m.totalConns.Add(1)
	slog.Debug("Registered client connection", "connID", connID, "remoteAddr", c.RemoteAddr())
	return connID
}

// RecordRequest increments the total request counter
func (m *connTransportManager) RecordRequest() {
	m.requestsTotal.Add(1)
}

// Record503 increments the 503 (no backend) counter
func (m *connTransportManager) Record503() {
	m.requests503.Add(1)
}

// Record502 increments the 502 (backend error) counter
func (m *connTransportManager) Record502() {
	m.requests502.Add(1)
}

// RejectConn increments the connection rejected counter
func (m *connTransportManager) RejectConn() {
	m.rejectedConns.Add(1)
}

// getOrCreateTransport returns the transport for a connection, creating one if needed
func (m *connTransportManager) getOrCreateTransport(connID uint64) *http.Transport {
	m.mu.RLock()
	t, ok := m.transports[connID]
	m.mu.RUnlock()
	if ok {
		return t
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Double-check after acquiring write lock
	if t, ok := m.transports[connID]; ok {
		return t
	}

	t = &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     m.idleTimeout,
	}
	m.transports[connID] = t
	m.transportsCreated.Add(1)
	slog.Debug("Created transport for connection", "connID", connID)
	return t
}

// closeConn closes the transport associated with a net.Conn
func (m *connTransportManager) closeConn(c net.Conn) {
	m.mu.Lock()
	connID, hasConnID := m.connToID[c]
	if hasConnID {
		delete(m.connToID, c)
	}
	var t *http.Transport
	hasTransport := false
	if hasConnID {
		t, hasTransport = m.transports[connID]
		if hasTransport {
			delete(m.transports, connID)
		}
	}
	m.mu.Unlock()

	if hasConnID {
		m.currentConns.Add(-1)
	}
	if hasTransport && t != nil {
		t.CloseIdleConnections()
		m.transportsClosed.Add(1)
		slog.Debug("Closed transport for connection", "connID", connID, "remoteAddr", c.RemoteAddr())
	}
}

// TransportStats holds all transport manager statistics
type TransportStats struct {
	// Connection-level stats
	CurrentConns      int64
	TotalConns        uint64
	RejectedConns     uint64
	CurrentTransports int
	TransportsCreated uint64
	TransportsClosed  uint64
	// Request-level stats
	RequestsTotal     uint64
	Requests503       uint64
	Requests502       uint64
}

// Stats returns current statistics for metrics
func (m *connTransportManager) Stats() TransportStats {
	m.mu.RLock()
	currentTransports := len(m.transports)
	m.mu.RUnlock()
	return TransportStats{
		CurrentConns:      m.currentConns.Load(),
		TotalConns:        m.totalConns.Load(),
		RejectedConns:     m.rejectedConns.Load(),
		CurrentTransports: currentTransports,
		TransportsCreated: m.transportsCreated.Load(),
		TransportsClosed:  m.transportsClosed.Load(),
		RequestsTotal:     m.requestsTotal.Load(),
		Requests503:       m.requests503.Load(),
		Requests502:       m.requests502.Load(),
	}
}

func listenerAndForwardHttp(porti, host, port string, proxyConfig ProxyProtocolConfig, isTls bool, cer tls.Certificate, pool Pool, selector BackendSelector, affinity *AffinityMap) *connTransportManager {
	// Create transport manager with 90 second idle timeout for backend connections
	transportMgr := newConnTransportManager(90 * time.Second)

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRequestAndRedirect(host, port, pool, selector, affinity, transportMgr))

	l1, err := net.Listen("tcp", ":"+porti)
	if err != nil {
		slog.Error("Failed to listen on TCP port", "port", porti, "err", err)
		os.Exit(1)
	}

	l2 := l1
	if proxyConfig.ServerEnabled {
		l2 = &proxyproto.Listener{Listener: l1}
		slog.Info("Server-side proxy protocol enabled", "port", porti, "version", proxyConfig.ServerVersion)
	}

	l3 := l2
	if isTls {
		config := &tls.Config{Certificates: []tls.Certificate{cer}}
		l3 = tls.NewListener(l2, config)
	}

	// Create custom server with connection lifecycle hooks
	server := &http.Server{
		Handler: mux,
		// Assign a unique connection ID when a new connection is established
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			connID := transportMgr.registerConn(c)
			return context.WithValue(ctx, connIDKey{}, connID)
		},
		// Clean up transport when client connection closes
		ConnState: func(c net.Conn, state http.ConnState) {
			if state == http.StateClosed || state == http.StateHijacked {
				transportMgr.closeConn(c)
			}
		},
	}

	go func() {
		defer l1.Close()
		if proxyConfig.ServerEnabled {
			defer l2.Close()
		}
		slog.Info("Forwarding", "port", porti, "host", host, "backendPort", port, "algorithm", selector.Name(), "listenaddr", l1.Addr())
		err := server.Serve(l3)
		slog.Error("http.Serve", "port", port, "err", err)
	}()

	return transportMgr
}

func handleRequestAndRedirect(host, port string, pool Pool, selector BackendSelector, affinity *AffinityMap, transportMgr *connTransportManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		addr := host + ":" + port
		newSession := false

		// Track every request
		transportMgr.RecordRequest()

		// Extract source IP for affinity tracking
		sourceIP := ""
		if remoteIP, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			sourceIP = remoteIP
		}

		var backend *Backend
		var err error

		// Priority 1: Check IP affinity (if enabled)
		if affinity != nil && sourceIP != "" {
			if backendIP, found := affinity.Get(sourceIP); found {
				if b := pool.GetBackend(backendIP); b != nil {
					backend = b
					slog.Info("IP affinity hit", "sourceIP", sourceIP, "backendIP", backendIP)
				}
			}
		}

		// Priority 2: Check cookie affinity
		if backend == nil {
			cookie, err := r.Cookie(cookieName)
			if err == nil && pool.CheckIP(cookie.Value) {
				backend = pool.GetBackend(cookie.Value)
				slog.Info("Cookie affinity hit", "sourceIP", sourceIP, "backendIP", cookie.Value)
			}
		}

		// Priority 3: Use load balancing algorithm
		if backend == nil {
			newSession = true
			backend, err = selector.Select(pool, sourceIP, affinity)
			if err != nil {
				slog.Error("Backend selection failed", "host", host, "err", err)
				transportMgr.Record503()
				http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
				return
			}
		}

		targetAddr := backend.IP + ":" + backend.Port

		// Set cookie for future requests (store ip:port to identify backend uniquely)
		cookie := &http.Cookie{
			Name:  cookieName,
			Value: backend.IP + ":" + backend.Port,
			Path:  "/",
		}
		http.SetCookie(w, cookie)

		// Always use HTTP to connect to backends (TLS is only for client connections)
		targetURL := fmt.Sprintf("http://%s", targetAddr)
		proxyURL, err := url.Parse(targetURL)
		if err != nil {
			transportMgr.Record503()
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			return
		}
		ops.Add(1)
		opened.Add(1)
		defer opened.Add(-1)

		// Track connection
		pool.OnConnect(backend)
		defer pool.OnDisconnect(backend)
		if affinity != nil && sourceIP != "" {
			defer affinity.Touch(sourceIP)
		}

		slog.Info("Forwarding start", "port", port, "from", r.RemoteAddr, "to", targetAddr, "backend", backend.IP, "algorithm", selector.Name(), "newSession", newSession, "count", ops.Load(), "opened", opened.Load())

		// Get the transport for this client connection
		// This allows backend connection reuse while the client connection is open,
		// and cleanup when the client disconnects
		var transport *http.Transport
		if connID, ok := r.Context().Value(connIDKey{}).(uint64); ok {
			transport = transportMgr.getOrCreateTransport(connID)
		} else {
			// Fallback: create a one-off transport (shouldn't happen normally)
			slog.Warn("No connection ID in context, using ephemeral transport", "remoteAddr", r.RemoteAddr)
			transport = &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			defer transport.CloseIdleConnections()
		}

		proxy := &httputil.ReverseProxy{
			Rewrite: func(r *httputil.ProxyRequest) {
				r.SetURL(proxyURL)
				r.Out.Host = r.In.Host // if desired
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				slog.Error("Backend error", "target", targetAddr, "err", err)
				transportMgr.Record502()
				http.Error(w, "Bad Gateway", http.StatusBadGateway)
			},
		}
		proxy.Transport = transport
		proxy.ServeHTTP(w, r)

		slog.Info("Forwarding close", "port", port, "from", r.RemoteAddr, "to", targetAddr,
			"addr", addr, "backend", backend.IP,
			"count", ops.Load(), "opened", opened.Load()-1,
		)
	}
}
