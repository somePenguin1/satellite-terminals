package dev.satfleet.fleetapi.sales;

import dev.satfleet.fleetapi.config.FleetProperties;
import io.swagger.v3.oas.annotations.Operation;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import java.util.List;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

/** Commercial read models: recurring revenue and data-driven upgrade opportunities. */
@RestController
@RequestMapping("/api/v1/sales")
public class SalesController {

    public record PlanRevenue(String code, String name, String family, long terminals, long mrrCents) {}

    public record RegionRevenue(String region, long terminals, long mrrCents) {}

    public record SegmentRevenue(String segment, long customers, long terminals, long mrrCents) {}

    public record SalesSummary(
            long mrrCents,
            long activeSubscriptions,
            long arpuCents,
            long provisioningBacklog,
            long suspended,
            List<PlanRevenue> byPlan,
            List<RegionRevenue> byRegion,
            List<SegmentRevenue> bySegment) {}

    public record PlanRef(String code, String name, int downlinkMbps, int monthlyPriceCents) {}

    public record UpsellCandidate(
            String terminalId,
            long customerId,
            String customerName,
            String accountManager,
            PlanRef currentPlan,
            PlanRef suggestedPlan,
            double avgUtilizationPct,
            double peakDownlinkMbps,
            long monthlyDeltaCents) {}

    public record Totals(long active, long mrr, long backlog, long suspended) {}

    private final JdbcClient jdbc;
    private final FleetProperties props;

    public SalesController(JdbcClient jdbc, FleetProperties props) {
        this.jdbc = jdbc;
        this.props = props;
    }

    @Operation(summary = "Monthly recurring revenue from active terminals, by plan, region and segment")
    @GetMapping("/summary")
    public SalesSummary summary() {
        Totals t = jdbc.sql("""
                SELECT count(*) FILTER (WHERE t.status = 'active') AS active,
                       coalesce(sum(p.monthly_price_cents) FILTER (WHERE t.status = 'active'), 0) AS mrr,
                       count(*) FILTER (WHERE t.status = 'provisioned') AS backlog,
                       count(*) FILTER (WHERE t.status = 'suspended') AS suspended
                FROM terminals t JOIN service_plans p ON p.code = t.plan_code
                """).query(Totals.class).single();
        List<PlanRevenue> byPlan = jdbc.sql("""
                SELECT p.code, p.name, p.family,
                       count(t.id) FILTER (WHERE t.status = 'active') AS terminals,
                       coalesce(sum(p.monthly_price_cents) FILTER (WHERE t.status = 'active'), 0) AS mrr_cents
                FROM service_plans p LEFT JOIN terminals t ON t.plan_code = p.code
                GROUP BY p.code ORDER BY mrr_cents DESC
                """).query(PlanRevenue.class).list();
        List<RegionRevenue> byRegion = jdbc.sql("""
                SELECT t.region, count(*) AS terminals, sum(p.monthly_price_cents) AS mrr_cents
                FROM terminals t JOIN service_plans p ON p.code = t.plan_code
                WHERE t.status = 'active'
                GROUP BY t.region ORDER BY mrr_cents DESC
                """).query(RegionRevenue.class).list();
        List<SegmentRevenue> bySegment = jdbc.sql("""
                SELECT c.segment, count(DISTINCT c.id) AS customers, count(t.id) AS terminals,
                       sum(p.monthly_price_cents) AS mrr_cents
                FROM customers c
                JOIN terminals t ON t.customer_id = c.id AND t.status = 'active'
                JOIN service_plans p ON p.code = t.plan_code
                GROUP BY c.segment ORDER BY mrr_cents DESC
                """).query(SegmentRevenue.class).list();
        long arpu = t.active() == 0 ? 0 : Math.round((double) t.mrr() / t.active());
        return new SalesSummary(t.mrr(), t.active(), arpu, t.backlog(), t.suspended(), byPlan, byRegion, bySegment);
    }

    @Operation(summary = "Active terminals consistently near their plan's downlink limit, with the next plan up")
    @GetMapping("/upsell-candidates")
    public List<UpsellCandidate> upsell(@RequestParam(defaultValue = "25") @Min(1) @Max(200) int limit) {
        return jdbc.sql("""
                WITH usage AS (
                    SELECT terminal_id,
                           sum(sum_downlink_mbps) / nullif(sum(samples), 0) AS avg_down,
                           max(max_downlink_mbps) AS peak_down
                    FROM terminal_usage_hourly
                    WHERE hour >= date_trunc('hour', now()) - interval '24 hours'
                    GROUP BY terminal_id
                    HAVING sum(samples) >= 30
                )
                SELECT t.id, c.id AS customer_id, c.name AS customer_name, c.account_manager,
                       cur.code AS cur_code, cur.name AS cur_name, cur.downlink_mbps AS cur_down,
                       cur.monthly_price_cents AS cur_price,
                       nxt.code AS nxt_code, nxt.name AS nxt_name, nxt.downlink_mbps AS nxt_down,
                       nxt.monthly_price_cents AS nxt_price,
                       100.0 * u.avg_down / cur.downlink_mbps AS utilization, u.peak_down
                FROM usage u
                JOIN terminals t ON t.id = u.terminal_id AND t.status = 'active'
                JOIN customers c ON c.id = t.customer_id
                JOIN service_plans cur ON cur.code = t.plan_code
                JOIN service_plans nxt ON nxt.family = cur.family AND nxt.tier = cur.tier + 1
                WHERE u.avg_down / cur.downlink_mbps >= :threshold
                ORDER BY (nxt.monthly_price_cents - cur.monthly_price_cents) * u.avg_down / cur.downlink_mbps DESC, t.id
                LIMIT :limit
                """)
                .param("threshold", props.upsell().utilizationThreshold())
                .param("limit", limit)
                .query((rs, i) -> new UpsellCandidate(
                        rs.getString("id"), rs.getLong("customer_id"), rs.getString("customer_name"),
                        rs.getString("account_manager"),
                        new PlanRef(rs.getString("cur_code"), rs.getString("cur_name"), rs.getInt("cur_down"),
                                rs.getInt("cur_price")),
                        new PlanRef(rs.getString("nxt_code"), rs.getString("nxt_name"), rs.getInt("nxt_down"),
                                rs.getInt("nxt_price")),
                        rs.getDouble("utilization"), rs.getDouble("peak_down"),
                        rs.getInt("nxt_price") - rs.getInt("cur_price")))
                .list();
    }
}
