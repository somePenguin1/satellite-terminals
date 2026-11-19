// Package ingest ties the transport-neutral pieces together: validation, the terminal
// registry, the write pipeline, rollups, the live feed and metrics. The gRPC and
// WebSocket front ends both call Service.Accept.
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"satfleet/ingest/internal/live"
	"satfleet/ingest/internal/metrics"
	"satfleet/ingest/internal/pipeline"
	"satfleet/ingest/internal/registry"
	"satfleet/ingest/internal/rollup"
	"satfleet/ingest/internal/telemetry"
)

type Metrics struct {
	Registry     *metrics.Registry
	Accepted     map[string]*metrics.Counter // by transport
	Rejected     map[string]*metrics.Counter // by reason
	Persisted    *metrics.Counter
	WriteFailed  *metrics.Counter
	Batches      *metrics.Counter
	BatchSeconds *metrics.Histogram
	GRPCStreams  *metrics.Gauge
	WSDevices    *metrics.Gauge
	LiveClients  *metrics.Gauge
}

func NewMetrics() *Metrics {
	r := &metrics.Registry{}
	reasons := make([]string, len(telemetry.AllReasons))
	for i, rs := range telemetry.AllReasons {
		reasons[i] = string(rs)
	}
	return &Metrics{
		Registry:     r,
		Accepted:     r.CounterVec("ingest_reports_accepted_total", "Reports accepted for persistence.", "transport", []string{"grpc", "ws"}),
		Rejected:     r.CounterVec("ingest_reports_rejected_total", "Reports rejected at the edge.", "reason", reasons),
		Persisted:    r.Counter("ingest_reports_persisted_total", "Reports durably written to Postgres."),
		WriteFailed:  r.Counter("ingest_reports_write_failed_total", "Reports dropped after exhausting write retries."),
		Batches:      r.Counter("ingest_batches_written_total", "Batches written to Postgres."),
		BatchSeconds: r.Histogram("ingest_batch_write_seconds", "Time to write one batch, including retries.", []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}),
		GRPCStreams:  r.Gauge("ingest_grpc_streams", "Open gRPC telemetry streams."),
		WSDevices:    r.Gauge("ingest_ws_devices", "Agents connected over WebSocket."),
		LiveClients:  r.Gauge("ingest_live_clients", "Browser sessions on the live feed."),
	}
}

type Service struct {
	log       *slog.Logger
	validator telemetry.Validator
	registry  *registry.Registry
	pipeline  *pipeline.Pipeline
	rollups   *rollup.Aggregator
	hub       *live.Hub
	m         *Metrics
	ready     atomic.Bool
}

type Deps struct {
	Log       *slog.Logger
	Validator telemetry.Validator
	Registry  *registry.Registry
	Sink      pipeline.Sink
	Pipeline  pipeline.Config
	Hub       *live.Hub
	Metrics   *Metrics
}

func New(d Deps) *Service {
	s := &Service{
		log:       d.Log,
		validator: d.Validator,
		registry:  d.Registry,
		hub:       d.Hub,
		m:         d.Metrics,
	}
	s.rollups = rollup.NewAggregator(func(id string) string {
		region, _ := d.Registry.Region(id)
		return region
	})
	s.pipeline = pipeline.New(d.Pipeline, d.Sink, s.onFlush)
	d.Metrics.Registry.GaugeFunc("ingest_queue_depth", "Reports accepted but not yet written.", func() float64 {
		return float64(s.pipeline.QueueDepth())
	})
	d.Metrics.Registry.GaugeFunc("ingest_registry_terminals", "Terminals allowed to report.", func() float64 {
		return float64(d.Registry.Size())
	})
	return s
}

func (s *Service) Start() { s.pipeline.Start(); s.ready.Store(true) }

func (s *Service) Rollups() *rollup.Aggregator { return s.rollups }
func (s *Service) Metrics() *Metrics           { return s.m }
func (s *Service) Hub() *live.Hub              { return s.hub }
func (s *Service) Ready() bool                 { return s.ready.Load() && s.registry.Ready() }

func (s *Service) onFlush(res pipeline.FlushResult) {
	s.m.BatchSeconds.Observe(res.Duration.Seconds())
	if res.Err != nil {
		s.m.WriteFailed.Add(uint64(len(res.Batch)))
		s.log.Error("dropping batch after retries", "shard", res.Shard, "reports", len(res.Batch), "attempts", res.Attempts, "err", res.Err)
		return
	}
	s.m.Batches.Inc()
	s.m.Persisted.Add(uint64(len(res.Batch)))
	s.rollups.Add(res.Batch)
}

// Accept validates and enqueues one report. It blocks while the pipeline is saturated
// (backpressure) and returns the rejection reason, if any.
func (s *Service) Accept(ctx context.Context, r *telemetry.Report) (telemetry.Reason, error) {
	r.ReceivedAt = time.Now()
	reason, ok := s.validator.Validate(r)
	if ok {
		if _, known := s.registry.Region(r.TerminalID); !known {
			reason, ok = telemetry.ReasonUnknownTerminal, false
		}
	}
	if !ok {
		s.m.Rejected[string(reason)].Inc()
		return reason, nil
	}
	if err := s.pipeline.Submit(ctx, *r); err != nil {
		if errors.Is(err, pipeline.ErrClosed) {
			s.m.Rejected[string(telemetry.ReasonOverloaded)].Inc()
			return telemetry.ReasonOverloaded, nil
		}
		return "", err // client went away while we were applying backpressure
	}
	s.m.Accepted[string(r.Transport)].Inc()
	s.hub.PublishReport(r)
	return "", nil
}

// Close drains the pipeline. Call after the front ends have stopped accepting.
func (s *Service) Close(ctx context.Context) error {
	s.ready.Store(false)
	return s.pipeline.Close(ctx)
}

// Tick is broadcast to every browser once a second.
type Tick struct {
	Type   string     `json:"type"`
	TS     int64      `json:"ts"`
	Ingest TickIngest `json:"ingest"`
}

type TickIngest struct {
	AcceptedPerSec float64 `json:"acceptedPerSec"`
	RejectedPerSec float64 `json:"rejectedPerSec"`
	TotalAccepted  uint64  `json:"totalAccepted"`
	QueueDepth     int     `json:"queueDepth"`
	BatchP99Ms     float64 `json:"batchP99Ms"`
	GRPCStreams    int     `json:"grpcStreams"`
	WSDevices      int     `json:"wsDevices"`
}

func sum(m map[string]*metrics.Counter) uint64 {
	var t uint64
	for _, c := range m {
		t += c.Value()
	}
	return t
}

// RunTicker computes per-second rates and broadcasts them until ctx is done.
func (s *Service) RunTicker(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	prevAcc, prevRej := sum(s.m.Accepted), sum(s.m.Rejected)
	prevHist := s.m.BatchSeconds.Snapshot()
	prevAt := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			acc, rej := sum(s.m.Accepted), sum(s.m.Rejected)
			hist := s.m.BatchSeconds.Snapshot()
			dt := now.Sub(prevAt).Seconds()
			tick := Tick{Type: "tick", TS: now.UnixMilli(), Ingest: TickIngest{
				AcceptedPerSec: float64(acc-prevAcc) / dt,
				RejectedPerSec: float64(rej-prevRej) / dt,
				TotalAccepted:  acc,
				QueueDepth:     s.pipeline.QueueDepth(),
				BatchP99Ms:     hist.Quantile(prevHist, 0.99) * 1000,
				GRPCStreams:    int(s.m.GRPCStreams.Value()),
				WSDevices:      int(s.m.WSDevices.Value()),
			}}
			prevAcc, prevRej, prevHist, prevAt = acc, rej, hist, now
			if msg, err := json.Marshal(tick); err == nil {
				s.hub.Broadcast(msg)
			}
		}
	}
}
