package main

import (
	"net"
	"strings"
	"testing"

	"github.com/pires/go-proxyproto"
)

func TestMintTrace(t *testing.T) {
	tc := MintTrace()
	if tc.TraceID == ([16]byte{}) {
		t.Error("minted trace-id must not be all-zero")
	}
	if tc.SpanID == ([8]byte{}) {
		t.Error("minted span-id must not be all-zero")
	}
	if tc.ParentSpanIDHex() != "" {
		t.Error("minted trace is a root and must have no parent span")
	}
	if tc.Flags != traceFlagSampled {
		t.Errorf("expected sampled flag, got %02x", tc.Flags)
	}
	tp := tc.Traceparent()
	if len(tp) != traceparentLen {
		t.Errorf("traceparent length = %d, want %d (%q)", len(tp), traceparentLen, tp)
	}
	if !traceparentRe.MatchString(tp) {
		t.Errorf("minted traceparent %q does not match grammar", tp)
	}

	// Two mints must differ.
	if MintTrace().TraceIDHex() == tc.TraceIDHex() {
		t.Error("two mints produced the same trace-id")
	}
}

func TestTraceEncoding(t *testing.T) {
	tc, ok := AdoptTraceparent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if !ok {
		t.Fatal("setup: valid traceparent rejected")
	}

	// Default is hex.
	if err := setTraceEncoding("hex"); err != nil {
		t.Fatalf("setTraceEncoding(hex): %v", err)
	}
	defer setTraceEncoding("hex") // restore global for other tests
	hexArgs := tc.logArgs()
	if hexArgs[1] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("hex trace_id = %v, want the 32-hex value", hexArgs[1])
	}

	// base62 is shorter, fixed-width, and deterministic; wire format stays hex.
	if err := setTraceEncoding("base62"); err != nil {
		t.Fatalf("setTraceEncoding(base62): %v", err)
	}
	b62Args := tc.logArgs()
	if got := len(b62Args[1].(string)); got != 22 {
		t.Errorf("base62 trace_id width = %d, want 22", got)
	}
	if got := len(b62Args[3].(string)); got != 11 {
		t.Errorf("base62 span_id width = %d, want 11", got)
	}
	if b62Args[1] != tc.logArgs()[1] {
		t.Error("base62 encoding is not deterministic")
	}
	if !traceparentRe.MatchString(tc.Traceparent()) {
		t.Error("wire traceparent must remain hex regardless of display encoding")
	}

	if err := setTraceEncoding("nope"); err == nil {
		t.Error("expected error for invalid encoding")
	}
}

func TestEncodeBase62FixedWidth(t *testing.T) {
	// Leading-zero bytes must still yield the full fixed width.
	cases := []struct {
		b     []byte
		width int
	}{
		{make([]byte, 16), 22},                 // all-zero trace-id -> "0"*22
		{append([]byte{0x00}, make([]byte, 7)...), 11}, // 8-byte span, leading zero
		{[]byte{0x00, 0x00, 0x00, 0x01}, 6},     // small value keeps width for 4 bytes
	}
	for _, c := range cases {
		got := encodeBase62(c.b)
		if len(got) != c.width {
			t.Errorf("encodeBase62(%x) width = %d (%q), want %d", c.b, len(got), got, c.width)
		}
	}
}

func TestAdoptTraceparent(t *testing.T) {
	valid := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	tc, ok := AdoptTraceparent(valid)
	if !ok {
		t.Fatal("valid traceparent was rejected")
	}
	if tc.TraceIDHex() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace-id = %s, want 4bf92f3577b34da6a3ce929d0e0e4736", tc.TraceIDHex())
	}
	// Inbound span becomes this hop's parent; a fresh span is minted.
	if tc.ParentSpanIDHex() != "00f067aa0ba902b7" {
		t.Errorf("parent_span_id = %s, want 00f067aa0ba902b7", tc.ParentSpanIDHex())
	}
	if tc.SpanID == ([8]byte{}) {
		t.Error("adopted trace must mint a fresh non-zero span")
	}
	if tc.SpanIDHex() == "00f067aa0ba902b7" {
		t.Error("adopted span must differ from inbound parent span")
	}
}

func TestAdoptTraceparentRejects(t *testing.T) {
	cases := map[string]string{
		"empty":              "",
		"too short":          "00-abc-def-01",
		"uppercase":          "00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01",
		"all-zero trace-id":  "00-00000000000000000000000000000000-00f067aa0ba902b7-01",
		"all-zero parent-id": "00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
		"trailing garbage":   "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01x",
		"wrong separators":   "00.4bf92f3577b34da6a3ce929d0e0e4736.00f067aa0ba902b7.01",
	}
	for name, tp := range cases {
		if _, ok := AdoptTraceparent(tp); ok {
			t.Errorf("%s: expected rejection, got adoption", name)
		}
	}
}

func TestTraceTLVRoundTripSubtype1(t *testing.T) {
	tp := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	v := EncodeTraceTLV(tp, "")
	if v[0] != 0x01 {
		t.Errorf("subtype/version byte = %02x, want 01", v[0])
	}
	gotTP, gotTS, ok := DecodeTraceTLV(v)
	if !ok {
		t.Fatal("decode failed for subtype 0x1")
	}
	if gotTP != tp || gotTS != "" {
		t.Errorf("round-trip = (%q, %q), want (%q, \"\")", gotTP, gotTS, tp)
	}
}

func TestTraceTLVRoundTripSubtype2(t *testing.T) {
	tp := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	ts := "vendor=abc123,other=xyz"
	v := EncodeTraceTLV(tp, ts)
	if v[0] != 0x02 {
		t.Errorf("subtype/version byte = %02x, want 02", v[0])
	}
	gotTP, gotTS, ok := DecodeTraceTLV(v)
	if !ok {
		t.Fatal("decode failed for subtype 0x2")
	}
	if gotTP != tp || gotTS != ts {
		t.Errorf("round-trip = (%q, %q), want (%q, %q)", gotTP, gotTS, tp, ts)
	}
}

func TestDecodeTraceTLVRejects(t *testing.T) {
	tp := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	valid := EncodeTraceTLV(tp, "")

	t.Run("empty", func(t *testing.T) {
		if _, _, ok := DecodeTraceTLV(nil); ok {
			t.Error("expected rejection of empty value")
		}
	})
	t.Run("unknown version nibble", func(t *testing.T) {
		bad := append([]byte(nil), valid...)
		bad[0] = 0x11 // version nibble 0x1
		if _, _, ok := DecodeTraceTLV(bad); ok {
			t.Error("expected rejection of unknown version")
		}
	})
	t.Run("unknown subtype", func(t *testing.T) {
		bad := append([]byte(nil), valid...)
		bad[0] = 0x09
		if _, _, ok := DecodeTraceTLV(bad); ok {
			t.Error("expected rejection of unknown subtype")
		}
	})
	t.Run("truncated traceparent", func(t *testing.T) {
		if _, _, ok := DecodeTraceTLV(valid[:len(valid)-1]); ok {
			t.Error("expected rejection of short traceparent")
		}
	})
	t.Run("oversized tracestate", func(t *testing.T) {
		big := strings.Repeat("a", maxTracestateBytes+1)
		if _, _, ok := DecodeTraceTLV(EncodeTraceTLV(tp, big)); ok {
			t.Error("expected rejection of oversized tracestate")
		}
	})
}

// TestTraceFromHeader confirms the TLV round-trips through a real go-proxyproto
// v2 header encode/decode and is adopted only from a v2 header.
func TestTraceFromHeader(t *testing.T) {
	tc := MintTrace()
	hdr := proxyproto.HeaderProxyFromAddrs(2, mustTCPAddr("1.2.3.4:5678"), mustTCPAddr("5.6.7.8:9090"))
	if err := hdr.SetTLVs([]proxyproto.TLV{traceTLV(tc)}); err != nil {
		t.Fatalf("SetTLVs: %v", err)
	}

	got, ok := traceFromHeader(hdr)
	if !ok {
		t.Fatal("expected adoption from v2 header with trace TLV")
	}
	if got.TraceIDHex() != tc.TraceIDHex() {
		t.Errorf("adopted trace-id = %s, want %s", got.TraceIDHex(), tc.TraceIDHex())
	}
	// The originator's span becomes the adopter's parent.
	if got.ParentSpanIDHex() != tc.SpanIDHex() {
		t.Errorf("parent_span_id = %s, want %s", got.ParentSpanIDHex(), tc.SpanIDHex())
	}

	t.Run("v1 header mints fresh", func(t *testing.T) {
		v1 := proxyproto.HeaderProxyFromAddrs(1, mustTCPAddr("1.2.3.4:5678"), mustTCPAddr("5.6.7.8:9090"))
		if _, ok := traceFromHeader(v1); ok {
			t.Error("v1 header must not yield an adopted trace")
		}
	})

	t.Run("no TLV mints fresh", func(t *testing.T) {
		plain := proxyproto.HeaderProxyFromAddrs(2, mustTCPAddr("1.2.3.4:5678"), mustTCPAddr("5.6.7.8:9090"))
		if _, ok := traceFromHeader(plain); ok {
			t.Error("v2 header without trace TLV must not yield an adopted trace")
		}
	})
}

func mustTCPAddr(s string) *net.TCPAddr {
	a, err := net.ResolveTCPAddr("tcp", s)
	if err != nil {
		panic(err)
	}
	return a
}
