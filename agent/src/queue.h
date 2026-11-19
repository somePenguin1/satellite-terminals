#ifndef AGENT_QUEUE_H
#define AGENT_QUEUE_H

/* Fixed-capacity ring buffer for store-and-forward: reports produced while the uplink is
 * down are kept and replayed oldest-first on reconnect. When full, the oldest is dropped. */

#include <stddef.h>
#include <stdint.h>

#include "report.h"

#define QUEUE_CAPACITY 720 /* one hour at the default 5 s interval, ~70 KB */

typedef struct {
    report_t items[QUEUE_CAPACITY];
    size_t head; /* index of oldest */
    size_t len;
    uint64_t dropped;
} report_queue_t;

void queue_init(report_queue_t *q);
void queue_push(report_queue_t *q, const report_t *r);
const report_t *queue_peek(const report_queue_t *q);
void queue_pop(report_queue_t *q);

#endif
