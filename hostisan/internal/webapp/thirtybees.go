package webapp

var thirtybees = &app{
	info: Info{ID: "thirtybees", Name: "ThirtyBees", Group: "ecommerce", Version: "1.7.0", Database: true},
	form: []Field{text("store_name", "thirty bees"), text("first_name", ""), text("last_name", ""), text("email", ""), password("password")},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://github.com/thirtybees/thirtybees/releases/download/1.7.0/thirtybees-v1.7.0-php7.4.zip"},
		Template: "prestashop",
		PHP:      []string{"7.4", "8.0", "8.1", "8.2", "8.3"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		x.PHP(o["php_version"], t.DocRoot("/install/index_cli.php"),
			"--db_server="+t.DB.Host, "--db_user="+t.DB.User, "--db_password="+t.DB.Password,
			"--db_name="+t.DB.Name, "--name="+o["store_name"], "--firstname="+o["first_name"],
			"--lastname="+o["last_name"], "--password="+o["password"], "--email="+o["email"],
			"--domain="+t.Domain)
		x.DeleteDir(t.DocRoot("/install"))
		return nil
	},
}
