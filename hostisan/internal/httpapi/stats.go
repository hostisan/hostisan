package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/session"
)

// statsRoutes registers the statistics page (Plan 3 Slice D). Called from registerAccountRoutes.
func (s *server) statsRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/stats", s.read(s.statsGet))
}

// statText reads a JSON string or number as text.
type statText string

func (t *statText) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*t = statText(s)
		return nil
	}
	*t = statText(b)
	return nil
}

// statsRow is one month of v-list-user-stats or v-list-users-stats.
type statsRow struct {
	TIME          statText `json:"TIME"`
	PACKAGE       statText `json:"PACKAGE"`
	IPOwned       statText `json:"IP_OWNED"`
	DiskQuota     statText `json:"DISK_QUOTA"`
	UDisk         statText `json:"U_DISK"`
	UDiskDirs     statText `json:"U_DISK_DIRS"`
	UDiskWeb      statText `json:"U_DISK_WEB"`
	UDiskMail     statText `json:"U_DISK_MAIL"`
	UDiskDB       statText `json:"U_DISK_DB"`
	Bandwidth     statText `json:"BANDWIDTH"`
	UBandwidth    statText `json:"U_BANDWIDTH"`
	UWebDomains   statText `json:"U_WEB_DOMAINS"`
	UWebSSL       statText `json:"U_WEB_SSL"`
	UWebAliases   statText `json:"U_WEB_ALIASES"`
	UDNSDomains   statText `json:"U_DNS_DOMAINS"`
	UDNSRecords   statText `json:"U_DNS_RECORDS"`
	UMailDomains  statText `json:"U_MAIL_DOMAINS"`
	UMailDKIM     statText `json:"U_MAIL_DKIM"`
	UMailAccounts statText `json:"U_MAIL_ACCOUNTS"`
	UDatabases    statText `json:"U_DATABASES"`
	UCronJobs     statText `json:"U_CRON_JOBS"`
	UBackups      statText `json:"U_BACKUPS"`
	UUsers        statText `json:"U_USERS"`
}

// decodeStats turns the stats JSON (keyed by month end date) into months, newest first.
// Blank output (no stats yet) is no months.
func decodeStats(out []byte) ([]StatsMonth, error) {
	months := []StatsMonth{}
	if strings.TrimSpace(string(out)) == "" {
		return months, nil
	}
	var rows map[string]statsRow
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, err
	}
	for d, r := range rows {
		months = append(months, StatsMonth{
			Date: d, Time: string(r.TIME), Package: string(r.PACKAGE), IPOwned: string(r.IPOwned),
			DiskQuota: string(r.DiskQuota), UDisk: string(r.UDisk), UDiskDirs: string(r.UDiskDirs),
			UDiskWeb: string(r.UDiskWeb), UDiskMail: string(r.UDiskMail), UDiskDB: string(r.UDiskDB),
			Bandwidth: string(r.Bandwidth), UBandwidth: string(r.UBandwidth),
			UWebDomains: string(r.UWebDomains), UWebSSL: string(r.UWebSSL), UWebAliases: string(r.UWebAliases),
			UDNSDomains: string(r.UDNSDomains), UDNSRecords: string(r.UDNSRecords),
			UMailDomains: string(r.UMailDomains), UMailDKIM: string(r.UMailDKIM), UMailAccounts: string(r.UMailAccounts),
			UDatabases: string(r.UDatabases), UCronJobs: string(r.UCronJobs), UBackups: string(r.UBackups),
			UUsers: string(r.UUsers),
		})
	}
	sort.Slice(months, func(i, j int) bool { return months[i].Date > months[j].Date })
	return months, nil
}

func (s *server) statsGet(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	admin := sess.UserContext == "admin"
	// An admin looking at another account sees that account, like any user.
	adminMode := admin && sess.Look == ""
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid", badRequestMsg)
		return
	}
	target := ""
	// An empty ?user= means no user.
	if q.Get("user") != "" {
		if !adminMode {
			WriteError(w, http.StatusForbidden, "forbidden", "")
			return
		}
		if target = q.Get("user"); !hestia.ValidUsername(target) {
			invalidFormat(w, "user", target)
			return
		}
	}
	reply := Stats{Users: []string{}}
	var out []byte
	var ok bool
	switch {
	case adminMode && target == "":
		reply.Overall = true
		out, ok = s.output(w, r, "v-list-users-stats", "json")
	case adminMode:
		reply.User = target
		out, ok = s.output(w, r, "v-list-user-stats", target, "json")
	default:
		reply.User = s.actingUser(r)
		out, ok = s.output(w, r, "v-list-user-stats", reply.User, "json")
	}
	if !ok {
		return
	}
	if reply.Months, err = decodeStats(out); err != nil {
		serverError(w, err)
		return
	}
	if adminMode {
		cfg, err := s.H.Config(r.Context())
		if err != nil {
			serverError(w, err)
			return
		}
		list, ok := s.output(w, r, "v-list-sys-users", "json")
		if !ok {
			return
		}
		var users []string
		if strings.TrimSpace(string(list)) != "" {
			if err := json.Unmarshal(list, &users); err != nil {
				serverError(w, err)
				return
			}
		}
		if cfg["POLICY_SYSTEM_HIDE_ADMIN"] == "yes" {
			users = slices.DeleteFunc(users, func(u string) bool { return u == rootUser(cfg) })
		}
		if users != nil {
			reply.Users = users
		}
	}
	writeJSON(w, http.StatusOK, reply)
}
