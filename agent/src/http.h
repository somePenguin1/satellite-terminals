#ifndef AGENT_HTTP_H
#define AGENT_HTTP_H

#include <stddef.h>

#define HTTP_MAX_RESPONSE (64 * 1024)

typedef struct {
    int status;
    char etag[64];
    char body[HTTP_MAX_RESPONSE];
    size_t body_len;
} http_response_t;

/* Parses a complete raw HTTP/1.1 response (status line, headers, identity or chunked body).
 * raw must be NUL-terminated at raw[len]. */
int http_parse_response(const char *raw, size_t len, http_response_t *out);

/* GET with optional If-None-Match. Returns 0 if any HTTP response was received (check out->status). */
int http_get(const char *url, const char *if_none_match, int timeout_ms, http_response_t *out);

#endif
