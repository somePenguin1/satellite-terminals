package dev.satfleet.fleetapi.alerts;

/**
 * Alert conditions, evaluated in SQL against {@code terminal_health_v}. Predicates and value
 * expressions are compile-time constants, never user input, so building SQL from them is safe.
 * Only active terminals alert: a suspended or not-yet-installed terminal being quiet is expected.
 */
public enum AlertRule {
    OFFLINE("critical", "No telemetry received recently; the terminal may be powered off or unreachable",
            "h.status = 'active' AND h.health = 'offline'",
            "extract(epoch FROM now() - h.last_seen_at)"),
    LINK_DOWN("critical", "The terminal reports its satellite link is down",
            "h.status = 'active' AND h.health = 'down'",
            "h.snr_db"),
    HIGH_LATENCY("warning", "Round-trip latency above 150 ms",
            "h.status = 'active' AND h.health IN ('healthy', 'degraded') AND h.latency_ms > 150",
            "h.latency_ms"),
    PACKET_LOSS("warning", "Packet loss above 5%",
            "h.status = 'active' AND h.health IN ('healthy', 'degraded') AND h.packet_loss_pct > 5",
            "h.packet_loss_pct"),
    LOW_SNR("warning", "Signal-to-noise ratio below 5 dB (rain fade or misalignment)",
            "h.status = 'active' AND h.health IN ('healthy', 'degraded') AND h.snr_db < 5",
            "h.snr_db");

    final String severity;
    final String message;
    final String predicate;
    final String valueExpr;

    AlertRule(String severity, String message, String predicate, String valueExpr) {
        this.severity = severity;
        this.message = message;
        this.predicate = predicate;
        this.valueExpr = valueExpr;
    }

    String openSql() {
        return """
                INSERT INTO alerts (terminal_id, kind, severity, message, observed_value)
                SELECT h.terminal_id, '%s', '%s', '%s', %s
                FROM terminal_health_v h
                WHERE %s
                ON CONFLICT (terminal_id, kind) WHERE resolved_at IS NULL
                DO UPDATE SET last_observed_at = now(), observed_value = EXCLUDED.observed_value
                """.formatted(name(), severity, message.replace("'", "''"), valueExpr, predicate);
    }

    String resolveSql() {
        return """
                UPDATE alerts a SET resolved_at = now()
                WHERE a.kind = '%s' AND a.resolved_at IS NULL
                  AND NOT EXISTS (SELECT 1 FROM terminal_health_v h WHERE h.terminal_id = a.terminal_id AND %s)
                """.formatted(name(), predicate);
    }
}
