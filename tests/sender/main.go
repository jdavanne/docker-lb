// Command sender is an integration-test helper that proves the LB's
// PROXY-protocol RECEIVE (adopt) path. On each HTTP request it dials the
// configured target, prepends a PROXY v2 header carrying a *known* W3C trace
// context in the 0xE1 TLV, forwards a minimal HTTP request, and returns the
// downstream backend's response verbatim. The test then asserts the known
// trace-id survived the LB (i.e. was adopted, not re-minted).
package main

import (
	"bufio"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/pires/go-proxyproto"
)

// pp2TypeTrace must match src/trace.go PP2TypeTrace.
const pp2TypeTrace = 0xE1

// knownTraceparent is the fixed trace context injected into the PROXY v2 TLV.
// Its trace-id must reappear downstream iff the LB adopted the inbound TLV.
const knownTraceparent = "00-11111111111111111111111111111111-2222222222222222-01"

var target string

// encodeTraceTLV builds the subtype 0x1 TLV value: [0x01] + ASCII traceparent.
func encodeTraceTLV(tp string) []byte {
	return append([]byte{0x01}, tp...)
}

func handler(w http.ResponseWriter, r *http.Request) {
	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		http.Error(w, "dial: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer conn.Close()

	src, _ := net.ResolveTCPAddr("tcp", "10.0.0.1:12345")
	dst, _ := net.ResolveTCPAddr("tcp", "10.0.0.2:80")
	hdr := proxyproto.HeaderProxyFromAddrs(2, src, dst)
	if err := hdr.SetTLVs([]proxyproto.TLV{{Type: pp2TypeTrace, Value: encodeTraceTLV(knownTraceparent)}}); err != nil {
		http.Error(w, "settlvs: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := hdr.WriteTo(conn); err != nil {
		http.Error(w, "write header: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Minimal HTTP request forwarded to the downstream backend.
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: sender\r\nConnection: close\r\n\r\n"); err != nil {
		http.Error(w, "write request: "+err.Error(), http.StatusBadGateway)
		return
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), r)
	if err != nil {
		http.Error(w, "read response: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

func main() {
	t := flag.String("target", "lb:10150", "downstream host:port to dial with PROXY v2 + trace TLV")
	addr := flag.String("addr", ":8080", "HTTP listen address")
	flag.Parse()
	target = *t

	log.Printf("sender: listening on %s, target %s, injecting %s", *addr, target, knownTraceparent)
	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
