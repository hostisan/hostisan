package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"log/slog"
	"strings"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/webapp"
)

// jobErrorBody is a failed job's error in the shape a synchronous request would have
// answered with.
func jobErrorBody(err error) *ErrorBody {
	var f *jobs.Failure
	var ce *runner.CommandError
	switch {
	case errors.As(err, &f):
		return &ErrorBody{Code: f.Code, Message: f.Message, Args: f.Args}
	case errors.As(err, &ce):
		code := "command_failed"
		if m, ok := exitErrors[ce.Exit]; ok {
			code = m.code
		}
		return &ErrorBody{Code: code, Message: commandMessage(ce)}
	case errors.Is(err, runner.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return &ErrorBody{Code: "timeout", Message: "Error: command timed out"}
	}
	slog.Error("background job failed", "err", err)
	return &ErrorBody{Code: "internal", Message: "Error: internal error"}
}

// jobStatusBody is a job's status as the UI polls it.
func jobStatusBody(st jobs.Status) JobStatus {
	out := JobStatus{
		ID:      st.ID,
		Title:   st.Title,
		State:   string(st.State),
		Step:    st.Step,
		Steps:   append([]string{}, st.Steps...),
		Started: st.Started.Format(time.RFC3339),
	}
	if st.State == jobs.Failed {
		out.Error = jobErrorBody(st.Err)
	}
	if st.Result != nil {
		out.Result = st.Result
	}
	if !st.Finished.IsZero() {
		out.Finished = st.Finished.Format(time.RFC3339)
	}
	return out
}

// JobFailure is a job's error as the jobs file saves it (jobs.Registry.Persist): the
// error body a poll would answer with, so a raw error's text never reaches the file.
func JobFailure(err error) *jobs.Failure {
	b := jobErrorBody(err)
	return &jobs.Failure{Code: b.Code, Message: b.Message, Args: b.Args}
}

// The panel notification for a job a restart cut short. Hestia stores notifications
// as written (they aren't msgids), so they are English, like the core's own.
const (
	interruptedTopic  = "Task interrupted"
	interruptedNotice = "was stopped because the panel restarted. Check what it changed and run it again."
)

// NotifyInterrupted adds a panel notification (the bell) for each job a restart cut
// short (jobs.Registry.Persist's list), to the job's owner. A failure is logged.
func NotifyInterrupted(ctx context.Context, r runner.Runner, sts []jobs.Status) {
	for _, st := range sts {
		if st.Owner == "" {
			continue
		}
		notice := `"` + html.EscapeString(st.Title) + `" ` + interruptedNotice
		res, err := r.Run(runner.WithTimeout(ctx, 30*time.Second), "v-add-user-notification", st.Owner, interruptedTopic, notice)
		if err == nil {
			err = res.Err("v-add-user-notification")
		}
		if err != nil {
			slog.Warn("interrupted job: notification not added", "job", st.ID, "user", st.Owner, "err", err)
		}
	}
}

// RecoverInterrupted is jobs.Registry.PersistRecover's undo for a panel killed in the
// middle of an app install: the job's key ("web:<user>:<domain>") and owner must agree
// and its saved webapp.Undo must name that domain's public_html (webapp.Recover checks
// it); then the entries the install wrote to public_html are removed, so no public
// setup page is left. Other jobs, and installs without a record (killed before any
// file arrived), are skipped. Failures are logged; the job is still reported interrupted.
func RecoverInterrupted(ctx context.Context, r runner.Runner) func(jobs.Status) {
	return func(st jobs.Status) {
		rest, ok := strings.CutPrefix(st.Key, "web:")
		user, domain, ok2 := strings.Cut(rest, ":")
		if !ok || !ok2 || user != st.Owner || !hestia.ValidUsername(user) || !hestia.ValidDomainArg(domain) || st.Data == nil {
			return
		}
		var u webapp.Undo
		if err := json.Unmarshal(st.Data, &u); err != nil {
			slog.Warn("interrupted install: undo record unreadable", "job", st.ID, "err", err)
			return
		}
		if err := webapp.Recover(ctx, &webapp.Host{R: r, User: user}, domain, u); err != nil {
			slog.Warn("interrupted install: undo refused", "job", st.ID, "user", user, "domain", domain, "err", err)
			return
		}
		slog.Info("interrupted install: added files removed", "job", st.ID, "user", user, "domain", domain)
	}
}
