#ifndef AGENT_BASE64_H
#define AGENT_BASE64_H

#include <stddef.h>
#include <stdint.h>

/* Bytes needed to encode `n` input bytes, including the NUL terminator. */
#define BASE64_ENCODED_SIZE(n) ((((n) + 2) / 3) * 4 + 1)

/* Standard (RFC 4648) base64 with padding. Returns characters written, or 0 if out_cap is too small. */
size_t base64_encode(const uint8_t *in, size_t len, char *out, size_t out_cap);

#endif
