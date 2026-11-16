package httpapi

import (
	"path/filepath"
	"testing"
)

// The core writes dumps to /backup whatever hestia.conf's BACKUP says (func/main.sh
// fixes it and refuses the key from config), so a changed BACKUP must not break the
// downloads (D3).
func TestAccelPathUsesInternalPrefix(t *testing.T) {
	for _, conf := range []string{"", "/backup", "/srv/bk"} {
		const dir = "/backup"
		fx := map[string][]fixtureEntry{
			"v-dump-database": {{Args: []string{"*", "*", "*", "*"}, Stdout: dir + "/d.sql.gz\n"}},
			"v-dump-site":     {{Args: []string{"*", "*", "*"}, Stdout: "s.zip\n" + dir + "/s.zip\n"}},
		}
		h := newHarnessWith(t, overrides{Fixtures: fx, Config: map[string]string{"BACKUP": conf}})
		h.loginAdmin()
		if res := getNoFollow(h, "/api/v1/db/admin_wordpress/download", ""); res.Header.Get("X-Accel-Redirect") != "/__accel"+dir+"/d.sql.gz" {
			t.Errorf("db %s: %q", dir, res.Header.Get("X-Accel-Redirect"))
		}
		if res := getNoFollow(h, "/api/v1/web/example.com/download", ""); res.Header.Get("X-Accel-Redirect") != "/__accel"+dir+"/s.zip" {
			t.Errorf("site %s: %q", dir, res.Header.Get("X-Accel-Redirect"))
		}
	}
	o := bkLocalDir(t, bkAdminNew)
	h := newHarnessWith(t, o)
	h.loginAdmin()
	h.call("POST", "/api/v1/backup/"+bkAdminNew+"/download", nil, nil)
	res, _ := bkGet(h, "/api/v1/backup/file/"+bkAdminNew)
	if want := "/__accel" + filepath.Join(o.BackupDir, bkAdminNew); res.Header.Get("X-Accel-Redirect") != want {
		t.Errorf("backup: %q want %q", res.Header.Get("X-Accel-Redirect"), want)
	}
}

func TestAccelRefusesPathOutsideBackup(t *testing.T) {
	// /srv/bk is hestia.conf's BACKUP here, but nothing outside /backup is served.
	for _, out := range []string{"/etc/passwd", "/backup/../etc/passwd", "/backupx/d.sql.gz", "/srv/bk/d.sql.gz"} {
		fx := map[string][]fixtureEntry{
			"v-dump-database": {{Args: []string{"*", "*", "*", "*"}, Stdout: out + "\n"}},
			"v-dump-site":     {{Args: []string{"*", "*", "*"}, Stdout: "s.zip\n" + out + "\n"}},
		}
		h := newHarnessWith(t, overrides{Fixtures: fx, Config: map[string]string{"BACKUP": "/srv/bk"}})
		h.loginAdmin()
		for _, p := range []string{"/api/v1/db/admin_wordpress/download", "/api/v1/web/example.com/download"} {
			res := getNoFollow(h, p, "")
			if res.StatusCode != 500 || res.Header.Get("X-Accel-Redirect") != "" {
				t.Errorf("%s %s: %d %q", p, out, res.StatusCode, res.Header.Get("X-Accel-Redirect"))
			}
		}
	}
}
