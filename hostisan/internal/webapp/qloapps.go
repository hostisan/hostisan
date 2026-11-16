package webapp

var qloapps = &app{
	info: Info{ID: "qloapps", Name: "QloApps", Group: "ecommerce", Version: "1.7.0", Database: true},
	form: []Field{
		text("first_name", ""), text("last_name", ""),
		sel("language", "en", []Option{
			opt("en", "English"),
			opt("ca", "Català (Catalan)"),
			opt("de", "Deutsch (German)"),
			opt("es", "Español (Spanish)"),
			opt("fr", "Français (French)"),
			opt("qc", "Français CA (French)"),
			opt("hr", "Hrvatski (Croatian)"),
			opt("id", "Indonesia (Indonesian)"),
			opt("it", "Italiano (Italian)"),
			opt("hu", "Magyar (Hungarian)"),
			opt("nl", "Nederlands (Dutch)"),
			opt("no", "Norsk (Norwegian)"),
			opt("pl", "Polski (Polish)"),
			opt("br", "Português (Brasil)"),
			opt("pt", "Português (Portuguese)"),
			opt("ro", "Română (Romanian)"),
			opt("sr", "Srpski (Serbian)"),
			opt("tr", "Türkçe (Turkish)"),
			opt("lt", "lietuvių kalba (Lithuanian)"),
			opt("si", "slovenski jezik (Slovene)"),
			opt("sv", "svenska (Swedish)"),
			opt("vn", "tiếng Việt (Vietnamese)"),
			opt("cs", "Čeština (Czech)"),
			opt("ru", "Русский (Russian)"),
			opt("bg", "български език (Bulgarian)"),
			opt("mk", "македонски јазик (Macedonian)"),
			opt("he", "עברית (Hebrew)"),
			opt("fa", "پارسی (Persian)"),
			opt("bn", "বাংলা (Bengali)"),
			opt("tw", "正體中文 (Traditional Chinese)"),
			opt("zh", "简体字 (Simplified Chinese)"),
		}),
		text("email", ""), password("password"),
	},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://github.com/Qloapps/QloApps/releases/download/v1.7.0/qloapps-1.7.0.zip"},
		Template: "default",
		PHP:      []string{"8.1", "8.2", "8.3", "8.4"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		x.CopyDir(t.DocRoot("/qloapps-1.7.0/."), t.DocRoot())
		x.PHP(o["php_version"], t.DocRoot("/install/index_cli.php"),
			"--domain="+t.Domain, "--db_server="+t.DB.Host, "--db_name="+t.DB.Name, "--db_user="+t.DB.User,
			"--db_password="+t.DB.Password, "--firstname="+o["first_name"], "--lastname="+o["last_name"],
			"--language="+o["language"], "--password="+o["password"], "--email="+o["email"])
		x.DeleteDir(t.DocRoot("/install"))
		x.DeleteDir(t.DocRoot("/qloapps-1.7.0"))
		return nil
	},
}
