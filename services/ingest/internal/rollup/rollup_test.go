package rollup

import (
	"context"
	"errors"
	"testing"
	"time"

	"satfleet/ingest/internal/telemetry"
)

var t0 = time.Date(2026, 11, 19, 10, 15, 0, 0, time.UTC)

func regions(id string) string {
	if id == "SAT-00001" {
		return "Europe"
	}
	return "Maritime"
}

func sample(id string, at time.Time, state telemetry.LinkState, latency, down float32) telemetry.Report {
	return telemetry.Report{TerminalID: id, Timestamp: at, LinkState: state, LatencyMs: latency, DownlinkMbps: down, PacketLossPct: 1}
}

func TestAggregatesByMinuteRegionAndHour(t *testing.T) {
	a := NewAggregator(regions)
	a.Add([]telemetry.Report{
		sample("SAT-00001", t0.Add(5*time.Second), telemetry.LinkUp, 40, 100),
		sample("SAT-00001", t0.Add(30*time.Second), telemetry.LinkDegraded, 60, 50),
		sample("SAT-00001", t0.Add(70*time.Second), telemetry.LinkDown, 0, 0), // next minute
		sample("SAT-00002", t0.Add(10*time.Second), telemetry.LinkUp, 30, 200),
	})
	minutes, hours := a.Drain()

	eu := minutes[MinuteKey{t0, "Europe"}]
	if eu.Samples != 2 || eu.LinkSamples != 2 || eu.DegradedSamples != 1 || eu.SumLatencyMs != 100 || eu.SumDownlinkMbps != 150 {
		t.Fatalf("europe minute = %+v", eu)
	}
	next := minutes[MinuteKey{t0.Add(time.Minute), "Europe"}]
	if next.Samples != 1 || next.LinkSamples != 0 || next.SumLatencyMs != 0 || next.DegradedSamples != 1 {
		t.Fatalf("down samples must not count toward latency: %+v", next)
	}
	if minutes[MinuteKey{t0, "Maritime"}].Samples != 1 {
		t.Fatalf("maritime minute missing: %+v", minutes)
	}

	h := hours[HourKey{"SAT-00001", t0.Truncate(time.Hour)}]
	if h.Samples != 3 || h.SumDownlinkMbps != 150 || h.MaxDownlinkMbps != 100 {
		t.Fatalf("hour = %+v", h)
	}
	if m, hh := a.Drain(); len(m) != 0 || len(hh) != 0 {
		t.Fatal("drain should reset state")
	}
}

type flakyFlusher struct {
	fail     bool
	received []map[MinuteKey]MinuteAgg
}

func (f *flakyFlusher) FlushRollups(_ context.Context, m map[MinuteKey]MinuteAgg, _ map[HourKey]HourAgg) error {
	if f.fail {
		return errors.New("db down")
	}
	f.received = append(f.received, m)
	return nil
}

func TestFailedFlushIsRetriedWithoutLoss(t *testing.T) {
	a := NewAggregator(regions)
	f := &flakyFlusher{fail: true}
	a.Add([]telemetry.Report{sample("SAT-00001", t0, telemetry.LinkUp, 40, 100)})
	if err := a.FlushOnce(context.Background(), f); err == nil {
		t.Fatal("expected error")
	}
	a.Add([]telemetry.Report{sample("SAT-00001", t0.Add(time.Second), telemetry.LinkUp, 20, 50)})
	f.fail = false
	if err := a.FlushOnce(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	got := f.received[0][MinuteKey{t0, "Europe"}]
	if got.Samples != 2 || got.SumLatencyMs != 60 {
		t.Fatalf("restored aggregate = %+v, want 2 samples / 60 ms", got)
	}
}
