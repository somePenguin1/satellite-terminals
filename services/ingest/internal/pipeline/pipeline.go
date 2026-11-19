// Package pipeline batches accepted reports and hands them to a Sink (Postgres).
//
// Reports are sharded by terminal ID, so each terminal's samples are written in order by
// exactly one goroutine and concurrent batches never upsert the same terminal_status row
// (no lock contention, no deadlocks). Each shard flushes when its batch is full or when
// the oldest queued report has waited FlushInterval, whichever comes first. Bounded queues
// give natural backpressure: when Postgres falls behind, Submit blocks, which in turn
// applies gRPC/TCP flow control to clients instead of growing memory without limit.
package pipeline

import (
	"context"
	"errors"
	"hash/fnv"
	"sync"
	"time"

	"satfleet/ingest/internal/telemetry"
)

type Sink interface {
	WriteBatch(ctx context.Context, batch []telemetry.Report) error
}

type Config struct {
	Shards        int
	QueueSize     int // per shard
	BatchSize     int
	FlushInterval time.Duration
	WriteTimeout  time.Duration
	MaxAttempts   int
	RetryBackoff  time.Duration
}

func DefaultConfig() Config {
	return Config{
		Shards:        4,
		QueueSize:     20_000,
		BatchSize:     1_000,
		FlushInterval: 100 * time.Millisecond,
		WriteTimeout:  10 * time.Second,
		MaxAttempts:   4,
		RetryBackoff:  200 * time.Millisecond,
	}
}

// FlushResult is passed to the OnFlush hook after every batch. Batch is only valid during
// the callback: the pipeline reuses the slice.
type FlushResult struct {
	Shard    int
	Batch    []telemetry.Report
	Duration time.Duration
	Attempts int
	Err      error
}

var ErrClosed = errors.New("pipeline closed")

type Pipeline struct {
	cfg     Config
	sink    Sink
	onFlush func(FlushResult)
	shards  []chan telemetry.Report

	mu     sync.RWMutex // guards closed against sends on closed channels
	closed bool
	wg     sync.WaitGroup
}

func New(cfg Config, sink Sink, onFlush func(FlushResult)) *Pipeline {
	if cfg.Shards < 1 {
		cfg.Shards = 1
	}
	if cfg.BatchSize < 1 {
		cfg.BatchSize = 1
	}
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 1
	}
	if onFlush == nil {
		onFlush = func(FlushResult) {}
	}
	p := &Pipeline{cfg: cfg, sink: sink, onFlush: onFlush}
	for i := 0; i < cfg.Shards; i++ {
		p.shards = append(p.shards, make(chan telemetry.Report, cfg.QueueSize))
	}
	return p
}

func (p *Pipeline) Start() {
	for i, ch := range p.shards {
		p.wg.Add(1)
		go p.run(i, ch)
	}
}

// ShardFor maps a terminal to its shard (FNV-1a, stable across restarts).
func ShardFor(terminalID string, shards int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(terminalID))
	return int(h.Sum32() % uint32(shards))
}

// Submit enqueues a report, blocking while the shard's queue is full.
func (p *Pipeline) Submit(ctx context.Context, r telemetry.Report) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return ErrClosed
	}
	select {
	case p.shards[ShardFor(r.TerminalID, len(p.shards))] <- r:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// QueueDepth is the number of reports accepted but not yet handed to the sink.
func (p *Pipeline) QueueDepth() int {
	n := 0
	for _, ch := range p.shards {
		n += len(ch)
	}
	return n
}

// Close stops accepting reports, flushes everything queued and waits for in-flight writes.
func (p *Pipeline) Close(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		for _, ch := range p.shards {
			close(ch)
		}
	}
	p.mu.Unlock()

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Pipeline) run(shard int, ch <-chan telemetry.Report) {
	defer p.wg.Done()
	batch := make([]telemetry.Report, 0, p.cfg.BatchSize)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	armed := false

	flush := func() {
		if armed {
			timer.Stop()
			armed = false
		}
		if len(batch) == 0 {
			return
		}
		p.write(shard, batch)
		batch = batch[:0]
	}

	for {
		select {
		case r, ok := <-ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, r)
			if !armed {
				timer.Reset(p.cfg.FlushInterval)
				armed = true
			}
			if len(batch) >= p.cfg.BatchSize {
				flush()
			}
		case <-timer.C:
			armed = false
			flush()
		}
	}
}

func (p *Pipeline) write(shard int, batch []telemetry.Report) {
	start := time.Now()
	backoff := p.cfg.RetryBackoff
	var err error
	attempts := 0
	for attempts < p.cfg.MaxAttempts {
		attempts++
		ctx, cancel := context.WithTimeout(context.Background(), p.cfg.WriteTimeout)
		err = p.sink.WriteBatch(ctx, batch)
		cancel()
		if err == nil {
			break
		}
		if attempts < p.cfg.MaxAttempts {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	p.onFlush(FlushResult{Shard: shard, Batch: batch, Duration: time.Since(start), Attempts: attempts, Err: err})
}
