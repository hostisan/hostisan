package httpapi

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The real callers of /api/ (Review Focus 2): each reply is byte-compared with a
// golden file written from web/api/index.php's logic for that request. The fixtures
// these sequences replay are in testdata/fixtures/legacy_api/ (v-insert-dns-domain and
// v-insert-dns-records print nothing on success: the bins end with log_event + exit)
// and the dev set.

type legacyGolden struct {
	Source string `json:"_source"`
	Steps  []struct {
		Name        string     `json:"name"`
		ContentType string     `json:"contentType"`
		Form        [][]string `json:"form"`
		Body        string     `json:"body"`
		Calls       []string   `json:"calls"`
		TmpFile     *struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		} `json:"tmpFile"`
		Status    int    `json:"status"`
		ExitCode  string `json:"exitCode"`
		ReplyType string `json:"replyType"`
		Reply     string `json:"reply"`
	} `json:"steps"`
}

func TestLegacyGolden(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(legacyFixtures, "golden", "*.json"))
	if err != nil || len(files) != 3 {
		t.Fatalf("golden files: %v %v", files, err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var g legacyGolden
			if err := json.Unmarshal(data, &g); err != nil {
				t.Fatal(err)
			}
			if g.Source == "" || len(g.Steps) == 0 {
				t.Fatal("golden file needs _source and steps")
			}
			e := newLegacyEnv(t, nil, nil)
			for _, st := range g.Steps {
				body := st.Body
				if st.Form != nil {
					var parts []string
					for _, kv := range st.Form {
						parts = append(parts, url.QueryEscape(kv[0])+"="+url.QueryEscape(kv[1]))
					}
					body = strings.Join(parts, "&")
				}
				r := e.send(e.h.client, e.h.srv.URL, "POST", "/api/", st.ContentType, body, nil)
				e.want(st.Name, r, st.Status, st.ExitCode, st.ReplyType, st.Reply)
				e.wantCalls(st.Name, st.Calls...)
				if st.TmpFile != nil {
					got, err := os.ReadFile(filepath.Join(e.tmp, st.TmpFile.Name))
					if err != nil || string(got) != st.TmpFile.Content {
						t.Errorf("%s: %s = %q, %v; want %q", st.Name, st.TmpFile.Name, got, err, st.TmpFile.Content)
					}
				}
				// Only v-make-tmp-file's files stay: the password/hash files are gone.
				for _, name := range e.tmpFiles() {
					if strings.HasPrefix(name, "hostisan-") {
						t.Errorf("%s: secret file %s left", st.Name, name)
					}
				}
			}
		})
	}
}
