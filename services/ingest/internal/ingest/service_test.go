package ingest

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"satfleet/ingest/internal/live"
	"satfleet/ingest/internal/pipeline"
	"satfleet/ingest/internal/registry"
	"satfleet/ingest/internal/telemetry"
)

type staticLoader []registry.Terminal

func (l staticLoader) LoadTerminals(context.Context) ([]registry.Terminal, error) { return l, nil }

type memSink struct {
	mu      sync.Mutex
	reports []telemetry.Report
}

func (s *memSink) WriteBatch(_ context.Context, b []telemetry.Report) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports = append(s.reports, b...)
	return nil
}

func newService(t *testing.T) (*Service, *memSink, *live.Hub) {
	t.Helper()
	reg := registry.New(staticLoader{{ID: "SAT-00001", Region: "Europe"}})
	if err := reg.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	sink := &memSink{}
	hub := live.NewHub()
	cfg := pipeline.DefaultConfig()
	cfg.FlushInterval = 5 * time.Millisecond
	svc := New(Deps{
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Validator: telemetry.DefaultValidator(),
		Registry:  reg,
		Sink:      sink,
		Pipeline:  cfg,
		Hub:       hub,
		Metrics:   NewMetrics(),
	})
	svc.Start()
	return svc, sink, hub
}

func good(id string) *telemetry.Report {
	return &telemetry.Report{TerminalID: id, Timestamp: time.Now(), Seq: 1, LinkState: telemetry.LinkUp,
		LatencyMs: 35, DownlinkMbps: 50, Transport: telemetry.TransportGRPC}
}

func TestAcceptPersistsAndPublishes(t *testing.T) {
	svc, sink, hub := newService(t)
	browser := hub.Register()
	hub.Subscribe(browser, []string{"SAT-00001"})

	if reason, err := svc.Accept(context.Background(), good("SAT-00001")); reason != "" || err != nil {
		t.Fatalf("Accept = %q, %v", reason, err)
	}
	if err := svc.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sink.reports) != 1 || sink.reports[0].ReceivedAt.IsZero() {
		t.Fatalf("sink = %+v", sink.reports)
	}
	if svc.m.Accepted["grpc"].Value() != 1 || svc.m.Persisted.Value() != 1 {
		t.Fatal("metrics not updated")
	}
	select {
	case <-browser.Messages():
	default:
		t.Fatal("subscriber did not receive the report")
	}
	minutes, hours := svc.Rollups().Drain()
	if len(minutes) != 1 || len(hours) != 1 {
		t.Fatalf("rollups not fed: %d minutes, %d hours", len(minutes), len(hours))
	}
}

func TestAcceptRejectsUnknownAndInvalid(t *testing.T) {
	svc, sink, _ := newService(t)
	ctx := context.Background()

	if reason, _ := svc.Accept(ctx, good("SAT-99999")); reason != telemetry.ReasonUnknownTerminal {
		t.Fatalf("unknown terminal reason = %q", reason)
	}
	bad := good("SAT-00001")
	bad.PacketLossPct = 250
	if reason, _ := svc.Accept(ctx, bad); reason != telemetry.ReasonOutOfRange {
		t.Fatalf("out of range reason = %q", reason)
	}
	_ = svc.Close(ctx)
	if len(sink.reports) != 0 {
		t.Fatal("rejected reports must not be persisted")
	}
	if svc.m.Rejected["unknown_terminal"].Value() != 1 || svc.m.Rejected["out_of_range"].Value() != 1 {
		t.Fatal("rejection metrics not updated")
	}
	if reason, _ := svc.Accept(ctx, good("SAT-00001")); reason != telemetry.ReasonOverloaded {
		t.Fatalf("after close reason = %q, want overloaded", reason)
	}
}
