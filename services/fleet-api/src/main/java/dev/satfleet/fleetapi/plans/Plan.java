package dev.satfleet.fleetapi.plans;

public record Plan(
        String code,
        String name,
        String family,
        int tier,
        int downlinkMbps,
        int uplinkMbps,
        int monthlyPriceCents) {}
