package httpapi

// Info, status, graphs, preview (Slice C, Task 4). Updates and white label are in
// sys_updates.go.

import (
	"context"
	"html"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/hostisan/hostisan/internal/brand"
	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
)

// infoRoutes mounts the info, status, graph, update, white label and preview routes.
func (sys *sysAPI) infoRoutes(m *sysMux) {
	m.handle("GET /api/v1/server/status/{kind}", sys.read(sys.statusText))
	m.handle("GET /api/v1/server/graphs", sys.read(sys.graphCatalog))
	m.handle("GET /api/v1/server/graphs/{chart}", sys.read(sys.graphData))
	m.handle("GET /api/v1/server/preview", sys.read(sys.previewFeatures))
	sys.updatesRoutes(m)
}

// statusMaxBytes caps a status dump (decision 21).
const statusMaxBytes = 1 << 20

// statusKinds maps the {kind} path value to its command and, for the kinds that
// depend on installed software, the hestia.conf key that must be set.
var statusKinds = map[string]struct{ cmd, system string }{
	"cpu":  {"v-list-sys-cpu-status", ""},
	"mem":  {"v-list-sys-memory-status", ""},
	"disk": {"v-list-sys-disk-status", ""},
	"net":  {"v-list-sys-network-status", ""},
	"web":  {"v-list-sys-web-status", "WEB_SYSTEM"},
	"dns":  {"v-list-sys-dns-status", "DNS_SYSTEM"},
	"mail": {"v-list-sys-mail-status", "MAIL_SYSTEM"},
	"db":   {"v-list-sys-db-status", "DB_SYSTEM"},
}

var (
	htmlBreaks = regexp.MustCompile(`(?i)<br\s*/?>|</(h[1-6]|dt|dd|dl|tr|p|div|table)>`)
	htmlTags   = regexp.MustCompile(`<[^>]*>`)
	htmlSkip   = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`),
		regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style\s*>`),
	}
	blankRuns = regexp.MustCompile(`\n{3,}`)
)

// stripHTML turns the Apache/nginx status page into text: script and style bodies
// go, block ends become line
// breaks, tags go, entities are decoded.
func stripHTML(s string) string {
	for _, re := range htmlSkip {
		s = re.ReplaceAllString(s, "")
	}
	s = htmlBreaks.ReplaceAllString(s, "\n")
	s = htmlTags.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return blankRuns.ReplaceAllString(s, "\n\n")
}

// truncateText cuts s to n bytes on a character boundary.
func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Cut at n, then step back at most UTFMax-1 bytes to a character start; bytes that
	// are invalid elsewhere in s stay as they are.
	cut := n
	for i := 0; i < utf8.UTFMax && cut > 0 && !utf8.RuneStart(s[cut]); i++ {
		cut--
	}
	return s[:cut]
}

// installed reports whether a hestia.conf system key names installed software.
func installed(v string) bool { return v != "" && v != "no" }

// sysIsRHEL reports whether v-list-sys-info's OS is a RHEL-family server: the script
// prints CentOS or Amazon when /etc/sysconfig exists.
func sysIsRHEL(os string) bool { return os == "CentOS" || os == "Amazon" }

// statusText is GET /api/v1/server/status/{kind}.
func (sys *sysAPI) statusText(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	k, ok := statusKinds[kind]
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	if k.system != "" {
		cfg, err := sys.H.Config(r.Context())
		if err != nil {
			serverError(w, err)
			return
		}
		if !installed(cfg[k.system]) {
			WriteError(w, http.StatusNotFound, "not_found", "")
			return
		}
	}
	if kind == "dns" {
		// On RHEL the script rewrites named.conf and restarts named: hidden until Plan 6.
		out, err := sys.sysRead(r.Context(), "v-list-sys-info", "json")
		if err != nil {
			writeCommandError(w, err)
			return
		}
		info, err := hestia.DecodeSysInfo(out)
		if err != nil {
			serverError(w, err)
			return
		}
		if sysIsRHEL(info.OS) {
			WriteError(w, http.StatusNotFound, "not_found", "")
			return
		}
	}
	res, err := sys.R.Run(runner.WithTimeout(r.Context(), sysTimeoutStatus), k.cmd)
	if err != nil {
		writeCommandError(w, err)
		return
	}
	text := string(res.Stdout)
	if res.Exit != 0 {
		// The command's own message instead of a blank page (decision 21).
		text = commandMessage(res.Err(k.cmd).(*runner.CommandError))
	}
	if kind == "web" {
		text = stripHTML(text)
	}
	writeJSON(w, http.StatusOK, StatusText{Kind: kind, Text: truncateText(brand.Rewrite(text), statusMaxBytes)})
}

// chartIDOf is the catalogue id of a v-list-sys-rrd entry (decision 22).
func chartIDOf(c hestia.RrdChart) string {
	if c.TYPE == "net" {
		return "net_" + c.RRD
	}
	return c.RRD
}

// charts lists v-list-sys-rrd json in file order, skipping ids that fail
// ValidChartID (they could never be loaded).
func (sys *sysAPI) charts(ctx context.Context) ([]GraphChart, error) {
	res, err := sys.R.Run(runner.WithTimeout(ctx, sysTimeoutDefault), "v-list-sys-rrd", "json")
	if err == nil {
		err = res.Err("v-list-sys-rrd")
	}
	if err != nil {
		return nil, err
	}
	list, err := hestia.DecodeObjects[hestia.RrdChart](res.Stdout)
	if err != nil {
		return nil, err
	}
	out := make([]GraphChart, 0, len(list))
	for _, c := range list {
		if id := chartIDOf(c); hestia.ValidChartID(id) {
			out = append(out, GraphChart{ID: id, Type: c.TYPE, Title: brand.Rewrite(c.TITLE)})
		}
	}
	return out, nil
}

// graphCatalog is GET /api/v1/server/graphs. A failed or unreadable listing is an
// empty list plus the error (SYS gotcha 10).
func (sys *sysAPI) graphCatalog(w http.ResponseWriter, r *http.Request) {
	charts, err := sys.charts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, GraphCatalog{Charts: []GraphChart{}, Error: errorMessage(err)})
		return
	}
	writeJSON(w, http.StatusOK, GraphCatalog{Charts: charts})
}

var graphPeriods = []string{"daily", "weekly", "monthly", "yearly", "biennially", "triennially"}

// graphUnit is the y-axis label of a chart (decision 23).
func graphUnit(id string) string {
	switch {
	case id == "la":
		return "Load"
	case id == "mem":
		return "MB"
	case strings.HasPrefix(id, "net_"):
		return "Bytes/s"
	case id == "mail":
		return "Emails in queue"
	case strings.HasPrefix(id, "mysql_"), strings.HasPrefix(id, "pgsql_"):
		return "Queries/s"
	}
	return "Connections" // nginx, apache2, httpd, ftp, ssh
}

// graphData is GET /api/v1/server/graphs/{chart}?period=. A malformed id is 400
// before any command; an id missing from the catalogue is 400 after listing it.
func (sys *sysAPI) graphData(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("chart")
	if !hestia.ValidChartID(id) {
		invalidFormat(w, "chart", id)
		return
	}
	charts, err := sys.charts(r.Context())
	if err != nil {
		writeCommandError(w, err)
		return
	}
	found := false
	for _, c := range charts {
		found = found || c.ID == id
	}
	if !found {
		invalidFormat(w, "chart", id)
		return
	}
	period := r.URL.Query().Get("period")
	if !contains(graphPeriods, period) {
		period = "daily"
	}
	res, err := sys.R.Run(runner.WithTimeout(r.Context(), sysTimeoutDefault), "v-export-rrd", id, period)
	if err == nil {
		err = res.Err("v-export-rrd")
	}
	if err != nil {
		writeCommandError(w, err)
		return
	}
	exp, err := hestia.DecodeRrdExport(res.Stdout)
	if err != nil {
		serverError(w, err)
		return
	}
	legend := exp.Meta.Legend
	if legend == nil {
		legend = []string{}
	}
	series := exp.Data
	if series == nil {
		series = [][]*float64{}
	}
	if id == "la" {
		// Load is stored times 100; Processes stay counts.
		for ci, name := range legend {
			if name != "Load" {
				continue
			}
			for _, row := range series {
				if ci < len(row) && row[ci] != nil {
					v := *row[ci] / 100
					row[ci] = &v
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, GraphData{ID: id, Period: period, Start: exp.Meta.Start, Step: exp.Meta.Step,
		Unit: graphUnit(id), Legend: legend, Series: series})
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// previewFeatures is GET /api/v1/server/preview: the one static row (decision 49).
func (sys *sysAPI) previewFeatures(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, PreviewFeatures{Rows: []PreviewFeature{{
		Category: "System",
		Name:     "Policy: Allow suspended users to log in with read-only access",
		Status:   "Partially implemented",
	}}})
}
