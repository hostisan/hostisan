package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
)

// cronRoutes registers cron jobs (Plan 3 Slice E). Called from registerAccountRoutes.
func (s *server) cronRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/cron", s.read(s.cronList))
	// Go's mux prefers the literal /options over the {job} wildcard.
	mux.Handle("GET /api/v1/cron/options", s.read(s.cronOptions))
	mux.Handle("GET /api/v1/cron/{job}", s.read(s.cronOne))
	mux.Handle("POST /api/v1/cron", s.write(s.cronAdd))
	mux.Handle("PUT /api/v1/cron/{job}", s.write(s.cronEdit))
	mux.Handle("DELETE /api/v1/cron/{job}", s.write(s.cronAction("v-delete-cron-job")))
	mux.Handle("POST /api/v1/cron/{job}/suspend", s.adminWrite(s.cronAction("v-suspend-cron-job")))
	mux.Handle("POST /api/v1/cron/{job}/unsuspend", s.adminWrite(s.cronAction("v-unsuspend-cron-job")))
	mux.Handle("POST /api/v1/cron/bulk", s.write(s.cronBulk))
	mux.Handle("POST /api/v1/cron/reports", s.write(s.cronReports))
}

// cronJobParam is the {job} path value; it answers 400 itself when unusable.
func cronJobParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	j := r.PathValue("job")
	if !hestia.ValidRecordID(j) {
		invalidFormat(w, "job", j)
		return "", false
	}
	return j, true
}

// cronJobs lists the jobs of `v-list-cron-jobs`/`v-list-cron-job` (args end in "json").
// Hestia prints a backslash or tab in a command unescaped, which makes the JSON
// invalid; then the same command is read again as `plain` (Parity 25).
func (s *server) cronJobs(w http.ResponseWriter, r *http.Request, cmd string, args ...string) ([]hestia.CronJob, bool) {
	// v-list-cron-job (args: user, job) prints no JOB column in plain; the list does.
	single := cmd == "v-list-cron-job"
	out, ok := s.output(w, r, cmd, append(args, "json")...)
	if !ok {
		return nil, false
	}
	jobs, err := hestia.DecodeObjects[hestia.CronJob](out)
	if err == nil {
		return jobs, true
	}
	out, ok = s.output(w, r, cmd, append(args, "plain")...)
	if !ok {
		return nil, false
	}
	if single {
		var j hestia.CronJob
		if j, err = hestia.DecodeCronPlainJob(out, args[1]); err == nil {
			jobs = []hestia.CronJob{j}
		}
	} else {
		jobs, err = hestia.DecodeCronPlain(out)
	}
	if err != nil {
		serverError(w, err)
		return nil, false
	}
	return jobs, true
}

func (s *server) cronList(w http.ResponseWriter, r *http.Request) {
	user := s.actingUser(r)
	jobs, ok := s.cronJobs(w, r, "v-list-cron-jobs", user)
	if !ok {
		return
	}
	u, ok, err := s.H.User(r.Context(), user)
	if err != nil || !ok {
		serverError(w, firstErr(err, "cron list: user "+user+" not found"))
		return
	}
	writeJSON(w, http.StatusOK, CronJobs{
		Jobs:    jobs,
		Reports: u["CRON_REPORTS"] == "yes",
		Usage:   CronUsage{Jobs: u["U_CRON_JOBS"], Limit: u["CRON_JOBS"], Suspended: u["SUSPENDED_CRON"]},
		Contact: u["CONTACT"],
	})
}

func (s *server) cronOne(w http.ResponseWriter, r *http.Request) {
	id, ok := cronJobParam(w, r)
	if !ok {
		return
	}
	jobs, ok := s.cronJobs(w, r, "v-list-cron-job", s.actingUser(r), id)
	if !ok {
		return
	}
	if len(jobs) != 1 {
		serverError(w, firstErr(nil, "v-list-cron-job: unexpected output"))
		return
	}
	writeJSON(w, http.StatusOK, jobs[0])
}

func (s *server) cronOptions(w http.ResponseWriter, r *http.Request) {
	out, ok := s.output(w, r, "v-get-sys-timezone")
	if !ok {
		return
	}
	tz, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	tz = strings.TrimSpace(tz)
	now := time.Now()
	if loc, err := time.LoadLocation(tz); err == nil && tz != "" {
		now = now.In(loc)
	} else {
		now = now.UTC()
		if tz == "" {
			tz = "UTC"
		}
	}
	writeJSON(w, http.StatusOK, CronOptions{Timezone: tz, Now: now.Format(time.RFC3339)})
}

// cronArgs checks a request like the bin does (blanks first, then Parity 23 and 24)
// and returns the five fields and the command in v-add-cron-job's order. false means
// the request was answered 400 and no command may run.
func cronArgs(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	var req CronRequest
	if !decodeBody(w, r, &req) {
		return nil, false
	}
	if !blankFields(w, "Minute", req.Min, "Hour", req.Hour, "Day", req.Day, "Month", req.Month,
		"Day of Week", req.WDay, "Command", req.Cmd) {
		return nil, false
	}
	for _, f := range []struct{ kind, v string }{
		{"min", req.Min}, {"hour", req.Hour}, {"day", req.Day}, {"month", req.Month}, {"wday", req.WDay},
	} {
		if !hestia.ValidCronField(f.kind, f.v) {
			writeJSON(w, http.StatusBadRequest, APIError{Error: ErrorBody{
				Code: "invalid", Message: "invalid %s format :: %s", Args: []string{f.kind, f.v}}})
			return nil, false
		}
	}
	if !hestia.ValidCronCommand(req.Cmd) {
		WriteError(w, http.StatusBadRequest, "invalid", "Invalid cron command format")
		return nil, false
	}
	return []string{req.Min, req.Hour, req.Day, req.Month, req.WDay, req.Cmd}, true
}

func (s *server) cronAdd(w http.ResponseWriter, r *http.Request) {
	f, ok := cronArgs(w, r)
	if !ok {
		return
	}
	if err := s.run(r.Context(), "v-add-cron-job", append([]string{s.actingUser(r)}, f...)...); err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, StatusReply{Status: "ok"})
}

func (s *server) cronEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := cronJobParam(w, r)
	if !ok {
		return
	}
	f, ok := cronArgs(w, r)
	if !ok {
		return
	}
	if err := s.run(r.Context(), "v-change-cron-job", append([]string{s.actingUser(r), id}, f...)...); err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, StatusReply{Status: "ok"})
}

// cronAction is delete, suspend or unsuspend of one job: `<cmd> <user> <job>`.
func (s *server) cronAction(cmd string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := cronJobParam(w, r)
		if !ok {
			return
		}
		if err := s.run(r.Context(), cmd, s.actingUser(r), id); err != nil {
			writeCommandError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, StatusReply{Status: "ok"})
	}
}

// cronBulk: delete is open to users, suspend and unsuspend are admin-only. Each item
// runs with restart "no", then v-restart-cron runs once.
func (s *server) cronBulk(w http.ResponseWriter, r *http.Request) {
	s.bulkFor(w, r, map[string]string{
		"delete": "v-delete-cron-job", "suspend": "v-suspend-cron-job", "unsuspend": "v-unsuspend-cron-job",
	}, []string{"delete"}, hestia.ValidRecordID,
		func(user, id string) []string { return []string{user, id, "no"} },
		[][]string{{"v-restart-cron"}})
}

func (s *server) cronReports(w http.ResponseWriter, r *http.Request) {
	var req CronReportsRequest
	if !decodeBody(w, r, &req) {
		return
	}
	cmd := "v-delete-cron-reports"
	if req.Enabled {
		cmd = "v-add-cron-reports"
	}
	if err := s.run(r.Context(), cmd, s.actingUser(r)); err != nil {
		writeCommandError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, StatusReply{Status: "ok"})
}
