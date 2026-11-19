#include "report.h"

#include <inttypes.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

const char *link_state_name(link_state_t s) {
    switch (s) {
        case LINK_UP:
            return "UP";
        case LINK_DEGRADED:
            return "DEGRADED";
        case LINK_DOWN:
            return "DOWN";
        default:
            return "UNSPECIFIED";
    }
}

/* Copies s into out with JSON string escaping. Returns chars written or -1 if it does not fit. */
static int json_escape(const char *s, char *out, size_t cap) {
    size_t o = 0;
    for (; *s; s++) {
        unsigned char ch = (unsigned char)*s;
        char tmp[8];
        const char *rep = NULL;
        if (ch == '"') {
            rep = "\\\"";
        } else if (ch == '\\') {
            rep = "\\\\";
        } else if (ch < 0x20) {
            snprintf(tmp, sizeof tmp, "\\u%04x", ch);
            rep = tmp;
        }
        if (rep) {
            size_t n = strlen(rep);
            if (o + n >= cap) {
                return -1;
            }
            memcpy(out + o, rep, n);
            o += n;
        } else {
            if (o + 1 >= cap) {
                return -1;
            }
            out[o++] = (char)ch;
        }
    }
    out[o] = '\0';
    return (int)o;
}

static double finite_or_zero(double v) { return isfinite(v) ? v : 0.0; }

int report_to_json(const report_t *r, const char *firmware, char *out, size_t cap) {
    char id[64], fw[64];
    if (json_escape(r->terminal_id, id, sizeof id) < 0 || json_escape(firmware, fw, sizeof fw) < 0) {
        return -1;
    }
    /* snrDb is omitted (not sent as null) when unknown, so the server falls back to its default. */
    char snr[48] = "";
    if (isfinite(r->snr_db)) {
        snprintf(snr, sizeof snr, ",\"snrDb\":%.2f", r->snr_db);
    }
    int n = snprintf(out, cap,
                     "{\"terminalId\":\"%s\",\"timestampMs\":%" PRId64 ",\"seq\":%" PRIu64
                     ",\"linkState\":\"%s\",\"latencyMs\":%.2f,\"jitterMs\":%.2f,\"packetLossPct\":%.2f"
                     ",\"downlinkMbps\":%.2f,\"uplinkMbps\":%.2f%s,\"obstructionPct\":%.2f"
                     ",\"firmwareVersion\":\"%s\"}",
                     id, r->timestamp_ms, r->seq, link_state_name(r->link_state),
                     finite_or_zero(r->latency_ms), finite_or_zero(r->jitter_ms),
                     finite_or_zero(r->packet_loss_pct), finite_or_zero(r->downlink_mbps),
                     finite_or_zero(r->uplink_mbps), snr, finite_or_zero(r->obstruction_pct), fw);
    return (n < 0 || (size_t)n >= cap) ? -1 : n;
}

/* Finds `"key"` followed by optional whitespace and a colon; returns pointer to the value. */
static const char *find_value(const char *json, const char *key) {
    size_t key_len = strlen(key);
    for (const char *p = strchr(json, '"'); p; p = strchr(p + 1, '"')) {
        if (strncmp(p + 1, key, key_len) == 0 && p[1 + key_len] == '"') {
            const char *v = p + 2 + key_len;
            while (*v == ' ' || *v == '\t' || *v == '\n' || *v == '\r') {
                v++;
            }
            if (*v != ':') {
                continue;
            }
            v++;
            while (*v == ' ' || *v == '\t' || *v == '\n' || *v == '\r') {
                v++;
            }
            return v;
        }
    }
    return NULL;
}

int json_find_number(const char *json, const char *key, double *out) {
    const char *v = find_value(json, key);
    if (!v) {
        return -1;
    }
    char *end = NULL;
    double d = strtod(v, &end);
    if (end == v) {
        return -1;
    }
    *out = d;
    return 0;
}

int json_find_string(const char *json, const char *key, char *out, size_t cap) {
    const char *v = find_value(json, key);
    if (!v || *v != '"') {
        return -1;
    }
    const char *end = strchr(v + 1, '"');
    if (!end || (size_t)(end - v - 1) >= cap) {
        return -1;
    }
    memcpy(out, v + 1, (size_t)(end - v - 1));
    out[end - v - 1] = '\0';
    return 0;
}
