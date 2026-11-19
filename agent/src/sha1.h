#ifndef AGENT_SHA1_H
#define AGENT_SHA1_H

#include <stddef.h>
#include <stdint.h>

#define SHA1_DIGEST_LEN 20

void sha1(const uint8_t *data, size_t len, uint8_t out[SHA1_DIGEST_LEN]);

#endif
