package dev.satfleet.fleetapi.common;

import java.util.OptionalInt;

/** Strong ETags of the form {@code "7"} carrying a resource's integer version. */
public final class ETags {

    private ETags() {}

    public static String of(int version) {
        return "\"" + version + "\"";
    }

    /**
     * Extracts the version from an If-Match / If-None-Match value. Accepts weak validators
     * ({@code W/"7"}) and takes the first of a comma-separated list. Empty if absent or unparseable.
     */
    public static OptionalInt parseVersion(String header) {
        if (header == null || header.isBlank()) {
            return OptionalInt.empty();
        }
        String v = header.split(",")[0].trim();
        if (v.startsWith("W/")) {
            v = v.substring(2);
        }
        if (v.length() >= 2 && v.startsWith("\"") && v.endsWith("\"")) {
            v = v.substring(1, v.length() - 1);
        }
        try {
            return OptionalInt.of(Integer.parseInt(v));
        } catch (NumberFormatException e) {
            return OptionalInt.empty();
        }
    }
}
