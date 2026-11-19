package dev.satfleet.fleetapi.deviceconfig;

import static org.assertj.core.api.Assertions.assertThat;

import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigChange;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigV1;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigV1Update;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigV2;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigV2Update;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.DeviceConfig;
import java.time.Instant;
import java.time.LocalTime;
import org.junit.jupiter.api.Test;

class ConfigMapperTest {

    private static final DeviceConfig STORED = new DeviceConfig(
            "SAT-00042", "active", 4, 2500, "detailed", "Ka", 28.5f, "priority", "beta",
            LocalTime.of(2, 0), LocalTime.of(4, 30), Instant.parse("2026-11-19T12:00:00Z"), "ops", 350, 40);

    @Test
    void v1IsFlatWithMilliseconds() {
        ConfigV1 v1 = ConfigMapper.toV1(STORED);
        assertThat(v1.version()).isEqualTo(4);
        assertThat(v1.reportIntervalMs()).isEqualTo(2500);
        assertThat(v1.maintenanceWindowStart()).isEqualTo("02:00");
        assertThat(v1.maintenanceWindowEnd()).isEqualTo("04:30");
        assertThat(v1.downlinkCapMbps()).isEqualTo(350);
    }

    @Test
    void v2GroupsFieldsAndUsesSeconds() {
        ConfigV2 v2 = ConfigMapper.toV2(STORED);
        assertThat(v2.revision()).isEqualTo(4);
        assertThat(v2.reporting().intervalSeconds()).isEqualTo(2.5);
        assertThat(v2.radio().band()).isEqualTo("Ka");
        assertThat(v2.firmware().maintenanceWindow().end()).isEqualTo("04:30");
        assertThat(v2.shaping().uplinkMbps()).isEqualTo(40);
        assertThat(v2.metadata().updatedBy()).isEqualTo("ops");
    }

    @Test
    void bothVersionsProduceTheSameChange() {
        ConfigChange fromV1 = ConfigMapper.fromV1(new ConfigV1Update(
                3000, "basic", "Ku", 27f, "standard", "stable", "01:15", "03:45"));
        ConfigChange fromV2 = ConfigMapper.fromV2(new ConfigV2Update(
                new ConfigV2Update.ReportingIn(3, "basic"),
                new ConfigV2Update.RadioIn("Ku", 27f),
                new ConfigV2Update.NetworkIn("standard"),
                new ConfigV2Update.FirmwareIn("stable", new ConfigV2Update.WindowIn("01:15", "03:45"))));
        assertThat(fromV2).isEqualTo(fromV1);
        assertThat(fromV1.windowStart()).isEqualTo(LocalTime.of(1, 15));
    }
}
