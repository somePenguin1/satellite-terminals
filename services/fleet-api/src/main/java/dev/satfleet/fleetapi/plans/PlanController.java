package dev.satfleet.fleetapi.plans;

import java.util.List;
import java.util.Optional;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/api/v1/plans")
public class PlanController {

    private final PlanRepository plans;

    public PlanController(PlanRepository plans) {
        this.plans = plans;
    }

    @GetMapping
    public List<Plan> list() {
        return plans.findAll();
    }

    @Repository
    public static class PlanRepository {

        private static final String COLUMNS =
                "code, name, family, tier, downlink_mbps, uplink_mbps, monthly_price_cents";

        private final JdbcClient jdbc;

        public PlanRepository(JdbcClient jdbc) {
            this.jdbc = jdbc;
        }

        public List<Plan> findAll() {
            return jdbc.sql("SELECT " + COLUMNS + " FROM service_plans ORDER BY family, tier")
                    .query(Plan.class).list();
        }

        public Optional<Plan> find(String code) {
            return jdbc.sql("SELECT " + COLUMNS + " FROM service_plans WHERE code = :code")
                    .param("code", code).query(Plan.class).optional();
        }
    }
}
