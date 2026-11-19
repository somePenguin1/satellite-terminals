package dev.satfleet.fleetapi;

import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.DynamicPropertyRegistry;
import org.springframework.test.context.DynamicPropertySource;
import org.springframework.transaction.annotation.Transactional;
import org.testcontainers.containers.PostgreSQLContainer;

/**
 * Base for integration tests against a real Postgres with the real Flyway migrations.
 * Uses Testcontainers by default; set TEST_DATABASE_URL (a JDBC URL) to reuse an existing
 * database instead, e.g. a CI service container. Each test runs in a transaction that is
 * rolled back, so tests can't leak state into each other.
 */
@SpringBootTest(properties = "fleet.scheduling.enabled=false")
@AutoConfigureMockMvc
@Transactional
public abstract class IntegrationTest {

    private static PostgreSQLContainer<?> postgres;

    @DynamicPropertySource
    static void database(DynamicPropertyRegistry registry) {
        String url = System.getenv("TEST_DATABASE_URL");
        if (url != null && !url.isBlank()) {
            registry.add("spring.datasource.url", () -> url);
            registry.add("spring.datasource.username", () -> envOr("TEST_DATABASE_USER", "fleet"));
            registry.add("spring.datasource.password", () -> envOr("TEST_DATABASE_PASSWORD", "fleet"));
            return;
        }
        synchronized (IntegrationTest.class) {
            if (postgres == null) {
                postgres = new PostgreSQLContainer<>("postgres:16-alpine");
                postgres.start(); // shared by all test classes; Ryuk removes it when the JVM exits
            }
        }
        registry.add("spring.datasource.url", postgres::getJdbcUrl);
        registry.add("spring.datasource.username", postgres::getUsername);
        registry.add("spring.datasource.password", postgres::getPassword);
    }

    private static String envOr(String key, String fallback) {
        String v = System.getenv(key);
        return v == null || v.isBlank() ? fallback : v;
    }
}
