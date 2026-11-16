package webapp

import "strings"

var laravel = &app{
	info: Info{ID: "laravel", Name: "Laravel", Group: "framework", Version: "latest", Database: true},
	form: []Field{},
	cfg: Config{
		Resource: Resource{Kind: "composer", Src: "laravel/laravel", Dst: "/"},
		Template: "laravel",
		PHP:      []string{"8.3", "8.4", "8.5"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		php := o["php_version"]
		x.Create(t.DocRoot(".htaccess"), embeddedFile("laravel.htaccess"))
		envPath := t.DocRoot(".env")
		env := x.Read(envPath)
		for _, r := range [][2]string{
			{"DB_CONNECTION=sqlite", "DB_CONNECTION=mysql"},
			{"# DB_HOST=127.0.0.1", "DB_HOST=" + t.DB.Host},
			{"# DB_PORT=3306", "DB_PORT=3306"},
			{"# DB_DATABASE=laravel", "DB_DATABASE=" + t.DB.Name},
			{"# DB_USERNAME=root", "DB_USERNAME=" + t.DB.User},
			{"# DB_PASSWORD=", "DB_PASSWORD=" + t.DB.Password},
		} {
			env = strings.ReplaceAll(env, r[0], r[1])
		}
		x.Create(envPath, env)
		x.PHP(php, t.DocRoot("artisan"), "key:generate", "--force")
		x.PHP(php, t.DocRoot("artisan"), "migrate", "--force")
		x.Delete(t.DocRoot("database/database.sqlite"))
		return nil
	},
}
