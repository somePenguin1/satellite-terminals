package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"satfleet/ingest/internal/telemetry"
)

type recordingSink struct {
	mu       sync.Mutex
	batches  [][]telemetry.Report
	failures atomic.Int32 // fail this many writes before succeeding
	delay    time.Duration
}

func (s *recordingSink) WriteBatch(_ context.Context, batch []telemetry.Report) error {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if s.failures.Load() > 0 {
		s.failures.Add(-1)
		return errors.New("transient failure")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batches = append(s.batches, append([]telemetry.Report(nil), batch...))
	return nil
}

func (s *recordingSink) all() []telemetry.Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []telemetry.Report
	for _, b := range s.batches {
		out = append(out, b...)
	}
	return out
}

func report(id string, seq uint64) telemetry.Report {
	return telemetry.Report{TerminalID: id, Seq: seq}
}

func testConfig() Config {
	c := DefaultConfig()
	c.Shards, c.QueueSize, c.BatchSize, c.FlushInterval, c.RetryBackoff = 4, 100, 50, 20*time.Millisecond, time.Millisecond
	return c
}

func TestFlushesFullBatchesAndPartialOnTimer(t *testing.T) {
	sink := &recordingSink{}
	cfg := testConfig()
	cfg.Shards = 1
	p := New(cfg, sink, nil)
	p.Start()
	ctx := context.Background()
	for i := 0; i < 120; i++ {
		if err := p.Submit(ctx, report("SAT-00001", uint64(i))); err != nil {
			t.Fatal(err)
		}
	}
	// Two full batches immediately, the remaining 20 after the flush interval.
	deadline := time.Now().Add(time.Second)
	for len(sink.all()) < 120 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := len(sink.all()); got != 120 {
		t.Fatalf("persisted %d reports, want 120", got)
	}
	sink.mu.Lock()
	sizes := []int{}
	for _, b := range sink.batches {
		sizes = append(sizes, len(b))
	}
	sink.mu.Unlock()
	if fmt.Sprint(sizes) != "[50 50 20]" {
		t.Fatalf("batch sizes %v, want [50 50 20]", sizes)
	}
	_ = p.Close(ctx)
}

func TestPreservesPerTerminalOrderAcrossShards(t *testing.T) {
	sink := &recordingSink{}
	p := New(testConfig(), sink, nil)
	p.Start()
	ctx := context.Background()
	var wg sync.WaitGroup
	for term := 0; term < 20; term++ {
		wg.Add(1)
		go func(term int) {
			defer wg.Done()
			id := fmt.Sprintf("SAT-%05d", term)
			for seq := uint64(1); seq <= 200; seq++ {
				if err := p.Submit(ctx, report(id, seq)); err != nil {
					t.Error(err)
					return
				}
			}
		}(term)
	}
	wg.Wait()
	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}
	last := map[string]uint64{}
	for _, r := range sink.all() {
		if r.Seq != last[r.TerminalID]+1 {
			t.Fatalf("%s: seq %d after %d", r.TerminalID, r.Seq, last[r.TerminalID])
		}
		last[r.TerminalID] = r.Seq
	}
	if len(sink.all()) != 4000 {
		t.Fatalf("persisted %d, want 4000", len(sink.all()))
	}
}

func TestRetriesTransientFailures(t *testing.T) {
	sink := &recordingSink{}
	sink.failures.Store(2)
	var results []FlushResult
	var mu sync.Mutex
	cfg := testConfig()
	cfg.Shards = 1
	p := New(cfg, sink, func(r FlushResult) {
		mu.Lock()
		results = append(results, FlushResult{Attempts: r.Attempts, Err: r.Err})
		mu.Unlock()
	})
	p.Start()
	_ = p.Submit(context.Background(), report("SAT-00001", 1))
	_ = p.Close(context.Background())
	if len(results) != 1 || results[0].Attempts != 3 || results[0].Err != nil {
		t.Fatalf("results = %+v, want one success after 3 attempts", results)
	}
}

func TestReportsPermanentFailure(t *testing.T) {
	sink := &recordingSink{}
	sink.failures.Store(100)
	var failed atomic.Int32
	cfg := testConfig()
	cfg.Shards, cfg.MaxAttempts = 1, 2
	p := New(cfg, sink, func(r FlushResult) {
		if r.Err != nil {
			failed.Add(int32(len(r.Batch)))
		}
	})
	p.Start()
	_ = p.Submit(context.Background(), report("SAT-00001", 1))
	_ = p.Close(context.Background())
	if failed.Load() != 1 {
		t.Fatalf("failed = %d, want 1", failed.Load())
	}
}

func TestBackpressureAndClose(t *testing.T) {
	sink := &recordingSink{delay: 50 * time.Millisecond}
	cfg := testConfig()
	cfg.Shards, cfg.QueueSize, cfg.BatchSize = 1, 2, 1
	p := New(cfg, sink, nil)
	p.Start()

	// Fill the queue; the next submit must block until the context expires.
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_ = p.Submit(ctx, report("SAT-00001", uint64(i)))
	}
	short, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	err := p.Submit(short, report("SAT-00001", 99))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Submit on full queue = %v, want deadline exceeded", err)
	}

	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.Submit(ctx, report("SAT-00001", 100)); !errors.Is(err, ErrClosed) {
		t.Fatalf("Submit after Close = %v, want ErrClosed", err)
	}
	if n := len(sink.all()); n != 3 {
		t.Fatalf("Close should drain the queue: persisted %d, want 3", n)
	}
}

func TestShardForIsStable(t *testing.T) {
	if ShardFor("SAT-00042", 8) != ShardFor("SAT-00042", 8) {
		t.Fatal("unstable shard")
	}
	seen := map[int]bool{}
	for i := 0; i < 1000; i++ {
		seen[ShardFor(fmt.Sprintf("SAT-%05d", i), 8)] = true
	}
	if len(seen) != 8 {
		t.Fatalf("poor distribution: %d of 8 shards used", len(seen))
	}
}

func BenchmarkSubmit(b *testing.B) {
	cfg := DefaultConfig()
	p := New(cfg, discardSink{}, nil)
	p.Start()
	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = p.Submit(ctx, report(fmt.Sprintf("SAT-%05d", i%5000), uint64(i)))
			i++
		}
	})
	b.StopTimer()
	_ = p.Close(ctx)
}

type discardSink struct{}

func (discardSink) WriteBatch(context.Context, []telemetry.Report) error { return nil }
