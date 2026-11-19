package sim

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"satfleet/ingest/internal/telemetry"
)

// Spec is a terminal's provisioning record, as served by the fleet API's bulk config export.
type Spec struct {
	TerminalID       string  `json:"terminalId"`
	Status           string  `json:"status"`
	ReportIntervalMs int     `json:"reportIntervalMs"`
	DownlinkCapMbps  float64 `json:"downlinkCapMbps"`
	UplinkCapMbps    float64 `json:"uplinkCapMbps"`
	Longitude        float64 `json:"longitude"`
}

// Reporting reports whether a terminal in this lifecycle state is installed and powered.
func (s Spec) Reporting() bool { return s.Status == "active" || s.Status == "suspended" }

type terminal struct {
	spec  Spec
	model *LinkModel
	next  time.Time
	last  time.Time
	seq   uint64
}

// Fleet holds simulated terminals. Workers call Due with their shard to collect reports.
type Fleet struct {
	mu    sync.Mutex
	terms map[string]*terminal
}

func NewFleet() *Fleet { return &Fleet{terms: map[string]*terminal{}} }

func seedFor(id string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64()
}

// Sync reconciles the fleet with the latest specs: new terminals start reporting at a random
// offset within their interval (no thundering herd), removed or deactivated ones stop, and
// changed intervals or plan caps apply from the next report.
func (f *Fleet) Sync(specs []Spec, now time.Time) (added, removed int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := make(map[string]Spec, len(specs))
	for _, s := range specs {
		if s.Reporting() {
			want[s.TerminalID] = s
		}
	}
	for id := range f.terms {
		if _, ok := want[id]; !ok {
			delete(f.terms, id)
			removed++
		}
	}
	for id, s := range want {
		if s.ReportIntervalMs <= 0 {
			s.ReportIntervalMs = 5000
		}
		t, ok := f.terms[id]
		if !ok {
			m := NewLinkModel(seedFor(id), s.DownlinkCapMbps, s.UplinkCapMbps)
			m.LongitudeHours = s.Longitude / 15
			offset := time.Duration(seedFor(id)%uint64(s.ReportIntervalMs)) * time.Millisecond
			f.terms[id] = &terminal{spec: s, model: m, next: now.Add(offset), last: now}
			added++
			continue
		}
		t.spec = s
		t.model.DownCapMbps, t.model.UpCapMbps = s.DownlinkCapMbps, s.UplinkCapMbps
	}
	return added, removed
}

func (f *Fleet) Size() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.terms)
}

// Due appends reports for this shard's terminals whose next report time has passed.
func (f *Fleet) Due(now time.Time, shard, shards int, out []telemetry.Report) []telemetry.Report {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, t := range f.terms {
		if t.next.After(now) || int(seedFor(id)%uint64(shards)) != shard {
			continue
		}
		interval := time.Duration(t.spec.ReportIntervalMs) * time.Millisecond
		dt := now.Sub(t.last)
		t.last = now
		t.next = t.next.Add(interval)
		if t.next.Before(now) { // fell behind (e.g. stream reconnect): don't burst
			t.next = now.Add(interval)
		}
		r := telemetry.Report{TerminalID: id, Timestamp: now, FirmwareVersion: "sim/2.3.1"}
		if !t.model.Step(dt, now, &r) {
			continue // silent: simulated outage
		}
		t.seq++
		r.Seq = t.seq
		out = append(out, r)
	}
	return out
}

// SyntheticSpecs builds n active terminals SAT-<start>..SAT-<start+n-1> for use without the fleet API.
func SyntheticSpecs(start, n, intervalMs int) []Spec {
	specs := make([]Spec, n)
	for i := range specs {
		specs[i] = Spec{
			TerminalID: fmt.Sprintf("SAT-%05d", start+i), Status: "active", ReportIntervalMs: intervalMs,
			DownlinkCapMbps: 220, UplinkCapMbps: 25, Longitude: float64((start+i)*37%360 - 180),
		}
	}
	return specs
}

// FetchSpecs pages through GET {api}/api/v1/terminal-configs (keyset pagination on terminal ID).
func FetchSpecs(ctx context.Context, client *http.Client, api string) ([]Spec, error) {
	var all []Spec
	after := ""
	for {
		u := fmt.Sprintf("%s/api/v1/terminal-configs?limit=1000&afterId=%s", api, url.QueryEscape(after))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		var page []Spec
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
		}
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode terminal configs: %w", err)
		}
		all = append(all, page...)
		if len(page) < 1000 {
			sort.Slice(all, func(i, j int) bool { return all[i].TerminalID < all[j].TerminalID })
			return all, nil
		}
		after = page[len(page)-1].TerminalID
	}
}

// ParseRange parses "1-5000" into start and count.
func ParseRange(s string) (start, count int, err error) {
	var a, b int
	if _, err := fmt.Sscanf(s, "%d-%d", &a, &b); err != nil || a < 1 || b < a {
		return 0, 0, fmt.Errorf("range must look like 101-5000, got %q", s)
	}
	return a, b - a + 1, nil
}
