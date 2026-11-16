package webapp

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

//go:embed files/*
var embedded embed.FS

// embeddedFile is an installer's bundled file (its .htaccess).
func embeddedFile(name string) string {
	b, err := embedded.ReadFile("files/" + name)
	if err != nil {
		panic(err) // a build error: the file is bundled
	}
	return string(b)
}

// Info is an installer's card.
type Info struct {
	ID       string // lowercased directory name: wordpress, osticket
	Name     string
	Group    string
	Version  string
	Database bool
}

// Option is one select option.
type Option struct{ Value, Label string }

// Field is one installer option. Type is text, password, select or boolean.
type Field struct {
	Key, Type, Value, Placeholder string
	Options                       []Option
}

// Label is PHP's default: ucwords of the key with "." and "_" turned into spaces.
func (f Field) Label() string {
	words := strings.Split(strings.NewReplacer(".", " ", "_", " ").Replace(f.Key), " ")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// Resource is what an install fetches first: Kind is composer, wp or archive.
type Resource struct{ Kind, Src, Dst string }

// Config is an installer's server side: its resource, nginx template and the PHP
// versions it supports.
type Config struct {
	Resource Resource
	Template string
	PHP      []string
}

// Database is the install's database: names already carry the "<user>_" prefix.
type Database struct {
	Host, Name, User, Password string
	Create                     bool
}

// Target is the domain an app is installed on.
type Target struct {
	Domain string
	Home   string // the user's home directory
	IP     string
	SSL    bool
	DB     Database
}

// DocRoot is public_html with p appended (Util::joinPaths).
func (t *Target) DocRoot(p ...string) string {
	return joinPaths(append([]string{t.Home, "web", t.Domain, "public_html"}, p...)...)
}

func (t *Target) scheme() string {
	if t.SSL {
		return "https://"
	}
	return "http://"
}

// URL is the site's address.
func (t *Target) URL() string { return t.scheme() + t.Domain }

// Resolve is curl's CURLOPT_RESOLVE entry for the site.
func (t *Target) Resolve() string {
	port := "80"
	if t.SSL {
		port = "443"
	}
	return t.Domain + ":" + port + ":" + t.IP
}

// Installer is one quick-install app.
type Installer interface {
	Info() Info
	Form() []Field
	Config() Config
	// Setup is setupApplication: it runs after the files, database and templates are in
	// place. opts holds the form values plus php_version.
	Setup(ctx context.Context, t *Target, opts map[string]string, h *Host) error
}

type app struct {
	info  Info
	form  []Field
	cfg   Config
	setup func(x *Seq, t *Target, o map[string]string) error
}

func (a *app) Info() Info     { return a.info }
func (a *app) Config() Config { return a.cfg }

// Form returns the fields. A select without a default starts on its first option, as a
// browser shows it.
func (a *app) Form() []Field {
	out := make([]Field, len(a.form))
	for i, f := range a.form {
		if f.Type == "select" && f.Value == "" && len(f.Options) > 0 {
			f.Value = f.Options[0].Value
		}
		out[i] = f
	}
	return out
}

func (a *app) Setup(ctx context.Context, t *Target, o map[string]string, h *Host) error {
	if a.setup == nil {
		return nil
	}
	x := &Seq{h: h, ctx: ctx}
	if err := a.setup(x, t, o); err != nil {
		return err
	}
	return x.err
}

// Seq runs host calls in order and stops at the first failure: after one, every call
// is a no-op and returns zero values.
type Seq struct {
	h      *Host
	ctx    context.Context
	err    error
	waited bool // the site answered (or was waited for) before the first Post
}

func (x *Seq) set(err error) {
	if x.err == nil {
		x.err = err
	}
}

func (x *Seq) Err() error { return x.err }

func (x *Seq) AddDir(p string) {
	if x.err == nil {
		x.set(x.h.AddDirectory(x.ctx, p))
	}
}
func (x *Seq) CopyDir(from, to string) {
	if x.err == nil {
		x.set(x.h.CopyDirectory(x.ctx, from, to))
	}
}
func (x *Seq) DeleteDir(p string) {
	if x.err == nil {
		x.set(x.h.DeleteDirectory(x.ctx, p))
	}
}
func (x *Seq) Read(p string) string {
	if x.err != nil {
		return ""
	}
	s, err := x.h.ReadFile(x.ctx, p)
	x.set(err)
	return s
}
func (x *Seq) Create(p, contents string) {
	if x.err == nil {
		x.set(x.h.CreateFile(x.ctx, p, contents))
	}
}
func (x *Seq) Move(from, to string) {
	if x.err == nil {
		x.set(x.h.MoveFile(x.ctx, from, to))
	}
}
func (x *Seq) Chmod(p, perm string) {
	if x.err == nil {
		x.set(x.h.ChangeFilePermissions(x.ctx, p, perm))
	}
}
func (x *Seq) Delete(p string) {
	if x.err == nil {
		x.set(x.h.DeleteFile(x.ctx, p))
	}
}
func (x *Seq) Extract(file, dir string) {
	if x.err == nil {
		x.set(x.h.ArchiveExtract(x.ctx, file, dir))
	}
}
func (x *Seq) Composer(php string, args ...string) {
	if x.err == nil {
		x.set(x.h.RunComposer(x.ctx, php, args...))
	}
}
func (x *Seq) Wp(php string, args ...string) {
	if x.err == nil {
		x.set(x.h.RunWp(x.ctx, php, args...))
	}
}
func (x *Seq) PHP(php, command string, args ...string) {
	if x.err == nil {
		x.set(x.h.RunPHP(x.ctx, php, command, args...))
	}
}

// Post is SendPost; the first one waits for the site to come up (WaitSite).
func (x *Seq) Post(rawurl string, form Form, headers []string, resolve string) {
	if x.err == nil && !x.waited {
		x.waited = true
		x.set(x.h.WaitSite(x.ctx, rawurl, resolve))
	}
	if x.err == nil {
		x.set(x.h.SendPost(x.ctx, rawurl, form, headers, resolve))
	}
}
func (x *Seq) Pause(d int) {
	if x.err == nil {
		x.set(x.h.Pause(x.ctx, secs(d)))
	}
}

// GlobDir finds the first directory in dir whose name starts with prefix (PHP globbed
// "<prefix>*/") and returns its path with a trailing slash.
func (x *Seq) GlobDir(dir, prefix string) string {
	if x.err != nil {
		return ""
	}
	files, err := x.h.listEntries(x.ctx, dir, "-A", "-p")
	if err != nil {
		x.set(err)
		return ""
	}
	sort.Strings(files)
	for _, f := range files {
		if strings.HasSuffix(f, "/") && strings.HasPrefix(f, prefix) {
			return joinPaths(dir, strings.TrimSuffix(f, "/")) + "/"
		}
	}
	x.set(Fail(`Failed to extract "%s"`, prefix+"*"))
	return ""
}

// randomString is Util::generateString(n, false): letters and digits.
func randomString(n int) string {
	const chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

// RandomHex is n random bytes as hex (PHP's bin2hex(random_bytes(n))).
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// DefaultDatabaseName is the generated name for both the database and its user:
// the lowercased app name with spaces as "_", then "_" and 10 hex characters.
func DefaultDatabaseName(appName string) string {
	return strings.ReplaceAll(strings.ToLower(appName), " ", "_") + "_" + RandomHex(5)
}

// DecodeVersions reads v-list-sys-php's JSON list.
func DecodeVersions(out string) ([]string, error) {
	var v []string
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return nil, Fail("Failed to load installed PHP versions")
	}
	return v, nil
}

// SupportedPHP is the installed versions the installer supports, lowest first.
func SupportedPHP(installed, supported []string) []string {
	out := []string{}
	for _, v := range installed {
		for _, s := range supported {
			if v == s {
				out = append(out, v)
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return versionLess(out[i], out[j]) })
	return out
}

func versionLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, _ := strconv.Atoi(as[i])
		y, _ := strconv.Atoi(bs[i])
		if x != y {
			return x < y
		}
	}
	return len(as) < len(bs)
}

// catalogue is the installers in Hestia's order: the glob order of the installer
// directories, which puts osTicket last.
var catalogue = []*app{
	castopod, classicpress, concretecms, dokuwiki, dolibarr, drupal, flarum, grav, joomla,
	laravel, mediawiki, namelessmc, nextcloud, opencart, prestashop, qloapps, shopware,
	symfony, thirtybees, vvveb, wordpress, osticket,
}

// All returns the installers in catalogue order.
func All() []Installer {
	out := make([]Installer, len(catalogue))
	for i, a := range catalogue {
		out[i] = a
	}
	return out
}

// Get finds an installer by its id (the lowercased directory name).
func Get(id string) (Installer, bool) {
	for _, a := range catalogue {
		if a.info.ID == id {
			return a, true
		}
	}
	return nil, false
}

// form-building helpers for the installer files

func text(key, def string) Field { return Field{Key: key, Type: "text", Value: def} }
func password(key string) Field  { return Field{Key: key, Type: "password"} }
func opt(value, label string) Option {
	return Option{Value: value, Label: label}
}

// list makes the options of a PHP list: value and label are the same text.
func list(vals ...string) []Option {
	out := make([]Option, len(vals))
	for i, v := range vals {
		out[i] = Option{Value: v, Label: v}
	}
	return out
}

func sel(key, def string, opts []Option) Field {
	return Field{Key: key, Type: "select", Value: def, Options: opts}
}
