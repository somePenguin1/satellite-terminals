#include "net.h"

#include <errno.h>
#include <fcntl.h>
#include <netdb.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>

int url_parse(const char *url, url_t *out) {
    memset(out, 0, sizeof *out);
    const char *sep = strstr(url, "://");
    if (!sep || (size_t)(sep - url) >= sizeof out->scheme) {
        return -1;
    }
    memcpy(out->scheme, url, (size_t)(sep - url));
    if (strcmp(out->scheme, "ws") == 0 || strcmp(out->scheme, "http") == 0) {
        out->port = 80;
    } else {
        return -1;
    }

    const char *host = sep + 3;
    const char *path = strchr(host, '/');
    const char *host_end = path ? path : host + strlen(host);
    const char *colon = memchr(host, ':', (size_t)(host_end - host));
    const char *name_end = colon ? colon : host_end;

    size_t host_len = (size_t)(name_end - host);
    if (host_len == 0 || host_len >= sizeof out->host) {
        return -1;
    }
    memcpy(out->host, host, host_len);

    if (colon) {
        char *end = NULL;
        long port = strtol(colon + 1, &end, 10);
        if (end != host_end || port <= 0 || port > 65535) {
            return -1;
        }
        out->port = (int)port;
    }

    const char *p = path ? path : "/";
    if (strlen(p) >= sizeof out->path) {
        return -1;
    }
    strcpy(out->path, p);
    return 0;
}

int tcp_connect(const char *host, int port, int timeout_ms) {
    char port_str[8];
    snprintf(port_str, sizeof port_str, "%d", port);

    struct addrinfo hints = {0};
    hints.ai_family = AF_UNSPEC;
    hints.ai_socktype = SOCK_STREAM;
    struct addrinfo *res = NULL;
    if (getaddrinfo(host, port_str, &hints, &res) != 0) {
        return -1;
    }

    int fd = -1;
    for (struct addrinfo *ai = res; ai; ai = ai->ai_next) {
        fd = socket(ai->ai_family, ai->ai_socktype, ai->ai_protocol);
        if (fd < 0) {
            continue;
        }
        int flags = fcntl(fd, F_GETFL, 0);
        fcntl(fd, F_SETFL, flags | O_NONBLOCK);

        int rc = connect(fd, ai->ai_addr, ai->ai_addrlen);
        if (rc < 0 && errno == EINPROGRESS) {
            struct pollfd pfd = {.fd = fd, .events = POLLOUT};
            rc = poll(&pfd, 1, timeout_ms);
            if (rc == 1) {
                int err = 0;
                socklen_t len = sizeof err;
                getsockopt(fd, SOL_SOCKET, SO_ERROR, &err, &len);
                rc = err == 0 ? 0 : -1;
            } else {
                rc = -1; /* timeout or poll error */
            }
        }
        if (rc == 0) {
            fcntl(fd, F_SETFL, flags);
            int one = 1;
            setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof one);
            break;
        }
        close(fd);
        fd = -1;
    }
    freeaddrinfo(res);
    return fd;
}

int send_all(int fd, const void *buf, size_t len) {
    const uint8_t *p = buf;
    while (len > 0) {
        ssize_t n = send(fd, p, len, MSG_NOSIGNAL);
        if (n < 0) {
            if (errno == EINTR) {
                continue;
            }
            return -1;
        }
        p += n;
        len -= (size_t)n;
    }
    return 0;
}

int recv_exact(int fd, void *buf, size_t len, int timeout_ms) {
    uint8_t *p = buf;
    int64_t deadline = now_monotonic_ms() + timeout_ms;
    while (len > 0) {
        int remaining = (int)(deadline - now_monotonic_ms());
        if (remaining <= 0) {
            return -1;
        }
        struct pollfd pfd = {.fd = fd, .events = POLLIN};
        int rc = poll(&pfd, 1, remaining);
        if (rc < 0 && errno == EINTR) {
            continue;
        }
        if (rc <= 0) {
            return -1;
        }
        ssize_t n = recv(fd, p, len, 0);
        if (n < 0 && errno == EINTR) {
            continue;
        }
        if (n <= 0) {
            return -1;
        }
        p += n;
        len -= (size_t)n;
    }
    return 0;
}

int64_t now_monotonic_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (int64_t)ts.tv_sec * 1000 + ts.tv_nsec / 1000000;
}

int64_t now_wall_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    return (int64_t)ts.tv_sec * 1000 + ts.tv_nsec / 1000000;
}

void random_bytes(uint8_t *buf, size_t len) {
    FILE *f = fopen("/dev/urandom", "rb");
    if (f) {
        size_t got = fread(buf, 1, len, f);
        fclose(f);
        if (got == len) {
            return;
        }
    }
    static uint32_t state = 0;
    if (state == 0) {
        state = (uint32_t)now_wall_ms() ^ (uint32_t)getpid();
    }
    for (size_t i = 0; i < len; i++) {
        state ^= state << 13;
        state ^= state >> 17;
        state ^= state << 5;
        buf[i] = (uint8_t)state;
    }
}
