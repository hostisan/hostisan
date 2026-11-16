package httpapi

// Tools (Plan 5) wire types: search, the web terminal token, the file-manager
// handoff and per-file snapshot restore. The terminal's websocket messages are not
// JSON bodies of a route and live with the protocol (Task 1 Interfaces).

// SearchRow is one search result (GET /api/v1/search), from hestia.SearchHit.
// Route is the Hostisan page for the object ("" when it has none); Linked is false
// when the row must be shown as plain text (protected admin, or a DNS record the
// handler couldn't verify). LoginAs: the row belongs to another account, so the UI
// posts /auth/login-as {user} before going to Route. Type is the core's TYPE (web,
// dns, mail, db, cron) and Key its KEY (DOMAIN, RECORD, ACCOUNT, DATABASE, JOB).
type SearchRow struct {
	ID        string `json:"id"`
	User      string `json:"user"`
	Type      string `json:"type"`
	Key       string `json:"key"`
	Result    string `json:"result"`
	Alias     string `json:"alias"`
	Link      string `json:"link"`
	Parent    string `json:"parent"`
	Suspended bool   `json:"suspended"`
	Date      string `json:"date"`
	Time      string `json:"time"`
	Route     string `json:"route"`
	Linked    bool   `json:"linked"`
	LoginAs   bool   `json:"loginAs"`
}

// SearchResults answers GET /api/v1/search?q=&u=. User is the owner filter applied
// ("" for none); Rows is [] when nothing matched, never null.
type SearchResults struct {
	Query string      `json:"query"`
	User  string      `json:"user"`
	Rows  []SearchRow `json:"rows"`
}

// TerminalToken answers POST /api/v1/terminal/token: a single-use token for the
// terminal websocket's auth message, valid until Expires (RFC 3339).
type TerminalToken struct {
	Token   string `json:"token"`
	Expires string `json:"expires"`
}

// FilesHandoff answers POST /api/v1/files/handoff: where the browser goes next.
type FilesHandoff struct {
	URL string `json:"url"`
}

// RestoreFilesRequest is POST /api/v1/backup/incremental/{snapshot}/restore-files:
// 1 to 50 absolute paths under the acting user's home.
type RestoreFilesRequest struct {
	Paths []string `json:"paths"`
}

// RestoreFilesResult is a finished restore job's result: the paths restored, in
// order. A failed job's error is a jobs.Failure whose Args[0] is the failing path;
// the job's Steps list every path tried.
type RestoreFilesResult struct {
	Restored []string `json:"restored"`
}
