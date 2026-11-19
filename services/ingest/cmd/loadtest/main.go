// Command loadtest drives the ingest service as hard as asked over gRPC and reports the
// rate at which reports were durably persisted to Postgres (not merely sent), read from
// the service's own Prometheus counters. It exits non-zero below -min-rate, so CI can
// gate on it.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	telemetryv1 "satfleet/ingest/internal/gen/telemetryv1"
	"satfleet/ingest/internal/metrics"
)

type result struct {
	TargetRate      int     `json:"targetRate"`
	DurationSec     float64 `json:"durationSec"`
	Streams         int     `json:"streams"`
	Terminals       int     `json:"terminals"`
	Sent            uint64  `json:"sent"`
	Accepted        uint64  `json:"accepted"`
	Rejected        uint64  `json:"rejected"`
	Persisted       uint64  `json:"persisted"`
	SentPerSec      float64 `json:"sentPerSec"`
	PersistedPerSec float64 `json:"persistedPerSec"`
	DrainSec        float64 `json:"drainSec"`
	BatchP50Ms      float64 `json:"batchP50Ms"`
	BatchP99Ms      float64 `json:"batchP99Ms"`
	WriteFailed     uint64  `json:"writeFailed"`
	MinRate         float64 `json:"minRate"`
	Passed          bool    `json:"passed"`
}

type scrape struct {
	persisted, writeFailed uint64
	queue                  float64
	hist                   metrics.Snapshot
}

// readMetrics parses the few series we need from the ingest /metrics endpoint.
func readMetrics(url string) (scrape, error) {
	resp, err := http.Get(url)
	if err != nil {
		return scrape{}, err
	}
	defer resp.Body.Close()
	var s scrape
	type bucket struct {
		le  float64
		cum uint64
	}
	var buckets []bucket
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		v, _ := strconv.ParseFloat(value, 64)
		switch {
		case name == "ingest_reports_persisted_total":
			s.persisted = uint64(v)
		case name == "ingest_reports_write_failed_total":
			s.writeFailed = uint64(v)
		case name == "ingest_queue_depth":
			s.queue = v
		case strings.HasPrefix(name, `ingest_batch_write_seconds_bucket{le="`):
			le := strings.TrimSuffix(strings.TrimPrefix(name, `ingest_batch_write_seconds_bucket{le="`), `"}`)
			if le == "+Inf" {
				buckets = append(buckets, bucket{le: 1e308, cum: uint64(v)})
			} else if f, err := strconv.ParseFloat(le, 64); err == nil {
				buckets = append(buckets, bucket{le: f, cum: uint64(v)})
			}
		}
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].le < buckets[j].le })
	var prev uint64
	for i, b := range buckets {
		if i < len(buckets)-1 {
			s.hist.Bounds = append(s.hist.Bounds, b.le)
		}
		s.hist.Counts = append(s.hist.Counts, b.cum-prev)
		prev = b.cum
	}
	return s, sc.Err()
}

func main() {
	target := flag.String("target", "localhost:9090", "ingest gRPC address")
	metricsURL := flag.String("metrics", "http://localhost:8081/metrics", "ingest Prometheus endpoint")
	terminals := flag.Int("terminals", 5000, "distinct terminal IDs to use (SAT-00001..)")
	streams := flag.Int("streams", 16, "concurrent gRPC streams")
	rate := flag.Int("rate", 6000, "target reports per second across all streams (0 = as fast as possible)")
	duration := flag.Duration("duration", 60*time.Second, "how long to send")
	minRate := flag.Float64("min-rate", 0, "fail if the persisted rate is below this")
	out := flag.String("json", "", `write the result as JSON to this file ("-" for stdout)`)
	token := flag.String("token", os.Getenv("INGEST_TOKEN"), "bearer token")
	flag.Parse()

	before, err := readMetrics(*metricsURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read ingest metrics:", err)
		os.Exit(2)
	}

	conn, err := grpc.NewClient(*target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer conn.Close()
	client := telemetryv1.NewTelemetryIngestClient(conn)

	ctx := context.Background()
	if *token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+*token)
	}

	var sent, accepted, rejected atomic.Uint64
	start := time.Now()
	deadline := start.Add(*duration)
	var wg sync.WaitGroup
	for w := 0; w < *streams; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			stream, err := client.StreamTelemetry(ctx)
			if err != nil {
				fmt.Fprintln(os.Stderr, "stream:", err)
				return
			}
			rng := rand.New(rand.NewPCG(uint64(w), 99))
			perStream := float64(*rate) / float64(*streams)
			seq := uint64(0)
			var owed float64
			last := time.Now()
			for i := w; time.Now().Before(deadline); {
				n := 1
				if *rate > 0 { // pace in 5 ms steps, carrying fractional reports forward
					time.Sleep(5 * time.Millisecond)
					now := time.Now()
					owed += perStream * now.Sub(last).Seconds()
					last = now
					n = int(owed)
					owed -= float64(n)
				}
				for k := 0; k < n; k++ {
					seq++
					msg := &telemetryv1.TelemetryReport{
						TerminalId:  fmt.Sprintf("SAT-%05d", i%*terminals+1),
						TimestampMs: time.Now().UnixMilli(), Seq: seq,
						LinkState: telemetryv1.LinkState_LINK_STATE_UP,
						LatencyMs: 30 + rng.Float32()*20, JitterMs: rng.Float32() * 3, PacketLossPct: rng.Float32() * 0.5,
						DownlinkMbps: 50 + rng.Float32()*150, UplinkMbps: 5 + rng.Float32()*20, SnrDb: 9 + rng.Float32()*4,
						ObstructionPct: rng.Float32() * 2, FirmwareVersion: "loadtest/1",
					}
					if err := stream.Send(msg); err != nil {
						fmt.Fprintln(os.Stderr, "send:", err)
						return
					}
					sent.Add(1)
					i += *streams
				}
			}
			ack, err := stream.CloseAndRecv()
			if err != nil {
				fmt.Fprintln(os.Stderr, "close:", err)
				return
			}
			accepted.Add(ack.GetAccepted())
			rejected.Add(ack.GetRejected())
		}(w)
	}
	wg.Wait()
	sendDone := time.Now()

	// Wait for the pipeline to drain: queue empty and the persisted counter stable.
	var after scrape
	stableSince := time.Now()
	for time.Since(sendDone) < 60*time.Second {
		time.Sleep(250 * time.Millisecond)
		s, err := readMetrics(*metricsURL)
		if err != nil {
			continue
		}
		if s.persisted != after.persisted {
			stableSince = time.Now()
		}
		after = s
		if s.queue == 0 && time.Since(stableSince) > time.Second && s.persisted-before.persisted >= accepted.Load() {
			break
		}
	}
	persistedAt := stableSince
	elapsed := persistedAt.Sub(start).Seconds()

	r := result{
		TargetRate: *rate, DurationSec: duration.Seconds(), Streams: *streams, Terminals: *terminals,
		Sent: sent.Load(), Accepted: accepted.Load(), Rejected: rejected.Load(),
		Persisted:       after.persisted - before.persisted,
		SentPerSec:      float64(sent.Load()) / sendDone.Sub(start).Seconds(),
		PersistedPerSec: float64(after.persisted-before.persisted) / elapsed,
		DrainSec:        persistedAt.Sub(sendDone).Seconds(),
		BatchP50Ms:      after.hist.Quantile(before.hist, 0.50) * 1000,
		BatchP99Ms:      after.hist.Quantile(before.hist, 0.99) * 1000,
		WriteFailed:     after.writeFailed - before.writeFailed,
		MinRate:         *minRate,
	}
	r.Passed = r.PersistedPerSec >= *minRate && r.WriteFailed == 0

	fmt.Fprintf(os.Stderr, "\nLoad test: %d streams, %d terminals, target %d/s for %s\n", r.Streams, r.Terminals, r.TargetRate, *duration)
	fmt.Fprintf(os.Stderr, "  sent       %10d  (%.0f/s)\n", r.Sent, r.SentPerSec)
	fmt.Fprintf(os.Stderr, "  accepted   %10d   rejected %d\n", r.Accepted, r.Rejected)
	fmt.Fprintf(os.Stderr, "  persisted  %10d  (%.0f/s end to end, drained %.1fs after sending stopped)\n", r.Persisted, r.PersistedPerSec, r.DrainSec)
	fmt.Fprintf(os.Stderr, "  batch write latency p50 %.1f ms, p99 %.1f ms; write failures %d\n", r.BatchP50Ms, r.BatchP99Ms, r.WriteFailed)
	if *minRate > 0 {
		verdict := "PASS"
		if !r.Passed {
			verdict = "FAIL"
		}
		fmt.Fprintf(os.Stderr, "  %s: persisted rate %.0f/s vs required %.0f/s\n", verdict, r.PersistedPerSec, *minRate)
	}
	if *out != "" {
		data, _ := json.MarshalIndent(r, "", "  ")
		if *out == "-" {
			fmt.Println(string(data))
		} else {
			_ = os.WriteFile(*out, data, 0o644)
		}
	}
	if !r.Passed {
		os.Exit(1)
	}
}
