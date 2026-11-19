#ifndef AGENT_REPORT_H
#define AGENT_REPORT_H

#include <stddef.h>
#include <stdint.h>

typedef enum { LINK_UNKNOWN = 0, LINK_UP = 1, LINK_DEGRADED = 2, LINK_DOWN = 3 } link_state_t;

typedef struct {
    char terminal_id[32];
    int64_t timestamp_ms;
    uint64_t seq;
    link_state_t link_state;
    double latency_ms;
    double jitter_ms;
    double packet_loss_pct;
    double downlink_mbps;
    double uplink_mbps;
    double snr_db; /* NaN when the modem does not expose it */
    double obstruction_pct;
} report_t;

#define REPORT_JSON_MAX 512

const char *link_state_name(link_state_t s);

/* Serializes a report as the ingest service's JSON telemetry message. Returns length or -1. */
int report_to_json(const report_t *r, const char *firmware, char *out, size_t cap);

/* Looks up a top-level numeric field in a flat JSON object. Returns 0 if found. */
int json_find_number(const char *json, const char *key, double *out);

/* Looks up a top-level string field (no escape sequences). Returns 0 if found. */
int json_find_string(const char *json, const char *key, char *out, size_t cap);

#endif
