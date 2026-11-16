package webapp

var nextcloud = &app{
	info: Info{ID: "nextcloud", Name: "Nextcloud", Group: "cloud", Version: "latest", Database: true},
	form: []Field{text("username", "admin"), password("password")},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://download.nextcloud.com/server/releases/latest.tar.bz2"},
		Template: "owncloud",
		PHP:      []string{"8.2", "8.3", "8.4", "8.5"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		php := o["php_version"]
		x.PHP(php, t.DocRoot("occ"), "maintenance:install", "--database", "mysql",
			"--database-name", t.DB.Name, "--database-host", t.DB.Host, "--database-user", t.DB.User,
			"--database-pass", t.DB.Password, "--admin-user", o["username"], "--admin-pass", o["password"])
		x.PHP(php, t.DocRoot("occ"), "config:system:set", "trusted_domains", "2", "--value="+t.Domain)
		// Raise the minimum memory limit to 512M. PHP appended the literal characters
		// \r\n here (single quotes), not a line break; kept as it was.
		ini := t.DocRoot(".user.ini")
		contents := x.Read(ini) + `memory_limit=512M\r\n`
		x.Create(ini, contents)
		return nil
	},
}
