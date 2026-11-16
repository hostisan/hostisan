package webapp

import (
	"context"
	"log/slog"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/hostisan/hostisan/internal/jobs"
)

// Env is the system settings an install depends on.
type Env struct {
	WebSystem  string // WEB_SYSTEM: the nginx template applies only for "nginx"
	WebBackend string // WEB_BACKEND: "php-fpm" sets the backend template
}

func secs(n int) time.Duration { return time.Duration(n) * time.Second }

// Install is BaseSetup::install (checklist section 3.9 step 2); step reports each phase.
// opts holds the form values plus php_version. Unlike PHP, a failed or interrupted
// install (jobs.Interrupted: the panel is stopping) removes the entries of public_html
// it wrote (removeAdded), and only those: a file the user uploads there meanwhile
// stays (final review M4). The database and the domain's templates stay as they are.
func Install(ctx context.Context, inst Installer, t *Target, opts map[string]string, h *Host, env Env, step func(string)) (err error) {
	if step == nil {
		step = func(string) {}
	}
	cfg := inst.Config()

	step("Cleaning the web folder")
	for _, f := range []string{"robots.txt", "index.html"} {
		if err := h.DeleteFile(ctx, t.DocRoot(f)); err != nil {
			return err
		}
	}
	// What public_html holds before the app's files arrive: a half-installed app can
	// leave a public setup page (WordPress's setup-config.php or install.php) that
	// anyone could finish with their own admin account (Plan 6 C6 D1, D2).
	// Cleaned: Host.writing matches written paths against it, and Recover takes only
	// a clean Dir.
	dir := path.Clean(t.DocRoot())
	before, err := h.listEntries(ctx, dir, "-A", "-p")
	if err != nil {
		return err
	}
	// From here the host records each entry of public_html the install writes
	// (Host.writing). Saved with the job before any file arrives, and again as it
	// grows: a panel killed mid-install never runs the defer below, so the next start
	// undoes it (Recover).
	h.track = &Undo{Dir: dir, Before: before, Tracked: true}
	defer func() {
		if err != nil {
			h.removeAdded(ctx, *h.track)
		}
		h.track = nil
	}()
	if err = h.saveUndo(); err != nil {
		return err
	}

	step("Fetching the files")
	if err := retrieve(ctx, cfg.Resource, t, opts, h, inst.Info().Version); err != nil {
		return err
	}

	if t.DB.Create {
		step("Creating the database")
		prefix := h.User + "_"
		name := strings.ReplaceAll(t.DB.Name, prefix, "")
		user := strings.ReplaceAll(t.DB.User, prefix, "")
		if err := h.DatabaseAdd(ctx, name, user, t.DB.Password, t.DB.Host); err != nil {
			return err
		}
	}

	step("Configuring the web server")
	tpl := "default"
	if env.WebSystem == "nginx" && cfg.Template != "" {
		tpl = cfg.Template
	}
	if err := h.ChangeWebTemplate(ctx, t.Domain, tpl); err != nil {
		return err
	}
	if env.WebBackend == "php-fpm" {
		installed, err := h.InstalledPHP(ctx)
		if err != nil {
			return err
		}
		if len(SupportedPHP(installed, cfg.PHP)) == 0 {
			return Fail("Required PHP version is not supported")
		}
		if err := h.ChangeBackendTemplate(ctx, t.Domain, "PHP-"+strings.ReplaceAll(opts["php_version"], ".", "_")); err != nil {
			return err
		}
	}

	step("Installing the application")
	return inst.Setup(ctx, t, opts, h)
}

// Undo is what Recover needs to undo an install: public_html and its entries (`ls -A
// -p`) before the app's files arrived, and what the install wrote there since.
type Undo struct {
	Dir    string   `json:"dir"`
	Before []string `json:"before"`
	// Tracked says Wrote and Open below are kept. A record without it (saved by an
	// older build) undoes every entry added since Before.
	Tracked bool `json:"tracked,omitempty"`
	// Wrote is the names of the entries the install wrote.
	Wrote []string `json:"wrote,omitempty"`
	// Running is set while a write whose names aren't known in advance runs (an
	// archive, composer, a PHP script, a post to the site); Open is public_html's
	// entries (`ls -A -p`) right before it, so what appeared since is the install's.
	Running bool     `json:"running,omitempty"`
	Open    []string `json:"open,omitempty"`
}

// trackFlags lists public_html around a write whose names aren't known in advance.
// -U (directory order) is all a set needs, and tells these listings apart.
var trackFlags = []string{"-A", "-p", "-U"}

// saveUndo hands the current undo record to h.Undo.
func (h *Host) saveUndo() error {
	if h.Undo == nil || h.track == nil {
		return nil
	}
	u := *h.track
	u.Before, u.Wrote, u.Open = slices.Clone(u.Before), slices.Clone(u.Wrote), slices.Clone(u.Open)
	if err := h.Undo(u); err != nil {
		return Fail("%s", "the install's undo record could not be saved")
	}
	return nil
}

// writing runs fn, a write of the install to p, and records the entry of public_html
// it writes: the top-level name of p, saved before fn runs, unless public_html
// already holds it. For p "" (a command that
// writes where it likes) or public_html itself (an archive or a directory unpacked or
// copied into it), the entries that appear while fn runs are the install's.
// Outside an install (no record) or for p outside public_html, fn just runs.
func (h *Host) writing(ctx context.Context, p string, fn func() error) error {
	u := h.track
	if u == nil {
		return fn()
	}
	if p != "" && path.Clean(p) != u.Dir {
		rel, inside := strings.CutPrefix(path.Clean(p), u.Dir+"/")
		if !inside {
			return fn()
		}
		name, _, _ := strings.Cut(rel, "/")
		if slices.Contains(u.Wrote, name) || slices.Contains(u.Before, name) || slices.Contains(u.Before, name+"/") {
			return fn()
		}
		// An entry already there that the install didn't write is the user's (a
		// directory made mid-install): writing into it doesn't make it the install's.
		now, err := h.listEntries(ctx, u.Dir, trackFlags...)
		if err != nil {
			return err
		}
		if !slices.Contains(now, name) && !slices.Contains(now, name+"/") {
			u.Wrote = append(u.Wrote, name)
			if err := h.saveUndo(); err != nil {
				return err
			}
		}
		return fn()
	}
	open, err := h.listEntries(ctx, u.Dir, trackFlags...)
	if err != nil {
		return err
	}
	u.Running, u.Open = true, open
	if err := h.saveUndo(); err != nil {
		return err
	}
	if err := fn(); err != nil {
		return err // removeAdded takes what appeared since Open
	}
	now, err := h.listEntries(ctx, u.Dir, trackFlags...)
	if err != nil {
		return err
	}
	u.Wrote = appendNew(u.Wrote, now, open)
	u.Running, u.Open = false, nil
	return h.saveUndo()
}

// appendNew adds to wrote the names of the entries in now that aren't in open.
func appendNew(wrote, now, open []string) []string {
	for _, e := range now {
		name := strings.TrimSuffix(e, "/")
		if !slices.Contains(open, e) && !slices.Contains(wrote, name) {
			wrote = append(wrote, name)
		}
	}
	return wrote
}

// Recover undoes an install the panel was killed in the middle of (the next start,
// from the job's saved Undo): it removes the entries of public_html the install wrote
// and keeps the rest; running it again is harmless. It refuses, running nothing, an
// Undo whose Dir isn't exactly domain's public_html under h's home or whose entries
// aren't plain names, so a tampered or stale record can't delete elsewhere.
func Recover(ctx context.Context, h *Host, domain string, u Undo) error {
	if domain == "" || domain == "." || domain == ".." || strings.ContainsAny(domain, "/\x00") ||
		strings.HasPrefix(domain, "-") {
		return Fail("%s", "invalid domain")
	}
	if u.Dir != (&Target{Domain: domain, Home: h.home()}).DocRoot() || path.Clean(u.Dir) != u.Dir {
		return Fail(`Invalid path "%s"`, u.Dir)
	}
	for _, list := range [][]string{u.Before, u.Wrote, u.Open} {
		for _, e := range list {
			name := strings.TrimSuffix(e, "/")
			if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
				return Fail(`Invalid path "%s"`, e)
			}
		}
	}
	h.removeAdded(ctx, u)
	return nil
}

// cleanupTimeout bounds removeAdded, which also runs after the job's own limit.
const cleanupTimeout = 2 * time.Minute

// removeAdded deletes the entries of u.Dir the install wrote: those in u.Wrote, and
// while a write was running (u.Running) those not in u.Open. An entry in u.Before
// (`ls -A -p` before the install) is never deleted. A record of an older build
// (!u.Tracked) deletes every entry not in u.Before. It runs even when the job was
// told to stop or timed out, and only logs a failure: the install's own error is the
// one reported.
func (h *Host) removeAdded(ctx context.Context, u Undo) {
	dir := u.Dir
	ctx, cancel := context.WithTimeout(jobs.WithoutInterrupt(context.WithoutCancel(ctx)), cleanupTimeout)
	defer cancel()
	now, err := h.listEntries(ctx, dir, "-A", "-p")
	if err != nil {
		slog.Warn("failed app install: files not removed", "dir", dir, "err", err)
		return
	}
	for _, e := range now {
		if slices.Contains(u.Before, e) || e == "./" || e == "../" || strings.Contains(strings.TrimSuffix(e, "/"), "/") {
			continue
		}
		if u.Tracked && !slices.Contains(u.Wrote, strings.TrimSuffix(e, "/")) &&
			(!u.Running || slices.Contains(u.Open, e)) {
			continue // not the install's: the user's own upload
		}
		if name, isDir := strings.CutSuffix(e, "/"); isDir {
			err = h.DeleteDirectory(ctx, joinPaths(dir, name))
		} else {
			err = h.DeleteFile(ctx, joinPaths(dir, e))
		}
		if err != nil {
			slog.Warn("failed app install: file not removed", "dir", dir, "entry", e, "err", err)
		}
	}
}

// retrieve is BaseSetup::retrieveResources.
func retrieve(ctx context.Context, r Resource, t *Target, opts map[string]string, h *Host, version string) error {
	dst := t.DocRoot()
	if r.Dst != "" {
		dst = t.DocRoot(r.Dst)
	}
	switch r.Kind {
	case "composer":
		return h.RunComposer(ctx, opts["php_version"], "create-project", "--no-progress",
			"--no-interaction", "--prefer-dist", r.Src, "-d", phpDirname(dst), phpBasename(dst))
	case "wp":
		return h.RunWp(ctx, opts["php_version"], "core", "download", "--locale="+opts["language"],
			"--version="+version, "--path="+dst)
	case "archive":
		file, err := h.DownloadURL(ctx, r.Src, dst)
		if err != nil {
			return err
		}
		if err := h.ArchiveExtract(ctx, file, dst); err != nil {
			return err
		}
		return h.DeleteFile(ctx, file)
	}
	return Fail(`Unknown resource type "%s"`, r.Kind)
}
