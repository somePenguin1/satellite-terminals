package dev.satfleet.fleetapi.alerts;

import static dev.satfleet.fleetapi.common.ApiExceptions.conflict;
import static dev.satfleet.fleetapi.common.ApiExceptions.notFound;
import static dev.satfleet.fleetapi.common.Rows.doubleOrNull;
import static dev.satfleet.fleetapi.common.Rows.instant;

import jakarta.validation.Valid;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.Pattern;
import jakarta.validation.constraints.Size;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.time.Instant;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/api/v1/alerts")
public class AlertController {

    public record Alert(
            long id,
            String terminalId,
            String customerName,
            String region,
            String kind,
            String severity,
            String message,
            Double observedValue,
            Instant openedAt,
            Instant lastObservedAt,
            Instant acknowledgedAt,
            String acknowledgedBy,
            String ackNote,
            Instant resolvedAt) {}

    public record Acknowledgement(@NotBlank @Size(max = 64) String by, @Size(max = 500) String note) {}

    private static final String SELECT = """
            SELECT a.id, a.terminal_id, c.name AS customer_name, t.region, a.kind, a.severity, a.message,
                   a.observed_value, a.opened_at, a.last_observed_at, a.acknowledged_at, a.acknowledged_by,
                   a.ack_note, a.resolved_at
            FROM alerts a
            JOIN terminals t ON t.id = a.terminal_id
            JOIN customers c ON c.id = t.customer_id
            """;

    private final JdbcClient jdbc;

    public AlertController(JdbcClient jdbc) {
        this.jdbc = jdbc;
    }

    private static Alert map(ResultSet rs, int i) throws SQLException {
        return new Alert(rs.getLong("id"), rs.getString("terminal_id"), rs.getString("customer_name"),
                rs.getString("region"), rs.getString("kind"), rs.getString("severity"), rs.getString("message"),
                doubleOrNull(rs, "observed_value"), instant(rs, "opened_at"), instant(rs, "last_observed_at"),
                instant(rs, "acknowledged_at"), rs.getString("acknowledged_by"), rs.getString("ack_note"),
                instant(rs, "resolved_at"));
    }

    /** Open alerts are ordered for triage: unacknowledged first, then critical, then newest. */
    @GetMapping
    public List<Alert> list(
            @RequestParam(defaultValue = "open") @Pattern(regexp = "open|resolved|all") String state,
            @RequestParam(required = false) @Pattern(regexp = "critical|warning") String severity,
            @RequestParam(required = false) String terminalId,
            @RequestParam(defaultValue = "50") @Min(1) @Max(500) int limit) {
        Map<String, Object> params = new HashMap<>();
        StringBuilder sql = new StringBuilder(SELECT).append(" WHERE true");
        if (state.equals("open")) {
            sql.append(" AND a.resolved_at IS NULL");
        } else if (state.equals("resolved")) {
            sql.append(" AND a.resolved_at IS NOT NULL");
        }
        if (severity != null) {
            sql.append(" AND a.severity = :severity");
            params.put("severity", severity);
        }
        if (terminalId != null && !terminalId.isBlank()) {
            sql.append(" AND a.terminal_id = :terminalId");
            params.put("terminalId", terminalId);
        }
        sql.append("""
                 ORDER BY (a.resolved_at IS NULL) DESC, (a.acknowledged_at IS NULL) DESC,
                          (a.severity = 'critical') DESC, a.opened_at DESC
                 LIMIT :limit
                """);
        params.put("limit", limit);
        return jdbc.sql(sql.toString()).params(params).query(AlertController::map).list();
    }

    private Optional<Alert> find(long id) {
        return jdbc.sql(SELECT + " WHERE a.id = :id").param("id", id).query(AlertController::map).optional();
    }

    /** Idempotent: acknowledging an already-acknowledged alert returns it unchanged. */
    @PostMapping("/{id}/acknowledge")
    @Transactional
    public Alert acknowledge(@PathVariable long id, @Valid @RequestBody Acknowledgement ack) {
        Alert alert = find(id).orElseThrow(() -> notFound("Alert " + id));
        if (alert.resolvedAt() != null) {
            throw conflict("Alert " + id + " has already resolved");
        }
        if (alert.acknowledgedAt() == null) {
            jdbc.sql("""
                    UPDATE alerts SET acknowledged_at = now(), acknowledged_by = :by, ack_note = :note
                    WHERE id = :id AND acknowledged_at IS NULL
                    """).param("id", id).param("by", ack.by()).param("note", ack.note()).update();
        }
        return find(id).orElseThrow();
    }
}
