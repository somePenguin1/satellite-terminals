package dev.satfleet.fleetapi.deviceconfig;

import static org.hamcrest.Matchers.containsString;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyInt;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.put;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.header;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import dev.satfleet.fleetapi.common.ApiExceptions;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.DeviceConfig;
import java.time.Instant;
import java.time.LocalTime;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.WebMvcTest;
import org.springframework.http.MediaType;
import org.springframework.test.context.bean.override.mockito.MockitoBean;
import org.springframework.test.web.servlet.MockMvc;

/** HTTP contract of the config endpoints: ETags, conditional requests and validation. */
@WebMvcTest(DeviceConfigController.class)
class DeviceConfigControllerTest {

    @Autowired
    MockMvc mvc;

    @MockitoBean
    DeviceConfigService service;

    private static DeviceConfig config(int version, int intervalMs) {
        return new DeviceConfig("SAT-00001", "active", version, intervalMs, "basic", "Ku", 27f, "standard",
                "stable", LocalTime.of(2, 0), LocalTime.of(4, 0), Instant.parse("2026-11-19T12:00:00Z"), "seed", 100, 10);
    }

    private static final String V2_BODY = """
            {"reporting":{"intervalSeconds":2,"level":"basic"},"radio":{"band":"Ku","txPowerDbm":27},
             "network":{"qosProfile":"standard"},
             "firmware":{"channel":"stable","maintenanceWindow":{"start":"02:00","end":"04:00"}}}
            """;

    @Test
    void getReturnsEtagAndHonoursIfNoneMatch() throws Exception {
        when(service.get("SAT-00001")).thenReturn(config(3, 5000));
        mvc.perform(get("/api/v1/terminals/SAT-00001/config"))
                .andExpect(status().isOk())
                .andExpect(header().string("ETag", "\"3\""))
                .andExpect(jsonPath("$.reportIntervalMs").value(5000));
        mvc.perform(get("/api/v2/terminals/SAT-00001/config").header("If-None-Match", "\"3\""))
                .andExpect(status().isNotModified());
        mvc.perform(get("/api/v2/terminals/SAT-00001/config").header("If-None-Match", "\"2\""))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.reporting.intervalSeconds").value(5.0));
    }

    @Test
    void putWithoutIfMatchIsRejected() throws Exception {
        mvc.perform(put("/api/v2/terminals/SAT-00001/config").contentType(MediaType.APPLICATION_JSON).content(V2_BODY))
                .andExpect(status().isPreconditionRequired());
        verify(service, never()).update(any(), anyInt(), any(), any());
    }

    @Test
    void putPassesVersionAndActorAndReturnsNewEtag() throws Exception {
        when(service.update(eq("SAT-00001"), eq(3), any(), eq("dana"))).thenReturn(config(4, 2000));
        mvc.perform(put("/api/v2/terminals/SAT-00001/config")
                        .header("If-Match", "\"3\"").header("X-Actor", "dana")
                        .contentType(MediaType.APPLICATION_JSON).content(V2_BODY))
                .andExpect(status().isOk())
                .andExpect(header().string("ETag", "\"4\""))
                .andExpect(jsonPath("$.revision").value(4));
    }

    @Test
    void staleIfMatchMapsTo412() throws Exception {
        when(service.update(eq("SAT-00001"), eq(2), any(), any()))
                .thenThrow(ApiExceptions.preconditionFailed("Configuration changed since revision 2"));
        mvc.perform(put("/api/v2/terminals/SAT-00001/config")
                        .header("If-Match", "\"2\"").contentType(MediaType.APPLICATION_JSON).content(V2_BODY))
                .andExpect(status().isPreconditionFailed())
                .andExpect(jsonPath("$.detail", containsString("revision 2")));
    }

    @Test
    void invalidBodyReportsFieldErrors() throws Exception {
        String bad = V2_BODY.replace("\"intervalSeconds\":2", "\"intervalSeconds\":0")
                .replace("\"txPowerDbm\":27", "\"txPowerDbm\":50");
        mvc.perform(put("/api/v2/terminals/SAT-00001/config")
                        .header("If-Match", "\"3\"").contentType(MediaType.APPLICATION_JSON).content(bad))
                .andExpect(status().isBadRequest())
                .andExpect(jsonPath("$.errors['reporting.intervalSeconds']").exists())
                .andExpect(jsonPath("$.errors['radio.txPowerDbm']").value("Transmit power must be between 10 and 33 dBm"));
    }
}
