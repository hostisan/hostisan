package webapp

var flarum = &app{
	info: Info{ID: "flarum", Name: "Flarum", Group: "forum", Version: "latest", Database: true},
	form: []Field{text("forum_title", "Flarum Forum"), text("admin_username", "fladmin"), text("admin_email", ""), password("admin_password")},
	cfg: Config{
		Resource: Resource{Kind: "composer", Src: "flarum/flarum"},
		Template: "flarum-composer",
		PHP:      []string{"8.2", "8.3"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		x.Create(t.DocRoot(".htaccess"), embeddedFile("flarum.htaccess"))
		x.Post(t.URL(), F(
			"forumTitle", o["forum_title"], "mysqlHost", t.DB.Host, "mysqlDatabase", t.DB.Name,
			"mysqlUsername", t.DB.User, "mysqlPassword", t.DB.Password, "tablePrefix", "fl"+randomString(5),
			"adminUsername", o["admin_username"], "adminEmail", o["admin_email"],
			"adminPassword", o["admin_password"], "adminPasswordConfirmation", o["admin_password"]), nil, t.Resolve())
		return nil
	},
}
