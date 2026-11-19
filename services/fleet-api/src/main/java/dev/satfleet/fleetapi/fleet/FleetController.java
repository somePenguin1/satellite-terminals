package dev.satfleet.fleetapi.fleet;

import static dev.satfleet.fleetapi.common.ApiExceptions.notFound;
import static dev.satfleet.fleetapi.common.Rows.doubleOrNull;
import static dev.satfleet.fleetapi.common.Rows.instant;

import io.swagger.v3.oas.annotations.Operation;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import java.time.Instant;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

/** Read models for operations dashboards: fleet health, map, per-minute trends and per-terminal history. */
@RestController
public class FleetController {

    public record RegionHealth(
            String region, long total, long healthy, long degraded, long down, long offline, long inactive,
            Double avgLatencyMs) {}

    public record OpenAlerts(long critical, long warning) {}

    public record FleetSummary(
            long total,
            Map<String, Long> byStatus,
            Map<String, Long> byHealth,
            List<RegionHealth> regions,
            OpenAlerts openAlerts) {}

    public record FleetMinute(
            Instant minute, long samples, Double avgLatencyMs, Double avgDownlinkMbps, Double avgPacketLossPct,
            Double degradedPct) {}

    public record FleetMap(List<String> ids, List<Double> lat, List<Double> lon, List<String> health) {}

    public record TelemetryBucket(
            Instant bucket, long samples, Double latencyAvg, Double latencyMax, Double downlinkAvg, Double uplinkAvg,
            Double lossAvg, Double snrAvg) {}

    private final JdbcClient jdbc;

    public FleetController(JdbcClient jdbc) {
        this.jdbc = jdbc;
    }

    private Map<String, Long> counts(String sql, String... keys) {
        Map<String, Long> out = new LinkedHashMap<>();
        for (String k : keys) {
            out.put(k, 0L);
        }
        jdbc.sql(sql).query(rs -> {
            out.put(rs.getString(1), rs.getLong(2));
        });
        return out;
    }

    @Operation(summary = "Fleet-wide health and status counts, by region")
    @GetMapping("/api/v1/fleet/summary")
    public FleetSummary summary() {
        Map<String, Long> byStatus = counts("SELECT status, count(*) FROM terminals GROUP BY status",
                "provisioned", "active", "suspended", "decommissioned");
        Map<String, Long> byHealth = counts("SELECT health, count(*) FROM terminal_health_v GROUP BY health",
                "healthy", "degraded", "down", "offline", "inactive");
        List<RegionHealth> regions = jdbc.sql("""
                SELECT region, count(*) AS total,
                       count(*) FILTER (WHERE health = 'healthy') AS healthy,
                       count(*) FILTER (WHERE health = 'degraded') AS degraded,
                       count(*) FILTER (WHERE health = 'down') AS down,
                       count(*) FILTER (WHERE health = 'offline') AS offline,
                       count(*) FILTER (WHERE health = 'inactive') AS inactive,
                       avg(latency_ms) FILTER (WHERE health IN ('healthy', 'degraded')) AS avg_latency_ms
                FROM terminal_health_v
                WHERE status <> 'decommissioned'
                GROUP BY region ORDER BY region
                """).query((rs, i) -> new RegionHealth(rs.getString("region"), rs.getLong("total"),
                rs.getLong("healthy"), rs.getLong("degraded"), rs.getLong("down"), rs.getLong("offline"),
                rs.getLong("inactive"), doubleOrNull(rs, "avg_latency_ms"))).list();
        Map<String, Long> alerts = counts(
                "SELECT severity, count(*) FROM alerts WHERE resolved_at IS NULL GROUP BY severity",
                "critical", "warning");
        long total = byStatus.values().stream().mapToLong(Long::longValue).sum();
        return new FleetSummary(total, byStatus, byHealth, regions,
                new OpenAlerts(alerts.get("critical"), alerts.get("warning")));
    }

    @Operation(summary = "Fleet-wide per-minute averages from write-time rollups")
    @GetMapping("/api/v1/fleet/timeseries")
    public List<FleetMinute> timeseries(@RequestParam(defaultValue = "60") @Min(1) @Max(1440) int minutes) {
        return jdbc.sql("""
                SELECT minute, sum(samples) AS samples,
                       sum(sum_latency_ms) / nullif(sum(link_samples), 0) AS avg_latency_ms,
                       sum(sum_downlink_mbps) / nullif(sum(samples), 0) AS avg_downlink_mbps,
                       sum(sum_packet_loss_pct) / nullif(sum(link_samples), 0) AS avg_packet_loss_pct,
                       100.0 * sum(degraded_samples) / nullif(sum(samples), 0) AS degraded_pct
                FROM fleet_minute_stats
                WHERE minute >= date_trunc('minute', now()) - make_interval(mins => :minutes)
                GROUP BY minute ORDER BY minute
                """).param("minutes", minutes)
                .query((rs, i) -> new FleetMinute(instant(rs, "minute"), rs.getLong("samples"),
                        doubleOrNull(rs, "avg_latency_ms"), doubleOrNull(rs, "avg_downlink_mbps"),
                        doubleOrNull(rs, "avg_packet_loss_pct"), doubleOrNull(rs, "degraded_pct")))
                .list();
    }

    @Operation(summary = "Every in-service terminal's position and health, columnar for compact transfer")
    @GetMapping("/api/v1/fleet/map")
    public FleetMap map() {
        List<String> ids = new ArrayList<>(5000);
        List<Double> lat = new ArrayList<>(5000);
        List<Double> lon = new ArrayList<>(5000);
        List<String> health = new ArrayList<>(5000);
        jdbc.sql("""
                SELECT t.id, round(t.latitude::numeric, 3) AS lat, round(t.longitude::numeric, 3) AS lon, h.health
                FROM terminals t JOIN terminal_health_v h ON h.terminal_id = t.id
                WHERE t.status <> 'decommissioned'
                ORDER BY t.id
                """).query(rs -> {
            ids.add(rs.getString("id"));
            lat.add(rs.getDouble("lat"));
            lon.add(rs.getDouble("lon"));
            health.add(rs.getString("health"));
        });
        return new FleetMap(ids, lat, lon, health);
    }

    @Operation(summary = "A terminal's telemetry in time buckets (avg/max per bucket)")
    @GetMapping("/api/v1/terminals/{id}/telemetry")
    public List<TelemetryBucket> telemetry(
            @PathVariable String id,
            @RequestParam(defaultValue = "60") @Min(1) @Max(10080) int minutes,
            @RequestParam(defaultValue = "60") @Min(10) @Max(86400) int bucketSeconds) {
        boolean exists = jdbc.sql("SELECT EXISTS (SELECT 1 FROM terminals WHERE id = :id)")
                .param("id", id).query(Boolean.class).single();
        if (!exists) {
            throw notFound("Terminal " + id);
        }
        // Down samples carry no meaningful latency/loss, so they're excluded from those averages.
        return jdbc.sql("""
                SELECT date_bin(make_interval(secs => :bucket), ts, TIMESTAMPTZ '2000-01-01') AS bucket,
                       count(*) AS samples,
                       avg(latency_ms) FILTER (WHERE link_state <> 3) AS latency_avg,
                       max(latency_ms) FILTER (WHERE link_state <> 3) AS latency_max,
                       avg(downlink_mbps) AS downlink_avg,
                       avg(uplink_mbps) AS uplink_avg,
                       avg(packet_loss_pct) FILTER (WHERE link_state <> 3) AS loss_avg,
                       avg(snr_db) AS snr_avg
                FROM telemetry
                WHERE terminal_id = :id AND ts >= now() - make_interval(mins => :minutes)
                GROUP BY 1 ORDER BY 1
                """)
                .param("id", id).param("bucket", (double) bucketSeconds).param("minutes", minutes)
                .query((rs, i) -> new TelemetryBucket(instant(rs, "bucket"), rs.getLong("samples"),
                        doubleOrNull(rs, "latency_avg"), doubleOrNull(rs, "latency_max"),
                        doubleOrNull(rs, "downlink_avg"), doubleOrNull(rs, "uplink_avg"),
                        doubleOrNull(rs, "loss_avg"), doubleOrNull(rs, "snr_avg")))
                .list();
    }
}
