#ifndef AGENT_CONFIG_H
#define AGENT_CONFIG_H

#define AGENT_VERSION "agent-c/1.4.0"

typedef enum { MODE_SIM = 0, MODE_SYSTEM = 1 } agent_mode_t;

typedef struct {
    char terminal_id[32];
    char ingest_url[256];
    char api_url[256];
    char probe_host[128];
    int probe_port;
    char token[128];
    char iface[32];
    agent_mode_t mode;

    int report_interval_ms; /* current effective interval */
    int interval_pinned;    /* set by --interval-ms; remote config cannot override */
    int config_poll_ms;
    int probes_per_report;
    long max_reports; /* 0 = run forever */
    int verbose;

    /* Plan shaping pushed down from the fleet API (read-only on the device). */
    double downlink_cap_mbps;
    double uplink_cap_mbps;
} agent_config_t;

/* Fills cfg from defaults, then environment variables, then command-line flags (highest wins).
 * Returns 0 on success, 1 if --help was requested, -1 on invalid input (message on stderr). */
int config_load(agent_config_t *cfg, int argc, char **argv);

/* Applies a v1 config document from GET /api/v1/terminals/{id}/config.
 * Returns 1 if anything changed, 0 if not, -1 if the document is unusable. */
int config_apply_remote(agent_config_t *cfg, const char *json);

void config_print_usage(const char *prog);

#endif
