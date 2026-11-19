package dev.satfleet.fleetapi.maintenance;

import dev.satfleet.fleetapi.config.FleetProperties;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.boot.context.event.ApplicationReadyEvent;
import org.springframework.context.event.EventListener;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

/**
 * Keeps telemetry storage bounded: pre-creates daily partitions a few days ahead (so inserts
 * never land in the default partition) and drops partitions past the retention window, which
 * is a metadata-only operation instead of a multi-million-row DELETE.
 */
@Component
public class TelemetryMaintenance {

    private static final Logger log = LoggerFactory.getLogger(TelemetryMaintenance.class);

    public record Result(int created, int dropped, int rollupsPruned) {}

    private final JdbcClient jdbc;
    private final FleetProperties props;

    public TelemetryMaintenance(JdbcClient jdbc, FleetProperties props) {
        this.jdbc = jdbc;
        this.props = props;
    }

    @EventListener(ApplicationReadyEvent.class)
    public void onStartup() {
        run();
    }

    @Scheduled(cron = "0 5 * * * *", zone = "UTC") // hourly, so a missed run is caught up quickly
    public void scheduled() {
        run();
    }

    public Result run() {
        int retention = props.telemetry().retentionDays();
        int created = jdbc.sql("SELECT ensure_telemetry_partitions(1, 3)").query(Integer.class).single();
        int dropped = jdbc.sql("SELECT drop_expired_telemetry_partitions(:days)")
                .param("days", retention).query(Integer.class).single();
        int pruned = jdbc.sql("DELETE FROM fleet_minute_stats WHERE minute < now() - interval '30 days'").update()
                + jdbc.sql("DELETE FROM terminal_usage_hourly WHERE hour < now() - interval '90 days'").update();
        if (created > 0 || dropped > 0) {
            log.info("telemetry partitions: {} created, {} dropped (retention {} days)", created, dropped, retention);
        }
        return new Result(created, dropped, pruned);
    }
}
