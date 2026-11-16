package webapp

import "golang.org/x/crypto/bcrypt"

var grav = &app{
	info: Info{ID: "grav", Name: "Grav", Group: "cms", Version: "2.0.24", Database: false},
	form: []Field{text("full_name", ""), text("username", "gravadmin"), password("password"), text("email", "")},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://github.com/getgrav/grav/releases/download/2.0.24/grav-admin-v2.0.24.zip"},
		Template: "grav",
		PHP:      []string{"8.3", "8.4", "8.5"},
	},
	setup: func(x *Seq, t *Target, o map[string]string) error {
		x.CopyDir(t.DocRoot("grav-admin/."), t.DocRoot())
		// bcrypt reads at most 72 bytes; PHP's password_hash cuts there too.
		pw := []byte(o["password"])
		if len(pw) > 72 {
			pw = pw[:72]
		}
		hash, err := bcrypt.GenerateFromPassword(pw, bcrypt.DefaultCost)
		if err != nil {
			return Fail("Error creating temp file")
		}
		yaml := "state: enabled\n" +
			"email: " + o["email"] + "\n" +
			"fullname: \"" + o["full_name"] + "\"\n" +
			"title: Administrator\n" +
			"access:\n" +
			"  site:\n" +
			"    login: true\n" +
			"  api:\n" +
			"    super: true\n" +
			"hashed_password: '" + string(hash) + "'\n"
		x.Create(t.DocRoot("user/accounts/"+o["username"]+".yaml"), yaml)
		x.DeleteDir(t.DocRoot("grav-admin"))
		return nil
	},
}
