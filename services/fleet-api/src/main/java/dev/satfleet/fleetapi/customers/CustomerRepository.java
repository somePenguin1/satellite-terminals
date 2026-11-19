package dev.satfleet.fleetapi.customers;

import java.time.Instant;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

@Repository
public class CustomerRepository {

    public record CustomerSummary(
            long id,
            String name,
            String segment,
            String region,
            String accountManager,
            long terminals,
            long activeTerminals,
            long mrrCents) {}

    public record CustomerRecord(
            long id, String name, String segment, String region, String accountManager, String contactEmail,
            Instant createdAt) {}

    private static final String SUMMARY = """
            SELECT c.id, c.name, c.segment, c.region, c.account_manager,
                   count(t.id) AS terminals,
                   count(t.id) FILTER (WHERE t.status = 'active') AS active_terminals,
                   coalesce(sum(p.monthly_price_cents) FILTER (WHERE t.status = 'active'), 0) AS mrr_cents
            FROM customers c
            LEFT JOIN terminals t ON t.customer_id = c.id
            LEFT JOIN service_plans p ON p.code = t.plan_code
            """;

    private final JdbcClient jdbc;

    public CustomerRepository(JdbcClient jdbc) {
        this.jdbc = jdbc;
    }

    private static String where(String q, String segment, Map<String, Object> params) {
        StringBuilder sql = new StringBuilder(" WHERE true");
        if (q != null) {
            sql.append(" AND c.name ILIKE :q");
            params.put("q", "%" + q.replace("\\", "\\\\").replace("%", "\\%").replace("_", "\\_") + "%");
        }
        if (segment != null) {
            sql.append(" AND c.segment = :segment");
            params.put("segment", segment);
        }
        return sql.toString();
    }

    public List<CustomerSummary> list(String q, String segment, int offset, int limit) {
        Map<String, Object> params = new HashMap<>();
        String sql = SUMMARY + where(q, segment, params)
                + " GROUP BY c.id ORDER BY mrr_cents DESC, c.name LIMIT :limit OFFSET :offset";
        params.put("limit", limit);
        params.put("offset", offset);
        return jdbc.sql(sql).params(params).query(CustomerSummary.class).list();
    }

    public long count(String q, String segment) {
        Map<String, Object> params = new HashMap<>();
        return jdbc.sql("SELECT count(*) FROM customers c" + where(q, segment, params))
                .params(params).query(Long.class).single();
    }

    public Optional<CustomerSummary> findSummary(long id) {
        return jdbc.sql(SUMMARY + " WHERE c.id = :id GROUP BY c.id").param("id", id)
                .query(CustomerSummary.class).optional();
    }

    public Optional<CustomerRecord> find(long id) {
        return jdbc.sql("""
                SELECT id, name, segment, region, account_manager, contact_email, created_at
                FROM customers WHERE id = :id
                """).param("id", id).query(CustomerRecord.class).optional();
    }
}
