/* Compact SHA-1 (FIPS 180-4). Used only to verify the WebSocket handshake
 * (RFC 6455 §4.2.2), never for security-sensitive hashing. */
#include "sha1.h"

#include <string.h>

#define ROL(v, n) (((v) << (n)) | ((v) >> (32 - (n))))

static void sha1_block(uint32_t h[5], const uint8_t block[64]) {
    uint32_t w[80];
    for (int i = 0; i < 16; i++) {
        w[i] = ((uint32_t)block[i * 4] << 24) | ((uint32_t)block[i * 4 + 1] << 16) |
               ((uint32_t)block[i * 4 + 2] << 8) | (uint32_t)block[i * 4 + 3];
    }
    for (int i = 16; i < 80; i++) {
        w[i] = ROL(w[i - 3] ^ w[i - 8] ^ w[i - 14] ^ w[i - 16], 1);
    }

    uint32_t a = h[0], b = h[1], c = h[2], d = h[3], e = h[4];
    for (int i = 0; i < 80; i++) {
        uint32_t f, k;
        if (i < 20) {
            f = (b & c) | (~b & d);
            k = 0x5A827999u;
        } else if (i < 40) {
            f = b ^ c ^ d;
            k = 0x6ED9EBA1u;
        } else if (i < 60) {
            f = (b & c) | (b & d) | (c & d);
            k = 0x8F1BBCDCu;
        } else {
            f = b ^ c ^ d;
            k = 0xCA62C1D6u;
        }
        uint32_t t = ROL(a, 5) + f + e + k + w[i];
        e = d;
        d = c;
        c = ROL(b, 30);
        b = a;
        a = t;
    }
    h[0] += a;
    h[1] += b;
    h[2] += c;
    h[3] += d;
    h[4] += e;
}

void sha1(const uint8_t *data, size_t len, uint8_t out[SHA1_DIGEST_LEN]) {
    uint32_t h[5] = {0x67452301u, 0xEFCDAB89u, 0x98BADCFEu, 0x10325476u, 0xC3D2E1F0u};
    uint8_t block[64];
    size_t i = 0;

    for (; i + 64 <= len; i += 64) {
        sha1_block(h, data + i);
    }

    /* Final block(s): remaining bytes, 0x80, zero padding, 64-bit big-endian bit length. */
    size_t rem = len - i;
    memset(block, 0, sizeof block);
    memcpy(block, data + i, rem);
    block[rem] = 0x80;
    if (rem >= 56) {
        sha1_block(h, block);
        memset(block, 0, sizeof block);
    }
    uint64_t bits = (uint64_t)len * 8u;
    for (int j = 0; j < 8; j++) {
        block[63 - j] = (uint8_t)(bits >> (8 * j));
    }
    sha1_block(h, block);

    for (int j = 0; j < 5; j++) {
        out[j * 4] = (uint8_t)(h[j] >> 24);
        out[j * 4 + 1] = (uint8_t)(h[j] >> 16);
        out[j * 4 + 2] = (uint8_t)(h[j] >> 8);
        out[j * 4 + 3] = (uint8_t)h[j];
    }
}
