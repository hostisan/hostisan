// Package hestia reads Hestia's data through its v-* commands.
package hestia

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hostisan/hostisan/internal/runner"
)

// SysConfig is the "config" object from `v-list-sys-config json`.
type SysConfig map[string]string

// Client wraps the runner with typed reads. Config is cached for ConfigTTL: the PHP
// panel re-read it on every request, which costs a bash process per request.
type Client struct {
	R         runner.Runner
	ConfigTTL time.Duration // default 5s

	mu         sync.Mutex
	cfg        SysConfig
	cfgUntil   time.Time
	gen        uint64        // bumped by InvalidateConfig
	refreshing chan struct{} // closed when the read in progress finishes
	refreshErr error
}

// Config returns the system config. One read runs at a time, shared by every caller
// and not holding the lock, so a hung v-list-sys-config can't queue requests behind
// it; each caller still stops waiting at its own deadline. If a refresh fails, the
// last good config is used.
func (c *Client) Config(ctx context.Context) (SysConfig, error) {
	c.mu.Lock()
	if c.cfg != nil && time.Now().Before(c.cfgUntil) {
		defer c.mu.Unlock()
		return c.cfg, nil
	}
	done := c.refreshing
	if done == nil {
		done = make(chan struct{})
		c.refreshing = done
		// Not the caller's context: other callers wait on this read.
		go c.refresh(done, c.gen)
	}
	c.mu.Unlock()

	select {
	case <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg != nil {
		return c.cfg, nil
	}
	return nil, c.refreshErr
}

// InvalidateConfig makes the next Config call read the config again, after a change
// to it. A read already in flight finishes for its callers but is not cached.
func (c *Client) InvalidateConfig() {
	c.mu.Lock()
	c.gen++
	c.cfgUntil = time.Time{}
	c.mu.Unlock()
}

func (c *Client) refresh(done chan struct{}, gen uint64) {
	var out struct {
		Config SysConfig `json:"config"`
	}
	err := c.runJSON(context.Background(), &out, "v-list-sys-config", "json")
	ttl := c.ConfigTTL
	if ttl == 0 {
		ttl = 5 * time.Second
	}
	c.mu.Lock()
	if err == nil {
		c.cfg = out.Config
		if gen == c.gen { // else invalidated while reading: its callers get it, the next call re-reads
			c.cfgUntil = time.Now().Add(ttl)
		}
	} else if c.cfg != nil {
		slog.Warn("config refresh failed; using the last good config", "err", err)
	}
	c.refreshErr = err
	c.refreshing = nil
	c.mu.Unlock()
	close(done)
}

// runJSON runs a command that must exit 0 and decodes its stdout.
func (c *Client) runJSON(ctx context.Context, v any, cmd string, args ...string) error {
	res, err := c.R.Run(ctx, cmd, args...)
	if err != nil {
		return err
	}
	if err := res.Err(cmd); err != nil {
		return err
	}
	if err := json.Unmarshal(res.Stdout, v); err != nil {
		return fmt.Errorf("hestia: parse %s output: %w", cmd, err)
	}
	return nil
}
