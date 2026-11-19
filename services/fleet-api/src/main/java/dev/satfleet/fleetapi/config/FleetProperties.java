package dev.satfleet.fleetapi.config;

import java.time.Duration;
import org.springframework.boot.context.properties.ConfigurationProperties;

@ConfigurationProperties(prefix = "fleet")
public record FleetProperties(Scheduling scheduling, Alerts alerts, Telemetry telemetry, Upsell upsell) {

    public record Scheduling(boolean enabled) {}

    public record Alerts(Duration evaluationInterval) {}

    public record Telemetry(int retentionDays) {}

    public record Upsell(double utilizationThreshold) {}
}
