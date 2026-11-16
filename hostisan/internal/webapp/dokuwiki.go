package webapp

import "strings"

var dokuwiki = &app{
	info: Info{ID: "dokuwiki", Name: "DokuWiki", Group: "wiki", Version: "latest", Database: false},
	form: []Field{
		text("wiki_name", ""), text("superuser", ""), text("real_name", ""), text("email", ""), password("password"),
		sel("initial_ACL_policy", "", list(
			"0: Open Wiki (read, write, upload for everyone)",
			"1: Public Wiki (read for everyone, write and upload for registered users)",
			"2: Closed Wiki (read, write, upload for registered users only)")),
		sel("content_license", "", list(
			"cc-zero: CC0 1.0 Universal",
			"publicdomain: Public Domain",
			"cc-by: CC Attribution 4.0 International",
			"cc-by-sa: CC Attribution-Share Alike 4.0 International",
			"gnufdl: GNU Free Documentation License 1.3",
			"cc-by-nc: CC Attribution-Noncommercial 4.0 International",
			"cc-by-nc-sa: CC Attribution-Noncommercial-Share Alike 4.0 International",
			"0: Do not show any license information")),
	},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://download.dokuwiki.org/src/dokuwiki/dokuwiki-stable.tgz"},
		Template: "dokuwiki",
		PHP:      []string{"8.0", "8.1", "8.2", "8.3", "8.4"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		x.Move(t.DocRoot(".htaccess.dist"), t.DocRoot(".htaccess"))
		policy := o["initial_ACL_policy"]
		if len(policy) > 1 {
			policy = policy[:1]
		}
		license, _, _ := strings.Cut(o["content_license"], ":")
		x.Post(t.URL()+"/install.php", F(
			"l", "en", "d[title]", o["wiki_name"], "d[acl]", "on", "d[superuser]", o["superuser"],
			"d[fullname]", o["real_name"], "d[email]", o["email"], "d[password]", o["password"],
			"d[confirm]", o["password"], "d[policy]", policy, "d[license]", license, "submit", ""),
			[]string{"Content-Type: application/x-www-form-urlencoded"}, t.Resolve())
		x.Delete(t.DocRoot("install.php"))
		return nil
	},
}
