package hestia

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/runner"
)

// slowConfig is a v-list-sys-config that blocks until release is closed.
type slowConfig struct {
	release chan struct{}
	calls   atomic.Int32
	fail    atomic.Bool
}

func (s *slowConfig) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	s.calls.Add(1)
	<-s.release
	if s.fail.Load() {
		return runner.Result{}, errors.New("hung")
	}
	return runner.Result{Stdout: []byte(`{"config":{"VERSION":"1.10.5"}}`)}, nil
}

func TestConfigRunsOnceForConcurrentCallers(t *testing.T) {
	r := &slowConfig{release: make(chan struct{})}
	c := &Client{R: r}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cfg, err := c.Config(context.Background()); err != nil || cfg["VERSION"] != "1.10.5" {
				t.Errorf("Config = %v, %v", cfg, err)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(r.release)
	wg.Wait()
	if n := r.calls.Load(); n != 1 {
		t.Fatalf("v-list-sys-config ran %d times for 20 concurrent callers, want 1", n)
	}
}

func TestConfigWaiterHonoursCancel(t *testing.T) {
	r := &slowConfig{release: make(chan struct{})}
	defer close(r.release)
	c := &Client{R: r}
	go c.Config(context.Background()) // stuck in the command
	time.Sleep(20 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.Config(ctx); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want the caller's deadline", err)
		}
	case <-time.After(time.Second):
		t.Fatal("a caller waiting on a hung config read ignored its own deadline")
	}
}

func TestConfigServesStaleWhenRefreshFails(t *testing.T) {
	r := &slowConfig{release: make(chan struct{})}
	close(r.release)
	c := &Client{R: r, ConfigTTL: time.Millisecond}
	if _, err := c.Config(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	r.fail.Store(true)
	cfg, err := c.Config(context.Background())
	if err != nil || cfg["VERSION"] != "1.10.5" {
		t.Fatalf("Config after a failed refresh = %v, %v; want the last good config", cfg, err)
	}
}

// countingConfig answers v-list-sys-config at once and counts the calls.
type countingConfig struct{ calls atomic.Int32 }

func (c *countingConfig) Run(ctx context.Context, cmd string, args ...string) (runner.Result, error) {
	c.calls.Add(1)
	return runner.Result{Stdout: []byte(`{"config":{"VERSION":"1.10.5"}}`)}, nil
}

func TestInvalidateConfig(t *testing.T) {
	r := &countingConfig{}
	c := &Client{R: r, ConfigTTL: time.Hour}
	for range 2 {
		if _, err := c.Config(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n := r.calls.Load(); n != 1 {
		t.Fatalf("two reads inside the TTL ran the command %d times, want 1", n)
	}
	c.InvalidateConfig()
	if _, err := c.Config(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := r.calls.Load(); n != 2 {
		t.Fatalf("after InvalidateConfig the command ran %d times in total, want 2", n)
	}
}

func TestInvalidateConfigDuringRefresh(t *testing.T) {
	r := &slowConfig{release: make(chan struct{})}
	c := &Client{R: r, ConfigTTL: time.Hour}
	done := make(chan error, 1)
	go func() { _, err := c.Config(context.Background()); done <- err }()
	for r.calls.Load() < 1 { // the refresh is inside the command
		time.Sleep(time.Millisecond)
	}
	c.InvalidateConfig()
	close(r.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := c.Config(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := r.calls.Load(); n != 2 {
		t.Fatalf("the command ran %d times, want 2: a refresh started before InvalidateConfig must not re-cache the old config", n)
	}
}
