package httpapi

import (
	"context"
	"sync"
	"time"

	"obsidian-sync-tunnel/internal/store"
)

type doctorResultCache struct {
	mu        sync.Mutex
	ttl       time.Duration
	report    store.DoctorReport
	expiresAt time.Time
	running   chan struct{}
}

func newDoctorResultCache(ttl time.Duration) *doctorResultCache {
	return &doctorResultCache{ttl: ttl}
}

func (c *doctorResultCache) Get(ctx context.Context, run func(context.Context) (store.DoctorReport, error)) (store.DoctorReport, bool, error) {
	for {
		c.mu.Lock()
		if !c.expiresAt.IsZero() && time.Now().Before(c.expiresAt) {
			report := c.report
			c.mu.Unlock()
			return report, true, nil
		}
		if c.running != nil {
			running := c.running
			c.mu.Unlock()
			select {
			case <-running:
				continue
			case <-ctx.Done():
				return store.DoctorReport{}, false, ctx.Err()
			}
		}
		c.running = make(chan struct{})
		running := c.running
		c.mu.Unlock()

		report, err := run(ctx)
		c.mu.Lock()
		if err == nil {
			c.report = report
			c.expiresAt = time.Now().Add(c.ttl)
		}
		c.running = nil
		close(running)
		c.mu.Unlock()
		return report, false, err
	}
}
