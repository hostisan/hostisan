package webapp

var wordpress = &app{
	info: Info{ID: "wordpress", Name: "WordPress", Group: "cms", Version: "latest", Database: true},
	form: []Field{
		text("site_name", "WordPress Blog"), text("username", "wpadmin"), text("email", ""), password("password"),
		sel("language", "en_US", []Option{
			opt("cs_CZ", "Czech"),
			opt("de_DE", "German"),
			opt("es_ES", "Spanish"),
			opt("en_US", "English"),
			opt("fr_FR", "French"),
			opt("hu_HU", "Hungarian"),
			opt("it_IT", "Italian"),
			opt("ja", "Japanese"),
			opt("nl_NL", "Dutch"),
			opt("pt_PT", "Portuguese"),
			opt("pt_BR", "Portuguese (Brazil)"),
			opt("sk_SK", "Slovak"),
			opt("sr_RS", "Serbian"),
			opt("sv_SE", "Swedish"),
			opt("tr_TR", "Turkish"),
			opt("ru_RU", "Russian"),
			opt("uk", "Ukrainian"),
			opt("zh-CN", "Simplified Chinese (China)"),
			opt("zh_TW", "Traditional Chinese"),
		}),
	},
	cfg: Config{
		Resource: Resource{Kind: "wp", Src: "https://wordpress.org/latest.tar.gz"},
		Template: "wordpress",
		PHP:      []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		x.Wp(o["php_version"], "config", "create", "--dbname="+t.DB.Name, "--dbuser="+t.DB.User,
			"--dbpass="+t.DB.Password, "--dbhost="+t.DB.Host, "--dbprefix=wp_"+randomString(5)+"_",
			"--dbcharset=utf8mb4", "--locale="+o["language"], "--path="+t.DocRoot())
		// Hestia installs through install.php because v-run-cli-cmd used to split a
		// site name with a space; kept as Hestia does it. The URL has no doubled "/"
		// (gotcha 13).
		x.Post(t.URL()+"/wp-admin/install.php?step=2", F(
			"weblog_title", o["site_name"], "user_name", o["username"],
			"admin_password", o["password"], "admin_password2", o["password"],
			"admin_email", o["email"]), nil, t.Resolve())
		return nil
	},
}
