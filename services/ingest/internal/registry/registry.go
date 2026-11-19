// Package registry keeps an in-memory set of terminals allowed to report, so the hot
// path can reject unknown or decommissioned IDs without a database round trip, and
// so rollups can be keyed by region.
package registry

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

type Terminal struct {
	ID     string
	Region string
}

type Loader interface {
	LoadTerminals(ctx context.Context) ([]Terminal, error)
}

type Registry struct {
	loader  Loader
	regions atomic.Pointer[map[string]string]
	loaded  atomic.Int64 // unix nanos of the last successful load
}

func New(loader Loader) *Registry {
	r := &Registry{loader: loader}
	empty := map[string]string{}
	r.regions.Store(&empty)
	return r
}

// Refresh swaps in a freshly loaded snapshot. Readers never block.
func (r *Registry) Refresh(ctx context.Context) error {
	terms, err := r.loader.LoadTerminals(ctx)
	if err != nil {
		return err
	}
	m := make(map[string]string, len(terms))
	for _, t := range terms {
		m[t.ID] = t.Region
	}
	r.regions.Store(&m)
	r.loaded.Store(time.Now().UnixNano())
	return nil
}

// Run refreshes periodically until ctx is done. Newly provisioned terminals are
// accepted within one interval; failures keep the previous snapshot.
func (r *Registry) Run(ctx context.Context, every time.Duration, log *slog.Logger) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := r.Refresh(ctx); err != nil && ctx.Err() == nil {
				log.Warn("terminal registry refresh failed; keeping previous snapshot", "err", err)
			}
		}
	}
}

// Region returns the terminal's region and whether it may report.
func (r *Registry) Region(id string) (string, bool) {
	region, ok := (*r.regions.Load())[id]
	return region, ok
}

func (r *Registry) Size() int { return len(*r.regions.Load()) }

// Ready reports whether at least one load has succeeded.
func (r *Registry) Ready() bool { return r.loaded.Load() > 0 }
