package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/pires/go-proxyproto"
)

const (
	cookieName = "proxy-affinity"
)

func listenerAndForwardHttp(porti, host, port string, clientProxyProtocol, serverProxyProtocol, isTls bool, cer tls.Certificate, pool *BackendPool, selector BackendSelector, affinity *AffinityMap) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRequestAndRedirect(host, port, pool, selector, affinity))

	l1, err := net.Listen("tcp", ":"+porti)
	if err != nil {
		log.Fatal(err)
	}

	l2 := l1
	if serverProxyProtocol {
		l2 = &proxyproto.Listener{Listener: l1}
	}

	l3 := l2
	if isTls {
		config := &tls.Config{Certificates: []tls.Certificate{cer}}
		l3 = tls.NewListener(l2, config)
	}

	go func() {
		defer l1.Close()
		if serverProxyProtocol {
			defer l2.Close()
		}
		slog.Info("Forwarding", "port", porti, "host", host, "backendPort", port, "algorithm", selector.Name(), "listenaddr", l1.Addr())
		err := http.Serve(l3, mux)
		slog.Error("http.Serve", "port", port, "err", err)
	}()
}

func handleRequestAndRedirect(host, port string, pool *BackendPool, selector BackendSelector, affinity *AffinityMap) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		addr := host + ":" + port
		newSession := false

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
			if err == nil && pool.checkIp(cookie.Value) {
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
				http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
				return
			}
		}

		targetAddr := backend.IP + ":" + port

		// Set cookie for future requests
		cookie := &http.Cookie{
			Name:  cookieName,
			Value: backend.IP,
			Path:  "/",
		}
		http.SetCookie(w, cookie)

		// Always use HTTP to connect to backends (TLS is only for client connections)
		targetURL := fmt.Sprintf("http://%s", targetAddr)
		proxyURL, err := url.Parse(targetURL)
		if err != nil {
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

		proxy := &httputil.ReverseProxy{
			Rewrite: func(r *httputil.ProxyRequest) {
				r.SetURL(proxyURL)
				r.Out.Host = r.In.Host // if desired
			},
		}

		//proxy := httputil.NewSingleHostReverseProxy(proxyURL)
		//r.Host = proxyURL.Host
		proxy.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		proxy.ServeHTTP(w, r)

		slog.Info("Forwarding close", "port", port, "from", r.RemoteAddr, "to", targetAddr,
			"addr", addr, "backend", backend.IP,
			"count", ops.Load(), "opened", opened.Load()-1,
		)
	}
}

/*
func resolveTargetIP(host string) (string, error) {
	ips, err := net.LookupIP(host)
	if err != nil {
		return "", err
	}
	for _, ip := range ips {
		if ipv4 := ip.To4(); ipv4 != nil {
			return ipv4.String(), nil
		}
	}
	return "", fmt.Errorf("no IPv4 address found for host %s", host)
}
*/
