#ifndef AGENT_WS_H
#define AGENT_WS_H

/* Minimal RFC 6455 WebSocket client: text frames out, ping/pong/close handling in.
 * Deliberately small (no TLS, no extensions, no fragmentation on send) so it fits
 * on constrained terminal CPUs without pulling in a library. */

#include <stddef.h>
#include <stdint.h>

#define WS_OP_CONT 0x0
#define WS_OP_TEXT 0x1
#define WS_OP_BINARY 0x2
#define WS_OP_CLOSE 0x8
#define WS_OP_PING 0x9
#define WS_OP_PONG 0xA

#define WS_MAX_HEADER 14 /* 2 + 8 (len) + 4 (mask) */

typedef struct {
    int fd;
    int open;
} ws_conn_t;

/* Computes Sec-WebSocket-Accept for a client key. out must hold 29 bytes. */
int ws_accept_key(const char *client_key, char out[29]);

/* Encodes one masked client frame into out. Returns bytes written, or 0 if out_cap is too small. */
size_t ws_encode_frame(uint8_t opcode, const uint8_t *payload, size_t len, const uint8_t mask[4],
                       uint8_t *out, size_t out_cap);

/* Opens a connection and completes the handshake. bearer_token may be NULL or "". */
int ws_connect(ws_conn_t *c, const char *url, const char *bearer_token, int timeout_ms);

int ws_send_text(ws_conn_t *c, const char *data, size_t len);

/* Services inbound control frames without blocking. Returns 0 while open, -1 once closed. */
int ws_poll(ws_conn_t *c);

void ws_close(ws_conn_t *c, uint16_t code);

#endif
