package webapp

import "strings"

var castopod = &app{
	info: Info{ID: "castopod", Name: "Castopod", Group: "podcasting", Version: "1.15.5", Database: true},
	form: []Field{text("admin_gateway", "cp-admin"), text("auth_gateway", "cp-auth")},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://code.castopod.org/-/project/2/uploads/419c795a5ab306a27082b7d86aa43df6/castopod-1.15.5.zip"},
		Template: "castopod",
		PHP:      []string{"8.1", "8.2", "8.3", "8.4", "8.5"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		x.CopyDir(t.DocRoot("/castopod/."), t.DocRoot())
		x.Create(t.DocRoot(".htaccess"), embeddedFile("castopod.htaccess"))
		x.Move(t.DocRoot(".env.example"), t.DocRoot(".env"))
		envPath := t.DocRoot(".env")
		env := x.Read(envPath)
		for _, r := range [][2]string{
			{`app.baseURL="https://YOUR_DOMAIN_NAME/"`, "app.baseURL=" + t.URL()},
			{`media.baseURL="https://YOUR_MEDIA_DOMAIN_NAME/"`, "media.baseURL=" + t.URL()},
			{`admin.gateway="cp-admin"`, "admin.gateway=" + o["admin_gateway"]},
			{`auth.gateway="cp-auth"`, "auth.gateway=" + o["auth_gateway"]},
			{`analytics.salt="RANDOM_STRING_OF_64_CHARACTERS"`, "analytics.salt=" + randomString(64)},
			{`database.default.hostname="localhost"`, "database.default.hostname=" + t.DB.Host},
			{`database.default.database="castopod"`, "database.default.database=" + t.DB.Name},
			{`database.default.username="root"`, "database.default.username=" + t.DB.User},
			{`database.default.password="****"`, "database.default.password=" + t.DB.Password},
		} {
			env = strings.ReplaceAll(env, r[0], r[1])
		}
		x.Create(envPath, env)
		x.PHP(o["php_version"], t.DocRoot("spark"), "install:init-database")
		x.DeleteDir(t.DocRoot("/castopod"))
		return nil
	},
}
