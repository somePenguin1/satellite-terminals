package metrics

import (
	"math"
	"strings"
	"sync"
	"testing"
)

func TestExposition(t *testing.T) {
	var r Registry
	c := r.Counter("ingest_reports_accepted_total", "Accepted reports.")
	byReason := r.CounterVec("ingest_reports_rejected_total", "Rejected reports.", "reason", []string{"too_old", "unknown_terminal"})
	g := r.Gauge("ingest_queue_depth", "Queued reports.")
	h := r.Histogram("ingest_batch_write_seconds", "Batch write latency.", []float64{0.01, 0.1, 1})

	c.Add(5)
	byReason["unknown_terminal"].Inc()
	g.Set(12)
	h.Observe(0.005)
	h.Observe(0.05)
	h.Observe(3)

	var sb strings.Builder
	r.WritePrometheus(&sb)
	out := sb.String()
	for _, want := range []string{
		"# TYPE ingest_reports_accepted_total counter\ningest_reports_accepted_total 5\n",
		`ingest_reports_rejected_total{reason="unknown_terminal"} 1`,
		`ingest_reports_rejected_total{reason="too_old"} 0`,
		"ingest_queue_depth 12\n",
		`ingest_batch_write_seconds_bucket{le="0.1"} 2`,
		`ingest_batch_write_seconds_bucket{le="+Inf"} 3`,
		"ingest_batch_write_seconds_count 3\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exposition missing %q\n%s", want, out)
		}
	}
}

func TestConcurrentCounterAndGauge(t *testing.T) {
	var c Counter
	var g Gauge
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10000; j++ {
				c.Inc()
				g.Add(0.5)
			}
		}()
	}
	wg.Wait()
	if c.Value() != 80000 || g.Value() != 40000 {
		t.Fatalf("got counter %d gauge %g", c.Value(), g.Value())
	}
}

func TestQuantileBetweenSnapshots(t *testing.T) {
	h := NewHistogram([]float64{1, 2, 4, 8})
	h.Observe(100) // before the window: must not count
	prev := h.Snapshot()
	for i := 0; i < 99; i++ {
		h.Observe(1.5) // bucket (1,2]
	}
	h.Observe(7) // bucket (4,8]
	now := h.Snapshot()

	if p50 := now.Quantile(prev, 0.5); p50 < 1 || p50 > 2 {
		t.Errorf("p50 = %g, want within (1,2]", p50)
	}
	if p100 := now.Quantile(prev, 1.0); p100 <= 4 || p100 > 8 {
		t.Errorf("p100 = %g, want within (4,8]", p100)
	}
	if q := now.Quantile(now, 0.99); q != 0 {
		t.Errorf("empty window quantile = %g, want 0", q)
	}
	if math.IsNaN(now.Quantile(prev, 0.99)) {
		t.Error("NaN quantile")
	}
}
