package dev.satfleet.fleetapi.deviceconfig;

import jakarta.validation.Valid;
import jakarta.validation.constraints.DecimalMax;
import jakarta.validation.constraints.DecimalMin;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Pattern;
import java.time.Instant;
import java.time.LocalTime;
import java.util.Map;

/**
 * Device configuration in its storage form and in both API versions.
 *
 * <p>v1 is flat and uses milliseconds. v2 groups related settings, reports the interval in
 * seconds and nests the maintenance window; that is a breaking change, hence a new version.
 * Both are mapped from the same stored record, so either API can read what the other wrote.
 */
public final class ConfigModels {

    static final String HHMM = "^([01]\\d|2[0-3]):[0-5]\\d$";

    private ConfigModels() {}

    /** Stored configuration plus read-only plan shaping and the terminal's lifecycle status. */
    public record DeviceConfig(
            String terminalId,
            String terminalStatus,
            int version,
            int reportIntervalMs,
            String telemetryLevel,
            String band,
            float txPowerDbm,
            String qosProfile,
            String firmwareChannel,
            LocalTime maintenanceWindowStart,
            LocalTime maintenanceWindowEnd,
            Instant updatedAt,
            String updatedBy,
            int downlinkCapMbps,
            int uplinkCapMbps) {}

    /** Version-neutral set of writable fields. */
    public record ConfigChange(
            int reportIntervalMs,
            String telemetryLevel,
            String band,
            float txPowerDbm,
            String qosProfile,
            String firmwareChannel,
            LocalTime windowStart,
            LocalTime windowEnd) {}

    // ---------- v1 ----------

    public record ConfigV1(
            String terminalId,
            int version,
            int reportIntervalMs,
            String telemetryLevel,
            String band,
            float txPowerDbm,
            String qosProfile,
            String firmwareChannel,
            String maintenanceWindowStart,
            String maintenanceWindowEnd,
            int downlinkCapMbps,
            int uplinkCapMbps,
            Instant updatedAt,
            String updatedBy) {}

    public record ConfigV1Update(
            @NotNull @Min(value = 1000, message = "Report interval must be at least 1000 ms")
                    @Max(value = 300000, message = "Report interval must be at most 300000 ms")
                    Integer reportIntervalMs,
            @NotNull @Pattern(regexp = "basic|detailed") String telemetryLevel,
            @NotNull @Pattern(regexp = "Ku|Ka") String band,
            @NotNull @DecimalMin(value = "10", message = "Transmit power must be between 10 and 33 dBm")
                    @DecimalMax(value = "33", message = "Transmit power must be between 10 and 33 dBm")
                    Float txPowerDbm,
            @NotNull @Pattern(regexp = "standard|priority|business-critical") String qosProfile,
            @NotNull @Pattern(regexp = "stable|beta") String firmwareChannel,
            @NotNull @Pattern(regexp = HHMM, message = "Use 24-hour HH:mm") String maintenanceWindowStart,
            @NotNull @Pattern(regexp = HHMM, message = "Use 24-hour HH:mm") String maintenanceWindowEnd) {}

    // ---------- v2 ----------

    public record ConfigV2(
            String terminalId,
            int revision,
            Reporting reporting,
            Radio radio,
            Network network,
            Firmware firmware,
            Shaping shaping,
            Metadata metadata) {

        public record Reporting(double intervalSeconds, String level) {}

        public record Radio(String band, float txPowerDbm) {}

        public record Network(String qosProfile) {}

        public record Firmware(String channel, Window maintenanceWindow) {}

        public record Window(String start, String end) {}

        public record Shaping(int downlinkMbps, int uplinkMbps) {}

        public record Metadata(Instant updatedAt, String updatedBy) {}
    }

    public record ConfigV2Update(
            @Valid @NotNull ReportingIn reporting,
            @Valid @NotNull RadioIn radio,
            @Valid @NotNull NetworkIn network,
            @Valid @NotNull FirmwareIn firmware) {

        public record ReportingIn(
                @NotNull @Min(value = 1, message = "Report interval must be 1 to 300 seconds")
                        @Max(value = 300, message = "Report interval must be 1 to 300 seconds")
                        Integer intervalSeconds,
                @NotNull @Pattern(regexp = "basic|detailed") String level) {}

        public record RadioIn(
                @NotNull @Pattern(regexp = "Ku|Ka") String band,
                @NotNull @DecimalMin(value = "10", message = "Transmit power must be between 10 and 33 dBm")
                        @DecimalMax(value = "33", message = "Transmit power must be between 10 and 33 dBm")
                        Float txPowerDbm) {}

        public record NetworkIn(@NotNull @Pattern(regexp = "standard|priority|business-critical") String qosProfile) {}

        public record FirmwareIn(
                @NotNull @Pattern(regexp = "stable|beta") String channel, @Valid @NotNull WindowIn maintenanceWindow) {}

        public record WindowIn(
                @NotNull @Pattern(regexp = HHMM, message = "Use 24-hour HH:mm") String start,
                @NotNull @Pattern(regexp = HHMM, message = "Use 24-hour HH:mm") String end) {}
    }

    // ---------- shared ----------

    public record HistoryEntry(int version, String changedBy, Instant changedAt, Map<String, Object> config) {}

    /** Bulk export row used by provisioning tooling and the fleet simulator. */
    public record ExportRow(
            String terminalId,
            String status,
            int reportIntervalMs,
            int downlinkCapMbps,
            int uplinkCapMbps,
            double longitude) {}
}
