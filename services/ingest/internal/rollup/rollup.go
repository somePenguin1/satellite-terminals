// Package rollup maintains write-time aggregates (per-minute fleet stats by region and
// per-terminal hourly usage) in memory and periodically flushes them to Postgres as
// additive upserts. Dashboards then read a few thousand rollup rows instead of scanning
// millions of raw samples. Each ingest replica flushes its own partial sums, so the
// totals stay correct when the service is scaled out.
package rollup

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"satfleet/ingest/internal/telemetry"
)

type MinuteKey struct {
	Minute time.Time
	Region string
}

type MinuteAgg struct {
	Samples         int64
	LinkSamples     int64 // samples where the link was not down (latency/loss are meaningful)
	DegradedSamples int64
	SumLatencyMs    float64
	SumPacketLoss   float64
	SumDownlinkMbps float64
	SumUplinkMbps   float64
}

func (a *MinuteAgg) merge(b MinuteAgg) {
	a.Samples += b.Samples
	a.LinkSamples += b.LinkSamples
	a.DegradedSamples += b.DegradedSamples
	a.SumLatencyMs += b.SumLatencyMs
	a.SumPacketLoss += b.SumPacketLoss
	a.SumDownlinkMbps += b.SumDownlinkMbps
	a.SumUplinkMbps += b.SumUplinkMbps
}

type HourKey struct {
	TerminalID string
	Hour       time.Time
}

type HourAgg struct {
	Samples         int64
	SumDownlinkMbps float64
	MaxDownlinkMbps float32
	SumUplinkMbps   float64
}

func (a *HourAgg) merge(b HourAgg) {
	a.Samples += b.Samples
	a.SumDownlinkMbps += b.SumDownlinkMbps
	a.SumUplinkMbps += b.SumUplinkMbps
	if b.MaxDownlinkMbps > a.MaxDownlinkMbps {
		a.MaxDownlinkMbps = b.MaxDownlinkMbps
	}
}

type Flusher interface {
	FlushRollups(ctx context.Context, minutes map[MinuteKey]MinuteAgg, hours map[HourKey]HourAgg) error
}

type Aggregator struct {
	regionOf func(terminalID string) string
	mu       sync.Mutex
	minutes  map[MinuteKey]MinuteAgg
	hours    map[HourKey]HourAgg
}

func NewAggregator(regionOf func(string) string) *Aggregator {
	return &Aggregator{regionOf: regionOf, minutes: map[MinuteKey]MinuteAgg{}, hours: map[HourKey]HourAgg{}}
}

// Add folds a persisted batch into the aggregates.
func (a *Aggregator) Add(batch []telemetry.Report) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range batch {
		r := &batch[i]
		ts := r.Timestamp.UTC()

		mk := MinuteKey{Minute: ts.Truncate(time.Minute), Region: a.regionOf(r.TerminalID)}
		m := a.minutes[mk]
		m.Samples++
		m.SumDownlinkMbps += float64(r.DownlinkMbps)
		m.SumUplinkMbps += float64(r.UplinkMbps)
		if r.LinkState != telemetry.LinkDown {
			m.LinkSamples++
			m.SumLatencyMs += float64(r.LatencyMs)
			m.SumPacketLoss += float64(r.PacketLossPct)
		}
		if r.LinkState != telemetry.LinkUp {
			m.DegradedSamples++
		}
		a.minutes[mk] = m

		hk := HourKey{TerminalID: r.TerminalID, Hour: ts.Truncate(time.Hour)}
		h := a.hours[hk]
		h.Samples++
		h.SumDownlinkMbps += float64(r.DownlinkMbps)
		h.SumUplinkMbps += float64(r.UplinkMbps)
		if r.DownlinkMbps > h.MaxDownlinkMbps {
			h.MaxDownlinkMbps = r.DownlinkMbps
		}
		a.hours[hk] = h
	}
}

// Drain hands back everything aggregated so far and starts fresh.
func (a *Aggregator) Drain() (map[MinuteKey]MinuteAgg, map[HourKey]HourAgg) {
	a.mu.Lock()
	defer a.mu.Unlock()
	m, h := a.minutes, a.hours
	a.minutes, a.hours = map[MinuteKey]MinuteAgg{}, map[HourKey]HourAgg{}
	return m, h
}

// Restore merges drained aggregates back after a failed flush, so nothing is lost.
func (a *Aggregator) Restore(minutes map[MinuteKey]MinuteAgg, hours map[HourKey]HourAgg) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, v := range minutes {
		cur := a.minutes[k]
		cur.merge(v)
		a.minutes[k] = cur
	}
	for k, v := range hours {
		cur := a.hours[k]
		cur.merge(v)
		a.hours[k] = cur
	}
}

// FlushOnce writes the current aggregates; on failure they are restored for the next try.
func (a *Aggregator) FlushOnce(ctx context.Context, f Flusher) error {
	minutes, hours := a.Drain()
	if len(minutes) == 0 && len(hours) == 0 {
		return nil
	}
	if err := f.FlushRollups(ctx, minutes, hours); err != nil {
		a.Restore(minutes, hours)
		return err
	}
	return nil
}

// Run flushes every interval until ctx is cancelled, then performs a final flush.
func (a *Aggregator) Run(ctx context.Context, every time.Duration, f Flusher, log *slog.Logger) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := a.FlushOnce(final, f); err != nil {
				log.Error("final rollup flush failed", "err", err)
			}
			cancel()
			return
		case <-t.C:
			fctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := a.FlushOnce(fctx, f); err != nil {
				log.Warn("rollup flush failed; will retry", "err", err)
			}
			cancel()
		}
	}
}
