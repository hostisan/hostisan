package webapp

var joomla = &app{
	info: Info{ID: "joomla", Name: "Joomla", Group: "cms", Version: "6.1.3", Database: true},
	form: []Field{text("site_name", "Joomla"), text("admin_user", ""), text("admin_username", "joomlaadmin"), password("password"), text("email", "")},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://downloads.joomla.org/cms/joomla6/6-1-3/Joomla_6-1-3-Stable-Full_Package.zip?format=zip"},
		Template: "joomla",
		PHP:      []string{"8.3", "8.4"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		x.Move(t.DocRoot("htaccess.txt"), t.DocRoot(".htaccess"))
		x.PHP(o["php_version"], t.DocRoot("installation/joomla.php"), "install",
			"--site-name="+o["site_name"], "--admin-user="+o["admin_user"], "--admin-username="+o["admin_username"],
			"--admin-password="+o["password"], "--admin-email="+o["email"], "--db-user="+t.DB.User,
			"--db-pass="+t.DB.Password, "--db-name="+t.DB.Name, "--db-host="+t.DB.Host,
			"--db-type=mysqli", "--db-encryption=0")
		return nil
	},
}
