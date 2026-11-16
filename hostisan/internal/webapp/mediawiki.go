package webapp

var mediawiki = &app{
	info: Info{ID: "mediawiki", Name: "MediaWiki", Group: "cms", Version: "1.46.0", Database: true},
	form: []Field{text("wiki_name", "MediaWiki"), text("admin_username", "wikiadmin"), password("admin_password"), text("language", "en")},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://releases.wikimedia.org/mediawiki/1.46/mediawiki-1.46.0.zip"},
		Template: "default",
		PHP:      []string{"8.2", "8.3", "8.4", "8.5"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		x.CopyDir(t.DocRoot("/mediawiki-1.46.0/."), t.DocRoot())
		x.PHP(o["php_version"], t.DocRoot("maintenance/install.php"),
			"--dbserver="+t.DB.Host, "--dbname="+t.DB.Name, "--installdbuser="+t.DB.User,
			"--installdbpass="+t.DB.Password, "--dbuser="+t.DB.Name, "--dbpass="+t.DB.Password,
			"--server="+t.URL(), "--scriptpath=", "--lang="+o["language"], "--pass="+o["admin_password"],
			o["wiki_name"], o["admin_username"])
		x.DeleteDir(t.DocRoot("/mediawiki-1.46.0/"))
		return nil
	},
}
