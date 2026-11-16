package webapp

var prestashop = &app{
	info: Info{ID: "prestashop", Name: "PrestaShop", Group: "ecommerce", Version: "9.1.5", Database: true},
	form: []Field{
		text("shop_name", "PrestaShop"), text("first_name", ""), text("last_name", ""),
		sel("language", "en", []Option{
			opt("en", "English"),
			opt("ar", "العربية (Arabic)"),
			opt("bs", "Bosanski (Bosnian)"),
			opt("bg", "български език (Bulgarian)"),
			opt("ca", "Català (Catalan)"),
			opt("cs", "Čeština (Czech)"),
			opt("da", "Dansk (Danish)"),
			opt("de", "Deutsch (German)"),
			opt("et", "Eesti keel (Estonian)"),
			opt("es", "Español (Spanish)"),
			opt("mx", "Español MX (Spanish)"),
			opt("fr", "Français (French)"),
			opt("qc", "Français CA (French)"),
			opt("gl", "Galego (Galician)"),
			opt("el", "ελληνικά (Greek)"),
			opt("ko", "한국어 (Korean)"),
			opt("hr", "Hrvatski (Croatian)"),
			opt("id", "Indonesia (Indonesian)"),
			opt("it", "Italiano (Italian)"),
			opt("ja", "日本語 (Japanese)"),
			opt("lv", "Latvija (Latvian)"),
			opt("lt", "lietuvių kalba (Lithuanian)"),
			opt("mk", "македонски јазик (Macedonian)"),
			opt("hu", "Magyar (Hungarian)"),
			opt("nl", "Nederlands (Dutch)"),
			opt("no", "Norsk (Norwegian)"),
			opt("fa", "پارسی (Persian)"),
			opt("pl", "Polski (Polish)"),
			opt("br", "Português (Brasil)"),
			opt("pt", "Português (Portuguese)"),
			opt("ro", "Română (Romanian)"),
			opt("ru", "Русский (Russian)"),
			opt("sr", "Srpski (Serbian)"),
			opt("sq", "Shqip (Albanian)"),
			opt("sk", "Slovenčina (Slovak)"),
			opt("si", "slovenski jezik (Slovene)"),
			opt("fi", "Suomi (Finnish)"),
			opt("sv", "svenska (Swedish)"),
			opt("tr", "Türkçe (Turkish)"),
			opt("uk", "Українська (Ukrainian)"),
			opt("vn", "tiếng Việt (Vietnamese)"),
			opt("he", "עברית (Hebrew)"),
			opt("hi", "हिन्दी (Hindi)"),
			opt("bn", "বাংলা (Bengali)"),
			opt("tw", "繁體中文 (Traditional Chinese)"),
			opt("zh", "简体字 (Simplified Chinese)"),
		}),
		text("email", ""), password("password"),
	},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://assets.prestashop3.com/dst/edition/corporate/9.1.5-5.0/prestashop_edition_classic_version_9.1.5-5.0.zip?source=hestiacp"},
		Template: "prestashop",
		PHP:      []string{"8.1", "8.2", "8.3", "8.4", "8.5"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		// Remove the browser-based installer.
		x.Delete(t.DocRoot("/index.php"))
		x.Delete(t.DocRoot("/Install_PrestaShop.html"))
		x.Extract(t.DocRoot("/prestashop.zip"), t.DocRoot("/"))
		ssl := ""
		if t.SSL {
			ssl = "1"
		}
		x.PHP(o["php_version"], t.DocRoot("/install/index_cli.php"),
			"--db_server="+t.DB.Host, "--db_user="+t.DB.User, "--db_password="+t.DB.Password,
			"--db_name="+t.DB.Name, "--name="+o["shop_name"], "--firstname="+o["first_name"],
			"--lastname="+o["last_name"], "--language="+o["language"], "--password="+o["password"],
			"--email="+o["email"], "--domain="+t.Domain, "--ssl="+ssl)
		x.DeleteDir(t.DocRoot("/install"))
		x.Delete(t.DocRoot("/prestashop.zip"))
		return nil
	},
}
