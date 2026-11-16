package webapp

var vvveb = &app{
	info: Info{ID: "vvveb", Name: "Vvveb", Group: "cms", Version: "latest", Database: true},
	form: []Field{text("vvveb_account_username", "admin"), text("vvveb_account_email", ""), password("vvveb_account_password")},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://www.vvveb.com/latest.zip"},
		Template: "vvveb",
		PHP:      []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		x.PHP(o["php_version"], t.DocRoot("/cli.php"), "install", "host="+t.DB.Host, "user="+t.DB.User,
			"password="+t.DB.Password, "database="+t.DB.Name, "admin[user]="+o["vvveb_account_username"],
			"admin[password]="+o["vvveb_account_password"], "admin[email]="+o["vvveb_account_email"])
		return nil
	},
}
