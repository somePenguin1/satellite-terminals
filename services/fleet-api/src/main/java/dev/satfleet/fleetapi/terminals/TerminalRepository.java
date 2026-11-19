package dev.satfleet.fleetapi.terminals;

import static dev.satfleet.fleetapi.common.Rows.floatOrNull;
import static dev.satfleet.fleetapi.common.Rows.instant;
import static dev.satfleet.fleetapi.common.Rows.linkState;

import dev.satfleet.fleetapi.plans.Plan;
import dev.satfleet.fleetapi.terminals.TerminalModels.CustomerInfo;
import dev.satfleet.fleetapi.terminals.TerminalModels.CustomerRef;
import dev.satfleet.fleetapi.terminals.TerminalModels.Filter;
import dev.satfleet.fleetapi.terminals.TerminalModels.LiveMetrics;
import dev.satfleet.fleetapi.terminals.TerminalModels.Location;
import dev.satfleet.fleetapi.terminals.TerminalModels.PlanRef;
import dev.satfleet.fleetapi.terminals.TerminalModels.TerminalDetail;
import dev.satfleet.fleetapi.terminals.TerminalModels.TerminalSummary;
import dev.satfleet.fleetapi.terminals.TerminalModels.TerminalV1Item;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

@Repository
public class TerminalRepository {

    private static final String SELECT = """
            SELECT t.id, t.serial_number, t.status, h.health, t.model, t.region, t.latitude, t.longitude,
                   t.activated_at, t.created_at,
                   c.id AS customer_id, c.name AS customer_name, c.segment, c.account_manager,
                   p.code AS plan_code, p.name AS plan_name, p.family, p.tier, p.downlink_mbps, p.uplink_mbps,
                   p.monthly_price_cents,
                   h.last_seen_at, h.link_state, h.latency_ms, h.jitter_ms, h.packet_loss_pct, h.downlink_mbps AS m_down,
                   h.uplink_mbps AS m_up, h.snr_db, h.obstruction_pct, h.firmware_version, h.transport
            FROM terminals t
            JOIN terminal_health_v h ON h.terminal_id = t.id
            JOIN customers c ON c.id = t.customer_id
            JOIN service_plans p ON p.code = t.plan_code
            """;

    private final JdbcClient jdbc;

    public TerminalRepository(JdbcClient jdbc) {
        this.jdbc = jdbc;
    }

    /** Builds a WHERE clause from the filters that are present; values are always bound, never inlined. */
    private static String where(Filter f, String afterId, Map<String, Object> params) {
        StringBuilder sql = new StringBuilder(" WHERE true");
        if (f.status() != null) {
            sql.append(" AND t.status = :status");
            params.put("status", f.status());
        }
        if (f.health() != null) {
            sql.append(" AND h.health = :health");
            params.put("health", f.health());
        }
        if (f.region() != null) {
            sql.append(" AND t.region = :region");
            params.put("region", f.region());
        }
        if (f.customerId() != null) {
            sql.append(" AND t.customer_id = :customerId");
            params.put("customerId", f.customerId());
        }
        if (f.q() != null) {
            sql.append(" AND (t.id ILIKE :q OR t.serial_number ILIKE :q OR c.name ILIKE :q)");
            params.put("q", "%" + f.q().replace("\\", "\\\\").replace("%", "\\%").replace("_", "\\_") + "%");
        }
        if (afterId != null) {
            sql.append(" AND t.id > :afterId");
            params.put("afterId", afterId);
        }
        return sql.toString();
    }

    public List<TerminalSummary> findPage(Filter f, String afterId, int limit) {
        Map<String, Object> params = new HashMap<>();
        String sql = SELECT + where(f, afterId, params) + " ORDER BY t.id LIMIT :limit";
        params.put("limit", limit);
        return jdbc.sql(sql).params(params).query((rs, i) -> summary(rs)).list();
    }

    public List<TerminalV1Item> findOffset(Filter f, int offset, int limit) {
        Map<String, Object> params = new HashMap<>();
        String sql = SELECT + where(f, null, params) + " ORDER BY t.id LIMIT :limit OFFSET :offset";
        params.put("limit", limit);
        params.put("offset", offset);
        return jdbc.sql(sql).params(params).query((rs, i) -> new TerminalV1Item(
                rs.getString("id"), rs.getLong("customer_id"), rs.getString("customer_name"),
                rs.getString("plan_code"), rs.getString("status"), rs.getString("health"), rs.getString("region"),
                instant(rs, "last_seen_at"), floatOrNull(rs, "latency_ms"))).list();
    }

    public long count(Filter f) {
        Map<String, Object> params = new HashMap<>();
        String sql = """
                SELECT count(*) FROM terminals t
                JOIN terminal_health_v h ON h.terminal_id = t.id
                JOIN customers c ON c.id = t.customer_id
                """ + where(f, null, params);
        return jdbc.sql(sql).params(params).query(Long.class).single();
    }

    public Optional<TerminalDetail> find(String id) {
        return jdbc.sql(SELECT + " WHERE t.id = :id").param("id", id).query((rs, i) -> detail(rs)).optional();
    }

    public Optional<String> findStatus(String id) {
        return jdbc.sql("SELECT status FROM terminals WHERE id = :id FOR UPDATE")
                .param("id", id).query(String.class).optional();
    }

    public void updateStatus(String id, String status) {
        jdbc.sql("""
                UPDATE terminals
                SET status = :status,
                    activated_at = CASE WHEN :status = 'active' THEN coalesce(activated_at, now()) ELSE activated_at END,
                    updated_at = now()
                WHERE id = :id
                """).param("id", id).param("status", status).update();
    }

    public void updatePlan(String id, String planCode) {
        jdbc.sql("UPDATE terminals SET plan_code = :plan, updated_at = now() WHERE id = :id")
                .param("id", id).param("plan", planCode).update();
    }

    /** Allocates the next fleet ID from the sequence and inserts the terminal plus default config. */
    public String insert(long customerId, Plan plan, String model, String region, double lat, double lon,
                         int reportIntervalMs, String qosProfile, String actor) {
        String id = jdbc.sql("SELECT 'SAT-' || lpad(nextval('terminal_number_seq')::text, 5, '0')")
                .query(String.class).single();
        jdbc.sql("""
                INSERT INTO terminals (id, serial_number, customer_id, plan_code, model, status, region, latitude, longitude)
                VALUES (:id, 'UT9-' || upper(substr(md5(random()::text), 1, 10)), :customerId, :plan, :model,
                        'provisioned', :region, :lat, :lon)
                """)
                .param("id", id).param("customerId", customerId).param("plan", plan.code())
                .param("model", model).param("region", region).param("lat", lat).param("lon", lon)
                .update();
        jdbc.sql("""
                INSERT INTO terminal_configs (terminal_id, report_interval_ms, qos_profile, updated_by)
                VALUES (:id, :interval, :qos, :actor)
                """)
                .param("id", id).param("interval", reportIntervalMs).param("qos", qosProfile).param("actor", actor)
                .update();
        return id;
    }

    static LiveMetrics metrics(ResultSet rs) throws SQLException {
        return new LiveMetrics(
                instant(rs, "last_seen_at"), linkState(rs, "link_state"), floatOrNull(rs, "latency_ms"),
                floatOrNull(rs, "jitter_ms"), floatOrNull(rs, "packet_loss_pct"), floatOrNull(rs, "m_down"),
                floatOrNull(rs, "m_up"), floatOrNull(rs, "snr_db"), floatOrNull(rs, "obstruction_pct"),
                rs.getString("firmware_version"), rs.getString("transport"));
    }

    static TerminalSummary summary(ResultSet rs) throws SQLException {
        return new TerminalSummary(
                rs.getString("id"), rs.getString("status"), rs.getString("health"), rs.getString("region"),
                new CustomerRef(rs.getLong("customer_id"), rs.getString("customer_name")),
                new PlanRef(rs.getString("plan_code"), rs.getString("plan_name")),
                new Location(rs.getDouble("latitude"), rs.getDouble("longitude")),
                metrics(rs));
    }

    private static TerminalDetail detail(ResultSet rs) throws SQLException {
        return new TerminalDetail(
                rs.getString("id"), rs.getString("serial_number"), rs.getString("status"), rs.getString("health"),
                rs.getString("model"), rs.getString("region"), rs.getDouble("latitude"), rs.getDouble("longitude"),
                instant(rs, "activated_at"), instant(rs, "created_at"),
                new CustomerInfo(rs.getLong("customer_id"), rs.getString("customer_name"), rs.getString("segment"),
                        rs.getString("account_manager")),
                new Plan(rs.getString("plan_code"), rs.getString("plan_name"), rs.getString("family"), rs.getInt("tier"),
                        rs.getInt("downlink_mbps"), rs.getInt("uplink_mbps"), rs.getInt("monthly_price_cents")),
                metrics(rs));
    }
}
