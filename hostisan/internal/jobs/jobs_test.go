package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// clock is a fake time source the tests move by hand.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTestRegistry() (*Registry, *clock) {
	c := &clock{t: time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)}
	return NewRegistry(time.Minute, c.now), c
}

func wait(t *testing.T, r *Registry, id string) Status {
	t.Helper()
	select {
	case <-r.Done(id):
	case <-time.After(5 * time.Second):
		t.Fatalf("job %s didn't end", id)
	}
	st, ok := r.Get(id)
	if !ok {
		t.Fatalf("Get(%s) = false after the job ended", id)
	}
	return st
}

func TestStartRunsAndStoresResult(t *testing.T) {
	r, _ := newTestRegistry()
	release := make(chan struct{})
	id, err := r.Start(context.Background(), Spec{Key: "k", Owner: "admin", Title: "Work"},
		func(ctx context.Context, j *Job) (any, error) {
			<-release
			return map[string]int{"n": 1}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if !ValidID(id) {
		t.Fatalf("id %q isn't valid", id)
	}
	st, ok := r.Get(id)
	if !ok || st.State != Running || st.ID != id || st.Key != "k" || st.Owner != "admin" || st.Title != "Work" {
		t.Fatalf("Get while running = %+v, %v", st, ok)
	}
	if !st.Finished.IsZero() {
		t.Errorf("Finished = %v while running", st.Finished)
	}
	close(release)
	st = wait(t, r, id)
	if st.State != Done || st.Err != nil {
		t.Fatalf("after Done: %+v", st)
	}
	if string(st.Result) != `{"n":1}` {
		t.Errorf("Result = %s", st.Result)
	}
	if st.Finished.IsZero() {
		t.Error("Finished is zero after the job ended")
	}
}

func TestNilResultIsNil(t *testing.T) {
	r, _ := newTestRegistry()
	id, _ := r.Start(context.Background(), Spec{Key: "k"}, func(ctx context.Context, j *Job) (any, error) { return nil, nil })
	if st := wait(t, r, id); st.Result != nil {
		t.Errorf("Result = %s, want nil", st.Result)
	}
}

func TestSteps(t *testing.T) {
	r, _ := newTestRegistry()
	id, _ := r.Start(context.Background(), Spec{Key: "k"}, func(ctx context.Context, j *Job) (any, error) {
		j.Step("a")
		j.Step("b")
		return nil, nil
	})
	st := wait(t, r, id)
	if st.Step != "b" || len(st.Steps) != 2 || st.Steps[0] != "a" || st.Steps[1] != "b" {
		t.Fatalf("Step = %q, Steps = %v", st.Step, st.Steps)
	}
}

func TestSingleFlightPerKey(t *testing.T) {
	r, _ := newTestRegistry()
	release := make(chan struct{})
	block := func(ctx context.Context, j *Job) (any, error) { <-release; return nil, nil }
	first, err := r.Start(context.Background(), Spec{Key: "server"}, block)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Start(context.Background(), Spec{Key: "server"}, block); !errors.Is(err, ErrBusy) {
		t.Fatalf("second Start on the same key: err = %v, want ErrBusy", err)
	}
	if st, ok := r.Running("server"); !ok || st.ID != first {
		t.Fatalf("Running(server) = %+v, %v", st, ok)
	}
	other, err := r.Start(context.Background(), Spec{Key: "web:bob:a.example"}, block)
	if err != nil {
		t.Fatalf("Start on another key: %v", err)
	}
	close(release)
	wait(t, r, first)
	wait(t, r, other)
	if _, ok := r.Running("server"); ok {
		t.Error("Running(server) = true after the job ended")
	}
	again, err := r.Start(context.Background(), Spec{Key: "server"}, func(ctx context.Context, j *Job) (any, error) { return nil, nil })
	if err != nil {
		t.Fatalf("Start after the first ended: %v", err)
	}
	wait(t, r, again)
}

func TestJobOutlivesStartContext(t *testing.T) {
	r, _ := newTestRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	id, _ := r.Start(ctx, Spec{Key: "k"}, func(ctx context.Context, j *Job) (any, error) {
		close(started)
		<-release
		return nil, ctx.Err()
	})
	<-started
	cancel()
	close(release)
	if st := wait(t, r, id); st.State != Done {
		t.Fatalf("job ended %s (%v) after its request was cancelled", st.State, st.Err)
	}
}

func TestLimit(t *testing.T) {
	r, _ := newTestRegistry()
	id, _ := r.Start(context.Background(), Spec{Key: "k", Limit: 10 * time.Millisecond}, func(ctx context.Context, j *Job) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	st := wait(t, r, id)
	if st.State != Failed || !errors.Is(st.Err, context.DeadlineExceeded) {
		t.Fatalf("State = %s, Err = %v", st.State, st.Err)
	}
}

func TestPanicEndsFailedWithoutValue(t *testing.T) {
	r, _ := newTestRegistry()
	id, _ := r.Start(context.Background(), Spec{Key: "k"}, func(ctx context.Context, j *Job) (any, error) {
		panic("secret-panic-value")
	})
	st := wait(t, r, id)
	if st.State != Failed || st.Err == nil {
		t.Fatalf("State = %s, Err = %v", st.State, st.Err)
	}
	if strings.Contains(st.Err.Error(), "secret-panic-value") {
		t.Errorf("Err %q contains the panic value", st.Err)
	}
	if _, ok := r.Running("k"); ok {
		t.Error("the key is still running after a panic")
	}
}

func TestFailureKept(t *testing.T) {
	r, _ := newTestRegistry()
	id, _ := r.Start(context.Background(), Spec{Key: "k"}, func(ctx context.Context, j *Job) (any, error) {
		return nil, fmt.Errorf("wrapped: %w", &Failure{Code: "busy", Message: "%s is busy", Args: []string{"x"}})
	})
	st := wait(t, r, id)
	var f *Failure
	if st.State != Failed || !errors.As(st.Err, &f) || f.Code != "busy" || f.Args[0] != "x" {
		t.Fatalf("State = %s, Err = %v", st.State, st.Err)
	}
	if f.Error() == "" {
		t.Error("Failure.Error is empty")
	}
}

func TestExpiry(t *testing.T) {
	r, c := newTestRegistry()
	id, _ := r.Start(context.Background(), Spec{Key: "k"}, func(ctx context.Context, j *Job) (any, error) { return nil, nil })
	wait(t, r, id)
	c.add(time.Minute - time.Second)
	if _, ok := r.Get(id); !ok {
		t.Fatal("job expired before retain passed")
	}
	c.add(2 * time.Second)
	if _, ok := r.Get(id); ok {
		t.Error("Get = true after retain + 1s")
	}
	if _, ok := r.Running("k"); ok {
		t.Error("Running = true after retain + 1s")
	}
}

func TestUnknownID(t *testing.T) {
	r, _ := newTestRegistry()
	if _, ok := r.Get(strings.Repeat("0", 32)); ok {
		t.Error("Get on an unknown id = true")
	}
}

func TestDefaultRetain(t *testing.T) {
	if r := NewRegistry(0, nil); r.retain != 15*time.Minute || r.now == nil {
		t.Errorf("retain = %v", r.retain)
	}
}

func TestValidID(t *testing.T) {
	good := strings.Repeat("a1", 16)
	for id, want := range map[string]bool{
		good:                    true,
		good[:31]:               false,
		good + "0":              false,
		strings.ToUpper(good):   false,
		"../x":                  false,
		"-h":                    false,
		"":                      false,
		strings.Repeat("g", 32): false,
	} {
		if got := ValidID(id); got != want {
			t.Errorf("ValidID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestConcurrentStarts(t *testing.T) {
	r, _ := newTestRegistry()
	var wg sync.WaitGroup
	ids := make([]string, 20)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := r.Start(context.Background(), Spec{Key: fmt.Sprintf("k%d", i)}, func(ctx context.Context, j *Job) (any, error) {
				j.Step("one")
				return i, nil
			})
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = id
			r.Get(id)
		}()
	}
	wg.Wait()
	for i, id := range ids {
		if id == "" {
			continue
		}
		st := wait(t, r, id)
		var n int
		if err := json.Unmarshal(st.Result, &n); err != nil || n != i {
			t.Errorf("job %d result = %s", i, st.Result)
		}
	}
}

func TestStartRejectsEmptyKey(t *testing.T) {
	r, _ := newTestRegistry()
	called := false
	id, err := r.Start(context.Background(), Spec{Title: "x"}, func(ctx context.Context, j *Job) (any, error) {
		called = true
		return nil, nil
	})
	if err == nil || id != "" {
		t.Fatalf("Start with an empty key = %q, %v; want an error", id, err)
	}
	if called {
		t.Fatal("fn ran for a job without a key")
	}
	r.mu.Lock()
	n := len(r.jobs)
	r.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d jobs were created, want 0", n)
	}
}
