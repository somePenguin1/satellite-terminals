package dev.satfleet.fleetapi.terminals;

import java.util.Map;
import java.util.Set;

/**
 * Service lifecycle of a terminal:
 *
 * <pre>
 * provisioned ──activate──▶ active ◀──resume── suspended
 *      │                      │ └──suspend──────▲
 *      └──────────────▶ decommissioned ◀────────┘
 * </pre>
 *
 * Decommissioned is terminal: the hardware is retired and its ID is never reused.
 */
public final class TerminalLifecycle {

    private static final Map<String, Set<String>> ALLOWED = Map.of(
            "provisioned", Set.of("active", "decommissioned"),
            "active", Set.of("suspended", "decommissioned"),
            "suspended", Set.of("active", "decommissioned"),
            "decommissioned", Set.of());

    private TerminalLifecycle() {}

    public static boolean canTransition(String from, String to) {
        return ALLOWED.getOrDefault(from, Set.of()).contains(to);
    }
}
