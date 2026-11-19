package telemetry

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func validReport() Report {
	return Report{
		TerminalID: "SAT-00042", Timestamp: fixedNow.Add(-time.Second), Seq: 1, LinkState: LinkUp,
		LatencyMs: 38.5, PacketLossPct: 0.1, DownlinkMbps: 120, UplinkMbps: 12, SnrDb: 11, HasSnr: true,
	}
}

func TestValidate(t *testing.T) {
	v := Validator{MaxAge: time.Hour, MaxFuture: time.Minute, Now: func() time.Time { return fixedNow }}

	cases := []struct {
		name   string
		mutate func(*Report)
		want   Reason
	}{
		{"valid", func(*Report) {}, ""},
		{"bad id", func(r *Report) { r.TerminalID = "sat-42" }, ReasonBadTerminalID},
		{"short id", func(r *Report) { r.TerminalID = "SAT-42" }, ReasonBadTerminalID},
		{"unspecified state", func(r *Report) { r.LinkState = LinkUnspecified }, ReasonBadLinkState},
		{"negative latency", func(r *Report) { r.LatencyMs = -1 }, ReasonOutOfRange},
		{"loss over 100", func(r *Report) { r.PacketLossPct = 101 }, ReasonOutOfRange},
		{"NaN throughput", func(r *Report) { r.DownlinkMbps = float32(math.NaN()) }, ReasonOutOfRange},
		{"absurd snr", func(r *Report) { r.SnrDb = 99 }, ReasonOutOfRange},
		{"absurd snr ignored when absent", func(r *Report) { r.SnrDb, r.HasSnr = 99, false }, ""},
		{"too old", func(r *Report) { r.Timestamp = fixedNow.Add(-2 * time.Hour) }, ReasonTooOld},
		{"from future", func(r *Report) { r.Timestamp = fixedNow.Add(5 * time.Minute) }, ReasonFromFuture},
		{"slight clock skew ok", func(r *Report) { r.Timestamp = fixedNow.Add(30 * time.Second) }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validReport()
			tc.mutate(&r)
			got, ok := v.Validate(&r)
			if got != tc.want || ok != (tc.want == "") {
				t.Fatalf("Validate() = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.want == "")
			}
		})
	}
}

func TestParseLinkState(t *testing.T) {
	for in, want := range map[string]LinkState{"UP": LinkUp, "degraded": LinkDegraded, "LINK_STATE_DOWN": LinkDown} {
		if got, ok := ParseLinkState(in); !ok || got != want {
			t.Errorf("ParseLinkState(%q) = %v, %v", in, got, ok)
		}
	}
	if _, ok := ParseLinkState("SIDEWAYS"); ok {
		t.Error("expected unknown state to fail")
	}
}

func TestDecodeJSONFromAgent(t *testing.T) {
	// Exactly what agent/src/report.c emits.
	msg := `{"terminalId":"SAT-00001","timestampMs":1763553600123,"seq":7,"linkState":"DEGRADED","latencyMs":41.26,` +
		`"jitterMs":1.50,"packetLossPct":2.40,"downlinkMbps":88.00,"uplinkMbps":9.10,"snrDb":5.50,"obstructionPct":3.00,` +
		`"firmwareVersion":"agent-c/1.4.0"}`
	r, err := DecodeJSON([]byte(msg))
	if err != nil {
		t.Fatal(err)
	}
	if r.TerminalID != "SAT-00001" || r.Seq != 7 || r.LinkState != LinkDegraded || !r.HasSnr || r.SnrDb != 5.5 {
		t.Fatalf("unexpected decode: %+v", r)
	}
	if r.Timestamp.UnixMilli() != 1763553600123 || r.Transport != TransportWS {
		t.Fatalf("bad timestamp/transport: %v %v", r.Timestamp, r.Transport)
	}

	noSnr, err := DecodeJSON([]byte(`{"terminalId":"SAT-00001","timestampMs":1,"linkState":"UP"}`))
	if err != nil || noSnr.HasSnr {
		t.Fatalf("snr should be absent: %+v %v", noSnr, err)
	}
}

func TestDecodeJSONErrors(t *testing.T) {
	for _, msg := range []string{`not json`, `{"terminalId":"SAT-00001"}`, `[]`} {
		if _, err := DecodeJSON([]byte(msg)); !errors.Is(err, ErrMalformed) {
			t.Errorf("DecodeJSON(%s) err = %v, want ErrMalformed", msg, err)
		}
	}
}

func TestToWireRoundTrip(t *testing.T) {
	r := validReport()
	r.Timestamp = time.UnixMilli(r.Timestamp.UnixMilli()).UTC()
	data, err := json.Marshal(r.ToWire())
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	back.Transport = r.Transport
	if back != r {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", back, r)
	}
}
