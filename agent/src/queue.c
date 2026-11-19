#include "queue.h"

#include <string.h>

void queue_init(report_queue_t *q) { memset(q, 0, sizeof *q); }

void queue_push(report_queue_t *q, const report_t *r) {
    if (q->len == QUEUE_CAPACITY) {
        q->head = (q->head + 1) % QUEUE_CAPACITY;
        q->len--;
        q->dropped++;
    }
    q->items[(q->head + q->len) % QUEUE_CAPACITY] = *r;
    q->len++;
}

const report_t *queue_peek(const report_queue_t *q) { return q->len ? &q->items[q->head] : NULL; }

void queue_pop(report_queue_t *q) {
    if (q->len) {
        q->head = (q->head + 1) % QUEUE_CAPACITY;
        q->len--;
    }
}
