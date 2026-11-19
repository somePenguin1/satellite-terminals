#include "linkmodel.h"

#include <math.h>

#ifndef M_PI
#define M_PI 3.14159265358979323846
#endif

double linkmodel_rand(uint32_t *state) {
    uint32_t x = *state ? *state : 0x9E3779B9u;
    x ^= x << 13;
    x ^= x >> 17;
    x ^= x << 5;
    *state = x;
    return (double)(x >> 8) / (double)(1u << 24);
}

static double clamp(double v, double lo, double hi) { return v < lo ? lo : v > hi ? hi : v; }

/* Approximately normal noise (Irwin-Hall with 4 uniforms), mean 0, sd ~1. */
static double noise(uint32_t *s) {
    double sum = 0;
    for (int i = 0; i < 4; i++) {
        sum += linkmodel_rand(s);
    }
    return (sum - 2.0) * 1.732;
}

void linkmodel_init(linkmodel_t *m, uint32_t seed) {
    m->rng = seed ? seed : 1;
    m->base_latency_ms = 26.0 + linkmodel_rand(&m->rng) * 18.0;
    m->base_snr_db = 10.5 + linkmodel_rand(&m->rng) * 3.0;
    double r = linkmodel_rand(&m->rng);
    m->obstruction_base = r < 0.8 ? r * 3.0 : 4.0 + r * 10.0; /* most sites clear, some wooded */
    m->fade_db = 0;
    m->fade_remaining_s = 0;
    m->downlink_cap_mbps = 220;
    m->uplink_cap_mbps = 25;
}

void linkmodel_step(linkmodel_t *m, double dt_s, double hour_of_day, linkmodel_sample_t *out) {
    /* Rain fade: rare events (~1 per 3 hours per terminal) lasting 2-12 minutes, 3-11 dB deep. */
    if (m->fade_remaining_s > 0) {
        m->fade_remaining_s -= dt_s;
        if (m->fade_remaining_s <= 0) {
            m->fade_db = 0;
        }
    } else if (linkmodel_rand(&m->rng) < dt_s / 10800.0) {
        m->fade_db = 3.0 + linkmodel_rand(&m->rng) * 8.0;
        m->fade_remaining_s = 120.0 + linkmodel_rand(&m->rng) * 600.0;
    }

    /* Evening peak congestion (around 21:00 local), quiet overnight. */
    double congestion = 0.5 + 0.5 * cos((hour_of_day - 21.0) / 24.0 * 2.0 * M_PI);

    out->snr_db = clamp(m->base_snr_db - m->fade_db + noise(&m->rng) * 0.6, -2.0, 20.0);
    out->obstruction_pct = clamp(m->obstruction_base + fabs(noise(&m->rng)) * 0.8, 0.0, 100.0);

    double snr_penalty = out->snr_db < 6.0 ? (6.0 - out->snr_db) : 0.0;
    out->loss_pct = clamp(0.05 + snr_penalty * 1.4 + out->obstruction_pct * 0.08 + fabs(noise(&m->rng)) * 0.05,
                          0.0, 100.0);
    out->extra_latency_ms = m->base_latency_ms + congestion * 14.0 + snr_penalty * 6.0 + fabs(noise(&m->rng)) * 3.0;

    double demand = clamp(0.08 + 0.55 * congestion + noise(&m->rng) * 0.08, 0.02, 1.0);
    double capacity = clamp(1.0 - snr_penalty * 0.15 - out->obstruction_pct / 100.0, 0.0, 1.0);
    out->downlink_mbps = m->downlink_cap_mbps * demand * capacity;
    out->uplink_mbps = m->uplink_cap_mbps * clamp(demand * 0.7 + noise(&m->rng) * 0.05, 0.01, 1.0) * capacity;
}
