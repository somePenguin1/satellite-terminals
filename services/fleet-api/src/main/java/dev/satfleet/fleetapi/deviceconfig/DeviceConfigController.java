package dev.satfleet.fleetapi.deviceconfig;

import dev.satfleet.fleetapi.common.ApiExceptions;
import dev.satfleet.fleetapi.common.ETags;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigV1;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigV1Update;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigV2;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigV2Update;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.DeviceConfig;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ExportRow;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.HistoryEntry;
import io.swagger.v3.oas.annotations.Operation;
import jakarta.validation.Valid;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import java.util.List;
import java.util.function.Function;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PutMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestHeader;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

/**
 * Device configuration over versioned REST. Reads return an ETag carrying the revision and honour
 * If-None-Match (agents poll cheaply and get 304 when nothing changed). Writes require If-Match,
 * so two operators editing the same terminal can't silently overwrite each other.
 */
@RestController
public class DeviceConfigController {

    private final DeviceConfigService service;

    public DeviceConfigController(DeviceConfigService service) {
        this.service = service;
    }

    private <T> ResponseEntity<T> read(String id, String ifNoneMatch, Function<DeviceConfig, T> view) {
        DeviceConfig c = service.get(id);
        String etag = ETags.of(c.version());
        var cached = ETags.parseVersion(ifNoneMatch);
        if (cached.isPresent() && cached.getAsInt() == c.version()) {
            return ResponseEntity.status(HttpStatus.NOT_MODIFIED).eTag(etag).build();
        }
        return ResponseEntity.ok().eTag(etag).body(view.apply(c));
    }

    private static int requireVersion(String ifMatch) {
        return ETags.parseVersion(ifMatch).orElseThrow(ApiExceptions::preconditionRequired);
    }

    // ---------- v1 ----------

    @Operation(summary = "Get device configuration (v1, flat, interval in milliseconds)")
    @GetMapping("/api/v1/terminals/{id}/config")
    public ResponseEntity<ConfigV1> getV1(
            @PathVariable String id, @RequestHeader(name = "If-None-Match", required = false) String ifNoneMatch) {
        return read(id, ifNoneMatch, ConfigMapper::toV1);
    }

    @Operation(summary = "Replace device configuration (v1); requires If-Match")
    @PutMapping("/api/v1/terminals/{id}/config")
    public ResponseEntity<ConfigV1> putV1(
            @PathVariable String id,
            @RequestHeader(name = "If-Match", required = false) String ifMatch,
            @RequestHeader(name = "X-Actor", defaultValue = "api") String actor,
            @Valid @RequestBody ConfigV1Update body) {
        DeviceConfig saved = service.update(id, requireVersion(ifMatch), ConfigMapper.fromV1(body), actor);
        return ResponseEntity.ok().eTag(ETags.of(saved.version())).body(ConfigMapper.toV1(saved));
    }

    @Operation(summary = "Every saved revision of a terminal's configuration, newest first")
    @GetMapping("/api/v1/terminals/{id}/config/history")
    public List<HistoryEntry> history(@PathVariable String id) {
        return service.history(id);
    }

    @Operation(summary = "Bulk export of reporting settings and plan shaping, keyset-paginated by terminal ID")
    @GetMapping("/api/v1/terminal-configs")
    public List<ExportRow> export(
            @RequestParam(required = false) String afterId,
            @RequestParam(defaultValue = "1000") @Min(1) @Max(5000) int limit) {
        return service.export(afterId, limit);
    }

    // ---------- v2 ----------

    @Operation(summary = "Get device configuration (v2, grouped, interval in seconds)")
    @GetMapping("/api/v2/terminals/{id}/config")
    public ResponseEntity<ConfigV2> getV2(
            @PathVariable String id, @RequestHeader(name = "If-None-Match", required = false) String ifNoneMatch) {
        return read(id, ifNoneMatch, ConfigMapper::toV2);
    }

    @Operation(summary = "Replace device configuration (v2); requires If-Match")
    @PutMapping("/api/v2/terminals/{id}/config")
    public ResponseEntity<ConfigV2> putV2(
            @PathVariable String id,
            @RequestHeader(name = "If-Match", required = false) String ifMatch,
            @RequestHeader(name = "X-Actor", defaultValue = "api") String actor,
            @Valid @RequestBody ConfigV2Update body) {
        DeviceConfig saved = service.update(id, requireVersion(ifMatch), ConfigMapper.fromV2(body), actor);
        return ResponseEntity.ok().eTag(ETags.of(saved.version())).body(ConfigMapper.toV2(saved));
    }
}
