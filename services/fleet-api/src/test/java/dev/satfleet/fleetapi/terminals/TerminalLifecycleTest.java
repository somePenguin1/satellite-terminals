package dev.satfleet.fleetapi.terminals;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.CsvSource;

class TerminalLifecycleTest {

    @ParameterizedTest(name = "{0} -> {1} allowed={2}")
    @CsvSource({
        "provisioned, active, true",
        "provisioned, decommissioned, true",
        "provisioned, suspended, false",
        "active, suspended, true",
        "active, decommissioned, true",
        "active, provisioned, false",
        "suspended, active, true",
        "suspended, decommissioned, true",
        "decommissioned, active, false",
        "decommissioned, provisioned, false",
        "unknown, active, false",
    })
    void transitions(String from, String to, boolean allowed) {
        assertThat(TerminalLifecycle.canTransition(from, to)).isEqualTo(allowed);
    }
}
