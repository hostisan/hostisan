package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html"
	"log/slog"
	"net/http"
	"net/mail"
	"net/netip"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/hostisan/hostisan/internal/brand"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/mailer"
	"github.com/hostisan/hostisan/internal/secret"
	"github.com/hostisan/hostisan/internal/session"
)

func (s *server) dbRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/db", s.read(s.dbList))
	// Go's mux prefers the literal /options over the {database} wildcard.
	mux.Handle("GET /api/v1/db/options", s.read(s.dbOptions))
	mux.Handle("GET /api/v1/db/{database}", s.read(s.dbOne))
	mux.Handle("GET /api/v1/db/{database}/download", s.read(s.dbDownload))
	mux.Handle("GET /api/v1/db/{database}/phpmyadmin", s.read(s.dbPhpMyAdmin))
	mux.Handle("POST /api/v1/db", s.write(s.dbAdd))
	mux.Handle("PUT /api/v1/db/{database}", s.write(s.dbEdit))
	mux.Handle("DELETE /api/v1/db/{database}", s.write(s.dbAction("v-delete-database")))
	mux.Handle("POST /api/v1/db/{database}/suspend", s.adminWrite(s.dbAction("v-suspend-database")))
	mux.Handle("POST /api/v1/db/{database}/unsuspend", s.adminWrite(s.dbAction("v-unsuspend-database")))
	mux.Handle("POST /api/v1/db/bulk", s.write(s.dbBulk))
}

// databaseParam is the {database} path value; it answers 400 itself when unusable.
func databaseParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	d := r.PathValue("database")
	if !hestia.ValidDatabaseArg(d) {
		invalidFormat(w, "database", d)
		return "", false
	}
	return d, true
}

func (s *server) dbList(w http.ResponseWriter, r *http.Request) {
	user := s.actingUser(r)
	out, ok := s.output(w, r, "v-list-databases", user, "json")
	if !ok {
		return
	}
	dbs, err := hestia.DecodeObjects[hestia.Database](out)
	if err != nil {
		serverError(w, err)
		return
	}
	u, ok, err := s.H.User(r.Context(), user)
	if err != nil || !ok {
		serverError(w, firstErr(err, "db list: user "+user+" not found"))
		return
	}
	writeJSON(w, http.StatusOK, Databases{
		Databases: dbs,
		Usage:     DBUsage{Databases: u["U_DATABASES"], DatabasesLimit: u["DATABASES"], Suspended: u["SUSPENDED_DB"]},
	})
}

func (s *server) dbOne(w http.ResponseWriter, r *http.Request) {
	name, ok := databaseParam(w, r)
	if !ok {
		return
	}
	out, ok := s.output(w, r, "v-list-database", s.actingUser(r), name, "json")
	if !ok {
		return
	}
	dbs, err := hestia.DecodeObjects[hestia.Database](out)
	if err != nil || len(dbs) != 1 {
		serverError(w, firstErr(err, "v-list-database: unexpected output"))
		return
	}
	writeJSON(w, http.StatusOK, dbs[0])
}

// mysqlCharsets is Hestia's static MySQL charset list (checklist section 2.1), in order.
var mysqlCharsets = []string{
	"big5", "dec8", "cp850", "hp8", "koi8r", "latin1", "latin2", "swe7", "ascii", "ujis", "sjis",
	"hebrew", "tis620", "euckr", "koi8u", "gb2312", "greek", "cp1250", "gbk", "latin5", "armscii8",
	"utf8", "utf8mb4", "ucs2", "cp866", "keybcs2", "macce", "macroman", "cp852", "latin7", "cp1251",
	"cp1256", "cp1257", "binary", "geostd8", "cp932", "eucjpms",
}

// maxDBUserLength is the longest database user name, prefix included.
const maxDBUserLength = 32

// splitList splits a comma-separated config value, trimming blanks.
func splitList(v string) []string {
	out := []string{}
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *server) dbOptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := s.actingUser(r)
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	u, ok, err := s.H.User(ctx, user)
	if err != nil || !ok {
		serverError(w, firstErr(err, "db options: user "+user+" not found"))
		return
	}
	var rows []hestia.DatabaseHost
	if !s.listJSON(w, ctx, &rows, "v-list-database-hosts", "json") {
		return
	}
	hosts := make([]DBHostOption, 0, len(rows))
	for _, h := range rows {
		hosts = append(hosts, DBHostOption{Host: h.HOST, Type: h.TYPE, Charsets: splitList(h.CHARSETS)})
	}

	types := splitList(cfg["DB_SYSTEM"])
	host := safeRequestHost(r)
	_, perr := netip.ParseAddr(host)
	isIP := perr == nil
	adminURL := func(typ, alias, def string) string {
		if host == "" {
			return ""
		}
		for _, t := range types {
			if t == typ {
				if alias == "" {
					alias = def
				}
				return "//" + host + "/" + alias + "/"
			}
		}
		return ""
	}

	// Decision 29 (D13): tell an admin why phpMyAdmin SSO, which signs in through the
	// API from this server, can't work with the API settings. Users see nothing.
	// Only where the SSO link is offered (SSOEnabled: a key and a hostname). The
	// server-IP case lists v-list-sys-ips on each load of the options; it runs only
	// for admins with a restricted API_ALLOWED_IP, and the page loads it once.
	ssoHint := ""
	ssoEnabled := cfg["PHPMYADMIN_KEY"] != "" && host != "" && !isIP
	if session.FromContext(ctx).UserContext == "admin" && ssoEnabled {
		allowed := map[string]bool{}
		for _, ip := range strings.Split(cfg["API_ALLOWED_IP"], ",") {
			allowed[strings.TrimSpace(ip)] = true
		}
		switch {
		case cfg["API_SYSTEM"] == "" || cfg["API_SYSTEM"] == "0":
			ssoHint = "phpMyAdmin single sign-on needs the API. Turn it on in the server configuration."
		case cfg["API_ALLOWED_IP"] == "allow-all" || allowed["127.0.0.1"] || allowed["0.0.0.0"]:
		default:
			var ips []hestia.SysIP
			res, err := s.R.Run(ctx, "v-list-sys-ips", "json")
			if err == nil {
				err = res.Err("v-list-sys-ips")
			}
			if err == nil {
				ips, err = hestia.DecodeObjects[hestia.SysIP](res.Stdout)
			}
			if err != nil {
				slog.Warn("db options: server IPs", "err", err)
				break
			}
			ssoHint = "phpMyAdmin single sign-on needs this server's IP address in the API allowed IP list."
			for _, ip := range ips {
				if allowed[ip.IP] || (ip.NAT != "" && allowed[ip.NAT]) {
					ssoHint = ""
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, DBOptions{
		Types:          types,
		Hosts:          hosts,
		MySQLCharsets:  mysqlCharsets,
		DefaultCharset: map[string]string{"mysql": "utf8mb4", "pgsql": "UTF8"},
		Prefix:         user + "_",
		MaxUserLength:  maxDBUserLength,
		PHPMyAdminURL:  adminURL("mysql", cfg["DB_PMA_ALIAS"], "phpmyadmin"),
		PHPPgAdminURL:  adminURL("pgsql", cfg["DB_PGA_ALIAS"], "phppgadmin"),
		SSOEnabled:     ssoEnabled,
		AdminGate:      u.Role() == "admin",
		SSOHint:        ssoHint,
	})
}

const (
	passwordRuleMsg = "Password does not match the minimum requirements."
	invalidEmailMsg = "Please enter a valid email address."
	credentialsBody = "Database has been created.\n\nDatabase: {{database}}\nUsername: {{username}}\nPassword: {{password}}\nSQL Manager: {{dbadmin}}\n\nBest regards,\n\n--\n{{appname}}"
)

func (s *server) tmpDir() string {
	if s.TmpDir == "" {
		return "/tmp"
	}
	return s.TmpDir
}

// phpEmpty is PHP's empty() on a string: "" and "0" both count as empty.
func phpEmpty(v string) string {
	if v == "0" {
		return ""
	}
	return v
}

// validEmail approximates PHP's FILTER_VALIDATE_EMAIL: a bare address whose domain
// has a dot.
func validEmail(e string) bool {
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e || strings.ContainsAny(e, "<>\r\n") {
		return false
	}
	at := strings.LastIndex(e, "@")
	return at > 0 && strings.Contains(e[at+1:], ".")
}

// withSecret writes password to a private temp file, runs the command with the path
// put at args[at], and removes the file again whether the command worked or not.
func (s *server) withSecret(r *http.Request, password string, cmd string, args []string, at int) error {
	path, cleanup, err := secret.TempFile(s.tmpDir(), password)
	if err != nil {
		return err
	}
	defer cleanup()
	args = append(append([]string(nil), args[:at]...), append([]string{path}, args[at:]...)...)
	return s.run(r.Context(), cmd, args...)
}

// dbAdminLink is the database manager's name and address for a new database; the link
// is "" when no safe host is known.
func dbAdminLink(cfg hestia.SysConfig, typ, dbHost, requestHost string) (name, link string) {
	var alias, def string
	switch typ {
	case "mysql":
		name, alias, def = "phpMyAdmin", cfg["DB_PMA_ALIAS"], "phpmyadmin/"
	case "pgsql":
		name, alias, def = "phpPgAdmin", cfg["DB_PGA_ALIAS"], "phppgadmin/"
	default:
		return "", ""
	}
	host := requestHost
	if dbHost != "localhost" {
		host = dbHost
	}
	if host == "" {
		return name, ""
	}
	if alias != "" {
		return name, "https://" + host + "/" + alias
	}
	return name, "https://" + host + "/" + def
}

func (s *server) dbAdd(w http.ResponseWriter, r *http.Request) {
	var req AddDBRequest
	if !decodeBody(w, r, &req) {
		return
	}
	name, dbuser := strings.TrimSpace(req.Database), strings.TrimSpace(req.DBUser)
	if !blankFields(w, "Database", phpEmpty(name), "Username", phpEmpty(dbuser), "Password", phpEmpty(req.Password),
		"Type", phpEmpty(req.Type), "Host", phpEmpty(req.Host), "Charset", phpEmpty(req.Charset)) {
		return
	}
	email := strings.TrimSpace(req.Email)
	if email != "" && !validEmail(email) {
		WriteError(w, http.StatusBadRequest, "invalid", invalidEmailMsg)
		return
	}
	if !hestia.ValidPassword(req.Password) {
		WriteError(w, http.StatusBadRequest, "invalid", passwordRuleMsg)
		return
	}
	ctx := r.Context()
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	user := s.actingUser(r)
	err = s.withSecret(r, req.Password, "v-add-database", []string{user, name, dbuser, req.Type, req.Host, req.Charset}, 3)
	if err != nil {
		writeCommandError(w, err)
		return
	}
	full, fullUser := user+"_"+name, user+"_"+dbuser
	if req.Type == "pgsql" { // v-add-database lowercases both for PostgreSQL
		full, fullUser = strings.ToLower(full), strings.ToLower(fullUser)
	}
	dbAdmin, link := dbAdminLink(cfg, req.Type, req.Host, safeRequestHost(r))
	if email != "" {
		s.sendCredentials(ctx, cfg, email, full, fullUser, req.Password, dbAdmin, link)
	}
	writeJSON(w, http.StatusOK, AddDBReply{Status: "ok", Database: full, DBAdmin: dbAdmin, DBAdminURL: link})
}

// sendCredentials emails the new database's credentials (Parity decision 5). A failure
// is logged, never reported to the client, and never includes the password.
func (s *server) sendCredentials(ctx context.Context, cfg hestia.SysConfig, to, database, dbuser, password, dbAdmin, link string) {
	if s.Mailer == nil {
		slog.Warn("database credentials email not sent: no mailer configured", "database", database)
		return
	}
	lang := firstNonEmpty(session.FromContext(ctx).Language, cfg["LANGUAGE"], "en")
	hostname := s.Sessions.SystemHostname
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	app := brand.Rewrite(cfg["APP_NAME"])
	var subject, body string
	var found bool
	if s.EmailTemplates != nil {
		subject, body, found = s.EmailTemplates.Load(lang, "database_credentials")
	}
	if !found {
		body = credentialsBody
		if msgs, err := s.catalogs.Messages(lang); err == nil && msgs[body] != "" {
			body = msgs[body]
		}
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		wrap := cfg["SUBJECT_EMAIL"]
		if wrap == "" {
			wrap = "{{subject}}"
		}
		subject = strings.NewReplacer("{{subject}}", "Database Credentials: "+database).Replace(wrap)
	}
	// "{{dabase}}" is Hestia's own typo, kept so existing templates keep working.
	subject = strings.NewReplacer("{{hostname}}", hostname, "{{appname}}", app,
		"{{dabase}}", database, "{{dbuser}}", dbuser).Replace(subject)
	body = strings.NewReplacer("{{database}}", html.EscapeString(database), "{{username}}", html.EscapeString(dbuser),
		"{{password}}", html.EscapeString(password), "{{dbadmin}}", link, "{{appname}}", app).Replace(body)
	if err := s.Mailer.Send(ctx, cfg, mailer.Message{To: to, Subject: subject, Body: body}); err != nil {
		slog.Warn("database credentials email failed", "database", database, "to", to, "err", err)
	}
}

func (s *server) dbEdit(w http.ResponseWriter, r *http.Request) {
	name, ok := databaseParam(w, r)
	if !ok {
		return
	}
	var req EditDBRequest
	if !decodeBody(w, r, &req) {
		return
	}
	dbuser := strings.TrimSpace(req.DBUser)
	if !blankFields(w, "Username", dbuser) {
		return
	}
	if req.Password != "" && !hestia.ValidPassword(req.Password) {
		WriteError(w, http.StatusBadRequest, "invalid", passwordRuleMsg)
		return
	}
	user := s.actingUser(r)
	out, ok := s.output(w, r, "v-list-database", user, name, "json")
	if !ok {
		return
	}
	dbs, err := hestia.DecodeObjects[hestia.Database](out)
	if err != nil || len(dbs) != 1 {
		serverError(w, firstErr(err, "v-list-database: unexpected output"))
		return
	}
	if dbuser != strings.TrimPrefix(dbs[0].DBUSER, user+"_") {
		if !s.steps(w, r, [][]string{{"v-change-database-user", user, name, dbuser}}) {
			return
		}
	}
	if req.Password != "" {
		if err := s.withSecret(r, req.Password, "v-change-database-password", []string{user, name}, 2); err != nil {
			writeCommandError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

// dbAction is delete, suspend or unsuspend: one command on the database.
func (s *server) dbAction(cmd string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, ok := databaseParam(w, r)
		if !ok {
			return
		}
		if s.steps(w, r, [][]string{{cmd, s.actingUser(r), name}}) {
			writeJSON(w, http.StatusOK, OK{Status: "ok"})
		}
	}
}

// Bulk actions: admins get every action, users delete only (Parity decision 1).
var dbBulkCmds = map[string]string{
	"rebuild": "v-rebuild-database", "suspend": "v-suspend-database",
	"unsuspend": "v-unsuspend-database", "delete": "v-delete-database",
}

func (s *server) dbBulk(w http.ResponseWriter, r *http.Request) {
	s.bulk(w, r, dbBulkCmds, hestia.ValidDatabaseArg, func(user, n string) []string { return []string{user, n} }, nil)
}

// dbDownload dumps the database and hands the file to hestia-nginx, which serves it from
// its internal /__accel/backup/ location (Parity decision 6). The body stays empty.
func (s *server) dbDownload(w http.ResponseWriter, r *http.Request) {
	if !sameOriginGET(w, r) {
		return
	}
	name, ok := databaseParam(w, r)
	if !ok {
		return
	}
	out, ok := s.output(w, r, "v-dump-database", s.actingUser(r), name, "file", "gzip")
	if !ok {
		return
	}
	file, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	file = strings.TrimSpace(file)
	if !path.IsAbs(file) {
		serverError(w, errors.New("v-dump-database: unexpected output"))
		return
	}
	if !s.accelRedirect(w, r, file) {
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/sql")
	h.Set("Content-Disposition", "attachment; filename="+strconv.Quote(path.Base(file)))
	w.WriteHeader(http.StatusOK)
}

// phpMyAdminToken is the single sign-on token hestia-sso.php checks with hash_equals:
// hex(HMAC-SHA256(PHPMYADMIN_KEY, database|user|ip|exp)) (SSO4). Hestia's bcrypt token
// read only 72 bytes of its input, so a long enough prefix left the key out.
func phpMyAdminToken(key, database, user, ip, exp string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(database + "|" + user + "|" + ip + "|" + exp))
	return hex.EncodeToString(mac.Sum(nil))
}

// dbPhpMyAdmin redirects to Hestia's hestia-sso.php with a phpMyAdminToken (Parity
// decision 7). The link is built at click time because it is only good for 60 seconds.
func (s *server) dbPhpMyAdmin(w http.ResponseWriter, r *http.Request) {
	name, ok := databaseParam(w, r)
	if !ok {
		return
	}
	host := safeRequestHost(r)
	if _, err := netip.ParseAddr(host); host == "" || err == nil {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	ctx := r.Context()
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	user := s.actingUser(r)
	// phpMyAdmin signs in with full read-write rights, so a read-only session gets no
	// link (PHP hid the button when $read_only). RequireWritable skips GET, so check here.
	u, _, err := s.H.User(ctx, user)
	if err != nil {
		serverError(w, err)
		return
	}
	if session.ReadOnly(session.FromContext(ctx), u, cfg) {
		WriteError(w, http.StatusForbidden, "read_only", "")
		return
	}
	key := cfg["PHPMYADMIN_KEY"]
	if key == "" {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	out, ok := s.output(w, r, "v-list-database", user, name, "json")
	if !ok {
		return
	}
	dbs, err := hestia.DecodeObjects[hestia.Database](out)
	if err != nil || len(dbs) != 1 {
		serverError(w, firstErr(err, "v-list-database: unexpected output"))
		return
	}
	if dbs[0].TYPE != "mysql" {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	exp := strconv.FormatInt(now.Unix(), 10)
	token := phpMyAdminToken(key, name, user, session.FromContext(r.Context()).CombinedIP, exp)
	alias := firstNonEmpty(cfg["DB_PMA_ALIAS"], "phpmyadmin")
	query := "host=" + url.QueryEscape(dbs[0].HOST) + "&database=" + url.QueryEscape(name) +
		"&user=" + url.QueryEscape(user) + "&exp=" + exp + "&hestia_token=" + token
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "//"+host+"/"+alias+"/hestia-sso.php?"+query, http.StatusFound)
}
