package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
)

func TestJobErrorBody(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ErrorBody
	}{
		{"failure", &jobs.Failure{Code: "busy", Message: "%s is busy", Args: []string{"x"}},
			ErrorBody{Code: "busy", Message: "%s is busy", Args: []string{"x"}}},
		{"command", &runner.CommandError{Cmd: "v-x", Exit: runner.ExitNotExist, Output: "Error: Hestia domain doesn't exist\n"},
			ErrorBody{Code: "not_found", Message: "Error: Hostisan domain doesn't exist"}},
		{"command, unmapped exit", &runner.CommandError{Cmd: "v-x", Exit: runner.ExitBackup},
			ErrorBody{Code: "command_failed", Message: "Error code: 21"}},
		{"runner timeout", runner.ErrTimeout, ErrorBody{Code: "timeout", Message: "Error: command timed out"}},
		{"job limit", context.DeadlineExceeded, ErrorBody{Code: "timeout", Message: "Error: command timed out"}},
		{"other", errors.New("disk on fire"), ErrorBody{Code: "internal", Message: "Error: internal error"}},
	}
	for _, c := range cases {
		got := jobErrorBody(c.err)
		if got == nil || got.Code != c.want.Code || got.Message != c.want.Message || !slices.Equal(got.Args, c.want.Args) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestJobStatusBody(t *testing.T) {
	start := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	running := jobStatusBody(jobs.Status{ID: "a", Title: "Install", State: jobs.Running, Started: start})
	if running.State != "running" || running.Error != nil || running.Finished != "" ||
		running.Started != "2026-01-10T12:00:00Z" || running.Steps == nil || running.Result != nil {
		t.Errorf("running = %+v", running)
	}
	done := jobStatusBody(jobs.Status{ID: "a", State: jobs.Done, Step: "b", Steps: []string{"a", "b"},
		Result: json.RawMessage(`{"n":1}`), Started: start, Finished: start.Add(time.Minute)})
	b, _ := json.Marshal(done)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	if doc["state"] != "done" || doc["finished"] != "2026-01-10T12:01:00Z" || doc["error"] != nil || doc["step"] != "b" {
		t.Errorf("done = %s", b)
	}
	if r, ok := doc["result"].(map[string]any); !ok || r["n"] != float64(1) {
		t.Errorf("done result = %s", b)
	}
	failed := jobStatusBody(jobs.Status{State: jobs.Failed, Err: &jobs.Failure{Code: "busy", Message: "m"}, Started: start, Finished: start})
	if failed.State != "failed" || failed.Error == nil || failed.Error.Code != "busy" {
		t.Errorf("failed = %+v", failed)
	}
}
