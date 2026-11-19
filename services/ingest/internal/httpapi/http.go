// Package httpapi serves the WebSocket endpoints (device ingest and browser live feed),
// Prometheus metrics and health probes.
package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"satfleet/ingest/internal/ingest"
	"satfleet/ingest/internal/telemetry"
)

type Options struct {
	Token          string   // required on /ws/ingest when set
	AllowedOrigins []string // extra browser origins for /ws/live (same-origin is always allowed)
	Ping           func(ctx context.Context) error
	Log            *slog.Logger
}

func NewHandler(svc *ingest.Service, opts Options) http.Handler {
	h := &handlers{svc: svc, opts: opts}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /readyz", h.ready)
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		svc.Metrics().Registry.WritePrometheus(w)
	})
	mux.HandleFunc("GET /ws/ingest", h.ingest)
	mux.HandleFunc("GET /ws/live", h.live)
	return mux
}

type handlers struct {
	svc  *ingest.Service
	opts Options
}

func (h *handlers) ready(w http.ResponseWriter, r *http.Request) {
	if !h.svc.Ready() {
		http.Error(w, "starting", http.StatusServiceUnavailable)
		return
	}
	if h.opts.Ping != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := h.opts.Ping(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	_, _ = w.Write([]byte("ready\n"))
}

func (h *handlers) authorized(r *http.Request) bool {
	if h.opts.Token == "" {
		return true
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(got), []byte(h.opts.Token)) == 1
}

type rejection struct {
	Type   string `json:"type"`
	Seq    uint64 `json:"seq"`
	Reason string `json:"reason"`
}

// ingest handles one device agent: a stream of JSON telemetry messages.
func (h *handlers) ingest(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		http.Error(w, "missing or invalid bearer token", http.StatusUnauthorized)
		return
	}
	c, err := websocket.Accept(w, r, nil) // agents send no Origin header
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(64 << 10)

	m := h.svc.Metrics()
	m.WSDevices.Add(1)
	defer m.WSDevices.Add(-1)

	ctx := r.Context()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return // closed by peer, network error or server shutdown
		}
		if typ != websocket.MessageText {
			continue
		}
		report, err := telemetry.DecodeJSON(data)
		var reason telemetry.Reason
		if err != nil {
			reason = telemetry.ReasonMalformed
			m.Rejected[string(reason)].Inc()
		} else if reason, err = h.svc.Accept(ctx, &report); err != nil {
			return
		}
		if reason != "" {
			// Tell the agent why, so field engineers can see it in the device log.
			msg, _ := json.Marshal(rejection{Type: "rejected", Seq: report.Seq, Reason: string(reason)})
			wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := c.Write(wctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// live streams fleet ticks and subscribed terminal reports to a browser.
func (h *handlers) live(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.opts.AllowedOrigins})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(16 << 10)

	hub := h.svc.Hub()
	client := hub.Register()
	defer hub.Unregister(client)
	m := h.svc.Metrics()
	m.LiveClients.Add(1)
	defer m.LiveClients.Add(-1)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		defer cancel()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageText {
				hub.HandleControl(client, data)
			}
		}
	}()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			c.Close(websocket.StatusNormalClosure, "")
			return
		case msg, ok := <-client.Messages():
			if !ok {
				return
			}
			wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.Write(wctx, websocket.MessageText, msg)
			wcancel()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pctx)
			pcancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				return
			}
		}
	}
}
