// Command simulator streams realistic telemetry for thousands of simulated terminals over gRPC.
//
// With -api it mirrors the provisioned fleet from the fleet API (status, report interval,
// plan caps) and re-syncs every minute, so activating a terminal in the UI makes it start
// reporting and changing its report interval changes its cadence.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	telemetryv1 "satfleet/ingest/internal/gen/telemetryv1"
	"satfleet/ingest/internal/sim"
	"satfleet/ingest/internal/telemetry"
)

func main() {
	target := flag.String("target", "localhost:9090", "ingest gRPC address; use dns:///host:port to balance across replicas")
	api := flag.String("api", "", "fleet API base URL to mirror the provisioned fleet (empty: synthetic fleet)")
	terminals := flag.String("terminals", "1-5000", "synthetic terminal ID range when -api is not set")
	exclude := flag.String("exclude", "", "terminal ID range to skip, e.g. 1-2 when real C agents report for those")
	interval := flag.Duration("interval", 5*time.Second, "report interval for the synthetic fleet")
	streams := flag.Int("streams", 8, "concurrent gRPC streams; terminals are sharded across them")
	token := flag.String("token", os.Getenv("INGEST_TOKEN"), "bearer token for the ingest service")
	resync := flag.Duration("resync", time.Minute, "how often to re-read the fleet from the API")
	recycle := flag.Duration("recycle", 5*time.Minute, "reopen each stream this often so new ingest replicas get traffic")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var skipStart, skipCount int
	if *exclude != "" {
		var err error
		if skipStart, skipCount, err = sim.ParseRange(*exclude); err != nil {
			log.Error("bad -exclude", "err", err)
			os.Exit(2)
		}
	}
	filter := func(specs []sim.Spec) []sim.Spec {
		if skipCount == 0 {
			return specs
		}
		out := specs[:0]
		for _, s := range specs {
			var n int
			if _, err := fmt.Sscanf(s.TerminalID, "SAT-%d", &n); err == nil && n >= skipStart && n < skipStart+skipCount {
				continue
			}
			out = append(out, s)
		}
		return out
	}

	fleet := sim.NewFleet()
	load := func() error {
		var specs []sim.Spec
		if *api == "" {
			start, n, err := sim.ParseRange(*terminals)
			if err != nil {
				return err
			}
			specs = sim.SyntheticSpecs(start, n, int(interval.Milliseconds()))
		} else {
			c, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			var err error
			if specs, err = sim.FetchSpecs(c, http.DefaultClient, *api); err != nil {
				return err
			}
		}
		added, removed := fleet.Sync(filter(specs), time.Now())
		if added > 0 || removed > 0 {
			log.Info("fleet synced", "reporting", fleet.Size(), "added", added, "removed", removed)
		}
		return nil
	}
	for err := load(); err != nil; err = load() {
		log.Warn("waiting for fleet API", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
	if *api != "" {
		go func() {
			t := time.NewTicker(*resync)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if err := load(); err != nil {
						log.Warn("fleet resync failed", "err", err)
					}
				}
			}
		}()
	}

	conn, err := grpc.NewClient(*target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`))
	if err != nil {
		log.Error("dial", "err", err)
		os.Exit(1)
	}
	defer conn.Close()
	client := telemetryv1.NewTelemetryIngestClient(conn)

	var sent, accepted, rejected atomic.Uint64
	var wg sync.WaitGroup
	for shard := 0; shard < *streams; shard++ {
		wg.Add(1)
		go func(shard int) {
			defer wg.Done()
			worker(ctx, log, client, fleet, shard, *streams, *token, *recycle, &sent, &accepted, &rejected)
		}(shard)
	}

	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	prev := uint64(0)
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			log.Info("stopped", "sent", sent.Load(), "accepted", accepted.Load(), "rejected", rejected.Load())
			return
		case <-t.C:
			cur := sent.Load()
			log.Info("streaming", "terminals", fleet.Size(), "reports_per_sec", float64(cur-prev)/10,
				"sent", cur, "acked", accepted.Load(), "rejected", rejected.Load())
			prev = cur
		}
	}
}

func toProto(r *telemetry.Report) *telemetryv1.TelemetryReport {
	return &telemetryv1.TelemetryReport{
		TerminalId: r.TerminalID, TimestampMs: r.Timestamp.UnixMilli(), Seq: r.Seq,
		LinkState: telemetryv1.LinkState(r.LinkState), LatencyMs: r.LatencyMs, JitterMs: r.JitterMs,
		PacketLossPct: r.PacketLossPct, DownlinkMbps: r.DownlinkMbps, UplinkMbps: r.UplinkMbps,
		SnrDb: r.SnrDb, ObstructionPct: r.ObstructionPct, FirmwareVersion: r.FirmwareVersion,
	}
}

// worker owns one shard of the fleet and one gRPC stream at a time.
func worker(ctx context.Context, log *slog.Logger, client telemetryv1.TelemetryIngestClient, fleet *sim.Fleet,
	shard, shards int, token string, recycle time.Duration, sent, accepted, rejected *atomic.Uint64) {
	if token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	}
	backoff := 500 * time.Millisecond
	var buf []telemetry.Report
	for ctx.Err() == nil {
		stream, err := client.StreamTelemetry(ctx)
		if err != nil {
			log.Warn("open stream failed", "shard", shard, "err", err)
			time.Sleep(backoff)
			backoff = min(backoff*2, 15*time.Second)
			continue
		}
		opened := time.Now()
		tick := time.NewTicker(100 * time.Millisecond)
		var sendErr error
		for sendErr == nil && ctx.Err() == nil && time.Since(opened) < recycle {
			select {
			case <-ctx.Done():
			case now := <-tick.C:
				buf = fleet.Due(now, shard, shards, buf[:0])
				for i := range buf {
					if sendErr = stream.Send(toProto(&buf[i])); sendErr != nil {
						break
					}
					sent.Add(1)
				}
			}
		}
		tick.Stop()
		ack, err := stream.CloseAndRecv()
		if err == nil {
			backoff = 500 * time.Millisecond
			accepted.Add(ack.GetAccepted())
			rejected.Add(ack.GetRejected())
			for _, r := range ack.GetRejections() {
				log.Debug("rejected", "terminal", r.GetTerminalId(), "reason", r.GetReason())
			}
		} else if ctx.Err() == nil {
			log.Warn("stream ended", "shard", shard, "send_err", sendErr, "err", err)
			time.Sleep(backoff)
			backoff = min(backoff*2, 15*time.Second)
		}
	}
}
