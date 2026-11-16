package webapp

var opencart = &app{
	info: Info{ID: "opencart", Name: "OpenCart", Group: "ecommerce", Version: "4.1.0.4", Database: true},
	form: []Field{text("username", "ocadmin"), text("email", ""), password("password")},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://github.com/opencart/opencart/releases/download/4.1.0.4/opencart-4.1.0.4.zip", Dst: "/tmp-opencart"},
		Template: "opencart",
		PHP:      []string{"8.0", "8.1", "8.2", "8.3", "8.4", "8.5"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		const extract = "/tmp-opencart"
		x.CopyDir(t.DocRoot(extract+"/upload/."), t.DocRoot())
		x.Move(t.DocRoot("config-dist.php"), t.DocRoot("config.php"))
		x.Move(t.DocRoot("admin/config-dist.php"), t.DocRoot("admin/config.php"))
		x.Move(t.DocRoot(".htaccess.txt"), t.DocRoot(".htaccess"))
		x.PHP(o["php_version"], t.DocRoot("/install/cli_install.php"), "install",
			"--db_hostname", t.DB.Host, "--db_username", t.DB.User, "--db_password", t.DB.Password,
			"--db_database", t.DB.Name, "--username", o["username"], "--password", o["password"],
			"--email", o["email"], "--http_server", t.URL()+"/")
		x.DeleteDir(t.DocRoot("/install"))
		x.DeleteDir(t.DocRoot(extract))
		return nil
	},
}
