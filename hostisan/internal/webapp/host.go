// Package webapp ports Hestia's quick-install apps (web/src/app/WebApp): the 22
// installers, the host calls they make (HestiaApp.php) and the install sequence
// (BaseSetup.php). It never logs a command's arguments: they carry installer passwords.
package webapp

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hostisan/hostisan/internal/jobs"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/secret"
)

// CommandTimeout is PHP's Process::setTimeout(300) for every installer command.
const CommandTimeout = 300 * time.Second

// failCode is the error code of every installer failure.
const failCode = "install_failed"

// Fail is an installer failure: the English msgid and its args, as the checklist's
// section 4 lists them.
func Fail(msg string, args ...string) *jobs.Failure {
	return &jobs.Failure{Code: failCode, Message: msg, Args: args}
}

// Host is HestiaApp.php for one user: every call is a v-* command with the user as the
// first argument.
type Host struct {
	R    runner.Runner
	User string
	// Home is the user's home directory; "" means /home/<user>.
	Home string
	// TmpDir is where temporary files go (Hestia only reads /tmp); "" means /tmp.
	TmpDir string
	// DialContext, TLSConfig and Sleep are seams for tests; nil takes the real thing.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	TLSConfig   *tls.Config
	Sleep       func(ctx context.Context, d time.Duration) error
	// Undo, when set, saves what Install needs to undo a killed install (the job's
	// data); Install stops if it fails.
	Undo func(Undo) error
	// track is the running install's undo record (Install sets it); the write
	// methods add to it (writing).
	track *Undo
}

func (h *Host) home() string {
	if h.Home != "" {
		return h.Home
	}
	return "/home/" + h.User
}

func (h *Host) tmp() string {
	if h.TmpDir != "" {
		return h.TmpDir
	}
	return "/tmp"
}

// run runs cmd with the user first and the arguments as they are. HestiaApp::run
// escaped every ' ' as '\ ' for v-run-cli-cmd's word splitting (Parity 20); the core
// now passes the arguments on as an array (B2c), so nothing is escaped. A non-zero
// exit is an error; the command's output is returned only on success.
func (h *Host) run(ctx context.Context, cmd string, args ...string) (string, error) {
	return h.exec(ctx, cmd, append([]string{h.User}, args...))
}

// plain runs cmd without the user argument (v-list-database-hosts, v-list-sys-php).
func (h *Host) plain(ctx context.Context, cmd string, args ...string) (string, error) {
	return h.exec(ctx, cmd, args)
}

func (h *Host) exec(ctx context.Context, cmd string, args []string) (string, error) {
	if err := jobs.Interrupted(ctx); err != nil {
		return "", err // a stopping panel: no new command (Install cleans up)
	}
	res, err := h.R.Run(runner.WithTimeout(ctx, CommandTimeout), cmd, args...)
	if err != nil {
		return "", err
	}
	if err := res.Err(cmd); err != nil {
		return "", err
	}
	return string(res.Stdout), nil
}

// failed turns a command error into the installer failure with msgid, unless the job's
// own context ended (the job limit), which is passed on as it is.
func failed(ctx context.Context, err error, msg string, args ...string) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, jobs.ErrInterrupted) {
		return err
	}
	return Fail(msg, args...)
}

func (h *Host) AddDirectory(ctx context.Context, p string) error {
	return h.writing(ctx, p, func() error {
		_, err := h.run(ctx, "v-add-fs-directory", p)
		return failed(ctx, err, `Failed to add directory "%s"`, p)
	})
}

func (h *Host) CopyDirectory(ctx context.Context, from, to string) error {
	return h.writing(ctx, to, func() error {
		_, err := h.run(ctx, "v-copy-fs-directory", from, to)
		return failed(ctx, err, `Failed to copy directory "%s" to "%s"`, from, to)
	})
}

func (h *Host) DeleteDirectory(ctx context.Context, p string) error {
	_, err := h.run(ctx, "v-delete-fs-directory", p)
	return failed(ctx, err, `Failed to remove directory "%s"`, p)
}

// ListFiles is `ls -A` of p, without blank lines.
func (h *Host) ListFiles(ctx context.Context, p string) ([]string, error) {
	return h.listEntries(ctx, p, "-A")
}

// listEntries is `ls <flags> p`; with -p, directories end in "/".
func (h *Host) listEntries(ctx context.Context, p string, flags ...string) ([]string, error) {
	out, err := h.run(ctx, "v-run-cli-cmd", append(append([]string{"ls"}, flags...), p)...)
	if err != nil {
		return nil, failed(ctx, err, "Cannot list domain files")
	}
	var files []string
	for _, l := range strings.Split(out, "\n") {
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

func (h *Host) ReadFile(ctx context.Context, p string) (string, error) {
	out, err := h.run(ctx, "v-open-fs-file", p)
	if err != nil {
		return "", failed(ctx, err, `Failed to read file "%s"`, p)
	}
	return out, nil
}

// CreateFile writes contents to p through v-copy-fs-file. The command copies as the
// user, who must be able to read the source, so the file is 0644 as in PHP. It sits in
// a directory nobody else can list (0711, random name) under a random file name and is removed straight after.
func (h *Host) CreateFile(ctx context.Context, p, contents string) error {
	return h.writing(ctx, p, func() error { return h.createFile(ctx, p, contents) })
}

func (h *Host) createFile(ctx context.Context, p, contents string) error {
	dir, err := os.MkdirTemp(h.tmp(), "hostisan-")
	if err != nil {
		return Fail("Error creating temp file")
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o711); err != nil {
		return Fail("Error creating temp file")
	}
	f, err := os.CreateTemp(dir, "f-*")
	if err != nil {
		return Fail("Error creating temp file")
	}
	src := f.Name()
	_, werr := f.WriteString(contents)
	if cerr := f.Close(); werr != nil || cerr != nil {
		return Fail("Error writing to temp file")
	}
	if err := os.Chmod(src, 0o644); err != nil {
		return Fail("Error writing to temp file")
	}
	_, err = h.run(ctx, "v-copy-fs-file", src, p)
	return failed(ctx, err, `Failed to create file "%s"`, p)
}

func (h *Host) MoveFile(ctx context.Context, from, to string) error {
	return h.writing(ctx, to, func() error {
		_, err := h.run(ctx, "v-move-fs-file", from, to)
		return failed(ctx, err, `Failed to move file "%s" to "%s"`, from, to)
	})
}

func (h *Host) ChangeFilePermissions(ctx context.Context, p, perm string) error {
	_, err := h.run(ctx, "v-change-fs-file-permission", p, perm)
	return failed(ctx, err, `Failed to change file "%s" permissions to "%s"`, p, perm)
}

func (h *Host) DeleteFile(ctx context.Context, p string) error {
	_, err := h.run(ctx, "v-delete-fs-file", p)
	return failed(ctx, err, `Failed to delete file "%s"`, p)
}

// ArchiveExtract unpacks file into dir, flattening one level (the trailing "" and 1).
func (h *Host) ArchiveExtract(ctx context.Context, file, dir string) error {
	if dir == "" {
		return Fail("Error extracting archive: missing target folder")
	}
	return h.writing(ctx, dir, func() error {
		_, err := h.run(ctx, "v-extract-fs-archive", file, dir, "", "1")
		return failed(ctx, err, `Failed to extract "%s"`, file)
	})
}

var wgetSaved = regexp.MustCompile(`URL:\s*.+?\s*\[.+?\]\s*->\s*"(.+?)"`)

// DownloadURL fetches rawurl into dir with wget and returns the saved file's path:
// what wget reported, else dir/<basename of the URL>.
func (h *Host) DownloadURL(ctx context.Context, rawurl, dir string) (string, error) {
	var out string
	err := h.writing(ctx, dir, func() error {
		var err error
		out, err = h.run(ctx, "v-run-cli-cmd", "/usr/bin/wget", "--tries", "3", "--timeout=30",
			"--no-dns-cache", "-nv", rawurl, "-P", dir)
		return failed(ctx, err, `Failed to download "%s" to path "%s"`, rawurl, dir)
	})
	if err != nil {
		return "", err
	}
	if m := wgetSaved.FindStringSubmatch(out); len(m) > 1 {
		return m[1], nil
	}
	return dir + "/" + phpBasename(rawurl), nil
}

// DatabaseAdd creates a MySQL database through v-add-database; the password goes in a
// private temp file that is removed again whatever the outcome.
func (h *Host) DatabaseAdd(ctx context.Context, name, user, password, dbHost string) error {
	file, cleanup, err := secret.TempFile(h.tmp(), password)
	if err != nil {
		return Fail("Error creating temp file")
	}
	defer cleanup()
	_, err = h.run(ctx, "v-add-database", name, user, file, "mysql", dbHost, "utf8mb4")
	return failed(ctx, err, "Unable to add database!")
}

func (h *Host) ChangeWebTemplate(ctx context.Context, domain, tpl string) error {
	_, err := h.run(ctx, "v-change-web-domain-tpl", domain, tpl)
	return failed(ctx, err, `Failed to change to template "%s"`, tpl)
}

func (h *Host) ChangeBackendTemplate(ctx context.Context, domain, tpl string) error {
	_, err := h.run(ctx, "v-change-web-domain-backend-tpl", domain, tpl)
	return failed(ctx, err, `Failed to change backend template to "%s"`, tpl)
}

// InstalledPHP is the installed PHP versions (v-list-sys-php json), as listed.
func (h *Host) InstalledPHP(ctx context.Context) ([]string, error) {
	out, err := h.plain(ctx, "v-list-sys-php", "json")
	if err != nil {
		return nil, failed(ctx, err, "Failed to load installed PHP versions")
	}
	return DecodeVersions(out)
}

// RunComposer runs composer (installed first) under the given PHP version.
func (h *Host) RunComposer(ctx context.Context, phpVersion string, args ...string) error {
	if _, err := h.run(ctx, "v-add-user-composer", "2", "yes"); err != nil {
		return failed(ctx, err, `Failed to run php command "%s"`, "v-add-user-composer")
	}
	return h.RunPHP(ctx, phpVersion, h.home()+"/.composer/composer", args...)
}

// RunWp runs wp-cli (installed first) under the given PHP version.
func (h *Host) RunWp(ctx context.Context, phpVersion string, args ...string) error {
	if _, err := h.run(ctx, "v-add-user-wp-cli", "yes"); err != nil {
		return failed(ctx, err, `Failed to run php command "%s"`, "v-add-user-wp-cli")
	}
	return h.RunPHP(ctx, phpVersion, h.home()+"/.wp-cli/wp", args...)
}

// RunPHP runs /usr/bin/php<version> command args... as the user. The failure names the
// binary and script only: the other arguments carry passwords.
// It may write anywhere in public_html (Host.writing with no path).
func (h *Host) RunPHP(ctx context.Context, phpVersion, command string, args ...string) error {
	bin := "/usr/bin/php" + phpVersion
	return h.writing(ctx, "", func() error {
		_, err := h.run(ctx, "v-run-cli-cmd", append([]string{bin, command}, args...)...)
		return failed(ctx, err, `Failed to run php command "%s"`, bin+" "+command)
	})
}

// Form is an ordered list of form fields (PHP's http_build_query keeps the order).
type Form [][2]string

func (f Form) encode() string {
	var b strings.Builder
	for i, kv := range f {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(kv[0]) + "=" + url.QueryEscape(kv[1]))
	}
	return b.String()
}

// F builds a Form from key, value pairs.
func F(kv ...string) Form {
	f := make(Form, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		f = append(f, [2]string{kv[i], kv[i+1]})
	}
	return f
}

// postStatusMsg fails an install whose installer page answered 4xx or 5xx: the page's
// path and the status.
const postStatusMsg = `The installer page "%s" answered with HTTP status %s`

// SendPost posts form to rawurl as PHP's curl did: follow at most 3 redirects and
// verify certificates. resolve is curl's CURLOPT_RESOLVE "domain:port:ip", for a domain
// the server can't resolve itself (NAT). Unlike PHP, a final 4xx or 5xx fails it
// (postStatusMsg): a 503 from a PHP pool that isn't up yet used to report "done" with
// nothing installed (Plan 6 C6 D1). The site's installer may write anywhere in
// public_html (Host.writing with no path).
func (h *Host) SendPost(ctx context.Context, rawurl string, form Form, headers []string, resolve string) error {
	if err := jobs.Interrupted(ctx); err != nil {
		return err
	}
	return h.writing(ctx, "", func() error { return h.sendPost(ctx, rawurl, form, headers, resolve) })
}

func (h *Host) sendPost(ctx context.Context, rawurl string, form Form, headers []string, resolve string) error {
	ctx, cancel := context.WithTimeout(ctx, CommandTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawurl, strings.NewReader(form.encode()))
	if err != nil {
		return Fail("%s", "invalid request address")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, hd := range headers {
		if k, v, ok := strings.Cut(hd, ":"); ok {
			req.Header.Set(strings.TrimSpace(k), strings.TrimSpace(v))
		}
	}
	res, err := h.client(resolve).Do(req)
	if err != nil {
		return transportFailure(ctx, err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if err := res.Body.Close(); err != nil {
		return err
	}
	if res.StatusCode >= 400 {
		return Fail(postStatusMsg, req.URL.Path, strconv.Itoa(res.StatusCode))
	}
	return nil
}

// siteWaitTries bounds WaitSite: one GET a second, so about a minute plus the GETs.
const (
	siteWaitTries   = 60
	siteWaitTimeout = 10 * time.Second
)

// WaitSite waits until the site of rawurl answers its front page (GET /, redirects
// followed) with anything but 5xx or a transport error, at most siteWaitTries times
// one second apart. Install switches the backend template just before the app's
// setup, and Apache answers 503 until the new PHP-FPM pool's socket is up (Plan 6 C6
// D1). It doesn't fail: a site still down is left to the POST that follows to report.
func (h *Host) WaitSite(ctx context.Context, rawurl, resolve string) error {
	u, err := url.Parse(rawurl)
	if err != nil || u.Host == "" {
		return nil
	}
	front := u.Scheme + "://" + u.Host + "/"
	client := h.client(resolve)
	for i := 0; i < siteWaitTries; i++ {
		if err := jobs.Interrupted(ctx); err != nil {
			return err
		}
		if i > 0 {
			if err := h.Pause(ctx, time.Second); err != nil {
				return err
			}
		}
		if siteUp(ctx, client, front) {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

// siteUp makes one GET of front and says whether it answered below 500.
func siteUp(ctx context.Context, client *http.Client, front string) bool {
	ctx, cancel := context.WithTimeout(ctx, siteWaitTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, front, nil)
	if err != nil {
		return false
	}
	res, err := client.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	_ = res.Body.Close()
	return res.StatusCode < 500
}

// client is the HTTP client SendPost and WaitSite use: at most 3 redirects, resolve
// applied to every dial.
func (h *Host) client(resolve string) *http.Client {
	dial := h.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 30 * time.Second}).DialContext
	}
	if parts := strings.SplitN(resolve, ":", 3); len(parts) == 3 {
		inner, from, to := dial, net.JoinHostPort(parts[0], parts[1]), net.JoinHostPort(parts[2], parts[1])
		dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if strings.EqualFold(addr, from) {
				addr = to
			}
			return inner(ctx, network, addr)
		}
	}
	return &http.Client{
		Transport: &http.Transport{DialContext: dial, TLSClientConfig: h.TLSConfig, DisableKeepAlives: true},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) > 3 {
				return errors.New("Maximum (3) redirects followed")
			}
			return nil
		},
	}
}

// transportFailure is a request error as curl_error reported it, without the URL.
func transportFailure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return Fail("%s", err.Error())
}

// Pause sleeps for d, or until ctx ends.
func (h *Host) Pause(ctx context.Context, d time.Duration) error {
	if err := jobs.Interrupted(ctx); err != nil {
		return err
	}
	if h.Sleep != nil {
		return h.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// phpBasename and phpDirname ignore trailing slashes, as PHP's do.
func phpBasename(p string) string {
	p = strings.TrimRight(p, "/")
	return p[strings.LastIndex(p, "/")+1:]
}

func phpDirname(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i > 0 {
		return strings.TrimRight(p[:i], "/")
	} else if i == 0 {
		return "/"
	}
	return "."
}

var slashes = regexp.MustCompile(`/+`)

// joinPaths is Util::joinPaths: empty parts dropped, runs of "/" collapsed.
func joinPaths(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if p != "" {
			keep = append(keep, p)
		}
	}
	return slashes.ReplaceAllString(strings.Join(keep, "/"), "/")
}
