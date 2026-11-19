#ifndef AGENT_NET_H
#define AGENT_NET_H

#include <stddef.h>
#include <stdint.h>

typedef struct {
    char scheme[8]; /* "ws", "http" */
    char host[128];
    int port;
    char path[256]; /* always starts with '/' */
} url_t;

/* Parses scheme://host[:port][/path]. Supports ws and http only (TLS terminates at the gateway). */
int url_parse(const char *url, url_t *out);

/* Connects with a timeout. Returns a blocking socket fd, or -1. */
int tcp_connect(const char *host, int port, int timeout_ms);

/* Writes all bytes or fails. Never raises SIGPIPE. */
int send_all(int fd, const void *buf, size_t len);

/* Reads exactly len bytes, waiting at most timeout_ms in total. 0 on success, -1 on error/EOF/timeout. */
int recv_exact(int fd, void *buf, size_t len, int timeout_ms);

/* Monotonic clock in milliseconds (for scheduling) and wall clock in ms (for timestamps). */
int64_t now_monotonic_ms(void);
int64_t now_wall_ms(void);

/* Fills buf with bytes from /dev/urandom, falling back to a time-seeded PRNG. */
void random_bytes(uint8_t *buf, size_t len);

#endif
