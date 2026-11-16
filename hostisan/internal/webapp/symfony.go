package webapp

var symfony = &app{
	info: Info{ID: "symfony", Name: "Symfony", Group: "framework", Version: "latest", Database: false},
	form: []Field{},
	cfg: Config{
		Resource: Resource{Kind: "composer", Src: "symfony/skeleton", Dst: "/"},
		Template: "symfony4-5",
		PHP:      []string{"8.4", "8.5"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		php := o["php_version"]
		x.Create(t.DocRoot(".htaccess"), embeddedFile("symfony.htaccess"))
		x.Composer(php, "config", "-d", t.DocRoot(), "extra.symfony.allow-contrib", "true")
		x.Composer(php, "require", "-d", t.DocRoot(), "webapp")
		x.Composer(php, "require", "-d", t.DocRoot(), "symfony/apache-pack")
		return nil
	},
}
