#ifndef AGENT_LINKMODEL_H
#define AGENT_LINKMODEL_H

/* Simulated satellite modem. Real terminals read SNR/obstruction from modem firmware;
 * in the lab the agent models them so the whole pipeline can be exercised on any Linux box.
 * Models clear-sky SNR, rain-fade events, partial sky obstruction, diurnal congestion and
 * plan rate shaping. */

#include <stdint.h>

typedef struct {
    uint32_t rng;
    double base_latency_ms;   /* LEO propagation + gateway processing */
    double base_snr_db;
    double obstruction_base;  /* site-specific, e.g. trees */
    double fade_db;           /* current rain fade depth */
    double fade_remaining_s;
    double downlink_cap_mbps; /* from plan shaping in the remote config */
    double uplink_cap_mbps;
} linkmodel_t;

typedef struct {
    double extra_latency_ms;
    double snr_db;
    double obstruction_pct;
    double loss_pct;
    double downlink_mbps;
    double uplink_mbps;
} linkmodel_sample_t;

void linkmodel_init(linkmodel_t *m, uint32_t seed);

/* Advances the model by dt_s seconds at the given local hour of day (0-23.99). */
void linkmodel_step(linkmodel_t *m, double dt_s, double hour_of_day, linkmodel_sample_t *out);

/* Deterministic xorshift32, exposed for tests. Returns a value in [0, 1). */
double linkmodel_rand(uint32_t *state);

#endif
