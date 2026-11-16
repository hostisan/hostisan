package webapp

var osticket = &app{
	info: Info{ID: "osticket", Name: "osTicket", Group: "helpdesk", Version: "1.18.4", Database: true},
	form: []Field{
		text("helpdesk_name", "osTicket"), text("email", ""), text("first_name", ""), text("last_name", ""),
		text("username", "osAdmin"), text("admin_email", ""), password("password"),
	},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://github.com/osTicket/osTicket/releases/download/v1.18.4/osTicket-v1.18.4.zip"},
		Template: "osticket",
		PHP:      []string{"8.2", "8.3", "8.4"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		x.CopyDir(t.DocRoot("upload/."), t.DocRoot())
		x.Move(t.DocRoot("include/ost-sampleconfig.php"), t.DocRoot("include/ost-config.php"))
		x.Post(t.URL()+"/setup/install.php", F(
			"s", "install", "name", o["helpdesk_name"], "email", o["email"], "lang_id", "en_US",
			"fname", o["first_name"], "lname", o["last_name"], "admin_email", o["admin_email"],
			"username", o["username"], "passwd", o["password"], "passwd2", o["password"], "prefix", "ost_",
			"dbhost", t.DB.Host, "dbname", t.DB.Name, "dbuser", t.DB.User, "dbpass", t.DB.Password), nil, t.Resolve())
		x.DeleteDir(t.DocRoot("upload"))
		x.DeleteDir(t.DocRoot("setup"))
		return nil
	},
}
