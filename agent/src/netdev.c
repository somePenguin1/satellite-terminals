#include "netdev.h"

#include <inttypes.h>
#include <stdio.h>
#include <string.h>

int netdev_read(const char *path, const char *iface, netdev_counters_t *out) {
    FILE *f = fopen(path, "r");
    if (!f) {
        return -1;
    }
    char line[512];
    int found = -1;
    /* Two header lines, then "  iface: rx_bytes rx_packets ... (8 rx fields) tx_bytes ..." */
    while (fgets(line, sizeof line, f)) {
        char *colon = strchr(line, ':');
        if (!colon) {
            continue;
        }
        *colon = '\0';
        char *name = line;
        while (*name == ' ') {
            name++;
        }
        if (iface && *iface ? strcmp(name, iface) != 0 : strcmp(name, "lo") == 0) {
            continue;
        }
        uint64_t f_[16] = {0};
        int n = sscanf(colon + 1,
                       "%" SCNu64 " %" SCNu64 " %" SCNu64 " %" SCNu64 " %" SCNu64 " %" SCNu64 " %" SCNu64
                       " %" SCNu64 " %" SCNu64,
                       &f_[0], &f_[1], &f_[2], &f_[3], &f_[4], &f_[5], &f_[6], &f_[7], &f_[8]);
        if (n == 9) {
            out->rx_bytes = f_[0];
            out->tx_bytes = f_[8];
            found = 0;
            break;
        }
    }
    fclose(f);
    return found;
}

double netdev_rate_mbps(uint64_t before, uint64_t after, int64_t dt_ms) {
    if (dt_ms <= 0 || after < before) { /* counter reset (interface bounce) */
        return 0.0;
    }
    return (double)(after - before) * 8.0 / 1e6 / ((double)dt_ms / 1000.0);
}
