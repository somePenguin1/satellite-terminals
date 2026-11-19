package dev.satfleet.fleetapi.common;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import org.junit.jupiter.api.Test;

class CommonTest {

    @Test
    void etagRoundTripsVersion() {
        assertThat(ETags.of(7)).isEqualTo("\"7\"");
        assertThat(ETags.parseVersion("\"7\"")).hasValue(7);
        assertThat(ETags.parseVersion("W/\"12\"")).hasValue(12);
        assertThat(ETags.parseVersion("\"3\", \"4\"")).hasValue(3);
        assertThat(ETags.parseVersion("7")).hasValue(7);
    }

    @Test
    void etagRejectsGarbage() {
        assertThat(ETags.parseVersion(null)).isEmpty();
        assertThat(ETags.parseVersion("  ")).isEmpty();
        assertThat(ETags.parseVersion("*")).isEmpty();
        assertThat(ETags.parseVersion("\"abc\"")).isEmpty();
    }

    @Test
    void cursorIsOpaqueAndReversible() {
        String c = Cursor.encode("SAT-00050");
        assertThat(c).doesNotContain("SAT");
        assertThat(Cursor.decode(c)).isEqualTo("SAT-00050");
        assertThat(Cursor.decode(null)).isNull();
        assertThat(Cursor.decode("")).isNull();
    }

    @Test
    void cursorRejectsTampering() {
        assertThatThrownBy(() -> Cursor.decode("not-base64!!")).isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> Cursor.decode(java.util.Base64.getUrlEncoder().encodeToString("x:1".getBytes())))
                .isInstanceOf(IllegalArgumentException.class);
    }
}
