package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/runner"
	"github.com/hostisan/hostisan/internal/session"
)

const (
	// searchTimeout stays under the server's 90 s WriteTimeout (Global Constraints).
	searchTimeout = 80 * time.Second
	// searchMaxZones caps the record listings one search runs (parity decision 5).
	searchMaxZones = 20
	// searchZoneTimeout caps one record listing; all of them share the request's
	// searchTimeout deadline.
	searchZoneTimeout = 10 * time.Second

	searchInvalidMsg = "Invalid search query"
	searchFailedMsg  = "The search stopped on an object it could not read. Try a more specific search."
	// searchDemoText is check_hestia_demo_mode's refusal (func/main.sh).
	searchDemoText = "Unable to perform operation due to security restrictions"
)

// searchNow is the clock for the request's deadline (tests replace it; they must not
// use t.Parallel()).
var searchNow = time.Now

// searchRoutes mounts GET /api/v1/search (Slice A, parity decisions 1-7).
func (s *server) searchRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/search", s.read(s.search))
}

func (s *server) search(w http.ResponseWriter, r *http.Request) {
	deadline := searchNow().Add(searchTimeout)
	q, owner := r.URL.Query().Get("q"), r.URL.Query().Get("u")
	// R2: the core answers "" with no rows, so an empty query is refused like a bad one.
	if q == "" || !hestia.ValidSearchQuery(q) {
		WriteError(w, http.StatusBadRequest, "invalid", searchInvalidMsg)
		return
	}
	if owner != "" && !hestia.ValidUsername(owner) {
		invalidFormat(w, "user", owner)
		return
	}
	ctx := runner.WithTimeout(r.Context(), searchTimeout)
	sess := session.FromContext(ctx)
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	effective := s.Sessions.Effective(sess)
	adminAll := sess.UserContext == "admin" && sess.Look == ""

	// Decision 1: only an admin who isn't looking at a user searches everyone, or
	// filters by owner; everyone else searches the account they act as.
	cmd, args, filter := "v-search-user-object", []string{effective, q, "json"}, ""
	if adminAll {
		if owner == "" {
			cmd, args = "v-search-object", []string{q, "json"}
		} else {
			args[0], filter = owner, owner
		}
	}
	res, err := s.R.Run(ctx, cmd, args...)
	if err == nil {
		err = res.Err(cmd)
	}
	if err != nil {
		searchError(w, err)
		return
	}
	hits, err := hestia.DecodeSearch(res.Stdout)
	if err != nil {
		serverError(w, err)
		return
	}

	root := rootUser(cfg)
	// Decision 3 (D2): ROOT_USER's rows are dropped for everyone but ROOT_USER.
	if cfg["POLICY_SYSTEM_HIDE_ADMIN"] == "yes" && sess.User != root {
		kept := hits[:0]
		for _, h := range hits {
			if h.USER != root {
				kept = append(kept, h)
			}
		}
		hits = kept
	}
	// Decision 4: another admin sees the protected root admin's rows as plain text.
	protected := sess.UserContext == "admin" && sess.User != root && cfg["POLICY_SYSTEM_PROTECTED_ADMIN"] == "yes"
	verified := s.searchVerifyRecords(r, hits, deadline)

	rows := make([]SearchRow, 0, len(hits))
	for i, h := range hits {
		route := searchRoute(h)
		linked := route != "" && !(protected && h.USER == root)
		if h.KEY == "RECORD" && !verified[i] {
			linked = false
		}
		rows = append(rows, SearchRow{
			ID: h.ID, User: h.USER, Type: h.TYPE, Key: h.KEY, Result: h.RESULT, Alias: h.ALIAS,
			Link: h.LINK, Parent: h.PARENT, Suspended: h.SUSPENDED == "yes", Date: h.DATE, Time: h.TIME,
			Route: route, Linked: linked,
			LoginAs: linked && adminAll && h.USER != sess.User,
		})
	}
	writeJSON(w, http.StatusOK, SearchResults{Query: q, User: filter, Rows: rows})
}

// searchError answers a failed search (decision 7).
func searchError(w http.ResponseWriter, err error) {
	var ce *runner.CommandError
	if !errors.As(err, &ce) {
		writeCommandError(w, err)
		return
	}
	switch {
	case ce.Exit == runner.ExitInvalid && strings.Contains(ce.Output, "Invalid key value format"):
		// A conf line the search can't parse (before SR32/B2g, a value with colons).
		WriteError(w, http.StatusUnprocessableEntity, "search_failed", searchFailedMsg)
	case ce.Exit == runner.ExitInvalid && strings.Contains(ce.Output, "invalid object format"):
		WriteError(w, http.StatusBadRequest, "invalid", searchInvalidMsg)
	case ce.Exit == 1 && strings.Contains(ce.Output, searchDemoText):
		WriteError(w, http.StatusForbidden, "forbidden", "")
	default:
		writeCommandError(w, err)
	}
}

// searchRoute is the Hostisan page for a hit (decision 6), or "".
func searchRoute(h hestia.SearchHit) string {
	seg := url.PathEscape
	switch {
	case h.TYPE == "web" && h.KEY == "DOMAIN" && h.RESULT != "":
		return "/web/" + seg(h.RESULT)
	case h.TYPE == "dns" && h.KEY == "DOMAIN" && h.RESULT != "":
		return "/dns/" + seg(h.RESULT) + "/edit"
	case h.TYPE == "dns" && h.KEY == "RECORD" && h.PARENT != "" && h.LINK != "":
		return "/dns/" + seg(h.PARENT) + "/records/" + seg(h.LINK)
	case h.TYPE == "mail" && h.KEY == "DOMAIN" && h.RESULT != "":
		return "/mail/" + seg(h.RESULT) + "/edit"
	case h.TYPE == "mail" && h.KEY == "ACCOUNT" && h.PARENT != "" && h.LINK != "":
		return "/mail/" + seg(h.PARENT) + "/accounts/" + seg(h.LINK)
	case h.TYPE == "db" && h.KEY == "DATABASE" && h.RESULT != "":
		return "/db/" + seg(h.RESULT)
	case h.TYPE == "cron" && h.KEY == "JOB" && h.LINK != "":
		return "/cron/" + seg(h.LINK)
	}
	return ""
}

// searchVerifyRecords checks DNS record hits against their zone (decision 5, SR33):
// the core takes a record's zone from whatever line it parsed before, so a record row
// is trusted only when the zone it names holds record LINK with that record name.
// Each (user, zone) is listed once, at most searchMaxZones of them; a zone that can't
// be listed, or isn't reached before deadline, leaves its rows unverified. The result
// is indexed like hits.
func (s *server) searchVerifyRecords(r *http.Request, hits []hestia.SearchHit, deadline time.Time) []bool {
	ok := make([]bool, len(hits))
	type zone struct{ user, domain string }
	records := map[zone]map[string]string{} // listed zones: ID -> RECORD
	listed := 0
	for i, h := range hits {
		if h.KEY != "RECORD" {
			continue
		}
		z := zone{h.USER, h.PARENT}
		name, cut := strings.CutSuffix(h.RESULT, "."+h.PARENT)
		if !cut || name == "" || !hestia.ValidUsername(z.user) || !hestia.ValidDomainArg(z.domain) || !hestia.ValidRecordID(h.LINK) {
			continue
		}
		recs, seen := records[z]
		if !seen {
			left := deadline.Sub(searchNow())
			if listed >= searchMaxZones || left <= 0 {
				continue
			}
			listed++
			recs = s.searchZone(r, z.user, z.domain, min(left, searchZoneTimeout))
			records[z] = recs
		}
		if got, found := recs[h.LINK]; found && got == name {
			ok[i] = true
		}
	}
	return ok
}

// searchZone lists a zone's records as ID -> RECORD; nil when it can't be listed.
func (s *server) searchZone(r *http.Request, user, domain string, limit time.Duration) map[string]string {
	ctx := runner.WithTimeout(r.Context(), limit)
	res, err := s.R.Run(ctx, "v-list-dns-records", user, domain, "json")
	if err == nil {
		err = res.Err("v-list-dns-records")
	}
	if err != nil {
		return nil
	}
	recs, err := hestia.DecodeObjects[hestia.DNSRecord](res.Stdout)
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(recs))
	for _, rec := range recs {
		out[rec.ID] = rec.RECORD
	}
	return out
}
