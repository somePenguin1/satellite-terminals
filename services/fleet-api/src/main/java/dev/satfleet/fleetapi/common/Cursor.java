package dev.satfleet.fleetapi.common;

import java.nio.charset.StandardCharsets;
import java.util.Base64;

/**
 * Opaque keyset-pagination cursor. Encodes the last terminal ID seen; clients must treat it
 * as an opaque string so the encoding can change without breaking them.
 */
public final class Cursor {

    private static final String PREFIX = "t1:";

    private Cursor() {}

    public static String encode(String lastId) {
        return Base64.getUrlEncoder().withoutPadding()
                .encodeToString((PREFIX + lastId).getBytes(StandardCharsets.UTF_8));
    }

    /** Returns the last-seen ID, or null for a missing cursor. */
    public static String decode(String cursor) {
        if (cursor == null || cursor.isBlank()) {
            return null;
        }
        String raw;
        try {
            raw = new String(Base64.getUrlDecoder().decode(cursor), StandardCharsets.UTF_8);
        } catch (IllegalArgumentException e) {
            throw new IllegalArgumentException("Invalid cursor; start again without one");
        }
        if (!raw.startsWith(PREFIX) || raw.length() == PREFIX.length()) {
            throw new IllegalArgumentException("Invalid cursor; start again without one");
        }
        return raw.substring(PREFIX.length());
    }
}
