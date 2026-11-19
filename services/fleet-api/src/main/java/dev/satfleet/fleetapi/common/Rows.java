package dev.satfleet.fleetapi.common;

import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Timestamp;
import java.time.Instant;

/** Null-safe ResultSet helpers (JDBC returns 0 for SQL NULL numerics otherwise). */
public final class Rows {

    private Rows() {}

    public static Float floatOrNull(ResultSet rs, String col) throws SQLException {
        float v = rs.getFloat(col);
        return rs.wasNull() ? null : v;
    }

    public static Double doubleOrNull(ResultSet rs, String col) throws SQLException {
        double v = rs.getDouble(col);
        return rs.wasNull() ? null : v;
    }

    public static Instant instant(ResultSet rs, String col) throws SQLException {
        Timestamp ts = rs.getTimestamp(col);
        return ts == null ? null : ts.toInstant();
    }

    public static String linkState(ResultSet rs, String col) throws SQLException {
        int v = rs.getInt(col);
        if (rs.wasNull()) {
            return null;
        }
        return switch (v) {
            case 1 -> "UP";
            case 2 -> "DEGRADED";
            case 3 -> "DOWN";
            default -> "UNSPECIFIED";
        };
    }
}
