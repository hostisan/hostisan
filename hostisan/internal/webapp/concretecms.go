package webapp

var concretecms = &app{
	info: Info{ID: "concretecms", Name: "ConcreteCMS", Group: "cms", Version: "latest", Database: true},
	form: []Field{
		text("email", ""), password("password"),
		sel("language", "en_US", []Option{
			opt("en_US", "English (United States)"),
			opt("en_GB", "English (United Kingdom)"),
			opt("cs_CZ", "čeština (Česko)"),
			opt("da_DK", "dansk (Danmark)"),
			opt("de_DE", "Deutsch (Deutschland)"),
			opt("de_CH", "Deutsch (Schweiz)"),
			opt("fr_FR", "français (France)"),
			opt("it_IT", "italiano (Italia)"),
			opt("nl_BE", "Nederlands (België)"),
			opt("nl_NL", "Nederlands (Nederland)"),
			opt("pl_PL", "polski (Polska)"),
			opt("fi_FI", "suomi (Suomi)"),
			opt("tr_TR", "Türkçe (Türkiye)"),
			opt("el_GR", "Ελληνικά (Ελλάδα)"),
			opt("ru_RU", "русский (Россия)"),
			opt("uk_UA", "українська (Україна)"),
			opt("zh_CN", "中文 (中国)"),
			opt("ja_JP", "日本語 (日本)"),
		}),
	},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://www.concretecms.org/download/latest.zip"},
		Template: "default",
		PHP:      []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		dir := x.GlobDir(t.DocRoot(), "concrete-cms-")
		x.CopyDir(dir+".", t.DocRoot())
		x.PHP(o["php_version"], t.DocRoot("concrete/bin/concrete"), "c5:install", "--no-interaction",
			"--db-server="+t.DB.Host, "--db-database="+t.DB.Name, "--db-username="+t.DB.User,
			"--db-password="+t.DB.Password, "--admin-email="+o["email"], "--admin-password="+o["password"],
			"--language="+o["language"], "--site-locale="+o["language"])
		x.DeleteDir(dir)
		return nil
	},
}
