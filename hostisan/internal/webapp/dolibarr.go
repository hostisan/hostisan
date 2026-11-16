package webapp

var dolibarr = &app{
	info: Info{ID: "dolibarr", Name: "Dolibarr", Group: "crm", Version: "24.0.1", Database: true},
	form: []Field{
		text("username", "doliadmin"), password("password"),
		sel("language", "en_EN", []Option{
			opt("en_EN", "English"),
			opt("fr_FR", "French"),
			opt("de_DE", "German"),
			opt("es_ES", "Spanish"),
			opt("it_IT", "Italian"),
			opt("pt_PT", "Portuguese"),
		}),
	},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://github.com/Dolibarr/dolibarr/archive/refs/tags/24.0.1.zip"},
		Template: "dolibarr",
		PHP:      []string{"8.1", "8.2", "8.3", "8.4"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		src := t.DocRoot("/dolibarr-24.0.1/")
		x.CopyDir(t.DocRoot("/dolibarr-24.0.1/."), t.DocRoot())
		lang := o["language"]
		conf := t.DocRoot("htdocs/conf/conf.php")
		x.Move(t.DocRoot("htdocs/conf/conf.php.example"), conf)
		x.Chmod(conf, "666")
		x.AddDir(t.DocRoot("documents"))
		x.Create(t.DocRoot(".htaccess"), embeddedFile("dolibarr.htaccess"))
		https := "0"
		if t.SSL {
			https = "1"
		}
		x.Post(t.URL()+"/install/step1.php", F(
			"testpost", "ok", "action", "set", "main_dir", t.DocRoot("htdocs"),
			"main_data_dir", t.DocRoot("documents"), "main_url", t.URL(), "main_force_https", https,
			"db_type", "mysqli", "db_host", t.DB.Host, "db_port", "3306", "db_prefix", "llx_",
			"db_name", t.DB.Name, "db_user", t.DB.User, "db_pass", t.DB.Password, "selectlang", lang), nil, "")
		x.Post(t.URL()+"/install/step2.php", F(
			"testpost", "ok", "action", "set", "dolibarr_main_db_character_set", "utf8",
			"dolibarr_main_db_collation", "utf8_unicode_ci", "selectlang", lang), nil, "")
		x.Pause(10) // give step 2 time to finish
		x.Post(t.URL()+"/install/step5.php", F(
			"testpost", "ok", "action", "set", "login", o["username"], "pass", o["password"],
			"pass_verif", o["password"], "installlock", "1", "selectlang", lang), nil, t.Resolve())
		x.Chmod(conf, "400")
		x.DeleteDir(src)
		return nil
	},
}
