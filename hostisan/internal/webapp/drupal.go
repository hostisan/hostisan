package webapp

var drupal = &app{
	info: Info{ID: "drupal", Name: "Drupal", Group: "cms", Version: "latest", Database: true},
	form: []Field{text("username", "admin"), password("password"), text("email", "")},
	cfg: Config{
		Resource: Resource{Kind: "composer", Src: "drupal/recommended-project", Dst: "/"},
		Template: "drupal-composer",
		PHP:      []string{"8.1", "8.2", "8.3", "8.4", "8.5"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		php := o["php_version"]
		x.Create(t.DocRoot(".htaccess"), `<IfModule mod_rewrite.c>
                    RewriteEngine On
                    RewriteRule ^(.*)$ web/$1 [L]
            </IfModule>`)
		x.Composer(php, "require", "-d", t.DocRoot(), "drush/drush")
		dbURL := "mysql://" + t.DB.User + ":" + t.DB.Password + "@" + t.DB.Host + ":3306/" + t.DB.Name
		x.PHP(php, t.DocRoot("/vendor/drush/drush/drush.php"), "site-install", "standard",
			"--db-url="+dbURL, "--account-name="+o["username"], "--account-pass="+o["password"],
			"--site-name=Drupal", "--site-mail="+o["email"])
		return nil
	},
}
