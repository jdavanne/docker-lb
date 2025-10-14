package main

import (
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pires/go-proxyproto"
)

func forward(local net.Conn, pool *BackendPool, port string, selector BackendSelector, affinity *AffinityMap, proxyConfig ProxyProtocolConfig) {
	defer local.Close()

	ops.Add(1)
	opened.Add(1)
	defer opened.Add(-1)

	// Extract source IP for affinity tracking
	sourceIP := ""
	if remoteAddr, ok := local.RemoteAddr().(*net.TCPAddr); ok {
		sourceIP = remoteAddr.IP.String()
	}

	// Select backend using algorithm and affinity
	backend, err := selector.Select(pool, sourceIP, affinity)
	if err != nil {
		slog.Error("Backend selection failed", "port", port, "from", local.RemoteAddr(), "err", err)
		return
	}

	addr := backend.IP + ":" + backend.Port

	// Track connection
	pool.OnConnect(backend)
	defer pool.OnDisconnect(backend)
	if affinity != nil && sourceIP != "" {
		defer affinity.Touch(sourceIP)
	}

	// Connect to the remote server
	remote, err := net.Dial("tcp", addr)
	if err != nil {
		slog.Error("Dial failed", "port", port, "from", local.RemoteAddr(), "addr", addr, "backend", backend.IP, "err", err)
		return
	}
	defer remote.Close()

	slog.Info("Forwarding start", "port", port, "from", local.RemoteAddr(), "to", remote.RemoteAddr(), "backend", backend.IP, "algorithm", selector.Name(), "count", ops.Load(), "opened", opened.Load())

	if proxyConfig.ClientEnabled {
		// Create a proxyprotocol header with configurable version
		header := &proxyproto.Header{
			Version:           proxyConfig.ClientVersion,
			Command:           proxyproto.PROXY,
			TransportProtocol: proxyproto.TCPv4,
			SourceAddr:        local.RemoteAddr(),
			DestinationAddr:   local.LocalAddr(),
		}
		// After the connection was created write the proxy headers first
		_, err = header.WriteTo(remote)
		if err != nil {
			slog.Error("Proxy protocol header write failed", "port", port, "from", local.RemoteAddr(), "to", remote.RemoteAddr(), "addr", addr, "version", proxyConfig.ClientVersion, "err", err)
			return
		}
		slog.Info("Proxy protocol header sent", "port", port, "version", proxyConfig.ClientVersion, "backend", backend.IP)
	}

	var sent, received int64
	var err1, err2 error
	var closed atomic.Bool
	start := time.Now()
	wg := sync.WaitGroup{}
	wg.Add(2)
	// Run in parallel to prevent blocking
	go func() {
		defer wg.Done()
		defer remote.Close()
		defer local.Close()
		// Copy the data from the client to the remote server
		received, err1 = io.Copy(remote, local)
		if err1 != nil && closed.Load() == false {
			slog.Error("Connection error", "port", port, "from", local.RemoteAddr(), "to", remote.RemoteAddr(), "addr", addr, "err", err)
		}
		//slog.Info("Forwarding close", "port", port, "from", local.RemoteAddr(), "to", remote.RemoteAddr(), "received", received)
		closed.Store(true)
	}()

	go func() {
		defer wg.Done()
		defer local.Close()
		defer remote.Close()
		// Copy the data from the remote server to the client
		sent, err = io.Copy(local, remote)
		if err2 != nil && closed.Load() == false {
			slog.Error("Connection error", "port", port, "from", local.RemoteAddr(), "to", remote.RemoteAddr(), "addr", addr, "err", err)
		}
		//slog.Info("Forwarding close", "port", port, "from", local.RemoteAddr(), "to", remote.RemoteAddr(), "sent", sent)
		closed.Store(true)
	}()
	wg.Wait()
	end := time.Now()
	duration := end.Sub(start)
	cumSent.Add(sent)
	cumReceived.Add(received)

	// Track bytes for backend
	pool.AddBytes(backend, sent+received)

	slog.Info("Forwarding close", "port", port, "from", local.RemoteAddr(), "to", remote.RemoteAddr(),
		"addr", addr, "backend", backend.IP, "sent", sent, "received", received, "duration", duration,
		"count", ops.Load(), "opened", opened.Load()-1,
		"cumSent", cumSent.Load(), "cumReceived", cumReceived.Load(),
	)
}

func listenAndForward(port string, pool *BackendPool, selector BackendSelector, affinity *AffinityMap, proxyConfig ProxyProtocolConfig) {
	l1, err := net.Listen("tcp", ":"+port)
	if err != nil {
		slog.Error("Failed to listen on TCP port", "port", port, "err", err)
		os.Exit(1)
	}

	l2 := l1
	if proxyConfig.ServerEnabled {
		l2 = &proxyproto.Listener{Listener: l1}
		slog.Info("Server-side proxy protocol enabled", "port", port, "version", proxyConfig.ServerVersion)
	}

	go func() {
		defer l1.Close()
		if proxyConfig.ServerEnabled {
			defer l2.Close()
		}
		slog.Info("Forwarding", "port", port, "host", pool.host, "backendPort", pool.port, "algorithm", selector.Name(), "listenaddr", l1.Addr())
		for {
			// Wait for a connection.
			conn, err := l2.Accept()
			if err != nil {
				slog.Error("Failed to accept connection", "port", port, "err", err)
				os.Exit(1)
			}
			// Handle the connection in a new goroutine.
			// The loop then returns to accepting, so that
			// multiple connections may be served concurrently.
			go forward(conn, pool, port, selector, affinity, proxyConfig)
		}
	}()
}
