/* satfleet-agent: lightweight telemetry agent for broadband satellite terminals.
 *
 * Every report interval it measures the link (TCP-connect RTT probes to the gateway for
 * latency/jitter/loss, interface counters or the modem model for throughput, SNR and
 * obstruction), then streams a JSON report to the ingest service over a WebSocket.
 * Desired configuration is pulled from the fleet REST API with ETag caching. Reports are
 * buffered in a ring and replayed when the uplink returns. */
#define _DEFAULT_SOURCE
#include <inttypes.h>
#include <math.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <time.h>

#include "config.h"
#include "http.h"
#include "linkmodel.h"
#include "log.h"
#include "net.h"
#include "netdev.h"
#include "probe.h"
#include "queue.h"
#include "report.h"
#include "ws.h"

static volatile sig_atomic_t g_stop = 0;

static void on_signal(int sig) {
    (void)sig;
    g_stop = 1;
}

typedef struct {
    agent_config_t cfg;
    ws_conn_t ws;
    report_queue_t queue;
    linkmodel_t model;
    char etag[64];
    uint64_t seq;
    uint64_t sent;
    netdev_counters_t last_counters;
    int64_t last_counters_ms;
    int have_counters;
} agent_t;

static void poll_remote_config(agent_t *a) {
    char url[384];
    snprintf(url, sizeof url, "%s/api/v1/terminals/%s/config", a->cfg.api_url, a->cfg.terminal_id);
    static http_response_t resp; /* 64 KB: keep it off the stack */
    if (http_get(url, a->etag, 3000, &resp) != 0) {
        log_warn("config fetch failed: %s unreachable", a->cfg.api_url);
        return;
    }
    if (resp.status == 304) {
        log_debug("config unchanged (%s)", a->etag);
        return;
    }
    if (resp.status != 200) {
        log_warn("config fetch returned HTTP %d", resp.status);
        return;
    }
    int changed = config_apply_remote(&a->cfg, resp.body);
    if (changed < 0) {
        log_warn("config document rejected");
        return;
    }
    snprintf(a->etag, sizeof a->etag, "%s", resp.etag);
    a->model.downlink_cap_mbps = a->cfg.downlink_cap_mbps;
    a->model.uplink_cap_mbps = a->cfg.uplink_cap_mbps;
    if (changed) {
        log_info("applied config %s: interval=%dms caps=%.0f/%.0f Mbps", a->etag, a->cfg.report_interval_ms,
                 a->cfg.downlink_cap_mbps, a->cfg.uplink_cap_mbps);
    }
}

static double local_hour(void) {
    time_t now = time(NULL);
    struct tm tm;
    localtime_r(&now, &tm);
    return tm.tm_hour + tm.tm_min / 60.0;
}

static void collect(agent_t *a, report_t *r) {
    memset(r, 0, sizeof *r);
    snprintf(r->terminal_id, sizeof r->terminal_id, "%s", a->cfg.terminal_id);
    r->seq = ++a->seq;

    probe_window_t win;
    probe_stats_t st;
    probe_run(a->cfg.probe_host, a->cfg.probe_port, (size_t)a->cfg.probes_per_report, 1000, 50, &win);
    probe_compute_stats(&win, &st);

    if (a->cfg.mode == MODE_SIM) {
        linkmodel_sample_t s;
        linkmodel_step(&a->model, a->cfg.report_interval_ms / 1000.0, local_hour(), &s);
        /* Real probe proves reachability; the model adds the satellite path on top. */
        r->latency_ms = st.successes ? st.avg_rtt_ms + s.extra_latency_ms : 0;
        r->jitter_ms = st.jitter_ms + fabs(s.extra_latency_ms - a->model.base_latency_ms) * 0.1;
        r->packet_loss_pct = st.successes ? fmax(st.loss_pct, s.loss_pct) : 100.0;
        r->downlink_mbps = st.successes ? s.downlink_mbps : 0;
        r->uplink_mbps = st.successes ? s.uplink_mbps : 0;
        r->snr_db = s.snr_db;
        r->obstruction_pct = s.obstruction_pct;
    } else {
        r->latency_ms = st.successes ? st.avg_rtt_ms : 0;
        r->jitter_ms = st.jitter_ms;
        r->packet_loss_pct = st.loss_pct;
        r->snr_db = NAN;
        r->obstruction_pct = 0;
        netdev_counters_t now_c;
        int64_t now_ms = now_monotonic_ms();
        if (netdev_read("/proc/net/dev", a->cfg.iface, &now_c) == 0) {
            if (a->have_counters) {
                int64_t dt = now_ms - a->last_counters_ms;
                r->downlink_mbps = netdev_rate_mbps(a->last_counters.rx_bytes, now_c.rx_bytes, dt);
                r->uplink_mbps = netdev_rate_mbps(a->last_counters.tx_bytes, now_c.tx_bytes, dt);
            }
            a->last_counters = now_c;
            a->last_counters_ms = now_ms;
            a->have_counters = 1;
        }
    }

    if (st.successes == 0 || (isfinite(r->snr_db) && r->snr_db < 2.0)) {
        r->link_state = LINK_DOWN;
    } else if (r->packet_loss_pct > 2.0 || (isfinite(r->snr_db) && r->snr_db < 6.0) || r->obstruction_pct > 15.0 ||
               r->latency_ms > 150.0) {
        r->link_state = LINK_DEGRADED;
    } else {
        r->link_state = LINK_UP;
    }
    r->timestamp_ms = now_wall_ms();
}

/* Sends queued reports oldest-first. Stops at the first failure, keeping the rest. */
static void drain(agent_t *a) {
    char json[REPORT_JSON_MAX];
    const report_t *r;
    while (a->ws.open && (r = queue_peek(&a->queue)) != NULL) {
        int n = report_to_json(r, AGENT_VERSION, json, sizeof json);
        if (n < 0) {
            queue_pop(&a->queue); /* unserializable: drop rather than wedge the queue */
            continue;
        }
        if (ws_send_text(&a->ws, json, (size_t)n) != 0) {
            log_warn("uplink lost while sending seq=%" PRIu64 "; %zu reports buffered", r->seq, a->queue.len);
            break;
        }
        queue_pop(&a->queue);
        a->sent++;
    }
}

static void sleep_ms(int64_t ms) {
    if (ms <= 0) {
        return;
    }
    struct timespec ts = {.tv_sec = ms / 1000, .tv_nsec = (long)(ms % 1000) * 1000000L};
    nanosleep(&ts, NULL);
}

int main(int argc, char **argv) {
    static agent_t a; /* ~70 KB queue: static storage, not stack */
    int rc = config_load(&a.cfg, argc, argv);
    if (rc != 0) {
        config_print_usage(argv[0]);
        return rc == 1 ? 0 : 2;
    }
    log_set_level(a.cfg.verbose ? LOG_DEBUG : LOG_INFO);

    struct sigaction sa = {0};
    sa.sa_handler = on_signal;
    sigaction(SIGINT, &sa, NULL);
    sigaction(SIGTERM, &sa, NULL);
    signal(SIGPIPE, SIG_IGN);

    uint32_t seed = 2166136261u; /* FNV-1a of the terminal id: stable per-device link profile */
    for (const char *p = a.cfg.terminal_id; *p; p++) {
        seed = (seed ^ (uint8_t)*p) * 16777619u;
    }
    linkmodel_init(&a.model, seed);
    queue_init(&a.queue);
    a.ws.fd = -1;

    log_info("%s starting: terminal=%s ingest=%s api=%s probe=%s:%d mode=%s", AGENT_VERSION, a.cfg.terminal_id,
             a.cfg.ingest_url, a.cfg.api_url, a.cfg.probe_host, a.cfg.probe_port,
             a.cfg.mode == MODE_SIM ? "sim" : "system");

    int64_t now = now_monotonic_ms();
    int64_t next_report = now, next_config = now, next_connect = now;
    int64_t backoff_ms = 500;
    long produced = 0;

    while (!g_stop) {
        now = now_monotonic_ms();

        if (now >= next_config) {
            poll_remote_config(&a);
            next_config = now + a.cfg.config_poll_ms;
        }

        if (!a.ws.open && now >= next_connect) {
            if (ws_connect(&a.ws, a.cfg.ingest_url, a.cfg.token, 3000) == 0) {
                log_info("uplink connected; replaying %zu buffered reports", a.queue.len);
                backoff_ms = 500;
                drain(&a);
            } else {
                /* Exponential backoff with jitter so a gateway restart doesn't get a thundering herd. */
                uint8_t r;
                random_bytes(&r, 1);
                next_connect = now + backoff_ms + (backoff_ms * r) / 512;
                log_warn("uplink connect failed; retrying in %" PRId64 " ms", next_connect - now);
                backoff_ms = backoff_ms * 2 > 30000 ? 30000 : backoff_ms * 2;
            }
        }
        if (a.ws.open && ws_poll(&a.ws) != 0) {
            log_warn("uplink closed by server");
            next_connect = now_monotonic_ms();
        }

        if (now >= next_report) {
            report_t r;
            collect(&a, &r);
            queue_push(&a.queue, &r);
            drain(&a);
            produced++;
            log_debug("seq=%" PRIu64 " state=%s rtt=%.1fms loss=%.1f%% down=%.1fMbps snr=%.1fdB", r.seq,
                      link_state_name(r.link_state), r.latency_ms, r.packet_loss_pct, r.downlink_mbps, r.snr_db);
            if (a.cfg.max_reports > 0 && produced >= a.cfg.max_reports) {
                break;
            }
            next_report += a.cfg.report_interval_ms;
            if (next_report < now) { /* we fell behind (e.g. suspended): don't burst */
                next_report = now + a.cfg.report_interval_ms;
            }
        }

        int64_t wake = next_report;
        if (next_config < wake) wake = next_config;
        if (!a.ws.open && next_connect < wake) wake = next_connect;
        int64_t delay = wake - now_monotonic_ms();
        sleep_ms(delay > 200 ? 200 : delay);
    }

    ws_close(&a.ws, 1000);
    log_info("stopped: produced=%ld sent=%" PRIu64 " buffered=%zu dropped=%" PRIu64, produced, a.sent, a.queue.len,
             a.queue.dropped);
    return 0;
}
