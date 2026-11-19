#ifndef AGENT_NETDEV_H
#define AGENT_NETDEV_H

#include <stdint.h>

typedef struct {
    uint64_t rx_bytes;
    uint64_t tx_bytes;
} netdev_counters_t;

/* Reads byte counters for `iface` from a /proc/net/dev formatted file.
 * An empty iface picks the first interface that is not loopback. Returns 0 on success. */
int netdev_read(const char *path, const char *iface, netdev_counters_t *out);

/* Converts a counter delta over dt_ms to megabits per second, tolerating counter wrap/reset. */
double netdev_rate_mbps(uint64_t before, uint64_t after, int64_t dt_ms);

#endif
