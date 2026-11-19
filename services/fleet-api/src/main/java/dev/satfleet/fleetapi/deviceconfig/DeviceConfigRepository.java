package dev.satfleet.fleetapi.deviceconfig;

import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.ObjectMapper;
import dev.satfleet.fleetapi.common.Rows;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigChange;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.DeviceConfig;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ExportRow;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.HistoryEntry;
import java.io.UncheckedIOException;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

@Repository
public class DeviceConfigRepository {

    private static final TypeReference<Map<String, Object>> MAP = new TypeReference<>() {};

    private final JdbcClient jdbc;
    private final ObjectMapper json;

    public DeviceConfigRepository(JdbcClient jdbc, ObjectMapper json) {
        this.jdbc = jdbc;
        this.json = json;
    }

    public Optional<DeviceConfig> find(String terminalId) {
        return jdbc.sql("""
                SELECT c.terminal_id, t.status, c.version, c.report_interval_ms, c.telemetry_level, c.band,
                       c.tx_power_dbm, c.qos_profile, c.firmware_channel, c.maintenance_window_start,
                       c.maintenance_window_end, c.updated_at, c.updated_by, p.downlink_mbps, p.uplink_mbps
                FROM terminal_configs c
                JOIN terminals t ON t.id = c.terminal_id
                JOIN service_plans p ON p.code = t.plan_code
                WHERE c.terminal_id = :id
                """)
                .param("id", terminalId)
                .query((rs, i) -> new DeviceConfig(
                        rs.getString("terminal_id"), rs.getString("status"), rs.getInt("version"),
                        rs.getInt("report_interval_ms"), rs.getString("telemetry_level"), rs.getString("band"),
                        rs.getFloat("tx_power_dbm"), rs.getString("qos_profile"), rs.getString("firmware_channel"),
                        rs.getTime("maintenance_window_start").toLocalTime(),
                        rs.getTime("maintenance_window_end").toLocalTime(),
                        Rows.instant(rs, "updated_at"), rs.getString("updated_by"),
                        rs.getInt("downlink_mbps"), rs.getInt("uplink_mbps")))
                .optional();
    }

    /**
     * Compare-and-set update: succeeds only if the stored version still equals {@code expectedVersion}.
     * Returns the number of rows changed (0 means someone else saved first, or the terminal is gone).
     */
    public int update(String terminalId, int expectedVersion, ConfigChange c, String actor) {
        return jdbc.sql("""
                UPDATE terminal_configs
                SET report_interval_ms = :interval, telemetry_level = :level, band = :band, tx_power_dbm = :tx,
                    qos_profile = :qos, firmware_channel = :channel, maintenance_window_start = :start,
                    maintenance_window_end = :end, version = version + 1, updated_at = now(), updated_by = :actor
                WHERE terminal_id = :id AND version = :expected
                """)
                .param("interval", c.reportIntervalMs()).param("level", c.telemetryLevel()).param("band", c.band())
                .param("tx", c.txPowerDbm()).param("qos", c.qosProfile()).param("channel", c.firmwareChannel())
                .param("start", c.windowStart()).param("end", c.windowEnd()).param("actor", actor)
                .param("id", terminalId).param("expected", expectedVersion)
                .update();
    }

    public List<HistoryEntry> history(String terminalId, int limit) {
        return jdbc.sql("""
                SELECT version, changed_by, changed_at, config::text AS config
                FROM terminal_config_history WHERE terminal_id = :id ORDER BY version DESC LIMIT :limit
                """)
                .param("id", terminalId).param("limit", limit)
                .query((rs, i) -> new HistoryEntry(rs.getInt("version"), rs.getString("changed_by"),
                        Rows.instant(rs, "changed_at"), parse(rs.getString("config"))))
                .list();
    }

    private Map<String, Object> parse(String raw) {
        try {
            return json.readValue(raw, MAP);
        } catch (com.fasterxml.jackson.core.JsonProcessingException e) {
            throw new UncheckedIOException(e);
        }
    }

    public List<ExportRow> export(String afterId, int limit) {
        return jdbc.sql("""
                SELECT t.id AS terminal_id, t.status, c.report_interval_ms, p.downlink_mbps AS downlink_cap_mbps,
                       p.uplink_mbps AS uplink_cap_mbps, t.longitude
                FROM terminals t
                JOIN terminal_configs c ON c.terminal_id = t.id
                JOIN service_plans p ON p.code = t.plan_code
                WHERE t.id > :after
                ORDER BY t.id
                LIMIT :limit
                """)
                .param("after", afterId == null ? "" : afterId).param("limit", limit)
                .query(ExportRow.class).list();
    }
}
