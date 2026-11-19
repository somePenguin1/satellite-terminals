#include "ws.h"

#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <unistd.h>

#include "base64.h"
#include "log.h"
#include "net.h"
#include "sha1.h"

static const char WS_GUID[] = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11";

int ws_accept_key(const char *client_key, char out[29]) {
    char buf[128];
    int n = snprintf(buf, sizeof buf, "%s%s", client_key, WS_GUID);
    if (n < 0 || (size_t)n >= sizeof buf) {
        return -1;
    }
    uint8_t digest[SHA1_DIGEST_LEN];
    sha1((const uint8_t *)buf, (size_t)n, digest);
    return base64_encode(digest, sizeof digest, out, 29) == 28 ? 0 : -1;
}

size_t ws_encode_frame(uint8_t opcode, const uint8_t *payload, size_t len, const uint8_t mask[4],
                       uint8_t *out, size_t out_cap) {
    size_t header = 2 + 4 + (len < 126 ? 0 : len <= 0xFFFF ? 2 : 8);
    if (out_cap < header + len) {
        return 0;
    }
    size_t o = 0;
    out[o++] = (uint8_t)(0x80 | (opcode & 0x0F)); /* FIN + opcode */
    if (len < 126) {
        out[o++] = (uint8_t)(0x80 | len);
    } else if (len <= 0xFFFF) {
        out[o++] = 0x80 | 126;
        out[o++] = (uint8_t)(len >> 8);
        out[o++] = (uint8_t)len;
    } else {
        out[o++] = 0x80 | 127;
        for (int i = 7; i >= 0; i--) {
            out[o++] = (uint8_t)((uint64_t)len >> (8 * i));
        }
    }
    memcpy(out + o, mask, 4);
    o += 4;
    for (size_t i = 0; i < len; i++) {
        out[o + i] = payload[i] ^ mask[i & 3];
    }
    return o + len;
}

/* Reads the HTTP response head one byte at a time so no frame bytes are swallowed. */
static int read_http_head(int fd, char *buf, size_t cap, int timeout_ms) {
    size_t n = 0;
    while (n + 1 < cap) {
        if (recv_exact(fd, buf + n, 1, timeout_ms) != 0) {
            return -1;
        }
        n++;
        if (n >= 4 && memcmp(buf + n - 4, "\r\n\r\n", 4) == 0) {
            buf[n] = '\0';
            return 0;
        }
    }
    return -1;
}

static const char *find_header(const char *head, const char *name, size_t *value_len) {
    size_t name_len = strlen(name);
    for (const char *line = strstr(head, "\r\n"); line && line[2]; line = strstr(line + 2, "\r\n")) {
        const char *h = line + 2;
        if (strncasecmp(h, name, name_len) == 0 && h[name_len] == ':') {
            const char *v = h + name_len + 1;
            while (*v == ' ' || *v == '\t') {
                v++;
            }
            const char *end = strstr(v, "\r\n");
            *value_len = end ? (size_t)(end - v) : strlen(v);
            return v;
        }
    }
    return NULL;
}

int ws_connect(ws_conn_t *c, const char *url, const char *bearer_token, int timeout_ms) {
    c->fd = -1;
    c->open = 0;

    url_t u;
    if (url_parse(url, &u) != 0 || strcmp(u.scheme, "ws") != 0) {
        log_error("invalid ingest url: %s", url);
        return -1;
    }
    int fd = tcp_connect(u.host, u.port, timeout_ms);
    if (fd < 0) {
        return -1;
    }

    uint8_t nonce[16];
    random_bytes(nonce, sizeof nonce);
    char key[BASE64_ENCODED_SIZE(16)];
    base64_encode(nonce, sizeof nonce, key, sizeof key);

    char auth[256] = "";
    if (bearer_token && *bearer_token) {
        snprintf(auth, sizeof auth, "Authorization: Bearer %s\r\n", bearer_token);
    }
    char req[1024];
    int n = snprintf(req, sizeof req,
                     "GET %s HTTP/1.1\r\n"
                     "Host: %s:%d\r\n"
                     "Upgrade: websocket\r\n"
                     "Connection: Upgrade\r\n"
                     "Sec-WebSocket-Key: %s\r\n"
                     "Sec-WebSocket-Version: 13\r\n"
                     "User-Agent: satfleet-agent/1\r\n"
                     "%s\r\n",
                     u.path, u.host, u.port, key, auth);
    if (n < 0 || (size_t)n >= sizeof req || send_all(fd, req, (size_t)n) != 0) {
        close(fd);
        return -1;
    }

    char head[2048];
    if (read_http_head(fd, head, sizeof head, timeout_ms) != 0) {
        close(fd);
        return -1;
    }
    if (strncmp(head, "HTTP/1.1 101", 12) != 0) {
        char *eol = strstr(head, "\r\n");
        if (eol) {
            *eol = '\0';
        }
        log_warn("websocket upgrade refused: %s", head);
        close(fd);
        return -1;
    }

    char expected[29];
    size_t accept_len = 0;
    const char *accept = find_header(head, "Sec-WebSocket-Accept", &accept_len);
    if (ws_accept_key(key, expected) != 0 || !accept || accept_len != 28 ||
        memcmp(accept, expected, 28) != 0) {
        log_warn("websocket handshake failed: bad Sec-WebSocket-Accept");
        close(fd);
        return -1;
    }

    c->fd = fd;
    c->open = 1;
    return 0;
}

static int ws_send_frame(ws_conn_t *c, uint8_t opcode, const uint8_t *payload, size_t len) {
    if (!c->open) {
        return -1;
    }
    uint8_t stack_buf[4096];
    uint8_t *buf = stack_buf;
    size_t cap = len + WS_MAX_HEADER;
    if (cap > sizeof stack_buf) {
        buf = malloc(cap);
        if (!buf) {
            return -1;
        }
    }
    uint8_t mask[4];
    random_bytes(mask, sizeof mask);
    size_t n = ws_encode_frame(opcode, payload, len, mask, buf, cap);
    int rc = (n > 0 && send_all(c->fd, buf, n) == 0) ? 0 : -1;
    if (buf != stack_buf) {
        free(buf);
    }
    if (rc != 0) {
        c->open = 0;
    }
    return rc;
}

int ws_send_text(ws_conn_t *c, const char *data, size_t len) {
    return ws_send_frame(c, WS_OP_TEXT, (const uint8_t *)data, len);
}

int ws_poll(ws_conn_t *c) {
    while (c->open) {
        struct pollfd pfd = {.fd = c->fd, .events = POLLIN};
        if (poll(&pfd, 1, 0) <= 0) {
            return 0; /* nothing pending */
        }
        if (pfd.revents & (POLLERR | POLLHUP)) {
            c->open = 0;
            break;
        }

        uint8_t hdr[2];
        if (recv_exact(c->fd, hdr, 2, 1000) != 0) {
            c->open = 0;
            break;
        }
        uint8_t opcode = hdr[0] & 0x0F;
        uint64_t len = hdr[1] & 0x7F;
        if (len == 126) {
            uint8_t ext[2];
            if (recv_exact(c->fd, ext, 2, 1000) != 0) {
                c->open = 0;
                break;
            }
            len = ((uint64_t)ext[0] << 8) | ext[1];
        } else if (len == 127) {
            uint8_t ext[8];
            if (recv_exact(c->fd, ext, 8, 1000) != 0) {
                c->open = 0;
                break;
            }
            len = 0;
            for (int i = 0; i < 8; i++) {
                len = (len << 8) | ext[i];
            }
        }
        if (len > 4096) { /* the ingest server only sends small control/ack frames */
            c->open = 0;
            break;
        }
        uint8_t payload[4096];
        if (len > 0 && recv_exact(c->fd, payload, (size_t)len, 1000) != 0) {
            c->open = 0;
            break;
        }

        switch (opcode) {
            case WS_OP_PING:
                ws_send_frame(c, WS_OP_PONG, payload, (size_t)len);
                break;
            case WS_OP_CLOSE:
                ws_send_frame(c, WS_OP_CLOSE, payload, len >= 2 ? 2 : 0);
                c->open = 0;
                break;
            case WS_OP_TEXT:
                log_debug("server: %.*s", (int)len, (const char *)payload);
                break;
            default:
                break;
        }
    }
    if (c->fd >= 0 && !c->open) {
        close(c->fd);
        c->fd = -1;
    }
    return c->open ? 0 : -1;
}

void ws_close(ws_conn_t *c, uint16_t code) {
    if (c->open) {
        uint8_t payload[2] = {(uint8_t)(code >> 8), (uint8_t)code};
        ws_send_frame(c, WS_OP_CLOSE, payload, sizeof payload);
    }
    c->open = 0;
    if (c->fd >= 0) {
        close(c->fd);
        c->fd = -1;
    }
}
