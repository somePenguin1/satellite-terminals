package sim

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"satfleet/ingest/internal/telemetry"
)

func TestModelProducesValidReports(t *testing.T) {
	v := telemetry.Validator{MaxAge: time.Hour, MaxFuture: time.Minute}
	m := NewLinkModel(42, 220, 25)
	now := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	v.Now = func() time.Time { return now }
	states := map[telemetry.LinkState]int{}
	silent := 0
	for i := 0; i < 50_000; i++ { // ~70 simulated hours at 5 s
		now = now.Add(5 * time.Second)
		r := telemetry.Report{TerminalID: "SAT-00001", Timestamp: now}
		if !m.Step(5*time.Second, now, &r) {
			silent++
			continue
		}
		if reason, ok := v.Validate(&r); !ok {
			t.Fatalf("invalid report at step %d (%s): %+v", i, reason, r)
		}
		if float64(r.DownlinkMbps) > m.DownCapMbps {
			t.Fatalf("downlink %v exceeds plan cap", r.DownlinkMbps)
		}
		states[r.LinkState]++
	}
	if states[telemetry.LinkUp] < 40_000 {
		t.Errorf("mostly-healthy link expected, got %v", states)
	}
	if states[telemetry.LinkDegraded] == 0 {
		t.Errorf("rain fade should degrade the link sometimes: %v", states)
	}
	t.Logf("states=%v silent=%d", states, silent)
}

func TestModelIsDeterministicPerSeed(t *testing.T) {
	a, b := NewLinkModel(7, 100, 10), NewLinkModel(7, 100, 10)
	now := time.Now()
	for i := 0; i < 100; i++ {
		var ra, rb telemetry.Report
		a.Step(5*time.Second, now, &ra)
		b.Step(5*time.Second, now, &rb)
		if ra != rb {
			t.Fatal("same seed should give the same samples")
		}
	}
}

func TestFleetSyncAndDue(t *testing.T) {
	f := NewFleet()
	now := time.Now()
	specs := SyntheticSpecs(1, 100, 1000)
	specs[0].Status = "provisioned" // not installed yet: must not report
	if added, _ := f.Sync(specs, now); added != 99 {
		t.Fatalf("added %d, want 99", added)
	}

	seen := map[string]int{}
	for step := 0; step < 30; step++ {
		now = now.Add(100 * time.Millisecond)
		for shard := 0; shard < 4; shard++ {
			for _, r := range f.Due(now, shard, 4, nil) {
				seen[r.TerminalID]++
			}
		}
	}
	// 3 seconds at a 1 s interval: each terminal reports 2-3 times (minus simulated outages).
	if len(seen) < 95 {
		t.Fatalf("only %d terminals reported", len(seen))
	}
	if seen["SAT-00001"] != 0 {
		t.Fatal("provisioned terminal reported")
	}
	for id, n := range seen {
		if n > 4 {
			t.Fatalf("%s reported %d times in 3 s", id, n)
		}
	}

	specs[0].Status = "active"
	specs[5].Status = "decommissioned"
	added, removed := f.Sync(specs, now)
	if added != 1 || removed != 1 || f.Size() != 99 {
		t.Fatalf("resync added=%d removed=%d size=%d", added, removed, f.Size())
	}
}

func TestFetchSpecsPaginates(t *testing.T) {
	all := SyntheticSpecs(1, 2500, 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		after := r.URL.Query().Get("afterId")
		var page []Spec
		for _, s := range all {
			if s.TerminalID > after && len(page) < 1000 {
				page = append(page, s)
			}
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	defer srv.Close()
	got, err := FetchSpecs(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2500 || got[2499].TerminalID != fmt.Sprintf("SAT-%05d", 2500) {
		t.Fatalf("got %d specs", len(got))
	}
}

func TestParseRange(t *testing.T) {
	if s, n, err := ParseRange("101-5000"); err != nil || s != 101 || n != 4900 {
		t.Fatalf("ParseRange = %d %d %v", s, n, err)
	}
	if _, _, err := ParseRange("5-1"); err == nil {
		t.Fatal("expected error")
	}
}
