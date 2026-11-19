package dev.satfleet.fleetapi.terminals;

import dev.satfleet.fleetapi.plans.Plan;
import jakarta.validation.constraints.DecimalMax;
import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Pattern;
import jakarta.validation.constraints.Size;
import java.time.Instant;
import java.util.List;

/** API representations for terminals. Kept together because they evolve together. */
public final class TerminalModels {

    private TerminalModels() {}

    /** Latest reported link metrics; every field is null until the terminal first reports. */
    public record LiveMetrics(
            Instant lastSeenAt,
            String linkState,
            Float latencyMs,
            Float jitterMs,
            Float packetLossPct,
            Float downlinkMbps,
            Float uplinkMbps,
            Float snrDb,
            Float obstructionPct,
            String firmwareVersion,
            String transport) {}

    public record CustomerRef(long id, String name) {}

    public record PlanRef(String code, String name) {}

    public record Location(double lat, double lon) {}

    /** v2 list item: nested, includes live metrics so lists need no follow-up calls. */
    public record TerminalSummary(
            String id,
            String status,
            String health,
            String region,
            CustomerRef customer,
            PlanRef plan,
            Location location,
            LiveMetrics metrics) {}

    public record CursorPage<T>(List<T> items, String nextCursor, long total) {}

    /** v1 list item: flat, offset-paginated. Retained for existing integrations. */
    public record TerminalV1Item(
            String id,
            long customerId,
            String customerName,
            String planCode,
            String status,
            String health,
            String region,
            Instant lastSeenAt,
            Float latencyMs) {}

    public record CustomerInfo(long id, String name, String segment, String accountManager) {}

    public record TerminalDetail(
            String id,
            String serialNumber,
            String status,
            String health,
            String model,
            String region,
            double latitude,
            double longitude,
            Instant activatedAt,
            Instant createdAt,
            CustomerInfo customer,
            Plan plan,
            LiveMetrics metrics) {}

    public record Filter(String q, String status, String health, String region, Long customerId) {}

    public record ProvisionRequest(
            @NotNull Long customerId,
            @NotBlank String planCode,
            @NotBlank @Size(max = 64) String model,
            @NotNull @DecimalMin(value = "-90", message = "Latitude must be between -90 and 90")
                    @DecimalMax(value = "90", message = "Latitude must be between -90 and 90")
                    Double latitude,
            @NotNull @DecimalMin(value = "-180", message = "Longitude must be between -180 and 180")
                    @DecimalMax(value = "180", message = "Longitude must be between -180 and 180")
                    Double longitude) {}

    public record TerminalPatch(
            @Pattern(regexp = "provisioned|active|suspended|decommissioned", message = "Unknown status") String status,
            @Size(max = 16) String planCode) {}
}
