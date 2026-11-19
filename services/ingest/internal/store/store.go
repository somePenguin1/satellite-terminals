// Package store is the Postgres adapter. Telemetry goes in with COPY (binary protocol,
// one round trip per batch), and the latest-state table is refreshed with a single
// set-based upsert built from arrays, so a 1,000-report batch costs two statements.
package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"satfleet/ingest/internal/registry"
	"satfleet/ingest/internal/rollup"
	"satfleet/ingest/internal/telemetry"
)

type Store struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, url string, maxConns int32) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "satfleet-ingest"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close()                         { s.pool.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

var telemetryColumns = []string{
	"terminal_id", "ts", "seq", "link_state", "latency_ms", "jitter_ms", "packet_loss_pct",
	"downlink_mbps", "uplink_mbps", "snr_db", "obstruction_pct", "received_at",
}

func float4(v float32, ok bool) pgtype.Float4 { return pgtype.Float4{Float32: v, Valid: ok} }

// WriteBatch persists raw samples and advances terminal_status in one transaction.
func (s *Store) WriteBatch(ctx context.Context, batch []telemetry.Report) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after commit

	rows := make([][]any, len(batch))
	for i := range batch {
		r := &batch[i]
		rows[i] = []any{
			r.TerminalID, r.Timestamp, int64(r.Seq), int16(r.LinkState), r.LatencyMs, r.JitterMs,
			r.PacketLossPct, r.DownlinkMbps, r.UplinkMbps, float4(r.SnrDb, r.HasSnr), r.ObstructionPct, r.ReceivedAt,
		}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"telemetry"}, telemetryColumns, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("copy telemetry: %w", err)
	}
	if err := upsertStatus(ctx, tx, LatestPerTerminal(batch)); err != nil {
		return fmt.Errorf("upsert terminal_status: %w", err)
	}
	return tx.Commit(ctx)
}

// LatestPerTerminal keeps the newest report per terminal (ties broken by seq), sorted by
// terminal ID so concurrent writers always lock rows in the same order.
func LatestPerTerminal(batch []telemetry.Report) []*telemetry.Report {
	idx := make(map[string]int, len(batch))
	for i := range batch {
		r := &batch[i]
		j, seen := idx[r.TerminalID]
		if !seen || r.Timestamp.After(batch[j].Timestamp) ||
			(r.Timestamp.Equal(batch[j].Timestamp) && r.Seq > batch[j].Seq) {
			idx[r.TerminalID] = i
		}
	}
	out := make([]*telemetry.Report, 0, len(idx))
	for _, i := range idx {
		out = append(out, &batch[i])
	}
	sort.Slice(out, func(a, b int) bool { return out[a].TerminalID < out[b].TerminalID })
	return out
}

const upsertStatusSQL = `
INSERT INTO terminal_status AS s (terminal_id, last_seen_at, last_seq, link_state, latency_ms, jitter_ms,
                                  packet_loss_pct, downlink_mbps, uplink_mbps, snr_db, obstruction_pct,
                                  firmware_version, transport, updated_at)
SELECT u.terminal_id, u.ts, u.seq, u.link_state, u.latency, u.jitter, u.loss, u.down, u.up, u.snr, u.obstruction,
       nullif(u.firmware, ''), u.transport, now()
FROM unnest($1::text[], $2::timestamptz[], $3::bigint[], $4::smallint[], $5::real[], $6::real[], $7::real[],
            $8::real[], $9::real[], $10::real[], $11::real[], $12::text[], $13::text[])
         AS u(terminal_id, ts, seq, link_state, latency, jitter, loss, down, up, snr, obstruction, firmware, transport)
JOIN terminals t ON t.id = u.terminal_id -- skip terminals deleted since the registry last refreshed
ORDER BY u.terminal_id
ON CONFLICT (terminal_id) DO UPDATE
    SET last_seen_at     = EXCLUDED.last_seen_at,
        last_seq         = EXCLUDED.last_seq,
        link_state       = EXCLUDED.link_state,
        latency_ms       = EXCLUDED.latency_ms,
        jitter_ms        = EXCLUDED.jitter_ms,
        packet_loss_pct  = EXCLUDED.packet_loss_pct,
        downlink_mbps    = EXCLUDED.downlink_mbps,
        uplink_mbps      = EXCLUDED.uplink_mbps,
        snr_db           = EXCLUDED.snr_db,
        obstruction_pct  = EXCLUDED.obstruction_pct,
        firmware_version = coalesce(EXCLUDED.firmware_version, s.firmware_version),
        transport        = EXCLUDED.transport,
        updated_at       = now()
    -- Store-and-forward replays deliver old samples; never move "latest" backwards.
    WHERE s.last_seen_at <= EXCLUDED.last_seen_at`

func upsertStatus(ctx context.Context, tx pgx.Tx, latest []*telemetry.Report) error {
	n := len(latest)
	ids, fw, transport := make([]string, n), make([]string, n), make([]string, n)
	ts := make([]time.Time, n)
	seq := make([]int64, n)
	state := make([]int16, n)
	lat, jit, loss, down, up, snr, obs := make([]float32, n), make([]float32, n), make([]float32, n),
		make([]float32, n), make([]float32, n), make([]pgtype.Float4, n), make([]float32, n)
	for i, r := range latest {
		ids[i], ts[i], seq[i], state[i] = r.TerminalID, r.Timestamp, int64(r.Seq), int16(r.LinkState)
		lat[i], jit[i], loss[i], down[i], up[i] = r.LatencyMs, r.JitterMs, r.PacketLossPct, r.DownlinkMbps, r.UplinkMbps
		snr[i], obs[i], fw[i], transport[i] = float4(r.SnrDb, r.HasSnr), r.ObstructionPct, r.FirmwareVersion, string(r.Transport)
	}
	_, err := tx.Exec(ctx, upsertStatusSQL, ids, ts, seq, state, lat, jit, loss, down, up, snr, obs, fw, transport)
	return err
}

// LoadTerminals implements registry.Loader: every terminal that may report.
func (s *Store) LoadTerminals(ctx context.Context) ([]registry.Terminal, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, region FROM terminals WHERE status <> 'decommissioned'`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (registry.Terminal, error) {
		var t registry.Terminal
		err := row.Scan(&t.ID, &t.Region)
		return t, err
	})
}

const upsertMinutesSQL = `
INSERT INTO fleet_minute_stats AS f (minute, region, samples, link_samples, degraded_samples, sum_latency_ms,
                                     sum_packet_loss_pct, sum_downlink_mbps, sum_uplink_mbps)
SELECT * FROM unnest($1::timestamptz[], $2::text[], $3::bigint[], $4::bigint[], $5::bigint[], $6::float8[],
                     $7::float8[], $8::float8[], $9::float8[])
ON CONFLICT (minute, region) DO UPDATE
    SET samples             = f.samples + EXCLUDED.samples,
        link_samples        = f.link_samples + EXCLUDED.link_samples,
        degraded_samples    = f.degraded_samples + EXCLUDED.degraded_samples,
        sum_latency_ms      = f.sum_latency_ms + EXCLUDED.sum_latency_ms,
        sum_packet_loss_pct = f.sum_packet_loss_pct + EXCLUDED.sum_packet_loss_pct,
        sum_downlink_mbps   = f.sum_downlink_mbps + EXCLUDED.sum_downlink_mbps,
        sum_uplink_mbps     = f.sum_uplink_mbps + EXCLUDED.sum_uplink_mbps`

const upsertHoursSQL = `
INSERT INTO terminal_usage_hourly AS h (terminal_id, hour, samples, sum_downlink_mbps, max_downlink_mbps, sum_uplink_mbps)
SELECT u.terminal_id, u.hour, u.samples, u.sum_down, u.max_down, u.sum_up
FROM unnest($1::text[], $2::timestamptz[], $3::bigint[], $4::float8[], $5::real[], $6::float8[])
         AS u(terminal_id, hour, samples, sum_down, max_down, sum_up)
JOIN terminals t ON t.id = u.terminal_id
ORDER BY u.terminal_id, u.hour
ON CONFLICT (terminal_id, hour) DO UPDATE
    SET samples           = h.samples + EXCLUDED.samples,
        sum_downlink_mbps = h.sum_downlink_mbps + EXCLUDED.sum_downlink_mbps,
        max_downlink_mbps = greatest(h.max_downlink_mbps, EXCLUDED.max_downlink_mbps),
        sum_uplink_mbps   = h.sum_uplink_mbps + EXCLUDED.sum_uplink_mbps`

// FlushRollups implements rollup.Flusher with additive upserts, sorted by key for lock ordering.
func (s *Store) FlushRollups(ctx context.Context, minutes map[rollup.MinuteKey]rollup.MinuteAgg, hours map[rollup.HourKey]rollup.HourAgg) error {
	mk := make([]rollup.MinuteKey, 0, len(minutes))
	for k := range minutes {
		mk = append(mk, k)
	}
	sort.Slice(mk, func(i, j int) bool {
		if !mk[i].Minute.Equal(mk[j].Minute) {
			return mk[i].Minute.Before(mk[j].Minute)
		}
		return mk[i].Region < mk[j].Region
	})
	n := len(mk)
	mMinute, mRegion := make([]time.Time, n), make([]string, n)
	mSamples, mLink, mDegraded := make([]int64, n), make([]int64, n), make([]int64, n)
	mLat, mLoss, mDown, mUp := make([]float64, n), make([]float64, n), make([]float64, n), make([]float64, n)
	for i, k := range mk {
		a := minutes[k]
		region := k.Region
		if region == "" {
			region = "Unknown"
		}
		mMinute[i], mRegion[i] = k.Minute, region
		mSamples[i], mLink[i], mDegraded[i] = a.Samples, a.LinkSamples, a.DegradedSamples
		mLat[i], mLoss[i], mDown[i], mUp[i] = a.SumLatencyMs, a.SumPacketLoss, a.SumDownlinkMbps, a.SumUplinkMbps
	}

	hk := make([]rollup.HourKey, 0, len(hours))
	for k := range hours {
		hk = append(hk, k)
	}
	h := len(hk)
	hID, hHour, hSamples := make([]string, h), make([]time.Time, h), make([]int64, h)
	hDown, hMax, hUp := make([]float64, h), make([]float32, h), make([]float64, h)
	for i, k := range hk {
		a := hours[k]
		hID[i], hHour[i], hSamples[i] = k.TerminalID, k.Hour, a.Samples
		hDown[i], hMax[i], hUp[i] = a.SumDownlinkMbps, a.MaxDownlinkMbps, a.SumUplinkMbps
	}

	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if n > 0 {
			if _, err := tx.Exec(ctx, upsertMinutesSQL, mMinute, mRegion, mSamples, mLink, mDegraded, mLat, mLoss, mDown, mUp); err != nil {
				return fmt.Errorf("upsert fleet_minute_stats: %w", err)
			}
		}
		if h > 0 {
			if _, err := tx.Exec(ctx, upsertHoursSQL, hID, hHour, hSamples, hDown, hMax, hUp); err != nil {
				return fmt.Errorf("upsert terminal_usage_hourly: %w", err)
			}
		}
		return nil
	})
}
