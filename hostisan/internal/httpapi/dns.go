package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hostisan/hostisan/internal/hestia"
	"github.com/hostisan/hostisan/internal/session"
)

func (s *server) dnsRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/dns", s.read(s.dnsList))
	// Go's mux prefers the literal /options over the {domain} wildcard.
	mux.Handle("GET /api/v1/dns/options", s.read(s.dnsOptions))
	mux.Handle("GET /api/v1/dns/{domain}", s.read(s.dnsZone))
	mux.Handle("GET /api/v1/dns/{domain}/records", s.read(s.dnsRecords))
	mux.Handle("GET /api/v1/dns/{domain}/dnssec", s.read(s.dnsSEC))
	mux.Handle("POST /api/v1/dns", s.write(s.dnsAdd))
	mux.Handle("PUT /api/v1/dns/{domain}", s.write(s.dnsEdit))
	mux.Handle("DELETE /api/v1/dns/{domain}", s.write(s.dnsAction("v-delete-dns-domain")))
	mux.Handle("POST /api/v1/dns/{domain}/suspend", s.adminWrite(s.dnsAction("v-suspend-dns-domain")))
	mux.Handle("POST /api/v1/dns/{domain}/unsuspend", s.adminWrite(s.dnsAction("v-unsuspend-dns-domain")))
	mux.Handle("POST /api/v1/dns/bulk", s.write(s.dnsBulkZones))
	mux.Handle("POST /api/v1/dns/{domain}/records", s.write(s.recordAdd))
	mux.Handle("POST /api/v1/dns/{domain}/records/bulk", s.write(s.dnsBulkRecords))
	mux.Handle("PUT /api/v1/dns/{domain}/records/{id}", s.write(s.recordEdit))
	mux.Handle("DELETE /api/v1/dns/{domain}/records/{id}", s.write(s.recordAction("v-delete-dns-record")))
	mux.Handle("POST /api/v1/dns/{domain}/records/{id}/suspend", s.adminWrite(s.recordAction("v-suspend-dns-record")))
	mux.Handle("POST /api/v1/dns/{domain}/records/{id}/unsuspend", s.adminWrite(s.recordAction("v-unsuspend-dns-record")))
}

// domainParam is the {domain} path value; it answers 400 itself when unusable.
func domainParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	d := r.PathValue("domain")
	if !hestia.ValidDomainArg(d) {
		invalidFormat(w, "domain", d)
		return "", false
	}
	return d, true
}

func (s *server) zone(w http.ResponseWriter, r *http.Request, user, domain string) (hestia.DNSDomain, bool) {
	out, ok := s.output(w, r, "v-list-dns-domain", user, domain, "json")
	if !ok {
		return hestia.DNSDomain{}, false
	}
	zones, err := hestia.DecodeObjects[hestia.DNSDomain](out)
	if err != nil || len(zones) != 1 {
		serverError(w, firstErr(err, "v-list-dns-domain: unexpected output"))
		return hestia.DNSDomain{}, false
	}
	return zones[0], true
}

func (s *server) dnsList(w http.ResponseWriter, r *http.Request) {
	out, ok := s.output(w, r, "v-list-dns-domains", s.actingUser(r), "json")
	if !ok {
		return
	}
	zones, err := hestia.DecodeObjects[hestia.DNSDomain](out)
	if err != nil {
		serverError(w, err)
		return
	}
	user := s.actingUser(r)
	u, ok, err := s.H.User(r.Context(), user)
	if err != nil || !ok {
		serverError(w, firstErr(err, "dns list: user "+user+" not found"))
		return
	}
	writeJSON(w, http.StatusOK, DNSZones{Zones: zones, Usage: dnsUsage(u)})
}

// dnsUsage builds the usage summary; with either limit unlimited (or not a number)
// the record limit is unlimited too.
func dnsUsage(u hestia.User) DNSUsage {
	use := DNSUsage{
		Zones: u["U_DNS_DOMAINS"], ZonesLimit: u["DNS_DOMAINS"],
		Suspended: u["SUSPENDED_DNS"], Records: u["U_DNS_RECORDS"], RecordsLimit: "unlimited",
	}
	perZone, err1 := strconv.Atoi(u["DNS_RECORDS"])
	zones, err2 := strconv.Atoi(u["DNS_DOMAINS"])
	if err1 == nil && err2 == nil {
		use.RecordsLimit = strconv.Itoa(perZone * zones)
	}
	return use
}

func (s *server) dnsZone(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	if z, ok := s.zone(w, r, s.actingUser(r), domain); ok {
		writeJSON(w, http.StatusOK, z)
	}
}

func (s *server) dnsRecords(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	user := s.actingUser(r)
	z, ok := s.zone(w, r, user, domain)
	if !ok {
		return
	}
	out, ok := s.output(w, r, "v-list-dns-records", user, domain, "json")
	if !ok {
		return
	}
	recs, err := hestia.DecodeObjects[hestia.DNSRecord](out)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, DNSRecords{Zone: z, Records: recs})
}

func (s *server) dnsSEC(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	user := s.actingUser(r)
	z, ok := s.zone(w, r, user, domain)
	if !ok {
		return
	}
	out, ok := s.output(w, r, "v-list-dnssec-public-key", user, domain, "json")
	if !ok {
		return
	}
	var keys map[string]hestia.DNSSECKey
	if err := json.Unmarshal(out, &keys); err != nil {
		serverError(w, err)
		return
	}
	key := keys[domain] // all empty when the zone has no key
	writeJSON(w, http.StatusOK, DNSSECView{Zone: z, Key: key, AlgorithmLabel: algorithmLabel(key.ALGORITHM), FlagLabel: flagLabel(key.FLAG)})
}

var dnssecAlgorithms = map[string]string{
	"3": "3 - DSA", "5": "5 - RSA/SHA1", "6": "6 - DSA-NSEC3-SHA1", "7": "7 - RSA/SHA1-NSEC3-SHA1",
	"8": "8 - RSA/SHA256", "10": "10 - RSA/SHA512", "12": "12 - ECC-GOST", "13": "13 - ECDSAP256/SHA256",
	"14": "14 - ECDSAP384/SHA384", "15": "15 - ED25519/SHA512", "16": "16 - ED448/SHA912",
}

func algorithmLabel(alg string) string {
	if l, ok := dnssecAlgorithms[alg]; ok {
		return l
	}
	return "Unknown"
}

func flagLabel(flag string) string {
	switch flag {
	case "257":
		return "KSK (257)"
	case "256":
		return "ZSK (256)"
	}
	return ""
}

func (s *server) dnsOptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess := session.FromContext(ctx)
	user := s.actingUser(r)
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	u, ok, err := s.H.User(ctx, user)
	if err != nil || !ok {
		serverError(w, firstErr(err, "dns options: user "+user+" not found"))
		return
	}

	var templates, nameservers []string
	if !s.listJSON(w, ctx, &templates, "v-list-dns-templates", "json") {
		return
	}
	if !s.listJSON(w, ctx, &nameservers, "v-list-user-ns", user, "json") {
		return
	}
	out, ok := s.output(w, r, "v-list-user-ips", user, "json")
	if !ok {
		return
	}
	ips, err := hestia.DecodeObjects[hestia.UserIP](out)
	if err != nil {
		serverError(w, err)
		return
	}
	displays := make([]string, 0, len(ips))
	for _, ip := range ips {
		displays = append(displays, ip.Display())
	}

	writeJSON(w, http.StatusOK, DNSOptions{
		Templates:        nonNil(templates),
		IPs:              displays,
		Nameservers:      nonNil(nameservers),
		DefaultTemplate:  u["DNS_TEMPLATE"],
		DefaultTTL:       defaultTTL,
		DefaultExpiry:    s.defaultExpiry(),
		CanEditTemplates: canEditTemplates(sess, cfg),
		SupportsDNSSEC:   cfg["DNS_CLUSTER_SYSTEM"] == "hestia-zone" && cfg["SUPPORT_DNSSEC"] == "yes",
		DNSSystem:        strings.ToUpper(cfg["DNS_SYSTEM"]),
		AdminGate:        u.Role() == "admin",
	})
}

// listJSON runs a command that prints a JSON value and decodes it into v.
func (s *server) listJSON(w http.ResponseWriter, ctx context.Context, v any, cmd string, args ...string) bool {
	res, err := s.R.Run(ctx, cmd, args...)
	if err == nil {
		err = res.Err(cmd)
	}
	if err == nil {
		err = json.Unmarshal(res.Stdout, v)
	}
	if err != nil {
		writeCommandError(w, err)
		return false
	}
	return true
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

const defaultTTL = "14400"

// defaultExpiry is a year from the server's today; add-zone compares against it too.
func (s *server) defaultExpiry() string {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	return now.AddDate(1, 0, 0).Format("2006-01-02")
}

// canEditTemplates is checklist section 5: admins, or users when the policy allows.
func canEditTemplates(sess *session.Session, cfg hestia.SysConfig) bool {
	return sess.UserContext == "admin" ||
		(sess.UserContext == "user" && cfg["POLICY_USER_EDIT_DNS_TEMPLATES"] == "yes")
}

func (s *server) dnsAdd(w http.ResponseWriter, r *http.Request) {
	var req AddZoneRequest
	if !decodeBody(w, r, &req) {
		return
	}
	raw := strings.TrimSpace(req.Domain)
	if !blankFields(w, "Domain", raw, "IP Address", req.IP) {
		return
	}
	// Parity decision 1: strip a leading "www." only, then lower-case.
	domain := strings.ToLower(raw)
	domain = strings.TrimPrefix(domain, "www.")
	if !hestia.ValidDomainArg(domain) {
		shown := domain
		if shown == "" {
			shown = raw
		}
		// Hestia's own wording for a bad domain.
		WriteError(w, http.StatusBadRequest, "invalid", "Error: invalid domain format :: "+shown)
		return
	}
	if len(req.Nameservers) > 8 {
		WriteError(w, http.StatusBadRequest, "invalid", tooManyNSMsg)
		return
	}

	ctx := r.Context()
	sess := session.FromContext(ctx)
	user := s.actingUser(r)
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	u, ok, err := s.H.User(ctx, user)
	if err != nil || !ok {
		serverError(w, firstErr(err, "dns add: user "+user+" not found"))
		return
	}

	dnssec := "no"
	if req.DNSSEC {
		dnssec = "yes"
	}
	add := []string{"v-add-dns-domain", user, domain, req.IP}
	for i := 0; i < 8; i++ {
		ns := ""
		if i < len(req.Nameservers) {
			ns = req.Nameservers[i]
		}
		add = append(add, ns)
	}
	add = append(add, "no", dnssec)
	steps := [][]string{add}
	if canEditTemplates(sess, cfg) && req.Template != "" && req.Template != u["DNS_TEMPLATE"] {
		steps = append(steps, []string{"v-change-dns-domain-tpl", user, domain, req.Template, "no"})
	}
	if req.Exp != "" && req.Exp != s.defaultExpiry() {
		steps = append(steps, []string{"v-change-dns-domain-exp", user, domain, req.Exp, "no"})
	}
	if req.TTL != "" && req.TTL != defaultTTL {
		steps = append(steps, []string{"v-change-dns-domain-ttl", user, domain, req.TTL, "no"})
	}
	steps = append(steps, []string{"v-restart-dns"})
	if s.steps(w, r, steps) {
		writeJSON(w, http.StatusOK, OK{Status: "ok"})
	}
}

func (s *server) dnsEdit(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	var req EditZoneRequest
	if !decodeBody(w, r, &req) {
		return
	}
	// A blank value would reach Hestia as '' (v-change-dns-domain-ip then strips the
	// old IP from the whole zone file), so all four are required.
	trim := strings.TrimSpace
	if !blankFields(w, "IP Address", trim(req.IP), "SOA", trim(req.SOA), "Expiration Date", trim(req.Exp), "TTL", trim(req.TTL)) {
		return
	}
	ctx := r.Context()
	sess := session.FromContext(ctx)
	user := s.actingUser(r)
	cfg, err := s.H.Config(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	cur, ok := s.zone(w, r, user, domain)
	if !ok {
		return
	}

	var steps [][]string
	restart := false
	change := func(restarts bool, cmd string, args ...string) {
		steps = append(steps, append([]string{cmd, user, domain}, args...))
		restart = restart || restarts
	}
	if req.IP != cur.IP {
		change(true, "v-change-dns-domain-ip", req.IP, "no")
	}
	if canEditTemplates(sess, cfg) && req.Template != "" && req.Template != cur.TPL {
		change(true, "v-change-dns-domain-tpl", req.Template, "no")
	}
	if req.SOA != cur.SOA {
		change(true, "v-change-dns-domain-soa", req.SOA, "no")
	}
	if req.Exp != cur.EXP {
		change(false, "v-change-dns-domain-exp", req.Exp, "no") // metadata, no restart
	}
	if req.TTL != cur.TTL {
		change(true, "v-change-dns-domain-ttl", req.TTL, "no")
	}
	if req.DNSSEC != nil && *req.DNSSEC != (cur.DNSSEC == "yes") {
		state := "no"
		if *req.DNSSEC {
			state = "yes"
		}
		change(true, "v-change-dns-domain-dnssec", state)
	}
	if restart {
		steps = append(steps, []string{"v-restart-dns"})
	}
	if s.steps(w, r, steps) {
		writeJSON(w, http.StatusOK, OK{Status: "ok"})
	}
}

// dnsAction is delete, suspend or unsuspend: one command on the zone.
func (s *server) dnsAction(cmd string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		domain, ok := domainParam(w, r)
		if !ok {
			return
		}
		if s.steps(w, r, [][]string{{cmd, s.actingUser(r), domain}}) {
			writeJSON(w, http.StatusOK, OK{Status: "ok"})
		}
	}
}

// recordParams is the {domain} and {id} path values; it answers 400 itself when
// either is unusable.
func recordParams(w http.ResponseWriter, r *http.Request) (domain, id string, ok bool) {
	if domain, ok = domainParam(w, r); !ok {
		return "", "", false
	}
	id = r.PathValue("id")
	if !hestia.ValidRecordID(id) {
		invalidFormat(w, "id", id)
		return "", "", false
	}
	return domain, id, true
}

func (s *server) recordAdd(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	var req RecordRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !blankFields(w, "Record", req.Record, "Type", req.Type, "IP or Value", req.Value) {
		return
	}
	// The trailing "yes" restarts DNS inside the command; no separate restart (decision 4).
	add := []string{"v-add-dns-record", s.actingUser(r), domain, req.Record, req.Type, req.Value, req.Priority, "", "yes", req.TTL}
	if s.steps(w, r, [][]string{add}) {
		writeJSON(w, http.StatusOK, OK{Status: "ok"})
	}
}

func (s *server) recordEdit(w http.ResponseWriter, r *http.Request) {
	domain, id, ok := recordParams(w, r)
	if !ok {
		return
	}
	var req RecordRequest
	if !decodeBody(w, r, &req) {
		return
	}
	user := s.actingUser(r)
	out, ok := s.output(w, r, "v-list-dns-records", user, domain, "json")
	if !ok {
		return
	}
	recs, err := hestia.DecodeObjects[hestia.DNSRecord](out)
	if err != nil {
		serverError(w, err)
		return
	}
	var cur *hestia.DNSRecord
	for i := range recs {
		if recs[i].ID == id {
			cur = &recs[i]
		}
	}
	if cur == nil {
		WriteError(w, http.StatusNotFound, "not_found", "Error: unknown record ID.")
		return
	}
	changed := req.Record != cur.RECORD || req.Type != cur.TYPE || req.Value != cur.VALUE ||
		req.Priority != cur.PRIORITY || req.TTL != cur.TTL
	if changed {
		// The "yes" is the one restart of this save (decision 4).
		change := []string{"v-change-dns-record", user, domain, id, req.Record, req.Type, req.Value, req.Priority, "yes", req.TTL}
		if !s.steps(w, r, [][]string{change}) {
			return
		}
	}
	writeJSON(w, http.StatusOK, OK{Status: "ok"})
}

// recordAction is delete, suspend or unsuspend on one record.
func (s *server) recordAction(cmd string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		domain, id, ok := recordParams(w, r)
		if !ok {
			return
		}
		if s.steps(w, r, [][]string{{cmd, s.actingUser(r), domain, id}}) {
			writeJSON(w, http.StatusOK, OK{Status: "ok"})
		}
	}
}

// Bulk actions (checklist 3.8, decision 5). Admins get every action; users delete only.
var (
	zoneBulk = map[string]string{
		"rebuild": "v-rebuild-dns-domain", "suspend": "v-suspend-dns-domain",
		"unsuspend": "v-unsuspend-dns-domain", "delete": "v-delete-dns-domain",
	}
	recordBulk = map[string]string{
		"suspend": "v-suspend-dns-record", "unsuspend": "v-unsuspend-dns-record", "delete": "v-delete-dns-record",
	}
)

// dnsRestart is the one restart that ends a DNS bulk run.
var dnsRestart = [][]string{{"v-restart-dns"}}

func (s *server) dnsBulkZones(w http.ResponseWriter, r *http.Request) {
	s.bulk(w, r, zoneBulk, hestia.ValidDomainArg, func(user, n string) []string {
		return []string{user, n, "no"}
	}, dnsRestart)
}

func (s *server) dnsBulkRecords(w http.ResponseWriter, r *http.Request) {
	domain, ok := domainParam(w, r)
	if !ok {
		return
	}
	s.bulk(w, r, recordBulk, hestia.ValidRecordID, func(user, n string) []string {
		return []string{user, domain, n, "no"}
	}, dnsRestart)
}
