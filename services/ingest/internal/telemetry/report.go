// Package telemetry holds the transport-neutral telemetry model shared by the
// gRPC and WebSocket front ends, the write pipeline and the live feed.
package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// LinkState mirrors fleet.telemetry.v1.LinkState and the smallint stored in Postgres.
type LinkState int16

const (
	LinkUnspecified LinkState = 0
	LinkUp          LinkState = 1
	LinkDegraded    LinkState = 2
	LinkDown        LinkState = 3
)

func (s LinkState) String() string {
	switch s {
	case LinkUp:
		return "UP"
	case LinkDegraded:
		return "DEGRADED"
	case LinkDown:
		return "DOWN"
	default:
		return "UNSPECIFIED"
	}
}

// ParseLinkState accepts "UP", "up", "LINK_STATE_UP" and so on.
func ParseLinkState(s string) (LinkState, bool) {
	switch strings.TrimPrefix(strings.ToUpper(s), "LINK_STATE_") {
	case "UP":
		return LinkUp, true
	case "DEGRADED":
		return LinkDegraded, true
	case "DOWN":
		return LinkDown, true
	default:
		return LinkUnspecified, false
	}
}

// Transport records how a report reached us; it's stored on terminal_status for support triage.
type Transport string

const (
	TransportGRPC Transport = "grpc"
	TransportWS   Transport = "ws"
)

// Report is one validated-or-not telemetry sample.
type Report struct {
	TerminalID      string
	Timestamp       time.Time
	Seq             uint64
	LinkState       LinkState
	LatencyMs       float32
	JitterMs        float32
	PacketLossPct   float32
	DownlinkMbps    float32
	UplinkMbps      float32
	SnrDb           float32
	HasSnr          bool // false when the terminal's modem doesn't expose SNR
	ObstructionPct  float32
	FirmwareVersion string
	Transport       Transport
	ReceivedAt      time.Time
}

// Reason explains why a report was rejected. Values are used as Prometheus label values.
type Reason string

const (
	ReasonBadTerminalID   Reason = "bad_terminal_id"
	ReasonUnknownTerminal Reason = "unknown_terminal"
	ReasonBadLinkState    Reason = "bad_link_state"
	ReasonOutOfRange      Reason = "out_of_range"
	ReasonTooOld          Reason = "too_old"
	ReasonFromFuture      Reason = "from_future"
	ReasonMalformed       Reason = "malformed"
	ReasonOverloaded      Reason = "overloaded"
)

// AllReasons lists every rejection reason, so metrics can pre-register label values.
var AllReasons = []Reason{
	ReasonBadTerminalID, ReasonUnknownTerminal, ReasonBadLinkState, ReasonOutOfRange,
	ReasonTooOld, ReasonFromFuture, ReasonMalformed, ReasonOverloaded,
}

var terminalIDPattern = regexp.MustCompile(`^SAT-[0-9]{5,}$`)

// Validator checks a report's shape and plausibility. Clocks on terminals drift and store-and-forward
// replays old samples, so the accepted window is generous in the past and tight in the future.
type Validator struct {
	MaxAge    time.Duration
	MaxFuture time.Duration
	Now       func() time.Time
}

func DefaultValidator() Validator {
	return Validator{MaxAge: 24 * time.Hour, MaxFuture: 2 * time.Minute, Now: time.Now}
}

func inRange(v, lo, hi float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0) && v >= lo && v <= hi
}

// Validate returns ("", true) for an acceptable report.
func (v Validator) Validate(r *Report) (Reason, bool) {
	if !terminalIDPattern.MatchString(r.TerminalID) {
		return ReasonBadTerminalID, false
	}
	if r.LinkState < LinkUp || r.LinkState > LinkDown {
		return ReasonBadLinkState, false
	}
	if !inRange(r.LatencyMs, 0, 10_000) || !inRange(r.JitterMs, 0, 10_000) ||
		!inRange(r.PacketLossPct, 0, 100) || !inRange(r.ObstructionPct, 0, 100) ||
		!inRange(r.DownlinkMbps, 0, 20_000) || !inRange(r.UplinkMbps, 0, 20_000) ||
		(r.HasSnr && !inRange(r.SnrDb, -30, 60)) {
		return ReasonOutOfRange, false
	}
	now := v.Now()
	if r.Timestamp.Before(now.Add(-v.MaxAge)) {
		return ReasonTooOld, false
	}
	if r.Timestamp.After(now.Add(v.MaxFuture)) {
		return ReasonFromFuture, false
	}
	return "", true
}

// Wire is the JSON shape used by WebSocket agents and by the browser live feed.
type Wire struct {
	TerminalID      string   `json:"terminalId"`
	TimestampMs     int64    `json:"timestampMs"`
	Seq             uint64   `json:"seq"`
	LinkState       string   `json:"linkState"`
	LatencyMs       float32  `json:"latencyMs"`
	JitterMs        float32  `json:"jitterMs"`
	PacketLossPct   float32  `json:"packetLossPct"`
	DownlinkMbps    float32  `json:"downlinkMbps"`
	UplinkMbps      float32  `json:"uplinkMbps"`
	SnrDb           *float32 `json:"snrDb,omitempty"`
	ObstructionPct  float32  `json:"obstructionPct"`
	FirmwareVersion string   `json:"firmwareVersion,omitempty"`
}

var ErrMalformed = errors.New("malformed telemetry message")

// DecodeJSON parses one WebSocket telemetry message.
func DecodeJSON(data []byte) (Report, error) {
	var w Wire
	if err := json.Unmarshal(data, &w); err != nil {
		return Report{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if w.TerminalID == "" || w.TimestampMs == 0 {
		return Report{}, fmt.Errorf("%w: terminalId and timestampMs are required", ErrMalformed)
	}
	state, _ := ParseLinkState(w.LinkState) // invalid states are rejected by the validator
	r := Report{
		TerminalID:      w.TerminalID,
		Timestamp:       time.UnixMilli(w.TimestampMs).UTC(),
		Seq:             w.Seq,
		LinkState:       state,
		LatencyMs:       w.LatencyMs,
		JitterMs:        w.JitterMs,
		PacketLossPct:   w.PacketLossPct,
		DownlinkMbps:    w.DownlinkMbps,
		UplinkMbps:      w.UplinkMbps,
		ObstructionPct:  w.ObstructionPct,
		FirmwareVersion: w.FirmwareVersion,
		Transport:       TransportWS,
	}
	if w.SnrDb != nil {
		r.SnrDb, r.HasSnr = *w.SnrDb, true
	}
	return r, nil
}

// ToWire converts a report for the browser live feed.
func (r Report) ToWire() Wire {
	w := Wire{
		TerminalID:      r.TerminalID,
		TimestampMs:     r.Timestamp.UnixMilli(),
		Seq:             r.Seq,
		LinkState:       r.LinkState.String(),
		LatencyMs:       r.LatencyMs,
		JitterMs:        r.JitterMs,
		PacketLossPct:   r.PacketLossPct,
		DownlinkMbps:    r.DownlinkMbps,
		UplinkMbps:      r.UplinkMbps,
		ObstructionPct:  r.ObstructionPct,
		FirmwareVersion: r.FirmwareVersion,
	}
	if r.HasSnr {
		snr := r.SnrDb
		w.SnrDb = &snr
	}
	return w
}
