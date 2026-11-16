package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// A stopping panel (Shutdown) first waits for running jobs, then asks them to stop,
// and every job is saved to a file (Persist) so the next start still answers for it.
// Before Plan 6 C6 D2 a restart lost a running app install: its id answered 404, no
// failure or notification was shown, and a half-installed site was left behind.

// InterruptedCode and InterruptedMsg are the failure of a job a panel stop cut short.
const (
	InterruptedCode = "interrupted"
	InterruptedMsg  = "The panel restarted before this task finished. It was stopped; check what it changed and run it again."
)

// ErrInterrupted is what Interrupted returns once the panel wants jobs to stop, and the
// error a job that the panel stop cut short ends with.
var ErrInterrupted error = &Failure{Code: InterruptedCode, Message: InterruptedMsg}

type stopKey struct{}

// Interrupted returns ErrInterrupted once a stopping panel has asked the job owning
// ctx to stop: the job should not start another step, clean up what it leaves half
// done (through WithoutInterrupt(ctx)) and return the error. It is nil outside a job.
func Interrupted(ctx context.Context) error {
	if c, _ := ctx.Value(stopKey{}).(<-chan struct{}); c != nil {
		select {
		case <-c:
			return ErrInterrupted
		default:
		}
	}
	return nil
}

// WithoutInterrupt is ctx with no stop request, for a job's clean-up after one.
func WithoutInterrupt(ctx context.Context) context.Context {
	return context.WithValue(ctx, stopKey{}, (<-chan struct{})(nil))
}

// Shutdown waits for the running jobs to end. After grace it closes the stop request
// (Interrupted), and it gives up when ctx ends, returning an error; the jobs still
// running then stay "running" in the file, which the next start reports as
// interrupted. New jobs should not be started once Shutdown is called.
func (r *Registry) Shutdown(ctx context.Context, grace time.Duration) error {
	r.mu.Lock()
	var pending []chan struct{}
	for _, id := range r.running {
		pending = append(pending, r.jobs[id].done)
	}
	r.mu.Unlock()

	t := time.NewTimer(grace)
	defer t.Stop()
	for _, done := range pending {
	wait:
		for {
			select {
			case <-done:
				break wait
			case <-t.C:
				r.stopOnce.Do(func() { close(r.stop) })
			case <-ctx.Done():
				return fmt.Errorf("%w waiting for background jobs", ctx.Err())
			}
		}
	}
	return nil
}

// saved is the jobs file: every job the registry holds, one record each.
type saved struct {
	Jobs []savedJob `json:"jobs"`
}

type savedJob struct {
	ID       string          `json:"id"`
	Key      string          `json:"key"`
	Owner    string          `json:"owner"`
	Title    string          `json:"title"`
	State    State           `json:"state"`
	Step     string          `json:"step"`
	Steps    []string        `json:"steps"`
	Err      *savedErr       `json:"error,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	Started  time.Time       `json:"started"`
	Finished time.Time       `json:"finished"`
	Notify   bool            `json:"notify,omitempty"` // not yet reported by a start
}

// savedErr is a *Failure in the file.
type savedErr struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Args    []string `json:"args,omitempty"`
}

// Persist loads the jobs file at path and saves every change to it from now on (mode
// 0600, written whole and renamed into place). Jobs that were still running when the
// last panel stopped, or that its Shutdown stopped, come back failed with
// ErrInterrupted and are returned once, for a notification to their owners. describe
// turns an error that isn't a *Failure into the one saved (nil: "internal"), so the
// file never holds a raw error's text. A missing file is an empty one; a file that
// can't be read is reported and replaced. Call it once, before any Start.
func (r *Registry) Persist(path string, describe func(error) *Failure) ([]Status, error) {
	return r.PersistRecover(path, describe, nil)
}

// PersistRecover is Persist that first hands each job the last panel left running
// (killed or crashed, so the job never cleaned up) to undo, with its Data, while
// the file still says "running": a recovery cut short by another kill is run again
// by the next start, so undo must be idempotent. Jobs a Shutdown stopped cleaned
// up themselves and aren't passed.
func (r *Registry) PersistRecover(path string, describe func(error) *Failure, undo func(Status)) ([]Status, error) {
	if describe == nil {
		describe = func(error) *Failure { return &Failure{Code: "internal", Message: "Error: internal error"} }
	}
	b, rerr := os.ReadFile(path)
	if errors.Is(rerr, os.ErrNotExist) {
		rerr = nil
	}
	var f saved
	if rerr == nil && len(b) > 0 {
		rerr = json.Unmarshal(b, &f)
	}

	if rerr == nil && undo != nil {
		for _, j := range f.Jobs {
			if j.State == Running && ValidID(j.ID) {
				undo(Status{ID: j.ID, Key: j.Key, Owner: j.Owner, Title: j.Title, State: j.State,
					Step: j.Step, Steps: append([]string{}, j.Steps...), Data: j.Data, Started: j.Started})
			}
		}
	}

	r.mu.Lock()
	r.path, r.describe = path, describe
	var interrupted []Status
	if rerr == nil {
		now := r.now()
		for _, j := range f.Jobs {
			if !ValidID(j.ID) {
				continue
			}
			e := &entry{st: Status{ID: j.ID, Key: j.Key, Owner: j.Owner, Title: j.Title, State: j.State,
				Step: j.Step, Steps: append([]string{}, j.Steps...), Result: j.Result, Data: j.Data,
				Started: j.Started, Finished: j.Finished}, done: make(chan struct{})}
			if j.Err != nil {
				e.st.Err = &Failure{Code: j.Err.Code, Message: j.Err.Message, Args: j.Err.Args}
			}
			close(e.done)
			if j.State == Running {
				e.st.State, e.st.Err, e.st.Result, e.st.Finished = Failed, ErrInterrupted, nil, now
				j.Notify = true
			} else if j.State != Done && j.State != Failed {
				continue
			} else if j.State == Failed && e.st.Err == nil {
				e.st.Err = describe(nil)
			}
			if j.Notify {
				interrupted = append(interrupted, e.status())
			}
			r.jobs[j.ID] = e
		}
		r.prune()
	}
	r.mu.Unlock()
	r.save()
	if rerr != nil {
		return nil, fmt.Errorf("jobs file %s: %w", path, rerr)
	}
	return interrupted, nil
}

// Close waits for a save in progress and stops persisting: later changes stay in
// memory only. A stopping panel calls it after Shutdown, so a job ending past the
// deadline doesn't rewrite the file as the process exits; tests call it before their
// temp dir is removed. Safe to call more than once, or without Persist.
func (r *Registry) Close() {
	r.saveMu.Lock()
	defer r.saveMu.Unlock()
	r.mu.Lock()
	r.path = ""
	r.mu.Unlock()
}

// save writes every job to the file, when Persist set one. A failure is logged: the
// jobs still run, they just won't survive a restart.
func (r *Registry) save() {
	r.saveMu.Lock()
	defer r.saveMu.Unlock()
	r.mu.Lock()
	if r.path == "" {
		r.mu.Unlock()
		return
	}
	path := r.path
	f := saved{Jobs: []savedJob{}}
	for _, e := range r.jobs {
		j := savedJob{ID: e.st.ID, Key: e.st.Key, Owner: e.st.Owner, Title: e.st.Title, State: e.st.State,
			Step: e.st.Step, Steps: append([]string{}, e.st.Steps...), Result: e.st.Result, Data: e.st.Data,
			Started: e.st.Started, Finished: e.st.Finished, Notify: e.notify}
		if e.st.Err != nil {
			var fl *Failure
			if !errors.As(e.st.Err, &fl) {
				fl = r.describe(e.st.Err)
			}
			j.Err = &savedErr{Code: fl.Code, Message: fl.Message, Args: fl.Args}
		}
		f.Jobs = append(f.Jobs, j)
	}
	r.mu.Unlock()

	if err := writeFile(path, f); err != nil {
		slog.Error("background jobs not saved", "file", path, "err", err)
	}
}

// writeFile writes f to path through a private temp file renamed into place.
func writeFile(path string, f saved) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".jobs-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
