package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"regexp"

	"github.com/pires/go-proxyproto"
)

// PP2TypeTrace is the PROXY protocol v2 TLV type that carries W3C trace context
// from a trusted edge LB to the terminating server.
//
// 0xE1 is within the application-custom range (PP2_TYPE_MIN_CUSTOM..MAX_CUSTOM,
// 0xE0-0xEF), so PP2Type(0xE1).App() is true and no peer mistakes it for a
// spec-registered type. The value is NOT globally registered: it is a
// coordinated contract between the emitting LB and the receiving server (see
// RFC-01 "Carrying W3C Trace Context over the PROXY Protocol").
const PP2TypeTrace proxyproto.PP2Type = 0xE1

const (
	// traceTLVVersion0 is the high nibble of the TLV subtype/version byte for
	// the format defined by this RFC. A decoder that sees any other version
	// treats the whole TLV as absent (mint fresh) — forward-compatible.
	traceTLVVersion0 = 0x00

	// traceSubtypeTP carries traceparent only; traceSubtypeTPTS also carries
	// tracestate as a length-prefixed second sub-field.
	traceSubtypeTP   = 0x01
	traceSubtypeTPTS = 0x02

	// maxTracestateBytes is the hard cap enforced on decode (W3C size guidance).
	maxTracestateBytes = 512

	// traceparentLen is the byte length of a W3C traceparent for version 00.
	traceparentLen = 55

	// traceFlagSampled marks the trace as sampled.
	traceFlagSampled = 0x01
)

// traceparentRe validates a W3C traceparent value (any version): two hex version
// digits, a 32-hex trace-id, a 16-hex parent-id, and two hex flag digits, all
// lowercase. The non-zero trace-id/parent-id rule is checked separately.
var traceparentRe = regexp.MustCompile(`^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)

// TraceContext holds a W3C trace context for a connection or request.
type TraceContext struct {
	TraceID      [16]byte
	SpanID       [8]byte
	ParentSpanID [8]byte // zero when this hop is the trace root
	Flags        byte
}

// MintTrace generates a brand-new trace context: this hop is the trace root, so
// ParentSpanID stays zero.
func MintTrace() TraceContext {
	var tc TraceContext
	rand.Read(tc.TraceID[:])
	rand.Read(tc.SpanID[:])
	tc.Flags = traceFlagSampled
	return tc
}

// TraceIDHex returns the 32-hex-digit trace-id.
func (tc TraceContext) TraceIDHex() string { return hex.EncodeToString(tc.TraceID[:]) }

// SpanIDHex returns the 16-hex-digit span-id.
func (tc TraceContext) SpanIDHex() string { return hex.EncodeToString(tc.SpanID[:]) }

// ParentSpanIDHex returns the 16-hex-digit parent span-id, or "" when this hop
// is the trace root.
func (tc TraceContext) ParentSpanIDHex() string {
	if tc.ParentSpanID == ([8]byte{}) {
		return ""
	}
	return hex.EncodeToString(tc.ParentSpanID[:])
}

// Traceparent formats the context as a W3C traceparent header value (version 00).
func (tc TraceContext) Traceparent() string {
	return fmt.Sprintf("00-%s-%s-%02x", tc.TraceIDHex(), tc.SpanIDHex(), tc.Flags)
}

// traceEncode renders raw trace/span id bytes for log display. It defaults to
// hex and is switched to base62 via --trace-encoding. Wire formats (the
// traceparent string and the PROXY TLV) always use hex regardless of this
// setting.
var traceEncode = hex.EncodeToString

// encodeBase62 renders id bytes as a base62 string by interpreting them as a
// big-endian integer. This is shorter than hex (a 16-byte trace-id is <= 22
// chars vs 32; an 8-byte span-id <= 11 vs 16) and is display-only.
func encodeBase62(b []byte) string {
	return new(big.Int).SetBytes(b).Text(62)
}

// setTraceEncoding selects the log display encoding for trace/span ids.
func setTraceEncoding(mode string) error {
	switch mode {
	case "hex":
		traceEncode = hex.EncodeToString
	case "base62":
		traceEncode = encodeBase62
	default:
		return fmt.Errorf("invalid --trace-encoding %q: expected hex or base62", mode)
	}
	return nil
}

// logArgs returns slog key/value pairs for the trace context, including
// parent_span_id only when this hop adopted an upstream trace. Ids are rendered
// with the configured display encoding (see traceEncode).
func (tc TraceContext) logArgs() []any {
	args := []any{"trace_id", traceEncode(tc.TraceID[:]), "span_id", traceEncode(tc.SpanID[:])}
	if tc.ParentSpanID != ([8]byte{}) {
		args = append(args, "parent_span_id", traceEncode(tc.ParentSpanID[:]))
	}
	return args
}

// AdoptTraceparent parses an inbound W3C traceparent value. On success it returns
// a new context that keeps the upstream trace-id, records the upstream span-id as
// this hop's parent, and mints a fresh span-id. On any validation failure it
// returns ok=false and the caller should mint a fresh trace.
func AdoptTraceparent(traceparent string) (TraceContext, bool) {
	if len(traceparent) != traceparentLen || !traceparentRe.MatchString(traceparent) {
		return TraceContext{}, false
	}

	traceID, err := hex.DecodeString(traceparent[3:35])
	if err != nil {
		return TraceContext{}, false
	}
	parentID, err := hex.DecodeString(traceparent[36:52])
	if err != nil {
		return TraceContext{}, false
	}
	flags, err := hex.DecodeString(traceparent[53:55])
	if err != nil {
		return TraceContext{}, false
	}

	var tc TraceContext
	copy(tc.TraceID[:], traceID)
	copy(tc.ParentSpanID[:], parentID)
	tc.Flags = flags[0]

	// Reject all-zero trace-id or parent-id per the W3C grammar.
	if tc.TraceID == ([16]byte{}) || tc.ParentSpanID == ([8]byte{}) {
		return TraceContext{}, false
	}

	rand.Read(tc.SpanID[:]) // fresh span for this hop
	return tc, true
}

// EncodeTraceTLV builds the PROXY v2 TLV value for a trace context. With an empty
// tracestate it emits subtype 0x1 ([0x01] + traceparent); otherwise subtype 0x2
// with length-prefixed traceparent and tracestate sub-fields.
func EncodeTraceTLV(traceparent, tracestate string) []byte {
	if tracestate == "" {
		v := make([]byte, 0, 1+len(traceparent))
		v = append(v, byte(traceTLVVersion0<<4|traceSubtypeTP))
		return append(v, traceparent...)
	}

	v := make([]byte, 0, 1+2+len(traceparent)+2+len(tracestate))
	v = append(v, byte(traceTLVVersion0<<4|traceSubtypeTPTS))
	v = binary.BigEndian.AppendUint16(v, uint16(len(traceparent)))
	v = append(v, traceparent...)
	v = binary.BigEndian.AppendUint16(v, uint16(len(tracestate)))
	v = append(v, tracestate...)
	return v
}

// DecodeTraceTLV parses a PROXY v2 trace-context TLV value. It validates the
// subtype/version nibble, the traceparent grammar (via AdoptTraceparent's rules
// applied by the caller), and the size caps. On any failure it returns ok=false
// so the caller mints a fresh trace.
func DecodeTraceTLV(v []byte) (traceparent, tracestate string, ok bool) {
	if len(v) < 1 {
		return "", "", false
	}
	version := (v[0] >> 4) & 0x0f
	subtype := v[0] & 0x0f
	if version != traceTLVVersion0 {
		return "", "", false // unknown format version → treat as absent
	}
	body := v[1:]

	switch subtype {
	case traceSubtypeTP:
		if len(body) != traceparentLen {
			return "", "", false
		}
		return string(body), "", true

	case traceSubtypeTPTS:
		if len(body) < 2 {
			return "", "", false
		}
		tpLen := int(binary.BigEndian.Uint16(body[:2]))
		body = body[2:]
		if tpLen != traceparentLen || len(body) < tpLen+2 {
			return "", "", false
		}
		tp := string(body[:tpLen])
		body = body[tpLen:]
		tsLen := int(binary.BigEndian.Uint16(body[:2]))
		body = body[2:]
		if tsLen > maxTracestateBytes || len(body) != tsLen {
			return "", "", false
		}
		return tp, string(body[:tsLen]), true

	default:
		return "", "", false
	}
}

// traceTLV builds the go-proxyproto TLV record for a trace context.
func traceTLV(tc TraceContext) proxyproto.TLV {
	return proxyproto.TLV{Type: PP2TypeTrace, Value: EncodeTraceTLV(tc.Traceparent(), "")}
}

// traceFromHeader extracts and adopts a trace context from an accepted PROXY v2
// header. It returns ok=false unless the header is v2 and carries a valid
// trace-context TLV.
func traceFromHeader(h *proxyproto.Header) (TraceContext, bool) {
	if h == nil || h.Version != 2 {
		return TraceContext{}, false
	}
	tlvs, err := h.TLVs()
	if err != nil {
		return TraceContext{}, false
	}
	for _, t := range tlvs {
		if t.Type != PP2TypeTrace {
			continue
		}
		tp, _, ok := DecodeTraceTLV(t.Value)
		if !ok {
			return TraceContext{}, false
		}
		return AdoptTraceparent(tp)
	}
	return TraceContext{}, false
}
