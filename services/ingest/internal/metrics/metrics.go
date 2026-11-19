// Package metrics is a small, dependency-free Prometheus text-format exporter.
// The ingest hot path only touches atomics, so recording a sample costs a few nanoseconds.
package metrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

type Counter struct{ v atomic.Uint64 }

func (c *Counter) Inc()          { c.v.Add(1) }
func (c *Counter) Add(n uint64)  { c.v.Add(n) }
func (c *Counter) Value() uint64 { return c.v.Load() }

type Gauge struct{ bits atomic.Uint64 }

func (g *Gauge) Set(v float64) { g.bits.Store(math.Float64bits(v)) }
func (g *Gauge) Add(d float64) {
	for {
		old := g.bits.Load()
		if g.bits.CompareAndSwap(old, math.Float64bits(math.Float64frombits(old)+d)) {
			return
		}
	}
}
func (g *Gauge) Value() float64 { return math.Float64frombits(g.bits.Load()) }

// Histogram with fixed upper bounds (seconds). Counts are cumulative only at export time.
type Histogram struct {
	bounds []float64
	counts []atomic.Uint64 // len(bounds)+1, last is +Inf
	sum    Gauge
	total  atomic.Uint64
}

func NewHistogram(bounds []float64) *Histogram {
	b := append([]float64(nil), bounds...)
	sort.Float64s(b)
	return &Histogram{bounds: b, counts: make([]atomic.Uint64, len(b)+1)}
}

func (h *Histogram) Observe(v float64) {
	i := sort.SearchFloat64s(h.bounds, v)
	h.counts[i].Add(1)
	h.sum.Add(v)
	h.total.Add(1)
}

// Snapshot captures per-bucket (non-cumulative) counts.
type Snapshot struct {
	Bounds []float64
	Counts []uint64
}

func (h *Histogram) Snapshot() Snapshot {
	s := Snapshot{Bounds: h.bounds, Counts: make([]uint64, len(h.counts))}
	for i := range h.counts {
		s.Counts[i] = h.counts[i].Load()
	}
	return s
}

// Quantile estimates the q-quantile of observations made between prev and s, interpolating
// linearly within the bucket. Returns 0 when nothing was observed.
func (s Snapshot) Quantile(prev Snapshot, q float64) float64 {
	var total uint64
	delta := make([]uint64, len(s.Counts))
	for i := range s.Counts {
		var p uint64
		if i < len(prev.Counts) {
			p = prev.Counts[i]
		}
		delta[i] = s.Counts[i] - p
		total += delta[i]
	}
	if total == 0 {
		return 0
	}
	rank := q * float64(total)
	var seen float64
	for i, c := range delta {
		if seen+float64(c) >= rank {
			lo := 0.0
			if i > 0 {
				lo = s.Bounds[i-1]
			}
			if i >= len(s.Bounds) {
				return lo // +Inf bucket: best we can say is "at least the top bound"
			}
			hi := s.Bounds[i]
			if c == 0 {
				return hi
			}
			return lo + (hi-lo)*(rank-seen)/float64(c)
		}
		seen += float64(c)
	}
	return s.Bounds[len(s.Bounds)-1]
}

type family struct {
	name, help, kind string
	write            func(w io.Writer, name string)
}

// Registry renders registered metrics in Prometheus text exposition format.
type Registry struct {
	mu       sync.Mutex
	families []family
}

func labelString(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf(`%s=%q`, k, labels[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func (r *Registry) add(f family) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.families = append(r.families, f)
}

// Counter registers an unlabelled counter.
func (r *Registry) Counter(name, help string) *Counter {
	c := &Counter{}
	r.add(family{name, help, "counter", func(w io.Writer, n string) { fmt.Fprintf(w, "%s %d\n", n, c.Value()) }})
	return c
}

// CounterVec registers a counter family with one label and a fixed set of label values.
func (r *Registry) CounterVec(name, help, label string, values []string) map[string]*Counter {
	m := make(map[string]*Counter, len(values))
	for _, v := range values {
		m[v] = &Counter{}
	}
	r.add(family{name, help, "counter", func(w io.Writer, n string) {
		for _, v := range values {
			fmt.Fprintf(w, "%s%s %d\n", n, labelString(map[string]string{label: v}), m[v].Value())
		}
	}})
	return m
}

func (r *Registry) Gauge(name, help string) *Gauge {
	g := &Gauge{}
	r.add(family{name, help, "gauge", func(w io.Writer, n string) { fmt.Fprintf(w, "%s %g\n", n, g.Value()) }})
	return g
}

// GaugeFunc registers a gauge computed at scrape time.
func (r *Registry) GaugeFunc(name, help string, fn func() float64) {
	r.add(family{name, help, "gauge", func(w io.Writer, n string) { fmt.Fprintf(w, "%s %g\n", n, fn()) }})
}

func (r *Registry) Histogram(name, help string, bounds []float64) *Histogram {
	h := NewHistogram(bounds)
	r.add(family{name, help, "histogram", func(w io.Writer, n string) {
		var cum uint64
		for i, b := range h.bounds {
			cum += h.counts[i].Load()
			fmt.Fprintf(w, "%s_bucket{le=\"%g\"} %d\n", n, b, cum)
		}
		cum += h.counts[len(h.bounds)].Load()
		fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", n, cum)
		fmt.Fprintf(w, "%s_sum %g\n%s_count %d\n", n, h.sum.Value(), n, h.total.Load())
	}})
	return h
}

func (r *Registry) WritePrometheus(w io.Writer) {
	r.mu.Lock()
	fams := append([]family(nil), r.families...)
	r.mu.Unlock()
	for _, f := range fams {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", f.name, f.help, f.name, f.kind)
		f.write(w, f.name)
	}
}
