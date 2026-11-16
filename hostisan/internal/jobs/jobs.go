// Package jobs runs background work that outlives the request that started it: one
// running job per key, polled by id, kept for a while after it ends. Jobs live in
// memory; with Persist they are also saved to a file, so a restarted panel still
// answers for them and reports the ones it cut short (persist.go).
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// State is where a job is: "running", "done" or "failed".
type State string

const (
	Running State = "running"
	Done    State = "done"
	Failed  State = "failed"
)

var ErrNoKey = errors.New("jobs: Spec.Key is required")

var ErrBusy = errors.New("jobs: a job with this key is running")

// errInternal ends a job whose function panicked or whose result can't be marshalled.
var errInternal = errors.New("jobs: internal error")

// Failure is a job error carrying an English msgid and args, shown like an API error.
type Failure struct {
	Code, Message string
	Args          []string
}

func (f *Failure) Error() string { return f.Code + ": " + f.Message }

type Spec struct {
	Key   string        // single flight: one running job per key, e.g. "server", "web:<user>:<domain>"
	Owner string        // who started it; readers compare it
	Title string        // shown while running
	Limit time.Duration // cap on the whole job; 0 = none
}

// Job is the handle a job's function reports its progress through.
type Job struct {
	r  *Registry
	id string
}

// Step records the step the job is on: Step becomes name and Steps gets name appended.
func (j *Job) Step(name string) {
	j.r.mu.Lock()
	if e := j.r.jobs[j.id]; e != nil {
		e.st.Step = name
		e.st.Steps = append(e.st.Steps, name)
	}
	j.r.mu.Unlock()
	j.r.save()
}

// SetData saves v (marshalled) with the job before it returns, for the next start to
// undo what a killed job left half done (PersistRecover). Each call replaces the last.
func (j *Job) SetData(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	j.r.mu.Lock()
	if e := j.r.jobs[j.id]; e != nil {
		e.st.Data = b
	}
	j.r.mu.Unlock()
	j.r.save()
	return nil
}

type Status struct {
	ID, Key, Owner, Title string
	State                 State
	Step                  string
	Steps                 []string
	Err                   error           // set when Failed
	Result                json.RawMessage // fn's result, marshalled; nil when none
	Data                  json.RawMessage // what the job saved for its recovery (SetData); nil when none
	Started, Finished     time.Time       // Finished is zero while running
}

type entry struct {
	st     Status
	done   chan struct{}
	notify bool // ended by a shutdown (ErrInterrupted): the next start reports it
}

// Registry holds the jobs. It is safe for concurrent use.
type Registry struct {
	retain time.Duration
	now    func() time.Time

	mu      sync.Mutex
	jobs    map[string]*entry
	running map[string]string // key → id of the running job

	stop     chan struct{} // closed by Shutdown past its grace period (Interrupted)
	stopOnce sync.Once

	saveMu   sync.Mutex           // serializes save; held before mu, never inside it
	path     string               // the jobs file; "" = not persisted (set once by Persist)
	describe func(error) *Failure // how save writes an error that isn't a *Failure
}

// NewRegistry keeps finished jobs for retain (0 = 15 minutes); now nil = time.Now.
func NewRegistry(retain time.Duration, now func() time.Time) *Registry {
	if retain == 0 {
		retain = 15 * time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &Registry{retain: retain, now: now, jobs: map[string]*entry{}, running: map[string]string{},
		stop: make(chan struct{})}
}

// Start runs fn in the background and returns the job's id, or ErrBusy while a job
// with the same key is running, or ErrNoKey when the key is empty. fn's context
// doesn't end with ctx; it ends after spec.Limit when that is set.
func (r *Registry) Start(ctx context.Context, spec Spec, fn func(ctx context.Context, j *Job) (any, error)) (string, error) {
	if spec.Key == "" {
		return "", ErrNoKey
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b[:])

	r.mu.Lock()
	r.prune()
	if _, busy := r.running[spec.Key]; busy {
		r.mu.Unlock()
		return "", ErrBusy
	}
	e := &entry{
		st:   Status{ID: id, Key: spec.Key, Owner: spec.Owner, Title: spec.Title, State: Running, Steps: []string{}, Started: r.now()},
		done: make(chan struct{}),
	}
	r.jobs[id] = e
	r.running[spec.Key] = id
	r.mu.Unlock()
	r.save()

	jctx := context.WithValue(context.WithoutCancel(ctx), stopKey{}, (<-chan struct{})(r.stop))
	cancel := context.CancelFunc(func() {})
	if spec.Limit > 0 {
		jctx, cancel = context.WithTimeout(jctx, spec.Limit)
	}
	go func() {
		defer cancel()
		res, err := r.call(jctx, id, fn)
		r.finish(id, res, err)
	}()
	return id, nil
}

// call runs fn, turning a panic into errInternal and marshalling the result.
func (r *Registry) call(ctx context.Context, id string, fn func(ctx context.Context, j *Job) (any, error)) (res json.RawMessage, err error) {
	defer func() {
		if p := recover(); p != nil {
			// The panic value may hold anything; only the stack is logged.
			slog.Error("background job panicked", "job", id, "stack", string(debug.Stack()))
			res, err = nil, errInternal
		}
	}()
	v, err := fn(ctx, &Job{r: r, id: id})
	if err != nil || v == nil {
		return nil, err
	}
	b, merr := json.Marshal(v)
	if merr != nil {
		slog.Error("background job result can't be marshalled", "job", id, "err", merr)
		return nil, errInternal
	}
	return b, nil
}

func (r *Registry) finish(id string, res json.RawMessage, err error) {
	r.mu.Lock()
	e := r.jobs[id]
	e.st.Finished = r.now()
	if err != nil {
		e.st.State, e.st.Err = Failed, err
		e.notify = errors.Is(err, ErrInterrupted)
	} else {
		e.st.State, e.st.Result = Done, res
	}
	delete(r.running, e.st.Key)
	r.mu.Unlock()
	r.save()
	close(e.done)
}

// Get returns the job's status; false when the id is unknown or expired.
func (r *Registry) Get(id string) (Status, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	e, ok := r.jobs[id]
	if !ok {
		return Status{}, false
	}
	return e.status(), true
}

// Running returns the job running under key, if any.
func (r *Registry) Running(key string) (Status, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	id, ok := r.running[key]
	if !ok {
		return Status{}, false
	}
	return r.jobs[id].status(), true
}

// Done returns a channel closed when the job ends (already closed for an unknown id).
func (r *Registry) Done(id string) <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.jobs[id]; ok {
		return e.done
	}
	c := make(chan struct{})
	close(c)
	return c
}

// prune drops finished jobs older than retain. Callers hold r.mu.
func (r *Registry) prune() {
	now := r.now()
	for id, e := range r.jobs {
		if e.st.State != Running && !now.Before(e.st.Finished.Add(r.retain)) {
			delete(r.jobs, id)
		}
	}
}

// status copies the status so callers never share Steps with the job. Callers hold r.mu.
func (e *entry) status() Status {
	st := e.st
	st.Steps = append([]string{}, e.st.Steps...)
	return st
}

// ValidID says whether id has the shape Start gives: exactly 32 lowercase hex characters.
func ValidID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}
