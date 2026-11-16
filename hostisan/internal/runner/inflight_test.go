package runner

import (
	"context"
	"errors"
	"testing"
	"time"
)

// gate is a command that runs until release is closed.
type gate struct {
	started chan string
	release chan struct{}
}

func (g gate) Run(_ context.Context, cmd string, _ ...string) (Result, error) {
	g.started <- cmd
	<-g.release
	return Result{}, nil
}

func (g gate) RunAny(ctx context.Context, cmd string, args ...string) (Result, error) {
	return g.Run(ctx, cmd, args...)
}

func (g gate) Exists(cmd string) bool { return cmd == "v-list-users" }

func newGate() gate { return gate{started: make(chan string, 4), release: make(chan struct{})} }

func TestInflightWaitsForRunAndRunAny(t *testing.T) {
	g := newGate()
	c := &Inflight{Runner: g}
	go c.Run(context.Background(), "v-add-user")
	go c.RunAny(context.Background(), "v-add-web-domain")
	<-g.started
	<-g.started

	short, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := c.Wait(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait with commands running = %v, want DeadlineExceeded", err)
	}

	done := make(chan error)
	go func() { done <- c.Wait(context.Background()) }()
	select {
	case err := <-done:
		t.Fatalf("Wait returned %v before the commands finished", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(g.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Wait = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after the commands finished")
	}
}

func TestInflightIdleWaitReturns(t *testing.T) {
	if err := (&Inflight{Runner: newGate()}).Wait(context.Background()); err != nil {
		t.Fatalf("Wait = %v", err)
	}
}

func TestInflightForwardsAnyRunner(t *testing.T) {
	var r Runner = &Inflight{Runner: newGate()}
	a, ok := r.(AnyRunner)
	if !ok {
		t.Fatal("Inflight hides RunAny")
	}
	if !a.Exists("v-list-users") || a.Exists("v-nope") {
		t.Error("Exists not forwarded")
	}
	type plain struct{ Runner }
	c := &Inflight{Runner: plain{newGate()}}
	if _, err := c.RunAny(context.Background(), "v-list-users"); !errors.Is(err, ErrNoCommand) {
		t.Errorf("RunAny without an AnyRunner = %v, want ErrNoCommand", err)
	}
	if c.Exists("v-list-users") {
		t.Error("Exists without an AnyRunner = true")
	}
	if err := c.Wait(context.Background()); err != nil {
		t.Errorf("a refused RunAny left a command counted: %v", err)
	}
}
