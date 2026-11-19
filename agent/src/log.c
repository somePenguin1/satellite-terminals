#include "log.h"

#include <stdarg.h>
#include <stdio.h>
#include <time.h>

static log_level_t g_level = LOG_INFO;
static const char *const NAMES[] = {"DEBUG", "INFO", "WARN", "ERROR"};

void log_set_level(log_level_t level) { g_level = level; }

void log_msg(log_level_t level, const char *fmt, ...) {
    if (level < g_level) {
        return;
    }
    char ts[32];
    time_t now = time(NULL);
    struct tm tm;
    gmtime_r(&now, &tm);
    strftime(ts, sizeof ts, "%Y-%m-%dT%H:%M:%SZ", &tm);

    fprintf(stderr, "%s %-5s ", ts, NAMES[level]);
    va_list ap;
    va_start(ap, fmt);
    vfprintf(stderr, fmt, ap);
    va_end(ap);
    fputc('\n', stderr);
}
