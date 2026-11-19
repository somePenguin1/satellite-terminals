/* Unit tests for the agent. No framework: each test is a function, CHECK records failures. */
#define _DEFAULT_SOURCE
#include <arpa/inet.h>
#include <math.h>
#include <netinet/in.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/wait.h>
#include <unistd.h>

#include "../src/base64.h"
#include "../src/config.h"
#include "../src/http.h"
#include "../src/linkmodel.h"
#include "../src/net.h"
#include "../src/netdev.h"
#include "../src/probe.h"
#include "../src/queue.h"
#include "../src/report.h"
#include "../src/sha1.h"
#include "../src/ws.h"

static int g_failures = 0, g_checks = 0;

#define CHECK(cond)                                                          \
    do {                                                                     \
        g_checks++;                                                          \
        if (!(cond)) {                                                       \
            g_failures++;                                                    \
            fprintf(stderr, "  FAIL %s:%d: %s\n", __FILE__, __LINE__, #cond); \
        }                                                                    \
    } while (0)
#define CHECK_STR(a, b) CHECK(strcmp((a), (b)) == 0)
#define CHECK_NEAR(a, b, eps) CHECK(fabs((a) - (b)) < (eps))

static void hex(const uint8_t *d, size_t n, char *out) {
    for (size_t i = 0; i < n; i++) sprintf(out + 2 * i, "%02x", d[i]);
}

static void test_sha1(void) {
    uint8_t d[20];
    char h[41];
    sha1((const uint8_t *)"abc", 3, d);
    hex(d, 20, h);
    CHECK_STR(h, "a9993e364706816aba3e25717850c26c9cd0d89d");
    sha1((const uint8_t *)"", 0, d);
    hex(d, 20, h);
    CHECK_STR(h, "da39a3ee5e6b4b0d3255bfef95601890afd80709");
    /* 56-byte message forces the two-block padding path. */
    const char *m = "abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq";
    sha1((const uint8_t *)m, strlen(m), d);
    hex(d, 20, h);
    CHECK_STR(h, "84983e441c3bd26ebaae4aa1f95129e5e54670f1");
}

static void test_base64(void) {
    char out[16];
    CHECK(base64_encode((const uint8_t *)"Man", 3, out, sizeof out) == 4);
    CHECK_STR(out, "TWFu");
    base64_encode((const uint8_t *)"Ma", 2, out, sizeof out);
    CHECK_STR(out, "TWE=");
    base64_encode((const uint8_t *)"M", 1, out, sizeof out);
    CHECK_STR(out, "TQ==");
    CHECK(base64_encode((const uint8_t *)"Man", 3, out, 4) == 0); /* no room for NUL */
}

static void test_ws_accept_key(void) {
    char accept[29];
    /* Example from RFC 6455 section 1.3 */
    CHECK(ws_accept_key("dGhlIHNhbXBsZSBub25jZQ==", accept) == 0);
    CHECK_STR(accept, "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=");
}

static void test_ws_frame_encoding(void) {
    uint8_t mask[4] = {0x37, 0xfa, 0x21, 0x3d};
    uint8_t out[80000];
    /* RFC 6455 section 5.7: masked "Hello" */
    size_t n = ws_encode_frame(WS_OP_TEXT, (const uint8_t *)"Hello", 5, mask, out, sizeof out);
    const uint8_t expect[] = {0x81, 0x85, 0x37, 0xfa, 0x21, 0x3d, 0x7f, 0x9f, 0x4d, 0x51, 0x58};
    CHECK(n == sizeof expect);
    CHECK(memcmp(out, expect, sizeof expect) == 0);

    static uint8_t payload[70000];
    memset(payload, 'x', sizeof payload);
    n = ws_encode_frame(WS_OP_TEXT, payload, 300, mask, out, sizeof out);
    CHECK(n == 2 + 2 + 4 + 300);
    CHECK(out[1] == (0x80 | 126) && out[2] == 0x01 && out[3] == 0x2c);

    n = ws_encode_frame(WS_OP_BINARY, payload, 70000, mask, out, sizeof out);
    CHECK(n == 2 + 8 + 4 + 70000);
    CHECK(out[1] == (0x80 | 127) && out[7] == 0x01 && out[8] == 0x11 && out[9] == 0x70);

    CHECK(ws_encode_frame(WS_OP_TEXT, payload, 300, mask, out, 100) == 0);
}

static void test_url_parse(void) {
    url_t u;
    CHECK(url_parse("ws://ingest:8081/ws/ingest", &u) == 0);
    CHECK_STR(u.scheme, "ws");
    CHECK_STR(u.host, "ingest");
    CHECK(u.port == 8081);
    CHECK_STR(u.path, "/ws/ingest");
    CHECK(url_parse("http://localhost", &u) == 0);
    CHECK(u.port == 80);
    CHECK_STR(u.path, "/");
    CHECK(url_parse("https://secure:443/", &u) != 0);
    CHECK(url_parse("ws://host:99999/", &u) != 0);
    CHECK(url_parse("ws://:80/", &u) != 0);
}

static void test_report_json(void) {
    report_t r = {0};
    strcpy(r.terminal_id, "SAT-00042");
    r.timestamp_ms = 1763550000123;
    r.seq = 7;
    r.link_state = LINK_DEGRADED;
    r.latency_ms = 41.256;
    r.snr_db = NAN;
    char json[REPORT_JSON_MAX];
    int n = report_to_json(&r, "agent-c/test", json, sizeof json);
    CHECK(n > 0);
    CHECK(strstr(json, "\"terminalId\":\"SAT-00042\"") != NULL);
    CHECK(strstr(json, "\"timestampMs\":1763550000123") != NULL);
    CHECK(strstr(json, "\"linkState\":\"DEGRADED\"") != NULL);
    CHECK(strstr(json, "\"latencyMs\":41.26") != NULL);
    CHECK(strstr(json, "snrDb") == NULL); /* omitted when unknown */

    r.snr_db = 9.5;
    strcpy(r.terminal_id, "bad\"id");
    report_to_json(&r, "fw", json, sizeof json);
    CHECK(strstr(json, "\"terminalId\":\"bad\\\"id\"") != NULL);
    CHECK(strstr(json, "\"snrDb\":9.50") != NULL);
    CHECK(report_to_json(&r, "fw", json, 20) == -1);
}

static void test_json_find(void) {
    const char *doc = "{\"terminalId\": \"SAT-00001\", \"version\":3,\n \"reportIntervalMs\" : 2500,"
                      "\"note\":\"reportIntervalMs\",\"downlinkCapMbps\":350.0}";
    double v = 0;
    CHECK(json_find_number(doc, "reportIntervalMs", &v) == 0 && v == 2500);
    CHECK(json_find_number(doc, "downlinkCapMbps", &v) == 0 && v == 350.0);
    CHECK(json_find_number(doc, "missing", &v) != 0);
    char s[32];
    CHECK(json_find_string(doc, "terminalId", s, sizeof s) == 0);
    CHECK_STR(s, "SAT-00001");
    CHECK(json_find_string(doc, "version", s, sizeof s) != 0); /* not a string */
}

static void test_config(void) {
    agent_config_t c;
    char *argv[] = {"agent", "--id", "SAT-00009", "--ingest", "ws://gw:9000/ws/ingest", "--count", "3", NULL};
    unsetenv("TERMINAL_ID");
    CHECK(config_load(&c, 7, argv) == 0);
    CHECK_STR(c.terminal_id, "SAT-00009");
    CHECK_STR(c.probe_host, "gw"); /* probe defaults to the ingest endpoint */
    CHECK(c.probe_port == 9000);
    CHECK(c.max_reports == 3);

    CHECK(config_apply_remote(&c, "{\"terminalId\":\"SAT-00009\",\"reportIntervalMs\":2000,\"downlinkCapMbps\":500}") == 1);
    CHECK(c.report_interval_ms == 2000);
    CHECK(c.downlink_cap_mbps == 500);
    CHECK(config_apply_remote(&c, "{\"terminalId\":\"SAT-00009\",\"reportIntervalMs\":2000}") == 0);
    CHECK(config_apply_remote(&c, "{\"terminalId\":\"SAT-00010\",\"reportIntervalMs\":9000}") == -1);
    CHECK(config_apply_remote(&c, "{\"terminalId\":\"SAT-00009\",\"reportIntervalMs\":10}") == 0); /* out of range */
    CHECK(c.report_interval_ms == 2000);

    char *pinned[] = {"agent", "--id", "SAT-1", "--interval-ms", "7000", NULL};
    CHECK(config_load(&c, 5, pinned) == 0);
    CHECK(config_apply_remote(&c, "{\"reportIntervalMs\":2000}") == 0);
    CHECK(c.report_interval_ms == 7000);

    char *bad[] = {"agent", "--id", "SAT-1", "--mode", "turbo", NULL};
    CHECK(config_load(&c, 5, bad) == -1);
    char *noid[] = {"agent", NULL};
    CHECK(config_load(&c, 1, noid) == -1);
}

static void test_probe_stats(void) {
    probe_window_t w = {.rtt_ms = {40, 44, -1, 42, 50}, .count = 5};
    probe_stats_t s;
    probe_compute_stats(&w, &s);
    CHECK(s.successes == 4);
    CHECK_NEAR(s.avg_rtt_ms, 44.0, 1e-9);
    CHECK_NEAR(s.loss_pct, 20.0, 1e-9);
    CHECK_NEAR(s.jitter_ms, (4.0 + 2.0 + 8.0) / 3.0, 1e-9); /* gaps skip the lost probe */

    probe_window_t lost = {.rtt_ms = {-1, -1}, .count = 2};
    probe_compute_stats(&lost, &s);
    CHECK(s.successes == 0 && isnan(s.avg_rtt_ms) && s.loss_pct == 100.0);
}

static void test_netdev(void) {
    const char *path = "/tmp/satfleet_test_netdev";
    FILE *f = fopen(path, "w");
    fputs("Inter-|   Receive                                                |  Transmit\n"
          " face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed\n"
          "    lo:  1000      10    0    0    0     0          0         0     1000      10    0    0    0     0       0          0\n"
          "  eth0: 5000000   4000    0    0    0     0          0         0   250000    2000    0    0    0     0       0          0\n",
          f);
    fclose(f);
    netdev_counters_t c;
    CHECK(netdev_read(path, "", &c) == 0);
    CHECK(c.rx_bytes == 5000000 && c.tx_bytes == 250000);
    CHECK(netdev_read(path, "lo", &c) == 0 && c.rx_bytes == 1000);
    CHECK(netdev_read(path, "wlan9", &c) != 0);
    unlink(path);

    CHECK_NEAR(netdev_rate_mbps(0, 1250000, 1000), 10.0, 1e-9);
    CHECK(netdev_rate_mbps(500, 100, 1000) == 0.0); /* counter reset */
    CHECK(netdev_rate_mbps(0, 100, 0) == 0.0);
}

static void test_queue(void) {
    static report_queue_t q;
    queue_init(&q);
    CHECK(queue_peek(&q) == NULL);
    report_t r = {0};
    for (uint64_t i = 1; i <= QUEUE_CAPACITY + 5; i++) {
        r.seq = i;
        queue_push(&q, &r);
    }
    CHECK(q.len == QUEUE_CAPACITY);
    CHECK(q.dropped == 5);
    CHECK(queue_peek(&q)->seq == 6); /* oldest five were dropped */
    queue_pop(&q);
    CHECK(queue_peek(&q)->seq == 7);
}

static void test_linkmodel(void) {
    linkmodel_t a, b;
    linkmodel_init(&a, 1234);
    linkmodel_init(&b, 1234);
    linkmodel_sample_t sa, sb;
    int fades = 0;
    for (int i = 0; i < 20000; i++) {
        linkmodel_step(&a, 5.0, (i * 5 / 3600.0), &sa);
        linkmodel_step(&b, 5.0, (i * 5 / 3600.0), &sb);
        if (sa.snr_db != sb.snr_db) { /* deterministic per seed */
            CHECK(0 && "model not deterministic");
            break;
        }
        if (sa.downlink_mbps < 0 || sa.downlink_mbps > a.downlink_cap_mbps || sa.loss_pct < 0 ||
            sa.loss_pct > 100 || sa.obstruction_pct < 0 || sa.extra_latency_ms < 20) {
            CHECK(0 && "sample out of range");
            break;
        }
        if (a.fade_db > 0) fades++;
    }
    CHECK(fades > 0); /* ~28 hours simulated: rain fade should occur */
    CHECK(fades < 20000 / 2);
}

static void test_http_parse(void) {
    http_response_t r;
    const char *plain = "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nETag: \"v3\"\r\n"
                        "Content-Length: 7\r\n\r\n{\"a\":1}extra";
    CHECK(http_parse_response(plain, strlen(plain), &r) == 0);
    CHECK(r.status == 200);
    CHECK_STR(r.etag, "\"v3\"");
    CHECK(r.body_len == 7); /* trailing bytes beyond Content-Length are ignored */
    CHECK_STR(r.body, "{\"a\":1}");

    const char *chunked = "HTTP/1.1 200 OK\r\ntransfer-encoding: chunked\r\netag: \"v9\"\r\n\r\n"
                          "7\r\n{\"a\":12\r\n3\r\n34}\r\n0\r\n\r\n";
    CHECK(http_parse_response(chunked, strlen(chunked), &r) == 0);
    CHECK_STR(r.body, "{\"a\":1234}");
    CHECK_STR(r.etag, "\"v9\"");

    const char *not_modified = "HTTP/1.1 304 Not Modified\r\nETag: \"v9\"\r\n\r\n";
    CHECK(http_parse_response(not_modified, strlen(not_modified), &r) == 0);
    CHECK(r.status == 304 && r.body_len == 0);
    CHECK(http_parse_response("garbage", 7, &r) != 0);
}

/* End-to-end over loopback: a forked child plays the server, verifying the handshake and
 * unmasking the first frame; the parent is the real client code path. */
static void test_ws_loopback(void) {
    int srv = socket(AF_INET, SOCK_STREAM, 0);
    struct sockaddr_in addr = {.sin_family = AF_INET, .sin_addr.s_addr = htonl(INADDR_LOOPBACK)};
    int one = 1;
    setsockopt(srv, SOL_SOCKET, SO_REUSEADDR, &one, sizeof one);
    CHECK(bind(srv, (struct sockaddr *)&addr, sizeof addr) == 0);
    socklen_t len = sizeof addr;
    getsockname(srv, (struct sockaddr *)&addr, &len);
    listen(srv, 1);
    int port = ntohs(addr.sin_port);

    pid_t pid = fork();
    if (pid == 0) {
        int c = accept(srv, NULL, NULL);
        char req[2048] = {0};
        size_t n = 0;
        while (n < sizeof req - 1 && !strstr(req, "\r\n\r\n")) {
            ssize_t r = recv(c, req + n, sizeof req - 1 - n, 0);
            if (r <= 0) _exit(10);
            n += (size_t)r;
        }
        if (!strstr(req, "Authorization: Bearer secret")) _exit(11);
        char *k = strstr(req, "Sec-WebSocket-Key: ");
        if (!k) _exit(12);
        k += 19;
        char key[64] = {0};
        memcpy(key, k, (size_t)(strstr(k, "\r\n") - k));
        char accept[29];
        ws_accept_key(key, accept);
        char resp[256];
        int rn = snprintf(resp, sizeof resp,
                          "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
                          "Sec-WebSocket-Accept: %s\r\n\r\n", accept);
        send(c, resp, (size_t)rn, 0);

        uint8_t hdr[6];
        if (recv_exact(c, hdr, 6, 2000) != 0) _exit(13);
        if (hdr[0] != 0x81 || !(hdr[1] & 0x80)) _exit(14);
        size_t plen = hdr[1] & 0x7F;
        uint8_t body[128];
        if (recv_exact(c, body, plen, 2000) != 0) _exit(15);
        for (size_t i = 0; i < plen; i++) body[i] ^= hdr[2 + (i & 3)];
        if (plen != 13 || memcmp(body, "{\"hello\":\"x\"}", 13) != 0) _exit(16);
        _exit(0);
    }
    close(srv);

    char url[64];
    snprintf(url, sizeof url, "ws://127.0.0.1:%d/ws/ingest", port);
    ws_conn_t c;
    CHECK(ws_connect(&c, url, "secret", 2000) == 0);
    CHECK(ws_send_text(&c, "{\"hello\":\"x\"}", 13) == 0);
    int status = -1;
    waitpid(pid, &status, 0);
    CHECK(WIFEXITED(status) && WEXITSTATUS(status) == 0);
    ws_close(&c, 1000);
}

int main(void) {
    struct {
        const char *name;
        void (*fn)(void);
    } tests[] = {
        {"sha1", test_sha1},           {"base64", test_base64},
        {"ws_accept_key", test_ws_accept_key}, {"ws_frame_encoding", test_ws_frame_encoding},
        {"url_parse", test_url_parse}, {"report_json", test_report_json},
        {"json_find", test_json_find}, {"config", test_config},
        {"probe_stats", test_probe_stats}, {"netdev", test_netdev},
        {"queue", test_queue},         {"linkmodel", test_linkmodel},
        {"http_parse", test_http_parse}, {"ws_loopback", test_ws_loopback},
    };
    for (size_t i = 0; i < sizeof tests / sizeof tests[0]; i++) {
        int before = g_failures;
        tests[i].fn();
        printf("%-20s %s\n", tests[i].name, g_failures == before ? "ok" : "FAILED");
    }
    printf("\n%d checks, %d failures\n", g_checks, g_failures);
    return g_failures ? 1 : 0;
}
