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

func listenerAndForwardHttp(porti, host, port string, clientProxyProtocol, serverProxyProtocol, isTls bool, cer tls.Certificate, probe *DnsProbe) {
	//slog.Info("Forwarding", "port", porti, "addr", host+":"+port)

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRequestAndRedirect(host, port, isTls, probe))

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
		slog.Info("Forwarding", "port", porti, "addr", host+":"+port, "listenaddr", l1.Addr())
		err := http.Serve(l3, mux)
		slog.Error("http.Serve", "port", port, "err", err)
	}()
}

func handleRequestAndRedirect(host, port string, isTls bool, probe *DnsProbe) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		addr := host + ":" + port
		newSession := false
		cookie, err := r.Cookie(cookieName)
		if err != nil || !probe.checkIp(cookie.Value) {
			newSession = true
			// Cookie not found, set a new one
			targetIP, err := probe.resolve()
			if err != nil {
				slog.Error("resolveTargetIP", "host", host, "err", err)
				http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
				return
			}
			cookie = &http.Cookie{
				Name:  cookieName,
				Value: targetIP,
				Path:  "/",
			}
			http.SetCookie(w, cookie)
		}
		targetAddr := cookie.Value + ":" + port

		scheme := "http"
		if isTls {
			scheme = "https"
		}
		targetURL := fmt.Sprintf("%s://%s", scheme, targetAddr)
		proxyURL, err := url.Parse(targetURL)
		if err != nil {
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			return
		}
		ops.Add(1)
		opened.Add(1)
		defer opened.Add(-1)

		slog.Info("Forwarding start", "port", port, "from", r.RemoteAddr, "scheme", scheme, "to", targetAddr, "newSession", newSession, "count", ops.Load(), "opened", opened.Load())

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
			"addr", addr,
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
