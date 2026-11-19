package dev.satfleet.fleetapi;

import static org.assertj.core.api.Assertions.assertThat;
import static org.hamcrest.Matchers.everyItem;
import static org.hamcrest.Matchers.greaterThan;
import static org.hamcrest.Matchers.hasItem;
import static org.hamcrest.Matchers.hasSize;
import static org.hamcrest.Matchers.is;
import static org.hamcrest.Matchers.matchesPattern;
import static org.hamcrest.Matchers.notNullValue;
import static org.hamcrest.Matchers.nullValue;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.patch;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.put;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.header;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import dev.satfleet.fleetapi.alerts.AlertEvaluator;
import dev.satfleet.fleetapi.maintenance.TelemetryMaintenance;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.http.MediaType;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.test.web.servlet.MockMvc;

/**
 * End-to-end tests of the REST API over the real schema and seed data
 * (5,000 terminals, 800 customers, 7 plans).
 */
class FleetApiIT extends IntegrationTest {

    @Autowired
    MockMvc mvc;

    @Autowired
    JdbcClient jdbc;

    @Autowired
    ObjectMapper json;

    @Autowired
    AlertEvaluator alerts;

    @Autowired
    TelemetryMaintenance maintenance;

    private JsonNode body(org.springframework.test.web.servlet.ResultActions r) throws Exception {
        return json.readTree(r.andReturn().getResponse().getContentAsString());
    }

    private void reportStatus(String terminalId, int linkState, float latency, float snr) {
        jdbc.sql("""
                INSERT INTO terminal_status (terminal_id, last_seen_at, last_seq, link_state, latency_ms, snr_db,
                                             packet_loss_pct, transport)
                VALUES (:id, now(), 1, :state, :latency, :snr, 0.1, 'grpc')
                ON CONFLICT (terminal_id) DO UPDATE SET link_state = EXCLUDED.link_state,
                    latency_ms = EXCLUDED.latency_ms, snr_db = EXCLUDED.snr_db, last_seen_at = now()
                """).param("id", terminalId).param("state", linkState).param("latency", latency).param("snr", snr)
                .update();
    }

    // ---------- fleet read models ----------

    @Test
    void plansAndSummaryReflectSeedData() throws Exception {
        mvc.perform(get("/api/v1/plans")).andExpect(status().isOk()).andExpect(jsonPath("$", hasSize(7)));
        mvc.perform(get("/api/v1/fleet/summary"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.total").value(5000))
                .andExpect(jsonPath("$.byStatus.active").value(4751))
                .andExpect(jsonPath("$.byStatus.provisioned").value(200))
                .andExpect(jsonPath("$.byHealth.inactive").value(200))
                .andExpect(jsonPath("$.regions", hasSize(7)));
    }

    @Test
    void mapIsColumnarAndCoversTheFleet() throws Exception {
        JsonNode map = body(mvc.perform(get("/api/v1/fleet/map")).andExpect(status().isOk()));
        assertThat(map.get("ids")).hasSize(5000);
        assertThat(map.get("lat")).hasSize(5000);
        assertThat(map.get("health").get(0).asText()).isIn("healthy", "degraded", "down", "offline", "inactive");
    }

    @Test
    void timeseriesReadsRollups() throws Exception {
        jdbc.sql("""
                INSERT INTO fleet_minute_stats VALUES
                    (date_trunc('minute', now()) - interval '2 minutes', 'Europe', 100, 90, 10, 3600, 9, 5000, 500)
                """).update();
        mvc.perform(get("/api/v1/fleet/timeseries?minutes=10"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$", hasSize(1)))
                .andExpect(jsonPath("$[0].avgLatencyMs").value(40.0))
                .andExpect(jsonPath("$[0].degradedPct").value(10.0));
    }

    @Test
    void terminalTelemetryIsBucketed() throws Exception {
        jdbc.sql("""
                INSERT INTO telemetry (terminal_id, ts, seq, link_state, latency_ms, downlink_mbps, snr_db)
                SELECT 'SAT-00001', now() - (g || ' seconds')::interval, g, 1, 40 + g % 5, 80, 11
                FROM generate_series(1, 300, 5) g
                """).update();
        mvc.perform(get("/api/v1/terminals/SAT-00001/telemetry?minutes=10&bucketSeconds=60"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$", hasSize(greaterThan(4))))
                .andExpect(jsonPath("$[0].latencyAvg", notNullValue()))
                .andExpect(jsonPath("$[0].downlinkAvg").value(80.0));
        mvc.perform(get("/api/v1/terminals/SAT-99999/telemetry")).andExpect(status().isNotFound());
    }

    @Test
    void maintenanceKeepsTodaysPartition() {
        maintenance.run();
        Boolean exists = jdbc.sql("SELECT to_regclass('telemetry_' || to_char(now() AT TIME ZONE 'UTC', 'YYYYMMDD')) IS NOT NULL")
                .query(Boolean.class).single();
        assertThat(exists).isTrue();
    }

    // ---------- terminal listing ----------

    @Test
    void v2CursorPaginationWalksTheFleetInOrder() throws Exception {
        JsonNode first = body(mvc.perform(get("/api/v2/terminals?limit=50")).andExpect(status().isOk()));
        assertThat(first.get("items")).hasSize(50);
        assertThat(first.get("total").asLong()).isEqualTo(5000);
        String cursor = first.get("nextCursor").asText();
        String lastId = first.get("items").get(49).get("id").asText();

        JsonNode second = body(mvc.perform(get("/api/v2/terminals?limit=50&cursor=" + cursor)));
        assertThat(second.get("items").get(0).get("id").asText()).isGreaterThan(lastId);
        assertThat(second.get("items").get(0).get("customer").get("name").asText()).isNotBlank();
        assertThat(second.get("items").get(0).has("metrics")).isTrue();
    }

    @Test
    void v2FiltersCombine() throws Exception {
        mvc.perform(get("/api/v2/terminals?region=Maritime&status=active&limit=500"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.items[*].region", everyItem(is("Maritime"))))
                .andExpect(jsonPath("$.items[*].status", everyItem(is("active"))));
        mvc.perform(get("/api/v2/terminals?q=SAT-0004"))
                .andExpect(jsonPath("$.total").value(10))
                .andExpect(jsonPath("$.nextCursor").value(nullValue()));
        mvc.perform(get("/api/v2/terminals?cursor=garbage")).andExpect(status().isBadRequest());
    }

    @Test
    void v1KeepsFlatOffsetPagination() throws Exception {
        mvc.perform(get("/api/v1/terminals?limit=10&offset=20"))
                .andExpect(status().isOk())
                .andExpect(header().string("X-Total-Count", "5000"))
                .andExpect(jsonPath("$", hasSize(10)))
                .andExpect(jsonPath("$[0].id").value("SAT-00021"))
                .andExpect(jsonPath("$[0].customerName", notNullValue()));
    }

    @Test
    void unknownTerminalIsA404Problem() throws Exception {
        mvc.perform(get("/api/v1/terminals/SAT-99999"))
                .andExpect(status().isNotFound())
                .andExpect(jsonPath("$.status").value(404))
                .andExpect(jsonPath("$.detail").value("Terminal SAT-99999 not found"));
    }

    // ---------- device configuration ----------

    private static final String V2_BODY = """
            {"reporting":{"intervalSeconds":2,"level":"detailed"},"radio":{"band":"Ku","txPowerDbm":26},
             "network":{"qosProfile":"priority"},
             "firmware":{"channel":"stable","maintenanceWindow":{"start":"01:00","end":"03:00"}}}
            """;

    @Test
    void optimisticConcurrencyAcrossVersions() throws Exception {
        mvc.perform(get("/api/v1/terminals/SAT-00001/config"))
                .andExpect(status().isOk())
                .andExpect(header().string("ETag", "\"1\""))
                .andExpect(jsonPath("$.reportIntervalMs").value(5000))
                .andExpect(jsonPath("$.downlinkCapMbps").value(100));
        mvc.perform(get("/api/v1/terminals/SAT-00001/config").header("If-None-Match", "\"1\""))
                .andExpect(status().isNotModified());

        mvc.perform(put("/api/v2/terminals/SAT-00001/config").contentType(MediaType.APPLICATION_JSON).content(V2_BODY))
                .andExpect(status().isPreconditionRequired());

        mvc.perform(put("/api/v2/terminals/SAT-00001/config")
                        .header("If-Match", "\"1\"").header("X-Actor", "test-operator")
                        .contentType(MediaType.APPLICATION_JSON).content(V2_BODY))
                .andExpect(status().isOk())
                .andExpect(header().string("ETag", "\"2\""))
                .andExpect(jsonPath("$.revision").value(2))
                .andExpect(jsonPath("$.reporting.intervalSeconds").value(2.0))
                .andExpect(jsonPath("$.metadata.updatedBy").value("test-operator"));

        // A second editor still holding revision 1 must not overwrite revision 2.
        mvc.perform(put("/api/v2/terminals/SAT-00001/config")
                        .header("If-Match", "\"1\"").contentType(MediaType.APPLICATION_JSON).content(V2_BODY))
                .andExpect(status().isPreconditionFailed());

        // v1 reads what v2 wrote, in its own units.
        mvc.perform(get("/api/v1/terminals/SAT-00001/config"))
                .andExpect(jsonPath("$.reportIntervalMs").value(2000))
                .andExpect(jsonPath("$.maintenanceWindowStart").value("01:00"));

        mvc.perform(get("/api/v1/terminals/SAT-00001/config/history"))
                .andExpect(jsonPath("$", hasSize(2)))
                .andExpect(jsonPath("$[0].version").value(2))
                .andExpect(jsonPath("$[0].changedBy").value("test-operator"))
                .andExpect(jsonPath("$[0].config.report_interval_ms").value(2000));
    }

    @Test
    void validationErrorsNameTheField() throws Exception {
        mvc.perform(put("/api/v2/terminals/SAT-00001/config")
                        .header("If-Match", "\"1\"").contentType(MediaType.APPLICATION_JSON)
                        .content(V2_BODY.replace("\"start\":\"01:00\"", "\"start\":\"25:00\"")))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errors['firmware.maintenanceWindow.start']").value("Use 24-hour HH:mm"));
    }

    @Test
    void bulkExportPagesByTerminalId() throws Exception {
        mvc.perform(get("/api/v1/terminal-configs?limit=1000"))
                .andExpect(jsonPath("$", hasSize(1000)))
                .andExpect(jsonPath("$[0].terminalId").value("SAT-00001"))
                .andExpect(jsonPath("$[0].downlinkCapMbps").value(100));
        mvc.perform(get("/api/v1/terminal-configs?limit=10&afterId=SAT-01000"))
                .andExpect(jsonPath("$[0].terminalId").value("SAT-01001"));
    }

    // ---------- lifecycle ----------

    @Test
    void activateChangePlanAndDecommission() throws Exception {
        // SAT-00025 is seeded as an order awaiting installation for a residential customer.
        mvc.perform(patch("/api/v1/terminals/SAT-00025").contentType(MediaType.APPLICATION_JSON)
                        .content("{\"status\":\"active\"}"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.status").value("active"))
                .andExpect(jsonPath("$.activatedAt", notNullValue()));

        mvc.perform(patch("/api/v1/terminals/SAT-00025").contentType(MediaType.APPLICATION_JSON)
                        .content("{\"status\":\"provisioned\"}"))
                .andExpect(status().isConflict());

        mvc.perform(patch("/api/v1/terminals/SAT-00025").contentType(MediaType.APPLICATION_JSON)
                        .content("{\"planCode\":\"MAR-GLB\"}"))
                .andExpect(status().isBadRequest());
        mvc.perform(patch("/api/v1/terminals/SAT-00025").contentType(MediaType.APPLICATION_JSON)
                        .content("{\"planCode\":\"RES-PRI\"}"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.plan.code").value("RES-PRI"));

        mvc.perform(patch("/api/v1/terminals/SAT-00025").contentType(MediaType.APPLICATION_JSON)
                        .content("{\"status\":\"decommissioned\"}"))
                .andExpect(status().isOk());
        mvc.perform(patch("/api/v1/terminals/SAT-00025").contentType(MediaType.APPLICATION_JSON)
                        .content("{\"status\":\"active\"}"))
                .andExpect(status().isConflict());
        mvc.perform(put("/api/v1/terminals/SAT-00025/config").header("If-Match", "\"1\"")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content("""
                                {"reportIntervalMs":5000,"telemetryLevel":"basic","band":"Ku","txPowerDbm":27,
                                 "qosProfile":"standard","firmwareChannel":"stable",
                                 "maintenanceWindowStart":"02:00","maintenanceWindowEnd":"04:00"}
                                """))
                .andExpect(status().isConflict());
    }

    @Test
    void provisionCreatesTerminalAndDefaultConfig() throws Exception {
        JsonNode created = body(mvc.perform(post("/api/v1/terminals").contentType(MediaType.APPLICATION_JSON)
                        .header("X-Actor", "sales-rep")
                        .content("""
                                {"customerId":601,"planCode":"MAR-CST","model":"Maritime Stabilized Ku",
                                 "latitude":12.5,"longitude":-40.25}
                                """))
                .andExpect(status().isCreated())
                .andExpect(header().string("Location", matchesPattern("/api/v1/terminals/SAT-\\d{5,}")))
                .andExpect(jsonPath("$.status").value("provisioned"))
                .andExpect(jsonPath("$.region").value("Maritime")));
        String id = created.get("id").asText();
        mvc.perform(get("/api/v1/terminals/" + id + "/config"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.reportIntervalMs").value(10000))
                .andExpect(jsonPath("$.updatedBy").value("sales-rep"));
    }

    @Test
    void provisionValidatesInput() throws Exception {
        mvc.perform(post("/api/v1/terminals").contentType(MediaType.APPLICATION_JSON)
                        .content("{\"customerId\":1,\"planCode\":\"RES-STD\",\"model\":\"Kit\",\"latitude\":120,\"longitude\":0}"))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errors.latitude").value("Latitude must be between -90 and 90"));
        mvc.perform(post("/api/v1/terminals").contentType(MediaType.APPLICATION_JSON)
                        .content("{\"customerId\":1,\"planCode\":\"ENT-MAX\",\"model\":\"Kit\",\"latitude\":10,\"longitude\":0}"))
                .andExpect(status().isBadRequest());
    }

    // ---------- alerts ----------

    @Test
    void linkDownOpensAcknowledgesAndResolves() throws Exception {
        reportStatus("SAT-00001", 3, 0, 1.2f);
        assertThat(alerts.evaluate().ran()).isTrue();

        JsonNode open = body(mvc.perform(get("/api/v1/alerts?terminalId=SAT-00001")).andExpect(status().isOk()));
        assertThat(open).hasSize(1);
        assertThat(open.get(0).get("kind").asText()).isEqualTo("LINK_DOWN");
        assertThat(open.get(0).get("severity").asText()).isEqualTo("critical");
        long id = open.get(0).get("id").asLong();

        mvc.perform(post("/api/v1/alerts/" + id + "/acknowledge").contentType(MediaType.APPLICATION_JSON)
                        .content("{\"by\":\"dana\",\"note\":\"Rain over the site\"}"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.acknowledgedBy").value("dana"));

        reportStatus("SAT-00001", 1, 38, 11);
        alerts.evaluate();
        mvc.perform(get("/api/v1/alerts?state=resolved&terminalId=SAT-00001"))
                .andExpect(jsonPath("$[*].id", hasItem((int) id)));
        mvc.perform(post("/api/v1/alerts/" + id + "/acknowledge").contentType(MediaType.APPLICATION_JSON)
                        .content("{\"by\":\"dana\"}"))
                .andExpect(status().isConflict());
    }

    @Test
    void silentActiveTerminalsRaiseOfflineButOrdersDoNot() throws Exception {
        alerts.evaluate();
        Long offlineForOrders = jdbc.sql("""
                SELECT count(*) FROM alerts a JOIN terminals t ON t.id = a.terminal_id
                WHERE a.kind = 'OFFLINE' AND t.status <> 'active'
                """).query(Long.class).single();
        Long offline = jdbc.sql("SELECT count(*) FROM alerts WHERE kind = 'OFFLINE' AND resolved_at IS NULL")
                .query(Long.class).single();
        assertThat(offlineForOrders).isZero();
        assertThat(offline).isGreaterThan(4000L); // nothing has reported in the test database
    }

    // ---------- sales ----------

    @Test
    void summaryCountsActiveSubscriptions() throws Exception {
        mvc.perform(get("/api/v1/sales/summary"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.activeSubscriptions").value(4751))
                .andExpect(jsonPath("$.provisioningBacklog").value(200))
                .andExpect(jsonPath("$.mrrCents", greaterThan(0)))
                .andExpect(jsonPath("$.byPlan", hasSize(7)))
                .andExpect(jsonPath("$.bySegment", hasSize(5)));
    }

    @Test
    void heavyUsersAreSuggestedTheNextPlanUp() throws Exception {
        // SAT-00001 is on Residential Standard (100 Mbps) and averages 90 Mbps.
        jdbc.sql("""
                INSERT INTO terminal_usage_hourly (terminal_id, hour, samples, sum_downlink_mbps, max_downlink_mbps, sum_uplink_mbps)
                VALUES ('SAT-00001', date_trunc('hour', now()), 100, 9000, 99, 500)
                """).update();
        mvc.perform(get("/api/v1/sales/upsell-candidates"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$[0].terminalId").value("SAT-00001"))
                .andExpect(jsonPath("$[0].suggestedPlan.code").value("RES-PRI"))
                .andExpect(jsonPath("$[0].avgUtilizationPct").value(90.0))
                .andExpect(jsonPath("$[0].monthlyDeltaCents").value(6600));
    }

    @Test
    void customersListAndDetail() throws Exception {
        mvc.perform(get("/api/v1/customers?segment=maritime"))
                .andExpect(status().isOk())
                .andExpect(header().string("X-Total-Count", "80"));
        mvc.perform(get("/api/v1/customers/601"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.segment").value("maritime"))
                .andExpect(jsonPath("$.terminalList", hasSize(greaterThan(0))));
        mvc.perform(get("/api/v1/customers/99999")).andExpect(status().isNotFound());
    }
}
