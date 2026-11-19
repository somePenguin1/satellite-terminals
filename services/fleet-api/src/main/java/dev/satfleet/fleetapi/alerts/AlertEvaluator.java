package dev.satfleet.fleetapi.alerts;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;
import org.springframework.transaction.annotation.Transactional;

/**
 * Opens and resolves alerts from the live health view. Runs on every API replica, but a
 * transaction-scoped advisory lock means only one replica evaluates per cycle.
 */
@Component
public class AlertEvaluator {

    private static final Logger log = LoggerFactory.getLogger(AlertEvaluator.class);
    private static final long LOCK_KEY = 0x5A7F1EE7L;

    public record Result(boolean ran, int opened, int resolved) {}

    private final JdbcClient jdbc;

    public AlertEvaluator(JdbcClient jdbc) {
        this.jdbc = jdbc;
    }

    @Scheduled(initialDelayString = "20s", fixedDelayString = "${fleet.alerts.evaluation-interval:15s}")
    public void scheduled() {
        Result r = evaluate();
        if (r.opened() > 0 || r.resolved() > 0) {
            log.info("alerts evaluated: {} opened or refreshed, {} resolved", r.opened(), r.resolved());
        }
    }

    @Transactional
    public Result evaluate() {
        Boolean locked = jdbc.sql("SELECT pg_try_advisory_xact_lock(:key)")
                .param("key", LOCK_KEY).query(Boolean.class).single();
        if (!Boolean.TRUE.equals(locked)) {
            return new Result(false, 0, 0); // another replica is evaluating
        }
        int opened = 0;
        int resolved = 0;
        for (AlertRule rule : AlertRule.values()) {
            resolved += jdbc.sql(rule.resolveSql()).update();
            opened += jdbc.sql(rule.openSql()).update();
        }
        return new Result(true, opened, resolved);
    }
}
