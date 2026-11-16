package runner

import (
	"context"
	"fmt"
	"sync"
)

// Inflight counts the v-* commands running through it, so a stopping panel can wait
// for them (Wait): a script killed halfway can leave a half-written config.
type Inflight struct {
	Runner
	mu   sync.Mutex
	n    int
	idle chan struct{} // closed when n drops to 0; nil while nobody waits
}

func (c *Inflight) begin() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *Inflight) end() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n--
	if c.n == 0 && c.idle != nil {
		close(c.idle)
		c.idle = nil
	}
}

// Wait returns once no command is running, or with ctx's error. Commands started
// while it waits are waited for too.
func (c *Inflight) Wait(ctx context.Context) error {
	for {
		c.mu.Lock()
		if c.n == 0 {
			c.mu.Unlock()
			return nil
		}
		if c.idle == nil {
			c.idle = make(chan struct{})
		}
		idle := c.idle
		c.mu.Unlock()
		select {
		case <-idle:
		case <-ctx.Done():
			return fmt.Errorf("%w waiting for v-* commands", ctx.Err())
		}
	}
}

func (c *Inflight) Run(ctx context.Context, cmd string, args ...string) (Result, error) {
	c.begin()
	defer c.end()
	return c.Runner.Run(ctx, cmd, args...)
}

// RunAny forwards AnyRunner (the legacy /api/ pass-through); without one underneath
// it runs nothing and returns ErrNoCommand.
func (c *Inflight) RunAny(ctx context.Context, cmd string, args ...string) (Result, error) {
	a, ok := c.Runner.(AnyRunner)
	if !ok {
		return Result{}, ErrNoCommand
	}
	c.begin()
	defer c.end()
	return a.RunAny(ctx, cmd, args...)
}

// Exists forwards AnyRunner.Exists.
func (c *Inflight) Exists(cmd string) bool {
	a, ok := c.Runner.(AnyRunner)
	return ok && a.Exists(cmd)
}
