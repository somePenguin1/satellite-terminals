package dev.satfleet.fleetapi.deviceconfig;

import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigChange;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigV1;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigV1Update;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigV2;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.ConfigV2Update;
import dev.satfleet.fleetapi.deviceconfig.ConfigModels.DeviceConfig;
import java.time.LocalTime;
import java.time.format.DateTimeFormatter;

public final class ConfigMapper {

    private static final DateTimeFormatter HHMM = DateTimeFormatter.ofPattern("HH:mm");

    private ConfigMapper() {}

    static String hhmm(LocalTime t) {
        return t.format(HHMM);
    }

    static LocalTime parseTime(String s) {
        return LocalTime.parse(s, HHMM);
    }

    public static ConfigV1 toV1(DeviceConfig c) {
        return new ConfigV1(c.terminalId(), c.version(), c.reportIntervalMs(), c.telemetryLevel(), c.band(),
                c.txPowerDbm(), c.qosProfile(), c.firmwareChannel(), hhmm(c.maintenanceWindowStart()),
                hhmm(c.maintenanceWindowEnd()), c.downlinkCapMbps(), c.uplinkCapMbps(), c.updatedAt(), c.updatedBy());
    }

    public static ConfigV2 toV2(DeviceConfig c) {
        return new ConfigV2(
                c.terminalId(),
                c.version(),
                new ConfigV2.Reporting(c.reportIntervalMs() / 1000.0, c.telemetryLevel()),
                new ConfigV2.Radio(c.band(), c.txPowerDbm()),
                new ConfigV2.Network(c.qosProfile()),
                new ConfigV2.Firmware(c.firmwareChannel(),
                        new ConfigV2.Window(hhmm(c.maintenanceWindowStart()), hhmm(c.maintenanceWindowEnd()))),
                new ConfigV2.Shaping(c.downlinkCapMbps(), c.uplinkCapMbps()),
                new ConfigV2.Metadata(c.updatedAt(), c.updatedBy()));
    }

    public static ConfigChange fromV1(ConfigV1Update u) {
        return new ConfigChange(u.reportIntervalMs(), u.telemetryLevel(), u.band(), u.txPowerDbm(), u.qosProfile(),
                u.firmwareChannel(), parseTime(u.maintenanceWindowStart()), parseTime(u.maintenanceWindowEnd()));
    }

    public static ConfigChange fromV2(ConfigV2Update u) {
        return new ConfigChange(
                u.reporting().intervalSeconds() * 1000,
                u.reporting().level(),
                u.radio().band(),
                u.radio().txPowerDbm(),
                u.network().qosProfile(),
                u.firmware().channel(),
                parseTime(u.firmware().maintenanceWindow().start()),
                parseTime(u.firmware().maintenanceWindow().end()));
    }
}
