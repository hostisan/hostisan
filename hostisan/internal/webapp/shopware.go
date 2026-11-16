package webapp

import "strings"

var shopware = &app{
	info: Info{ID: "shopware", Name: "Shopware", Group: "ecommerce", Version: "latest", Database: true},
	form: []Field{
		text("shop_name", "Shopware"), text("shop_email", ""),
		sel("shop_locale", "en-GB", []Option{
			opt("en-GB", "English (UK)"),
			opt("en-US", "English (US)"),
			opt("ach-UG", "Lwo (Acholi)"),
			opt("af-ZA", "Afrikaans - South Africa"),
			opt("ar-SA", "العربية (Arabic - Saudi Arabia)"),
			opt("bg-BG", "български език (Bulgarian)"),
			opt("bs-BA", "Bosanski (Bosnian)"),
			opt("ca-ES", "Català (Catalan)"),
			opt("cs-CZ", "Čeština (Czech)"),
			opt("da-DK", "Dansk (Danish)"),
			opt("de-AT", "Deutsch (German - Austria)"),
			opt("de-CH", "Deutsch (German - Switzerland)"),
			opt("de-DE", "Deutsch (German)"),
			opt("el-GR", "Ελληνικά (Greek)"),
			opt("es-AR", "Español (Argentina)"),
			opt("es-ES", "Español (Spanish)"),
			opt("et-EE", "Eesti keel (Estonian)"),
			opt("fi-FI", "Suomi (Finnish)"),
			opt("fr-FR", "Français (French)"),
			opt("hi-IN", "हिन्दी (Hindi)"),
			opt("hr-HR", "Hrvatski (Croatian)"),
			opt("hu-HU", "Magyar (Hungarian)"),
			opt("hy-AM", "Հայերեն (Armenian)"),
			opt("id-ID", "Indonesia (Indonesian)"),
			opt("it-IT", "Italiano (Italian)"),
			opt("ja-JP", "日本語 (Japanese)"),
			opt("ko-KR", "한국어 (Korean)"),
			opt("lt-LT", "lietuvių kalba (Lithuanian)"),
			opt("lv-LV", "Latviešu (Latvian)"),
			opt("nl-NL", "Nederlands (Dutch)"),
			opt("nn-NO", "Norsk Nynorsk (Norwegian)"),
			opt("pl-PL", "Polski (Polish)"),
			opt("pt-PT", "Português (Portuguese)"),
			opt("ro-RO", "Română (Romanian)"),
			opt("ru-RU", "Русский (Russian)"),
			opt("sk-SK", "Slovenčina (Slovak)"),
			opt("sl-SI", "Slovenščina (Slovenian)"),
			opt("sq-AL", "Shqip (Albanian)"),
			opt("sr-RS", "Srpski (Serbian)"),
			opt("sv-SE", "Svenska (Swedish)"),
			opt("th-TH", "ภาษาไทย (Thai)"),
			opt("tr-TR", "Türkçe (Turkish)"),
			opt("uk-UA", "Українська (Ukrainian)"),
			opt("vi-VN", "Tiếng Việt (Vietnamese)"),
		}),
		text("shop_currency", "USD"), text("username", "shopadmin"), text("first_name", ""), text("last_name", ""),
		text("email", ""), password("password"),
	},
	cfg: Config{
		Resource: Resource{Kind: "composer", Src: "shopware/production", Dst: "/"},
		Template: "shopware",
		PHP:      []string{"8.2", "8.3", "8.4", "8.5"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		php := o["php_version"]
		console := t.DocRoot("bin/console")
		x.Create(t.DocRoot(".htaccess"), embeddedFile("shopware.htaccess"))
		envPath := t.DocRoot(".env")
		env := x.Read(envPath)
		env = strings.ReplaceAll(env, "APP_URL=http://127.0.0.1:8000", "APP_URL="+t.URL())
		env = strings.ReplaceAll(env, "DATABASE_URL=mysql://root:root@localhost/shopware",
			"DATABASE_URL=mysql://"+t.DB.User+":"+t.DB.Password+"@"+t.DB.Host+":3306/"+t.DB.Name)
		x.Create(envPath, env)
		x.PHP(php, console, "system:install", "-n", "--shop-name="+o["shop_name"], "--shop-email="+o["shop_email"],
			"--shop-locale="+o["shop_locale"], "--shop-currency="+o["shop_currency"], "--basic-setup")
		if o["shop_locale"] != "en-GB" && o["shop_locale"] != "de-DE" {
			x.PHP(php, console, "translation:install", "-n", "--locales="+o["shop_locale"])
		}
		x.PHP(php, console, "user:create", o["username"], "-a", "--firstName="+o["first_name"],
			"--lastName="+o["last_name"], "--password="+o["password"], "--email="+o["email"], "-n")
		return nil
	},
}
