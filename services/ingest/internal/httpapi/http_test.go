package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"satfleet/ingest/internal/ingest"
	"satfleet/ingest/internal/live"
	"satfleet/ingest/internal/pipeline"
	"satfleet/ingest/internal/registry"
	"satfleet/ingest/internal/telemetry"
)

type loader struct{}

func (loader) LoadTerminals(context.Context) ([]registry.Terminal, error) {
	return []registry.Terminal{{ID: "SAT-00001", Region: "Europe"}}, nil
}

type sink struct {
	mu sync.Mutex
	n  int
}

func (s *sink) WriteBatch(_ context.Context, b []telemetry.Report) error {
	s.mu.Lock()
	s.n += len(b)
	s.mu.Unlock()
	return nil
}

func (s *sink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

func newServer(t *testing.T, token string) (*httptest.Server, *ingest.Service, *sink) {
	t.Helper()
	reg := registry.New(loader{})
	_ = reg.Refresh(context.Background())
	snk := &sink{}
	cfg := pipeline.DefaultConfig()
	cfg.FlushInterval = 5 * time.Millisecond
	svc := ingest.New(ingest.Deps{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Validator: telemetry.DefaultValidator(),
		Registry: reg, Sink: snk, Pipeline: cfg, Hub: live.NewHub(), Metrics: ingest.NewMetrics(),
	})
	svc.Start()
	srv := httptest.NewServer(NewHandler(svc, Options{Token: token}))
	t.Cleanup(srv.Close)
	return srv, svc, snk
}

func wsURL(srv *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + path
}

func agentMessage(seq int) string {
	return `{"terminalId":"SAT-00001","timestampMs":` + itoa(time.Now().UnixMilli()) + `,"seq":` + itoa(int64(seq)) +
		`,"linkState":"UP","latencyMs":40,"jitterMs":1,"packetLossPct":0.1,"downlinkMbps":90,"uplinkMbps":9,"snrDb":11,"obstructionPct":1}`
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestAgentIngestAndLiveFeed(t *testing.T) {
	srv, _, snk := newServer(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// A browser subscribes to the terminal first.
	browser, _, err := websocket.Dial(ctx, wsURL(srv, "/ws/live"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer browser.CloseNow()
	if err := browser.Write(ctx, websocket.MessageText, []byte(`{"type":"subscribe","terminalIds":["SAT-00001"]}`)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond) // let the subscription register

	agent, _, err := websocket.Dial(ctx, wsURL(srv, "/ws/ingest"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.CloseNow()
	for i := 1; i <= 3; i++ {
		if err := agent.Write(ctx, websocket.MessageText, []byte(agentMessage(i))); err != nil {
			t.Fatal(err)
		}
	}

	// The browser sees the report stream (ticks may interleave).
	got := 0
	for got < 3 {
		_, data, err := browser.Read(ctx)
		if err != nil {
			t.Fatalf("browser read: %v (got %d reports)", err, got)
		}
		var m struct{ Type string }
		_ = json.Unmarshal(data, &m)
		if m.Type == "telemetry" {
			got++
		}
	}

	// Bad message: the agent is told why.
	_ = agent.Write(ctx, websocket.MessageText, []byte(`{"terminalId":"SAT-00001","timestampMs":1,"seq":9,"linkState":"UP"}`))
	_, data, err := agent.Read(ctx)
	if err != nil || !strings.Contains(string(data), `"reason":"too_old"`) {
		t.Fatalf("expected too_old rejection, got %s (%v)", data, err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for snk.count() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if snk.count() != 3 {
		t.Fatalf("persisted %d, want 3", snk.count())
	}
}

func TestIngestRequiresToken(t *testing.T) {
	srv, _, _ := newServer(t, "s3cret")
	ctx := context.Background()
	if _, resp, err := websocket.Dial(ctx, wsURL(srv, "/ws/ingest"), nil); err == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("dial without token should be 401, got %v", err)
	}
	c, _, err := websocket.Dial(ctx, wsURL(srv, "/ws/ingest"), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer s3cret"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	c.CloseNow()
}

func TestMetricsAndProbes(t *testing.T) {
	srv, _, _ := newServer(t, "")
	for path, want := range map[string]string{"/healthz": "ok", "/readyz": "ready", "/metrics": "ingest_reports_accepted_total"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(body), want) {
			t.Errorf("GET %s = %d %q", path, resp.StatusCode, body)
		}
	}
}
