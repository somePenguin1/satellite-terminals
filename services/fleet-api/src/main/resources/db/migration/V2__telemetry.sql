-- High-volume telemetry, range-partitioned by UTC day so retention is a cheap DROP TABLE
-- instead of a slow DELETE. No foreign key: the ingest service validates terminal IDs
-- against an in-memory cache, which keeps COPY throughput high.
CREATE TABLE telemetry (
    terminal_id     text        NOT NULL,
    ts              timestamptz NOT NULL,
    seq             bigint      NOT NULL,
    link_state      smallint    NOT NULL,
    latency_ms      real,
    jitter_ms       real,
    packet_loss_pct real,
    downlink_mbps   real,
    uplink_mbps     real,
    snr_db          real,
    obstruction_pct real,
    received_at     timestamptz NOT NULL DEFAULT now()
) PARTITION BY RANGE (ts);

CREATE INDEX telemetry_terminal_ts_idx ON telemetry (terminal_id, ts DESC);
CREATE INDEX telemetry_ts_brin_idx ON telemetry USING brin (ts);

-- Safety net so a late maintenance job never makes inserts fail.
CREATE TABLE telemetry_default PARTITION OF telemetry DEFAULT;

CREATE FUNCTION ensure_telemetry_partitions(days_back int, days_ahead int) RETURNS int
    LANGUAGE plpgsql AS
$$
DECLARE
    today   date := (now() AT TIME ZONE 'UTC')::date;
    d       date;
    part    text;
    created int  := 0;
BEGIN
    FOR d IN SELECT generate_series(today - days_back, today + days_ahead, interval '1 day')::date
        LOOP
            part := format('telemetry_%s', to_char(d, 'YYYYMMDD'));
            IF to_regclass(part) IS NULL THEN
                BEGIN
                    EXECUTE format(
                            'CREATE TABLE %I PARTITION OF telemetry FOR VALUES FROM (%L) TO (%L)',
                            part, d::timestamp AT TIME ZONE 'UTC', (d + 1)::timestamp AT TIME ZONE 'UTC');
                    created := created + 1;
                EXCEPTION
                    WHEN check_violation THEN
                        -- Rows for this day already landed in the default partition.
                        RAISE WARNING 'telemetry partition % skipped: default partition holds rows for that day', part;
                END;
            END IF;
        END LOOP;
    RETURN created;
END
$$;

CREATE FUNCTION drop_expired_telemetry_partitions(retain_days int) RETURNS int
    LANGUAGE plpgsql AS
$$
DECLARE
    cutoff  date := (now() AT TIME ZONE 'UTC')::date - retain_days;
    rec     record;
    dropped int  := 0;
BEGIN
    FOR rec IN
        SELECT c.relname
        FROM pg_inherits i
                 JOIN pg_class c ON c.oid = i.inhrelid
                 JOIN pg_class p ON p.oid = i.inhparent
        WHERE p.relname = 'telemetry'
          AND c.relname ~ '^telemetry_[0-9]{8}$'
        LOOP
            IF to_date(substr(rec.relname, 11), 'YYYYMMDD') < cutoff THEN
                EXECUTE format('DROP TABLE %I', rec.relname);
                dropped := dropped + 1;
            END IF;
        END LOOP;
    RETURN dropped;
END
$$;

SELECT ensure_telemetry_partitions(1, 3);

-- Write-time rollups maintained by the ingest service: per-minute fleet stats by region,
-- and per-terminal hourly usage (drives the sales team's upsell view).
CREATE TABLE fleet_minute_stats (
    minute              timestamptz      NOT NULL,
    region              text             NOT NULL,
    samples             bigint           NOT NULL,
    link_samples        bigint           NOT NULL, -- samples where the link was not down
    degraded_samples    bigint           NOT NULL,
    sum_latency_ms      double precision NOT NULL,
    sum_packet_loss_pct double precision NOT NULL,
    sum_downlink_mbps   double precision NOT NULL,
    sum_uplink_mbps     double precision NOT NULL,
    PRIMARY KEY (minute, region)
);

CREATE TABLE terminal_usage_hourly (
    terminal_id       text             NOT NULL REFERENCES terminals (id) ON DELETE CASCADE,
    hour              timestamptz      NOT NULL,
    samples           bigint           NOT NULL,
    sum_downlink_mbps double precision NOT NULL,
    max_downlink_mbps real             NOT NULL,
    sum_uplink_mbps   double precision NOT NULL,
    PRIMARY KEY (terminal_id, hour)
);
CREATE INDEX terminal_usage_hourly_hour_idx ON terminal_usage_hourly (hour);

-- Single definition of "health" shared by the REST API, the alert evaluator and dashboards.
CREATE VIEW terminal_health_v AS
SELECT t.id AS terminal_id,
       t.status,
       t.region,
       s.last_seen_at,
       s.link_state,
       s.latency_ms,
       s.jitter_ms,
       s.packet_loss_pct,
       s.downlink_mbps,
       s.uplink_mbps,
       s.snr_db,
       s.obstruction_pct,
       s.firmware_version,
       s.transport,
       CASE
           WHEN t.status IN ('provisioned', 'decommissioned') THEN 'inactive'
           WHEN s.terminal_id IS NULL
               OR s.last_seen_at < now() - make_interval(secs => greatest(60, 3 * coalesce(c.report_interval_ms, 5000) / 1000.0))
               THEN 'offline'
           WHEN s.link_state = 3 THEN 'down'
           WHEN s.link_state = 2 OR s.latency_ms > 150 OR s.packet_loss_pct > 5 OR s.snr_db < 5 THEN 'degraded'
           ELSE 'healthy'
           END AS health
FROM terminals t
         LEFT JOIN terminal_status s ON s.terminal_id = t.id
         LEFT JOIN terminal_configs c ON c.terminal_id = t.id;
