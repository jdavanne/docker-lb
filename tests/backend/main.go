package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/pires/go-proxyproto"
)

// pp2TypeTrace is the PROXY v2 TLV type docker-lb uses to carry W3C trace
// context (must match src/trace.go PP2TypeTrace).
const pp2TypeTrace = 0xE1

var (
	requestCount atomic.Uint64
	serviceName  string
	proxyMode    bool
)

type connKey struct{}

type Response struct {
	Service      string `json:"service"`
	Hostname     string `json:"hostname"`
	Port         string `json:"port"`
	RequestCount uint64 `json:"request_count"`
	Message      string `json:"message"`
	// Traceparent is the inbound W3C traceparent HTTP header, if any.
	Traceparent string `json:"traceparent,omitempty"`
	// ProxyTraceparent is the traceparent decoded from the PROXY v2 trace TLV
	// (only populated in --proxy mode when a valid 0xE1 TLV is present).
	ProxyTraceparent string `json:"proxy_traceparent,omitempty"`
}

func handler(port string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		hostname, _ := os.Hostname()

		resp := Response{
			Service:      serviceName,
			Hostname:     hostname,
			Port:         port,
			RequestCount: count,
			Message:      fmt.Sprintf("Hello from %s:%s:%s (request #%d)", serviceName, hostname, port, count),
			Traceparent:  r.Header.Get("traceparent"),
		}

		if proxyMode {
			if c, ok := r.Context().Value(connKey{}).(net.Conn); ok {
				resp.ProxyTraceparent = proxyTraceparent(c)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

		log.Printf("Request #%d on port %s from %s traceparent=%q proxy_traceparent=%q",
			count, port, r.RemoteAddr, resp.Traceparent, resp.ProxyTraceparent)
	}
}

// proxyTraceparent extracts the W3C traceparent from the PROXY v2 trace TLV
// (0xE1, subtype 0x1) on the accepted connection, or "" if absent/invalid.
func proxyTraceparent(c net.Conn) string {
	pc, ok := c.(*proxyproto.Conn)
	if !ok {
		return ""
	}
	h := pc.ProxyHeader()
	if h == nil || h.Version != 2 {
		return ""
	}
	tlvs, err := h.TLVs()
	if err != nil {
		return ""
	}
	for _, t := range tlvs {
		if byte(t.Type) != pp2TypeTrace || len(t.Value) < 1 {
			continue
		}
		// subtype/version byte: high nibble version (0x0), low nibble subtype.
		// 0x01 = traceparent only; the remaining bytes are the ASCII value.
		if t.Value[0] == 0x01 {
			return string(t.Value[1:])
		}
	}
	return ""
}

func serve(port string, foreground bool) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler(port))

	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("listen on port %s: %v", port, err)
	}
	if proxyMode {
		ln = &proxyproto.Listener{Listener: ln}
	}

	srv := &http.Server{
		Handler: mux,
		// Stash the accepted connection so the handler can read its PROXY header.
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, connKey{}, c)
		},
	}

	log.Printf("Listening on port %s (foreground=%v, proxy=%v)", port, foreground, proxyMode)
	if foreground {
		log.Fatal(srv.Serve(ln))
	} else {
		go func() { log.Fatal(srv.Serve(ln)) }()
	}
}

func main() {
	ports := flag.String("ports", "8081", "Comma-separated list of ports to listen on")
	service := flag.String("service", "unknown", "Service name")
	proxy := flag.Bool("proxy", false, "Accept PROXY protocol v2 and decode the trace TLV")
	flag.Parse()

	serviceName = *service
	proxyMode = *proxy
	portList := strings.Split(*ports, ",")
	if len(portList) == 0 {
		log.Fatal("No ports specified")
	}

	hostname, _ := os.Hostname()
	log.Printf("Starting HTTP server on %s, listening on ports: %v (proxy=%v)", hostname, portList, proxyMode)

	for i, port := range portList {
		serve(strings.TrimSpace(port), i == len(portList)-1)
	}
}
