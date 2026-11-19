#ifndef AGENT_PROBE_H
#define AGENT_PROBE_H

#include <stddef.h>
#include <stdint.h>

#define PROBE_MAX_SAMPLES 32

/* Outcome of one probe window: RTT samples in ms, with lost probes marked as negative. */
typedef struct {
    double rtt_ms[PROBE_MAX_SAMPLES];
    size_t count;
} probe_window_t;

typedef struct {
    double avg_rtt_ms;      /* over successful probes; NaN if none succeeded */
    double jitter_ms;       /* mean |rtt[i] - rtt[i-1]| over consecutive successes (RFC 3550 style) */
    double loss_pct;        /* lost / sent * 100 */
    size_t successes;
} probe_stats_t;

/* Runs `count` TCP-connect probes against host:port, spaced gap_ms apart. */
void probe_run(const char *host, int port, size_t count, int timeout_ms, int gap_ms, probe_window_t *out);

/* Pure function so the math is unit-testable without a network. */
void probe_compute_stats(const probe_window_t *w, probe_stats_t *out);

#endif
