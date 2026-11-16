package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// persisted is a registry saving to a file in a fresh temp dir.
func persisted(t *testing.T) (*Registry, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jobs.json")
	r, _ := newTestRegistry()
	t.Cleanup(r.Close) // runs before the temp dir is removed
	if got, err := r.Persist(path, nil); err != nil || len(got) != 0 {
		t.Fatalf("Persist on a missing file = %v, %v", got, err)
	}
	return r, path
}

// restart is a new registry loading path, as the panel's next start does.
func restart(t *testing.T, path string) (*Registry, []Status) {
	t.Helper()
	r, _ := newTestRegistry()
	t.Cleanup(r.Close)
	got, err := r.Persist(path, nil)
	if err != nil {
		t.Fatalf("Persist after restart: %v", err)
	}
	return r, got
}

func isInterrupted(err error) bool {
	var f *Failure
	return errors.As(err, &f) && f.Code == InterruptedCode && f.Message == InterruptedMsg
}

// A job that was running when the panel died (killed, crashed) comes back failed and
// "interrupted" with its steps, is reported once for a notification, and the file is
// private to the panel user (Plan 6 C6 D2: the job used to vanish with a 404).
func TestPersistedRunningJobComesBackInterrupted(t *testing.T) {
	r, path := persisted(t)
	release := make(chan struct{})
	defer close(release)
	stepped := make(chan struct{})
	id, err := r.Start(context.Background(), Spec{Key: "web:bob:a.example", Owner: "bob", Title: "WordPress"},
		func(ctx context.Context, j *Job) (any, error) {
			j.Step("Fetching the files")
			close(stepped)
			<-release
			return nil, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	<-stepped
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("jobs file: %v, %v", fi, err)
	}

	r2, interrupted := restart(t, path)
	if len(interrupted) != 1 || interrupted[0].ID != id || interrupted[0].Owner != "bob" || interrupted[0].Title != "WordPress" {
		t.Fatalf("interrupted = %+v", interrupted)
	}
	st, ok := r2.Get(id)
	if !ok || st.State != Failed || !isInterrupted(st.Err) || st.Finished.IsZero() ||
		st.Step != "Fetching the files" || len(st.Steps) != 1 {
		t.Fatalf("Get after restart = %+v, %v", st, ok)
	}
	if _, busy := r2.Running("web:bob:a.example"); busy {
		t.Error("the interrupted job still holds its key")
	}
	// The next start doesn't report it again, but still answers for it.
	r3, again := restart(t, path)
	if len(again) != 0 {
		t.Errorf("reported again: %+v", again)
	}
	if st, ok := r3.Get(id); !ok || !isInterrupted(st.Err) {
		t.Errorf("Get after a second restart = %+v, %v", st, ok)
	}
}

// Finished jobs survive a restart as they were: result, failure and its args.
func TestPersistedFinishedJobsSurvive(t *testing.T) {
	r, path := persisted(t)
	ok1, _ := r.Start(context.Background(), Spec{Key: "a", Owner: "admin", Title: "A"},
		func(ctx context.Context, j *Job) (any, error) { return map[string]int{"n": 1}, nil })
	bad, _ := r.Start(context.Background(), Spec{Key: "b", Owner: "admin", Title: "B"},
		func(ctx context.Context, j *Job) (any, error) {
			return nil, &Failure{Code: "install_failed", Message: `Failed "%s"`, Args: []string{"x"}}
		})
	plain, _ := r.Start(context.Background(), Spec{Key: "c", Owner: "admin", Title: "C"},
		func(ctx context.Context, j *Job) (any, error) { return nil, errors.New("secret detail") })
	wait(t, r, ok1)
	wait(t, r, bad)
	wait(t, r, plain)

	r2, interrupted := restart(t, path)
	if len(interrupted) != 0 {
		t.Fatalf("interrupted = %+v", interrupted)
	}
	if st, ok := r2.Get(ok1); !ok || st.State != Done || string(st.Result) != `{"n":1}` {
		t.Errorf("done job = %+v, %v", st, ok)
	}
	var f *Failure
	if st, ok := r2.Get(bad); !ok || st.State != Failed || !errors.As(st.Err, &f) ||
		f.Code != "install_failed" || f.Message != `Failed "%s"` || len(f.Args) != 1 || f.Args[0] != "x" {
		t.Errorf("failed job = %+v, %v", st, ok)
	}
	// An error that isn't a Failure is saved as Describe made it (default: internal).
	if st, ok := r2.Get(plain); !ok || !errors.As(st.Err, &f) || f.Code != "internal" {
		t.Errorf("plain error = %+v, %v", st, ok)
	}
}

// Shutdown waits for a running job to finish when it ends within the grace period.
func TestShutdownWaitsForJobs(t *testing.T) {
	r, path := persisted(t)
	id, _ := r.Start(context.Background(), Spec{Key: "k", Owner: "admin"},
		func(ctx context.Context, j *Job) (any, error) {
			time.Sleep(100 * time.Millisecond)
			return nil, Interrupted(ctx)
		})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.Shutdown(ctx, 2*time.Second); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if st, _ := r.Get(id); st.State != Done {
		t.Fatalf("job = %+v", st)
	}
	if _, interrupted := restart(t, path); len(interrupted) != 0 {
		t.Errorf("interrupted = %+v", interrupted)
	}
}

// Past the grace period a job is told to stop (Interrupted) and may clean up with
// WithoutInterrupt; it ends failed and the next start reports it for a notification.
func TestShutdownStopsJobsAtGrace(t *testing.T) {
	r, path := persisted(t)
	cleaned := false
	id, _ := r.Start(context.Background(), Spec{Key: "k", Owner: "bob", Title: "WordPress"},
		func(ctx context.Context, j *Job) (any, error) {
			for {
				if err := Interrupted(ctx); err != nil {
					cleaned = Interrupted(WithoutInterrupt(ctx)) == nil
					return nil, err
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if err := r.Shutdown(ctx, 50*time.Millisecond); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("Shutdown took %v", time.Since(start))
	}
	if st, _ := r.Get(id); st.State != Failed || !isInterrupted(st.Err) || !cleaned {
		t.Fatalf("job = %+v, cleaned %v", st, cleaned)
	}
	_, interrupted := restart(t, path)
	if len(interrupted) != 1 || interrupted[0].ID != id {
		t.Fatalf("interrupted = %+v", interrupted)
	}
}

// A job that ignores the stop is given up on when ctx ends; it is still saved as
// running, so the next start marks it interrupted.
func TestShutdownGivesUpAtDeadline(t *testing.T) {
	r, path := persisted(t)
	release := make(chan struct{})
	defer close(release)
	id, _ := r.Start(context.Background(), Spec{Key: "k", Owner: "admin"},
		func(ctx context.Context, j *Job) (any, error) { <-release; return nil, nil })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := r.Shutdown(ctx, 10*time.Millisecond); err == nil {
		t.Fatal("Shutdown = nil with a job still running")
	}
	_, interrupted := restart(t, path)
	if len(interrupted) != 1 || interrupted[0].ID != id {
		t.Fatalf("interrupted = %+v", interrupted)
	}
}

// Without Persist nothing is written, and Interrupted is nil outside a job.
func TestNoPersistAndPlainContext(t *testing.T) {
	r, _ := newTestRegistry()
	id, _ := r.Start(context.Background(), Spec{Key: "k"}, func(ctx context.Context, j *Job) (any, error) { return nil, nil })
	wait(t, r, id)
	if err := Interrupted(context.Background()); err != nil {
		t.Errorf("Interrupted(Background) = %v", err)
	}
	if err := r.Shutdown(context.Background(), time.Second); err != nil {
		t.Errorf("idle Shutdown = %v", err)
	}
}

// A corrupt file doesn't stop the panel: Persist reports it and starts empty.
func TestPersistCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, _ := newTestRegistry()
	t.Cleanup(r.Close)
	if _, err := r.Persist(path, nil); err == nil {
		t.Fatal("Persist of a corrupt file = nil error")
	}
	id, _ := r.Start(context.Background(), Spec{Key: "k"}, func(ctx context.Context, j *Job) (any, error) { return nil, nil })
	wait(t, r, id)
	if _, interrupted := restart(t, path); len(interrupted) != 0 {
		t.Errorf("interrupted = %+v", interrupted)
	}
}

// A job's data (SetData) is saved with it at once; after a kill, the next start hands
// the interrupted job, data included, to recover while the file still says
// "running", so a recovery cut short is retried by the start after (C6 D2 follow-up).
func TestPersistRecoversKilledJobBeforeMarkingIt(t *testing.T) {
	r, path := persisted(t)
	release, saved := make(chan struct{}), make(chan struct{})
	defer close(release)
	id, _ := r.Start(context.Background(), Spec{Key: "web:bob:a.example", Owner: "bob", Title: "WordPress"},
		func(ctx context.Context, j *Job) (any, error) {
			if err := j.SetData(map[string]string{"dir": "/x"}); err != nil {
				return nil, err
			}
			close(saved)
			<-release
			return nil, nil
		})
	<-saved

	var got []Status
	var during []byte
	r2, _ := newTestRegistry()
	t.Cleanup(r2.Close)
	interrupted, err := r2.PersistRecover(path, nil, func(st Status) {
		got = append(got, st)
		during, _ = os.ReadFile(path)
	})
	if err != nil || len(interrupted) != 1 {
		t.Fatalf("PersistRecover = %+v, %v", interrupted, err)
	}
	if len(got) != 1 || got[0].ID != id || string(got[0].Data) != `{"dir":"/x"}` || got[0].State != Running {
		t.Fatalf("recover got %+v", got)
	}
	if !strings.Contains(string(during), `"state":"running"`) {
		t.Errorf("file during recover: %s", during)
	}
	if st, _ := r2.Get(id); st.State != Failed || !isInterrupted(st.Err) {
		t.Errorf("after recover: %+v", st)
	}
	// Recovered once: the next start doesn't run it again.
	n := 0
	r3, _ := newTestRegistry()
	t.Cleanup(r3.Close)
	if _, err := r3.PersistRecover(path, nil, func(Status) { n++ }); err != nil || n != 0 {
		t.Errorf("second start: recover ran %d times, %v", n, err)
	}
}

// Close waits for a save in progress and stops later ones: a job ending afterwards
// (the test's release, or a job still running when the panel exits) writes nothing,
// so a test's temp dir or the stopped panel's file isn't touched again.
func TestCloseStopsSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	r, _ := newTestRegistry()
	if _, err := r.Persist(path, nil); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	id, _ := r.Start(context.Background(), Spec{Key: "k"}, func(ctx context.Context, j *Job) (any, error) {
		<-release
		return nil, nil
	})
	r.Close()
	saved, _ := os.ReadFile(path)
	close(release)
	wait(t, r, id)
	if now, _ := os.ReadFile(path); string(now) != string(saved) {
		t.Errorf("saved after Close:\n%s\nwas:\n%s", now, saved)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("dir holds %v", entries)
	}
	r.Close() // twice is fine
}
