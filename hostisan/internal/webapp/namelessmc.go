package webapp

var namelessmc = &app{
	info: Info{ID: "namelessmc", Name: "NamelessMC", Group: "cms", Version: "2.2.5", Database: false},
	form: []Field{sel("protocol", "https", list("http", "https"))},
	cfg: Config{
		Resource: Resource{Kind: "archive", Src: "https://github.com/NamelessMC/Nameless/releases/download/v2.2.5/nameless-deps-dist.zip"},
		Template: "default",
		PHP:      []string{"7.4", "8.0", "8.1", "8.2"},
	},
	// Nothing to do after the resources are installed.
}
