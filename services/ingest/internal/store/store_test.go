package store

import (
	"testing"
	"time"

	"satfleet/ingest/internal/telemetry"
)

func TestLatestPerTerminal(t *testing.T) {
	t0 := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	batch := []telemetry.Report{
		{TerminalID: "SAT-00002", Timestamp: t0, Seq: 1},
		{TerminalID: "SAT-00001", Timestamp: t0.Add(time.Second), Seq: 5},
		{TerminalID: "SAT-00001", Timestamp: t0, Seq: 9}, // older replayed sample
		{TerminalID: "SAT-00002", Timestamp: t0, Seq: 2}, // same instant, higher seq wins
	}
	got := LatestPerTerminal(batch)
	if len(got) != 2 {
		t.Fatalf("got %d terminals, want 2", len(got))
	}
	if got[0].TerminalID != "SAT-00001" || got[0].Seq != 5 {
		t.Errorf("first = %+v, want SAT-00001 seq 5", *got[0])
	}
	if got[1].TerminalID != "SAT-00002" || got[1].Seq != 2 {
		t.Errorf("second = %+v, want SAT-00002 seq 2", *got[1])
	}
}
