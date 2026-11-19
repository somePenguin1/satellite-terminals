package live

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"satfleet/ingest/internal/telemetry"
)

func drain(c *Client) [][]byte {
	var out [][]byte
	for {
		select {
		case m := <-c.Messages():
			out = append(out, m)
		default:
			return out
		}
	}
}

func rep(id string) *telemetry.Report {
	return &telemetry.Report{TerminalID: id, Timestamp: time.UnixMilli(1000), LinkState: telemetry.LinkUp, LatencyMs: 33}
}

func TestOnlySubscribersReceiveReports(t *testing.T) {
	h := NewHub()
	a, b := h.Register(), h.Register()
	h.HandleControl(a, []byte(`{"type":"subscribe","terminalIds":["SAT-00001"]}`))

	h.PublishReport(rep("SAT-00001"))
	h.PublishReport(rep("SAT-00002"))

	got := drain(a)
	if len(got) != 1 {
		t.Fatalf("subscriber got %d messages, want 1", len(got))
	}
	var msg struct {
		Type   string         `json:"type"`
		Report telemetry.Wire `json:"report"`
	}
	if err := json.Unmarshal(got[0], &msg); err != nil || msg.Type != "telemetry" || msg.Report.TerminalID != "SAT-00001" || msg.Report.LinkState != "UP" {
		t.Fatalf("bad message %s (%v)", got[0], err)
	}
	if len(drain(b)) != 0 {
		t.Fatal("non-subscriber received a report")
	}

	h.HandleControl(a, []byte(`{"type":"unsubscribe","terminalIds":["SAT-00001"]}`))
	h.PublishReport(rep("SAT-00001"))
	if len(drain(a)) != 0 || h.watched.Load() != 0 {
		t.Fatal("unsubscribe did not stop delivery")
	}
}

func TestBroadcastAndSlowClientDrops(t *testing.T) {
	h := NewHub()
	slow := h.Register()
	for i := 0; i < clientBuffer+10; i++ {
		h.Broadcast([]byte(`{"type":"tick"}`))
	}
	if slow.Dropped() != 10 {
		t.Fatalf("dropped = %d, want 10", slow.Dropped())
	}
}

func TestSubscriptionLimitAndUnregister(t *testing.T) {
	h := NewHub()
	c := h.Register()
	ids := make([]string, 0, MaxSubscriptions+5)
	for i := 0; i < MaxSubscriptions+5; i++ {
		ids = append(ids, fmt.Sprintf("SAT-%05d", i))
	}
	h.Subscribe(c, ids)
	if len(c.subs) != MaxSubscriptions {
		t.Fatalf("subs = %d, want %d", len(c.subs), MaxSubscriptions)
	}
	h.Unregister(c)
	if h.watched.Load() != 0 || h.ClientCount() != 0 {
		t.Fatal("unregister left state behind")
	}
	if _, open := <-c.Messages(); open {
		t.Fatal("client channel should be closed")
	}
	h.Unregister(c) // idempotent
	h.PublishReport(rep("SAT-00001"))
}

func TestConcurrentPublishAndChurn(t *testing.T) {
	h := NewHub()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				h.PublishReport(rep("SAT-00001"))
			}
		}
	}()
	for i := 0; i < 200; i++ {
		c := h.Register()
		h.Subscribe(c, []string{"SAT-00001"})
		h.Unregister(c)
	}
	close(stop)
	wg.Wait()
}
