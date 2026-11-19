#define _DEFAULT_SOURCE
#include "probe.h"

#include <math.h>
#include <time.h>
#include <unistd.h>

#include "net.h"

static int64_t now_us(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (int64_t)ts.tv_sec * 1000000 + ts.tv_nsec / 1000;
}

void probe_run(const char *host, int port, size_t count, int timeout_ms, int gap_ms, probe_window_t *out) {
    if (count > PROBE_MAX_SAMPLES) {
        count = PROBE_MAX_SAMPLES;
    }
    out->count = 0;
    for (size_t i = 0; i < count; i++) {
        int64_t start = now_us();
        int fd = tcp_connect(host, port, timeout_ms);
        int64_t elapsed = now_us() - start;
        if (fd >= 0) {
            close(fd);
            out->rtt_ms[out->count++] = (double)elapsed / 1000.0;
        } else {
            out->rtt_ms[out->count++] = -1.0;
        }
        if (i + 1 < count && gap_ms > 0) {
            struct timespec ts = {.tv_sec = gap_ms / 1000, .tv_nsec = (long)(gap_ms % 1000) * 1000000L};
            nanosleep(&ts, NULL);
        }
    }
}

void probe_compute_stats(const probe_window_t *w, probe_stats_t *out) {
    double sum = 0, jitter_sum = 0, prev = -1;
    size_t ok = 0, jitter_n = 0;
    for (size_t i = 0; i < w->count; i++) {
        double r = w->rtt_ms[i];
        if (r < 0) {
            continue;
        }
        sum += r;
        ok++;
        if (prev >= 0) {
            jitter_sum += fabs(r - prev);
            jitter_n++;
        }
        prev = r;
    }
    out->successes = ok;
    out->avg_rtt_ms = ok ? sum / (double)ok : NAN;
    out->jitter_ms = jitter_n ? jitter_sum / (double)jitter_n : 0.0;
    out->loss_pct = w->count ? 100.0 * (double)(w->count - ok) / (double)w->count : 100.0;
}
