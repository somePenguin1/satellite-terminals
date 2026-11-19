package grpcapi

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	telemetryv1 "satfleet/ingest/internal/gen/telemetryv1"
	"satfleet/ingest/internal/ingest"
	"satfleet/ingest/internal/live"
	"satfleet/ingest/internal/pipeline"
	"satfleet/ingest/internal/registry"
	"satfleet/ingest/internal/telemetry"
)

type loader struct{}

func (loader) LoadTerminals(context.Context) ([]registry.Terminal, error) {
	return []registry.Terminal{{ID: "SAT-00001", Region: "Europe"}, {ID: "SAT-00002", Region: "Africa"}}, nil
}

type countingSink struct {
	mu sync.Mutex
	n  int
}

func (s *countingSink) WriteBatch(_ context.Context, b []telemetry.Report) error {
	s.mu.Lock()
	s.n += len(b)
	s.mu.Unlock()
	return nil
}

// newClient serves the API over an in-memory listener: real gRPC framing, no ports.
func newClient(t *testing.T, token string) (telemetryv1.TelemetryIngestClient, *ingest.Service, *countingSink) {
	t.Helper()
	reg := registry.New(loader{})
	_ = reg.Refresh(context.Background())
	sink := &countingSink{}
	svc := ingest.New(ingest.Deps{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Validator: telemetry.DefaultValidator(),
		Registry: reg, Sink: sink, Pipeline: pipeline.DefaultConfig(), Hub: live.NewHub(), Metrics: ingest.NewMetrics(),
	})
	svc.Start()
	gs, _ := New(svc, token)
	lis := bufconn.Listen(1 << 20)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return telemetryv1.NewTelemetryIngestClient(conn), svc, sink
}

func msg(id string, seq uint64) *telemetryv1.TelemetryReport {
	return &telemetryv1.TelemetryReport{
		TerminalId: id, TimestampMs: time.Now().UnixMilli(), Seq: seq,
		LinkState: telemetryv1.LinkState_LINK_STATE_UP, LatencyMs: 35, DownlinkMbps: 80, SnrDb: 11,
	}
}

func TestStreamAcksAcceptedAndRejected(t *testing.T) {
	client, svc, sink := newClient(t, "")
	stream, err := client.StreamTelemetry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i := uint64(1); i <= 100; i++ {
		if err := stream.Send(msg("SAT-00001", i)); err != nil {
			t.Fatal(err)
		}
	}
	_ = stream.Send(msg("SAT-77777", 1))            // unknown terminal
	_ = stream.Send(&telemetryv1.TelemetryReport{}) // malformed
	ack, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatal(err)
	}
	if ack.Accepted != 100 || ack.Rejected != 2 || len(ack.Rejections) != 2 {
		t.Fatalf("ack = %+v", ack)
	}
	if ack.Rejections[0].Reason != string(telemetry.ReasonUnknownTerminal) {
		t.Fatalf("first rejection = %+v", ack.Rejections[0])
	}
	_ = svc.Close(context.Background())
	if sink.n != 100 {
		t.Fatalf("persisted %d, want 100", sink.n)
	}
}

func TestSendBatch(t *testing.T) {
	client, _, _ := newClient(t, "")
	ack, err := client.SendBatch(context.Background(), &telemetryv1.TelemetryBatch{
		Reports: []*telemetryv1.TelemetryReport{msg("SAT-00001", 1), msg("SAT-00002", 1)},
	})
	if err != nil || ack.Accepted != 2 {
		t.Fatalf("SendBatch = %+v, %v", ack, err)
	}
}

func TestBearerTokenRequired(t *testing.T) {
	client, _, _ := newClient(t, "s3cret")
	_, err := client.SendBatch(context.Background(), &telemetryv1.TelemetryBatch{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("no token: err = %v, want Unauthenticated", err)
	}
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer s3cret")
	if _, err := client.SendBatch(ctx, &telemetryv1.TelemetryBatch{Reports: []*telemetryv1.TelemetryReport{msg("SAT-00001", 1)}}); err != nil {
		t.Fatalf("with token: %v", err)
	}
}
