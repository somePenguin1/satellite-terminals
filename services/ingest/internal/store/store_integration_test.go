//go:build integration

// Integration tests against a real Postgres. Run with:
//
//	TEST_DATABASE_URL=postgres://fleet:fleet@localhost:5432/fleet_test go test -tags=integration ./internal/store
//
// The schema comes from the fleet API's Flyway migrations, the single source of truth.
package store

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"satfleet/ingest/internal/rollup"
	"satfleet/ingest/internal/telemetry"
)

const migrationsDir = "../../../fleet-api/src/main/resources/db/migration"

func setup(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.terminals') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		files, _ := filepath.Glob(filepath.Join(migrationsDir, "V*.sql"))
		sort.Strings(files)
		if len(files) == 0 {
			t.Fatalf("no migrations found in %s", migrationsDir)
		}
		for _, f := range files {
			sql, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, string(sql)); err != nil {
				t.Fatalf("apply %s: %v", filepath.Base(f), err)
			}
		}
	}
	if _, err := pool.Exec(ctx, `TRUNCATE telemetry, terminal_status, fleet_minute_stats, terminal_usage_hourly`); err != nil {
		t.Fatal(err)
	}

	s, err := Open(ctx, url, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, pool
}

func sample(id string, at time.Time, seq uint64, latency float32) telemetry.Report {
	return telemetry.Report{
		TerminalID: id, Timestamp: at, Seq: seq, LinkState: telemetry.LinkUp, LatencyMs: latency,
		DownlinkMbps: 100, UplinkMbps: 10, SnrDb: 11, HasSnr: true, FirmwareVersion: "test/1",
		Transport: telemetry.TransportGRPC, ReceivedAt: time.Now(),
	}
}

func TestWriteBatchPersistsSamplesAndLatestStatus(t *testing.T) {
	s, pool := setup(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	batch := []telemetry.Report{
		sample("SAT-00001", now.Add(-2*time.Second), 1, 40),
		sample("SAT-00001", now, 2, 45),
		sample("SAT-00002", now, 1, 30),
	}
	batch[2].HasSnr = false
	if err := s.WriteBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}

	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM telemetry`).Scan(&rows); err != nil || rows != 3 {
		t.Fatalf("telemetry rows = %d (%v), want 3", rows, err)
	}
	var seq int64
	var latency float32
	if err := pool.QueryRow(ctx, `SELECT last_seq, latency_ms FROM terminal_status WHERE terminal_id = 'SAT-00001'`).Scan(&seq, &latency); err != nil {
		t.Fatal(err)
	}
	if seq != 2 || latency != 45 {
		t.Fatalf("status = seq %d latency %v, want latest sample", seq, latency)
	}
	var snrNull bool
	if err := pool.QueryRow(ctx, `SELECT snr_db IS NULL FROM terminal_status WHERE terminal_id = 'SAT-00002'`).Scan(&snrNull); err != nil || !snrNull {
		t.Fatalf("missing SNR should be stored as NULL (%v)", err)
	}

	// A replayed older sample is stored but must not move "latest" backwards.
	if err := s.WriteBatch(ctx, []telemetry.Report{sample("SAT-00001", now.Add(-time.Minute), 0, 999)}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT last_seq FROM terminal_status WHERE terminal_id = 'SAT-00001'`).Scan(&seq); err != nil || seq != 2 {
		t.Fatalf("status regressed to seq %d (%v)", seq, err)
	}
}

func TestLoadTerminalsExcludesDecommissioned(t *testing.T) {
	s, pool := setup(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `UPDATE terminals SET status = 'decommissioned' WHERE id = 'SAT-04999'`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `UPDATE terminals SET status = 'active' WHERE id = 'SAT-04999'`) })

	terms, err := s.LoadTerminals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(terms) != 4999 {
		t.Fatalf("loaded %d terminals, want 4999", len(terms))
	}
	for _, term := range terms {
		if term.ID == "SAT-04999" {
			t.Fatal("decommissioned terminal returned")
		}
		if term.Region == "" {
			t.Fatalf("%s has no region", term.ID)
		}
	}
}

func TestFlushRollupsIsAdditive(t *testing.T) {
	s, pool := setup(t)
	ctx := context.Background()
	minute := time.Now().UTC().Truncate(time.Minute)
	minutes := map[rollup.MinuteKey]rollup.MinuteAgg{
		{Minute: minute, Region: "Europe"}: {Samples: 10, LinkSamples: 9, SumLatencyMs: 360},
	}
	hours := map[rollup.HourKey]rollup.HourAgg{
		{TerminalID: "SAT-00001", Hour: minute.Truncate(time.Hour)}: {Samples: 10, SumDownlinkMbps: 1000, MaxDownlinkMbps: 150},
		{TerminalID: "SAT-99999", Hour: minute.Truncate(time.Hour)}: {Samples: 1}, // unknown: skipped, not an error
	}
	for i := 0; i < 2; i++ { // two replicas flushing partial sums
		if err := s.FlushRollups(ctx, minutes, hours); err != nil {
			t.Fatal(err)
		}
	}
	var samples int64
	var latency float64
	if err := pool.QueryRow(ctx, `SELECT samples, sum_latency_ms FROM fleet_minute_stats WHERE region = 'Europe'`).Scan(&samples, &latency); err != nil {
		t.Fatal(err)
	}
	if samples != 20 || latency != 720 {
		t.Fatalf("minute stats = %d samples / %v ms, want 20 / 720", samples, latency)
	}
	var maxDown float32
	if err := pool.QueryRow(ctx, `SELECT samples, max_downlink_mbps FROM terminal_usage_hourly WHERE terminal_id = 'SAT-00001'`).Scan(&samples, &maxDown); err != nil {
		t.Fatal(err)
	}
	if samples != 20 || maxDown != 150 {
		t.Fatalf("hourly usage = %d samples / max %v, want 20 / 150", samples, maxDown)
	}
}
