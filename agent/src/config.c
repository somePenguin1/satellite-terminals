#include "config.h"

#include <getopt.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "net.h"
#include "report.h"

static void copy_str(char *dst, size_t cap, const char *src) {
    if (src) {
        snprintf(dst, cap, "%s", src);
    }
}

static int parse_host_port(const char *s, char *host, size_t host_cap, int *port) {
    const char *colon = strrchr(s, ':');
    if (!colon || colon == s || (size_t)(colon - s) >= host_cap) {
        return -1;
    }
    long p = strtol(colon + 1, NULL, 10);
    if (p <= 0 || p > 65535) {
        return -1;
    }
    memcpy(host, s, (size_t)(colon - s));
    host[colon - s] = '\0';
    *port = (int)p;
    return 0;
}

void config_print_usage(const char *prog) {
    fprintf(stderr,
            "Usage: %s --id SAT-00001 [options]\n"
            "  --id ID              terminal id (env TERMINAL_ID)\n"
            "  --ingest URL         ws://host:port/ws/ingest (env INGEST_URL)\n"
            "  --api URL            fleet API base, e.g. http://host:8080 (env FLEET_API_URL)\n"
            "  --probe HOST:PORT    latency probe target; defaults to the ingest endpoint (env PROBE_TARGET)\n"
            "  --token TOKEN        ingest bearer token (env INGEST_TOKEN)\n"
            "  --mode sim|system    sim models the modem; system reads /proc/net/dev (env AGENT_MODE)\n"
            "  --iface NAME         interface for system mode, default first non-loopback (env AGENT_IFACE)\n"
            "  --interval-ms N      pin the report interval, ignoring remote config\n"
            "  --config-poll-ms N   how often to re-check remote config (default 30000)\n"
            "  --probes N           probes per report window (default 4)\n"
            "  --count N            exit after N reports (default: run forever)\n"
            "  -v                   verbose logging\n",
            prog);
}

int config_load(agent_config_t *cfg, int argc, char **argv) {
    memset(cfg, 0, sizeof *cfg);
    copy_str(cfg->ingest_url, sizeof cfg->ingest_url, "ws://localhost:8081/ws/ingest");
    copy_str(cfg->api_url, sizeof cfg->api_url, "http://localhost:8080");
    cfg->report_interval_ms = 5000;
    cfg->config_poll_ms = 30000;
    cfg->probes_per_report = 4;
    cfg->mode = MODE_SIM;
    cfg->downlink_cap_mbps = 220;
    cfg->uplink_cap_mbps = 25;

    copy_str(cfg->terminal_id, sizeof cfg->terminal_id, getenv("TERMINAL_ID"));
    copy_str(cfg->ingest_url, sizeof cfg->ingest_url, getenv("INGEST_URL"));
    copy_str(cfg->api_url, sizeof cfg->api_url, getenv("FLEET_API_URL"));
    copy_str(cfg->token, sizeof cfg->token, getenv("INGEST_TOKEN"));
    copy_str(cfg->iface, sizeof cfg->iface, getenv("AGENT_IFACE"));
    const char *probe = getenv("PROBE_TARGET");
    const char *mode = getenv("AGENT_MODE");

    static const struct option opts[] = {
        {"id", required_argument, 0, 'i'},          {"ingest", required_argument, 0, 'g'},
        {"api", required_argument, 0, 'a'},         {"probe", required_argument, 0, 'p'},
        {"token", required_argument, 0, 't'},       {"mode", required_argument, 0, 'm'},
        {"iface", required_argument, 0, 'f'},       {"interval-ms", required_argument, 0, 'n'},
        {"config-poll-ms", required_argument, 0, 'c'}, {"probes", required_argument, 0, 'b'},
        {"count", required_argument, 0, 'k'},       {"help", no_argument, 0, 'h'},
        {0, 0, 0, 0}};
    optind = 1;
    int ch;
    while ((ch = getopt_long(argc, argv, "vh", opts, NULL)) != -1) {
        switch (ch) {
            case 'i': copy_str(cfg->terminal_id, sizeof cfg->terminal_id, optarg); break;
            case 'g': copy_str(cfg->ingest_url, sizeof cfg->ingest_url, optarg); break;
            case 'a': copy_str(cfg->api_url, sizeof cfg->api_url, optarg); break;
            case 'p': probe = optarg; break;
            case 't': copy_str(cfg->token, sizeof cfg->token, optarg); break;
            case 'm': mode = optarg; break;
            case 'f': copy_str(cfg->iface, sizeof cfg->iface, optarg); break;
            case 'n':
                cfg->report_interval_ms = atoi(optarg);
                cfg->interval_pinned = 1;
                break;
            case 'c': cfg->config_poll_ms = atoi(optarg); break;
            case 'b': cfg->probes_per_report = atoi(optarg); break;
            case 'k': cfg->max_reports = atol(optarg); break;
            case 'v': cfg->verbose = 1; break;
            case 'h': return 1;
            default: return -1;
        }
    }

    if (cfg->terminal_id[0] == '\0') {
        fprintf(stderr, "error: terminal id is required (--id or TERMINAL_ID)\n");
        return -1;
    }
    if (mode) {
        if (strcmp(mode, "sim") == 0) {
            cfg->mode = MODE_SIM;
        } else if (strcmp(mode, "system") == 0) {
            cfg->mode = MODE_SYSTEM;
        } else {
            fprintf(stderr, "error: --mode must be sim or system\n");
            return -1;
        }
    }
    if (cfg->report_interval_ms < 1000 || cfg->report_interval_ms > 300000) {
        fprintf(stderr, "error: interval must be between 1000 and 300000 ms\n");
        return -1;
    }
    if (cfg->probes_per_report < 1 || cfg->probes_per_report > 32) {
        fprintf(stderr, "error: --probes must be between 1 and 32\n");
        return -1;
    }
    if (cfg->config_poll_ms < 1000) {
        cfg->config_poll_ms = 1000;
    }

    if (probe && *probe) {
        if (parse_host_port(probe, cfg->probe_host, sizeof cfg->probe_host, &cfg->probe_port) != 0) {
            fprintf(stderr, "error: --probe must be HOST:PORT\n");
            return -1;
        }
    } else {
        url_t u;
        if (url_parse(cfg->ingest_url, &u) != 0) {
            fprintf(stderr, "error: invalid ingest url %s\n", cfg->ingest_url);
            return -1;
        }
        copy_str(cfg->probe_host, sizeof cfg->probe_host, u.host);
        cfg->probe_port = u.port;
    }
    return 0;
}

int config_apply_remote(agent_config_t *cfg, const char *json) {
    char id[32];
    if (json_find_string(json, "terminalId", id, sizeof id) == 0 && strcmp(id, cfg->terminal_id) != 0) {
        return -1; /* config for some other terminal: refuse rather than misconfigure */
    }
    int changed = 0;
    double v;
    if (!cfg->interval_pinned && json_find_number(json, "reportIntervalMs", &v) == 0 && v >= 1000 &&
        v <= 300000 && (int)v != cfg->report_interval_ms) {
        cfg->report_interval_ms = (int)v;
        changed = 1;
    }
    if (json_find_number(json, "downlinkCapMbps", &v) == 0 && v > 0 && v != cfg->downlink_cap_mbps) {
        cfg->downlink_cap_mbps = v;
        changed = 1;
    }
    if (json_find_number(json, "uplinkCapMbps", &v) == 0 && v > 0 && v != cfg->uplink_cap_mbps) {
        cfg->uplink_cap_mbps = v;
        changed = 1;
    }
    return changed;
}
