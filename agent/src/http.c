#include "http.h"

#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/socket.h>
#include <unistd.h>

#include "net.h"

static const char *header_value(const char *headers, const char *end, const char *name, size_t *len) {
    size_t name_len = strlen(name);
    const char *line = headers;
    while (line < end) {
        const char *eol = strstr(line, "\r\n");
        if (!eol || eol > end) {
            eol = end;
        }
        if ((size_t)(eol - line) > name_len && strncasecmp(line, name, name_len) == 0 && line[name_len] == ':') {
            const char *v = line + name_len + 1;
            while (v < eol && (*v == ' ' || *v == '\t')) {
                v++;
            }
            *len = (size_t)(eol - v);
            return v;
        }
        line = eol + 2;
    }
    return NULL;
}

static int decode_chunked(const char *p, const char *end, http_response_t *out) {
    out->body_len = 0;
    while (p < end) {
        char *ext = NULL;
        unsigned long size = strtoul(p, &ext, 16);
        const char *eol = strstr(p, "\r\n");
        if (!eol || ext == p) {
            return -1;
        }
        p = eol + 2;
        if (size == 0) {
            break;
        }
        if (p + size > end || out->body_len + size >= sizeof out->body) {
            return -1;
        }
        memcpy(out->body + out->body_len, p, size);
        out->body_len += size;
        p += size + 2; /* skip CRLF after chunk */
    }
    out->body[out->body_len] = '\0';
    return 0;
}

int http_parse_response(const char *raw, size_t len, http_response_t *out) {
    memset(out, 0, sizeof *out);
    const char *end = raw + len;
    const char *head_end = NULL;
    for (const char *p = raw; p + 4 <= end; p++) {
        if (memcmp(p, "\r\n\r\n", 4) == 0) {
            head_end = p;
            break;
        }
    }
    if (!head_end || len < 12 || strncmp(raw, "HTTP/1.", 7) != 0) {
        return -1;
    }
    out->status = atoi(raw + 9);

    const char *headers = strstr(raw, "\r\n") + 2;
    size_t vlen = 0;
    const char *etag = header_value(headers, head_end, "ETag", &vlen);
    if (etag && vlen < sizeof out->etag) {
        memcpy(out->etag, etag, vlen);
        out->etag[vlen] = '\0';
    }

    const char *body = head_end + 4;
    const char *te = header_value(headers, head_end, "Transfer-Encoding", &vlen);
    if (te && vlen >= 7 && strncasecmp(te, "chunked", 7) == 0) {
        return decode_chunked(body, end, out);
    }
    size_t body_len = (size_t)(end - body);
    const char *cl = header_value(headers, head_end, "Content-Length", &vlen);
    if (cl) {
        size_t declared = (size_t)strtoul(cl, NULL, 10);
        if (declared < body_len) {
            body_len = declared;
        }
    }
    if (body_len >= sizeof out->body) {
        return -1;
    }
    memcpy(out->body, body, body_len);
    out->body[body_len] = '\0';
    out->body_len = body_len;
    return 0;
}

int http_get(const char *url, const char *if_none_match, int timeout_ms, http_response_t *out) {
    url_t u;
    if (url_parse(url, &u) != 0 || strcmp(u.scheme, "http") != 0) {
        return -1;
    }
    int fd = tcp_connect(u.host, u.port, timeout_ms);
    if (fd < 0) {
        return -1;
    }

    char cond[128] = "";
    if (if_none_match && *if_none_match) {
        snprintf(cond, sizeof cond, "If-None-Match: %s\r\n", if_none_match);
    }
    char req[1024];
    int n = snprintf(req, sizeof req,
                     "GET %s HTTP/1.1\r\nHost: %s:%d\r\nAccept: application/json\r\n"
                     "User-Agent: satfleet-agent/1\r\n%sConnection: close\r\n\r\n",
                     u.path, u.host, u.port, cond);
    if (n < 0 || (size_t)n >= sizeof req || send_all(fd, req, (size_t)n) != 0) {
        close(fd);
        return -1;
    }

    /* Connection: close means the server ends the body by closing the socket. */
    size_t cap = HTTP_MAX_RESPONSE + 4096, got = 0;
    char *raw = malloc(cap + 1);
    if (!raw) {
        close(fd);
        return -1;
    }
    int64_t deadline = now_monotonic_ms() + timeout_ms;
    while (got < cap) {
        int remaining = (int)(deadline - now_monotonic_ms());
        struct pollfd pfd = {.fd = fd, .events = POLLIN};
        if (remaining <= 0 || poll(&pfd, 1, remaining) <= 0) {
            break;
        }
        ssize_t r = recv(fd, raw + got, cap - got, 0);
        if (r <= 0) {
            break;
        }
        got += (size_t)r;
    }
    close(fd);
    raw[got] = '\0';

    int rc = http_parse_response(raw, got, out);
    free(raw);
    return rc;
}
