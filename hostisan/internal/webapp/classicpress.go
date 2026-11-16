package webapp

var classicpress = &app{
	info: Info{ID: "classicpress", Name: "ClassicPress", Group: "cms", Version: "latest", Database: true},
	form: []Field{
		text("site_name", "ClassicPress Blog"), text("username", "cpadmin"), text("email", ""), password("password"),
		sel("language", "en_US", []Option{
			opt("ar_AR", "Arabic"),
			opt("zh_CN", "Chinese Simplified"),
			opt("cs_CZ", "Czech"),
			opt("nl_NL", "Dutch"),
			opt("en_GB", "English (British)"),
			opt("en_US", "English (United States)"),
			opt("fr_FR", "French"),
			opt("de_DE", "German"),
			opt("it_IT", "Italian"),
			opt("sv_SE", "Swedish"),
			opt("pt_BR", "Portuguese (Brazil)"),
		}),
		sel("search_engine_indexing", "", list("Allow", "Discourage")),
		sel("disable_XML-RPC?", "", list("Yes", "No")),
	},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://www.classicpress.net/latest.zip"},
		Template: "classicpress",
		PHP:      []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		php := o["php_version"]
		dir := x.GlobDir(t.DocRoot(), "ClassicPress-")
		x.CopyDir(dir+".", t.DocRoot())
		x.Wp(php, "config", "create", "--dbname="+t.DB.Name, "--dbuser="+t.DB.User, "--dbpass="+t.DB.Password,
			"--dbhost="+t.DB.Host, "--dbprefix=cp_"+randomString(5)+"_", "--dbcharset=utf8mb4",
			"--locale="+o["language"], "--path="+t.DocRoot())
		if o["language"] != "en_US" {
			x.Post(t.URL()+"/wp-admin/install.php?step=1", F("language", o["language"]), nil, "")
		}
		public := "0"
		if o["search_engine_indexing"] == "Allow" {
			public = "1"
		}
		x.Post(t.URL()+"/wp-admin/install.php?step=2", F(
			"weblog_title", o["site_name"], "user_name", o["username"],
			"admin_password", o["password"], "admin_password2", o["password"],
			"admin_email", o["email"], "blog_public", public, "language", o["language"]), nil, t.Resolve())
		if o["disable_XML-RPC?"] == "Yes" {
			x.Wp(php, "option", "update", "disable_xml_rpc", "1", "--path="+t.DocRoot())
		}
		x.DeleteDir(dir)
		return nil
	},
}
