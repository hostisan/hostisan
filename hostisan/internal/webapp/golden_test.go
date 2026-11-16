package webapp

// Golden tests: each installer's own steps (everything after the backend-template
// call), written out from the PHP *Setup.php sources in web/src/app/WebApp/Installers.

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	gRoot = "/home/u/web/d.example/public_html"
	gWP   = "v-add-user-wp-cli u yes"
	gComp = "v-add-user-composer u 2 yes"
)

// gBody is PHP's http_build_query for ordered pairs.
func gBody(kv ...string) string {
	var parts []string
	for i := 0; i+1 < len(kv); i += 2 {
		parts = append(parts, url.QueryEscape(kv[i])+"="+url.QueryEscape(kv[i+1]))
	}
	return strings.Join(parts, "&")
}

func gPHP(v, script string, args ...string) string {
	// Arguments reach v-run-cli-cmd as they are (B2c): PHP's "\ " escaping is gone.
	return strings.Join(append([]string{"v-run-cli-cmd u /usr/bin/php" + v, script}, args...), " ")
}

func gHtaccess(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile("../../../web/src/app/WebApp/Installers/" + dir + "/.htaccess")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type golden struct {
	name, id, php string
	ssl           bool
	opts          map[string]string
	reads         map[string]string
	calls         []string
	files         map[string]string // destination → contents, after masking
	posts         []string          // "<dialed address> <request URI> <body>"
}

func TestInstallerSteps(t *testing.T) {
	cp := "ClassicPress-release-2.0/"
	cases := []golden{
		{
			name: "castopod", id: "castopod", php: "8.4",
			opts: map[string]string{"admin_gateway": "my-admin", "auth_gateway": "my-auth"},
			reads: map[string]string{gRoot + "/.env": "app.baseURL=\"https://YOUR_DOMAIN_NAME/\"\n" +
				"media.baseURL=\"https://YOUR_MEDIA_DOMAIN_NAME/\"\nadmin.gateway=\"cp-admin\"\nauth.gateway=\"cp-auth\"\n" +
				"analytics.salt=\"RANDOM_STRING_OF_64_CHARACTERS\"\ndatabase.default.hostname=\"localhost\"\n" +
				"database.default.database=\"castopod\"\ndatabase.default.username=\"root\"\ndatabase.default.password=\"****\"\n"},
			calls: []string{
				"v-copy-fs-directory u " + gRoot + "/castopod/. " + gRoot,
				"v-copy-fs-file u <T> " + gRoot + "/.htaccess",
				"v-move-fs-file u " + gRoot + "/.env.example " + gRoot + "/.env",
				"v-open-fs-file u " + gRoot + "/.env",
				"v-copy-fs-file u <T> " + gRoot + "/.env",
				gPHP("8.4", gRoot+"/spark", "install:init-database"),
				"v-delete-fs-directory u " + gRoot + "/castopod",
			},
			files: map[string]string{
				gRoot + "/.env": "app.baseURL=http://d.example\nmedia.baseURL=http://d.example\nadmin.gateway=my-admin\n" +
					"auth.gateway=my-auth\nanalytics.salt=SALT\ndatabase.default.hostname=localhost\n" +
					"database.default.database=u_dbn\ndatabase.default.username=u_dbu\ndatabase.default.password=dbpw\n",
				gRoot + "/.htaccess": gHtaccess(t, "Castopod"),
			},
		},
		{
			name: "classicpress", id: "classicpress", php: "8.4", ssl: true,
			opts: map[string]string{"site_name": "My CP", "username": "cpadmin", "email": "e@x.org", "password": "pw1",
				"language": "de_DE", "search_engine_indexing": "Allow", "disable_XML-RPC?": "Yes"},
			calls: []string{
				"v-run-cli-cmd u ls -A -p " + gRoot,
				"v-copy-fs-directory u " + gRoot + "/" + cp + ". " + gRoot,
				gWP,
				gPHP("8.4", "/home/u/.wp-cli/wp", "config", "create", "--dbname=u_dbn", "--dbuser=u_dbu", "--dbpass=dbpw",
					"--dbhost=localhost", "--dbprefix=cp_RAND_", "--dbcharset=utf8mb4", "--locale=de_DE", "--path="+gRoot),
				gWP,
				gPHP("8.4", "/home/u/.wp-cli/wp", "option", "update", "disable_xml_rpc", "1", "--path="+gRoot),
				"v-delete-fs-directory u " + gRoot + "/" + cp,
			},
			posts: []string{
				"d.example:443 /wp-admin/install.php?step=1 " + gBody("language", "de_DE"),
				"203.0.113.5:443 /wp-admin/install.php?step=2 " + gBody("weblog_title", "My CP", "user_name", "cpadmin",
					"admin_password", "pw1", "admin_password2", "pw1", "admin_email", "e@x.org", "blog_public", "1", "language", "de_DE"),
			},
		},
		{
			name: "classicpress discourage, English, XML-RPC kept", id: "classicpress", php: "8.4",
			opts: map[string]string{"site_name": "S", "username": "u1", "email": "e@x.org", "password": "p",
				"language": "en_US", "search_engine_indexing": "Discourage", "disable_XML-RPC?": "No"},
			calls: []string{
				"v-run-cli-cmd u ls -A -p " + gRoot,
				"v-copy-fs-directory u " + gRoot + "/" + cp + ". " + gRoot,
				gWP,
				gPHP("8.4", "/home/u/.wp-cli/wp", "config", "create", "--dbname=u_dbn", "--dbuser=u_dbu", "--dbpass=dbpw",
					"--dbhost=localhost", "--dbprefix=cp_RAND_", "--dbcharset=utf8mb4", "--locale=en_US", "--path="+gRoot),
				"v-delete-fs-directory u " + gRoot + "/" + cp,
			},
			posts: []string{
				"203.0.113.5:80 /wp-admin/install.php?step=2 " + gBody("weblog_title", "S", "user_name", "u1",
					"admin_password", "p", "admin_password2", "p", "admin_email", "e@x.org", "blog_public", "0", "language", "en_US"),
			},
		},
		{
			name: "concretecms", id: "concretecms", php: "8.4",
			opts: map[string]string{"email": "e@x.org", "password": "pw1", "language": "en_US"},
			calls: []string{
				"v-run-cli-cmd u ls -A -p " + gRoot,
				"v-copy-fs-directory u " + gRoot + "/concrete-cms-9.3/. " + gRoot,
				gPHP("8.4", gRoot+"/concrete/bin/concrete", "c5:install", "--no-interaction", "--db-server=localhost",
					"--db-database=u_dbn", "--db-username=u_dbu", "--db-password=dbpw", "--admin-email=e@x.org",
					"--admin-password=pw1", "--language=en_US", "--site-locale=en_US"),
				"v-delete-fs-directory u " + gRoot + "/concrete-cms-9.3/",
			},
		},
		{
			name: "dokuwiki", id: "dokuwiki", php: "8.4",
			opts: map[string]string{"wiki_name": "My Wiki", "superuser": "root", "real_name": "Ann A", "email": "e@x.org",
				"password": "pw1", "initial_ACL_policy": "1: Public Wiki (read for everyone, write and upload for registered users)",
				"content_license": "cc-by-sa: CC Attribution-Share Alike 4.0 International"},
			calls: []string{
				"v-move-fs-file u " + gRoot + "/.htaccess.dist " + gRoot + "/.htaccess",
				"v-delete-fs-file u " + gRoot + "/install.php",
			},
			posts: []string{"203.0.113.5:80 /install.php " + gBody("l", "en", "d[title]", "My Wiki", "d[acl]", "on",
				"d[superuser]", "root", "d[fullname]", "Ann A", "d[email]", "e@x.org", "d[password]", "pw1", "d[confirm]", "pw1",
				"d[policy]", "1", "d[license]", "cc-by-sa", "submit", "")},
		},
		{
			name: "dolibarr", id: "dolibarr", php: "8.4", ssl: true,
			opts: map[string]string{"username": "doliadmin", "password": "pw1", "language": "en_EN"},
			calls: []string{
				"v-copy-fs-directory u " + gRoot + "/dolibarr-24.0.1/. " + gRoot,
				"v-move-fs-file u " + gRoot + "/htdocs/conf/conf.php.example " + gRoot + "/htdocs/conf/conf.php",
				"v-change-fs-file-permission u " + gRoot + "/htdocs/conf/conf.php 666",
				"v-add-fs-directory u " + gRoot + "/documents",
				"v-copy-fs-file u <T> " + gRoot + "/.htaccess",
				"v-change-fs-file-permission u " + gRoot + "/htdocs/conf/conf.php 400",
				"v-delete-fs-directory u " + gRoot + "/dolibarr-24.0.1/",
			},
			files: map[string]string{gRoot + "/.htaccess": gHtaccess(t, "Dolibarr")},
			posts: []string{
				"d.example:443 /install/step1.php " + gBody("testpost", "ok", "action", "set", "main_dir", gRoot+"/htdocs",
					"main_data_dir", gRoot+"/documents", "main_url", "https://d.example", "main_force_https", "1", "db_type", "mysqli",
					"db_host", "localhost", "db_port", "3306", "db_prefix", "llx_", "db_name", "u_dbn", "db_user", "u_dbu",
					"db_pass", "dbpw", "selectlang", "en_EN"),
				"d.example:443 /install/step2.php " + gBody("testpost", "ok", "action", "set", "dolibarr_main_db_character_set", "utf8",
					"dolibarr_main_db_collation", "utf8_unicode_ci", "selectlang", "en_EN"),
				"203.0.113.5:443 /install/step5.php " + gBody("testpost", "ok", "action", "set", "login", "doliadmin", "pass", "pw1",
					"pass_verif", "pw1", "installlock", "1", "selectlang", "en_EN"),
			},
		},
		{
			name: "drupal", id: "drupal", php: "8.4",
			opts: map[string]string{"username": "admin", "password": "pw1", "email": "e@x.org"},
			calls: []string{
				"v-copy-fs-file u <T> " + gRoot + "/.htaccess",
				gComp,
				gPHP("8.4", "/home/u/.composer/composer", "require", "-d", gRoot, "drush/drush"),
				gPHP("8.4", gRoot+"/vendor/drush/drush/drush.php", "site-install", "standard",
					"--db-url=mysql://u_dbu:dbpw@localhost:3306/u_dbn", "--account-name=admin", "--account-pass=pw1",
					"--site-name=Drupal", "--site-mail=e@x.org"),
			},
			files: map[string]string{gRoot + "/.htaccess": "<IfModule mod_rewrite.c>\n" +
				"                    RewriteEngine On\n" +
				"                    RewriteRule ^(.*)$ web/$1 [L]\n" +
				"            </IfModule>"},
		},
		{
			name: "flarum", id: "flarum", php: "8.3",
			opts:  map[string]string{"forum_title": "My Forum", "admin_username": "fladmin", "admin_email": "e@x.org", "admin_password": "pw1"},
			calls: []string{"v-copy-fs-file u <T> " + gRoot + "/.htaccess"},
			files: map[string]string{gRoot + "/.htaccess": gHtaccess(t, "Flarum")},
			posts: []string{"203.0.113.5:80 / " + gBody("forumTitle", "My Forum", "mysqlHost", "localhost", "mysqlDatabase", "u_dbn",
				"mysqlUsername", "u_dbu", "mysqlPassword", "dbpw", "tablePrefix", "flRAND", "adminUsername", "fladmin",
				"adminEmail", "e@x.org", "adminPassword", "pw1", "adminPasswordConfirmation", "pw1")},
		},
		{
			name: "grav", id: "grav", php: "8.4",
			opts: map[string]string{"full_name": "Ann A", "username": "ann", "password": "pw1", "email": "e@x.org"},
			calls: []string{
				"v-copy-fs-directory u " + gRoot + "/grav-admin/. " + gRoot,
				"v-copy-fs-file u <T> " + gRoot + "/user/accounts/ann.yaml",
				"v-delete-fs-directory u " + gRoot + "/grav-admin",
			},
			files: map[string]string{gRoot + "/user/accounts/ann.yaml": "state: enabled\nemail: e@x.org\nfullname: \"Ann A\"\n" +
				"title: Administrator\naccess:\n  site:\n    login: true\n  api:\n    super: true\nhashed_password: 'HASH'\n"},
		},
		{
			name: "joomla", id: "joomla", php: "8.4",
			opts: map[string]string{"site_name": "My Joomla", "admin_user": "Ann", "admin_username": "joomlaadmin", "password": "pw1", "email": "e@x.org"},
			calls: []string{
				"v-move-fs-file u " + gRoot + "/htaccess.txt " + gRoot + "/.htaccess",
				gPHP("8.4", gRoot+"/installation/joomla.php", "install", "--site-name=My Joomla", "--admin-user=Ann",
					"--admin-username=joomlaadmin", "--admin-password=pw1", "--admin-email=e@x.org", "--db-user=u_dbu",
					"--db-pass=dbpw", "--db-name=u_dbn", "--db-host=localhost", "--db-type=mysqli", "--db-encryption=0"),
			},
		},
		{
			name: "laravel", id: "laravel", php: "8.4",
			reads: map[string]string{gRoot + "/.env": "APP_NAME=Laravel\nDB_CONNECTION=sqlite\n# DB_HOST=127.0.0.1\n# DB_PORT=3306\n" +
				"# DB_DATABASE=laravel\n# DB_USERNAME=root\n# DB_PASSWORD=\n"},
			calls: []string{
				"v-copy-fs-file u <T> " + gRoot + "/.htaccess",
				"v-open-fs-file u " + gRoot + "/.env",
				"v-copy-fs-file u <T> " + gRoot + "/.env",
				gPHP("8.4", gRoot+"/artisan", "key:generate", "--force"),
				gPHP("8.4", gRoot+"/artisan", "migrate", "--force"),
				"v-delete-fs-file u " + gRoot + "/database/database.sqlite",
			},
			files: map[string]string{
				gRoot + "/.env": "APP_NAME=Laravel\nDB_CONNECTION=mysql\nDB_HOST=localhost\nDB_PORT=3306\nDB_DATABASE=u_dbn\n" +
					"DB_USERNAME=u_dbu\nDB_PASSWORD=dbpw\n",
				gRoot + "/.htaccess": gHtaccess(t, "Laravel"),
			},
		},
		{
			name: "mediawiki", id: "mediawiki", php: "8.4", ssl: true,
			opts: map[string]string{"wiki_name": "My Wiki", "admin_username": "wikiadmin", "admin_password": "pw1", "language": "en"},
			calls: []string{
				"v-copy-fs-directory u " + gRoot + "/mediawiki-1.46.0/. " + gRoot,
				gPHP("8.4", gRoot+"/maintenance/install.php", "--dbserver=localhost", "--dbname=u_dbn", "--installdbuser=u_dbu",
					"--installdbpass=dbpw", "--dbuser=u_dbn", "--dbpass=dbpw", "--server=https://d.example", "--scriptpath=",
					"--lang=en", "--pass=pw1", "My Wiki", "wikiadmin"),
				"v-delete-fs-directory u " + gRoot + "/mediawiki-1.46.0/",
			},
		},
		{
			name: "nextcloud", id: "nextcloud", php: "8.4",
			opts:  map[string]string{"username": "admin", "password": "pw1"},
			reads: map[string]string{gRoot + "/.user.ini": "upload_max_filesize=513M\n"},
			calls: []string{
				gPHP("8.4", gRoot+"/occ", "maintenance:install", "--database", "mysql", "--database-name", "u_dbn",
					"--database-host", "localhost", "--database-user", "u_dbu", "--database-pass", "dbpw",
					"--admin-user", "admin", "--admin-pass", "pw1"),
				gPHP("8.4", gRoot+"/occ", "config:system:set", "trusted_domains", "2", "--value=d.example"),
				"v-open-fs-file u " + gRoot + "/.user.ini",
				"v-copy-fs-file u <T> " + gRoot + "/.user.ini",
			},
			// PHP's single quotes make \r\n four literal characters.
			files: map[string]string{gRoot + "/.user.ini": "upload_max_filesize=513M\n" + `memory_limit=512M\r\n`},
		},
		{
			name: "opencart", id: "opencart", php: "8.4",
			opts: map[string]string{"username": "ocadmin", "email": "e@x.org", "password": "pw1"},
			calls: []string{
				"v-copy-fs-directory u " + gRoot + "/tmp-opencart/upload/. " + gRoot,
				"v-move-fs-file u " + gRoot + "/config-dist.php " + gRoot + "/config.php",
				"v-move-fs-file u " + gRoot + "/admin/config-dist.php " + gRoot + "/admin/config.php",
				"v-move-fs-file u " + gRoot + "/.htaccess.txt " + gRoot + "/.htaccess",
				gPHP("8.4", gRoot+"/install/cli_install.php", "install", "--db_hostname", "localhost", "--db_username", "u_dbu",
					"--db_password", "dbpw", "--db_database", "u_dbn", "--username", "ocadmin", "--password", "pw1",
					"--email", "e@x.org", "--http_server", "http://d.example/"),
				"v-delete-fs-directory u " + gRoot + "/install",
				"v-delete-fs-directory u " + gRoot + "/tmp-opencart",
			},
		},
		{
			name: "prestashop with SSL", id: "prestashop", php: "8.4", ssl: true,
			opts: map[string]string{"shop_name": "My Shop", "first_name": "Ann", "last_name": "Lee", "language": "en",
				"email": "e@x.org", "password": "pw1"},
			calls: prestaCalls("--ssl=1"),
		},
		{
			name: "prestashop without SSL", id: "prestashop", php: "8.4",
			opts: map[string]string{"shop_name": "My Shop", "first_name": "Ann", "last_name": "Lee", "language": "en",
				"email": "e@x.org", "password": "pw1"},
			calls: prestaCalls("--ssl="),
		},
		{
			name: "qloapps", id: "qloapps", php: "8.4",
			opts: map[string]string{"first_name": "Ann", "last_name": "Lee", "language": "en", "email": "e@x.org", "password": "pw1"},
			calls: []string{
				"v-copy-fs-directory u " + gRoot + "/qloapps-1.7.0/. " + gRoot,
				gPHP("8.4", gRoot+"/install/index_cli.php", "--domain=d.example", "--db_server=localhost", "--db_name=u_dbn",
					"--db_user=u_dbu", "--db_password=dbpw", "--firstname=Ann", "--lastname=Lee", "--language=en",
					"--password=pw1", "--email=e@x.org"),
				"v-delete-fs-directory u " + gRoot + "/install",
				"v-delete-fs-directory u " + gRoot + "/qloapps-1.7.0",
			},
		},
		{
			name: "shopware non-English", id: "shopware", php: "8.4",
			opts: map[string]string{"shop_name": "My Shop", "shop_email": "s@x.org", "shop_locale": "fr-FR", "shop_currency": "USD",
				"username": "shopadmin", "first_name": "Ann", "last_name": "Lee", "email": "e@x.org", "password": "pw1"},
			reads: shopwareEnv(),
			calls: shopwareCalls("fr-FR", true),
			files: shopwareFiles(t),
		},
		{
			name: "shopware en-GB", id: "shopware", php: "8.4",
			opts: map[string]string{"shop_name": "My Shop", "shop_email": "s@x.org", "shop_locale": "en-GB", "shop_currency": "USD",
				"username": "shopadmin", "first_name": "Ann", "last_name": "Lee", "email": "e@x.org", "password": "pw1"},
			reads: shopwareEnv(),
			calls: shopwareCalls("en-GB", false),
			files: shopwareFiles(t),
		},
		{
			name: "symfony", id: "symfony", php: "8.4",
			calls: []string{
				"v-copy-fs-file u <T> " + gRoot + "/.htaccess",
				gComp, gPHP("8.4", "/home/u/.composer/composer", "config", "-d", gRoot, "extra.symfony.allow-contrib", "true"),
				gComp, gPHP("8.4", "/home/u/.composer/composer", "require", "-d", gRoot, "webapp"),
				gComp, gPHP("8.4", "/home/u/.composer/composer", "require", "-d", gRoot, "symfony/apache-pack"),
			},
			files: map[string]string{gRoot + "/.htaccess": gHtaccess(t, "Symfony")},
		},
		{
			name: "thirtybees", id: "thirtybees", php: "8.3",
			opts: map[string]string{"store_name": "thirty bees", "first_name": "Ann", "last_name": "Lee", "email": "e@x.org", "password": "pw1"},
			calls: []string{
				gPHP("8.3", gRoot+"/install/index_cli.php", "--db_server=localhost", "--db_user=u_dbu", "--db_password=dbpw",
					"--db_name=u_dbn", "--name=thirty bees", "--firstname=Ann", "--lastname=Lee", "--password=pw1",
					"--email=e@x.org", "--domain=d.example"),
				"v-delete-fs-directory u " + gRoot + "/install",
			},
		},
		{
			name: "vvveb", id: "vvveb", php: "8.4",
			opts: map[string]string{"vvveb_account_username": "admin", "vvveb_account_email": "e@x.org", "vvveb_account_password": "pw1"},
			calls: []string{
				gPHP("8.4", gRoot+"/cli.php", "install", "host=localhost", "user=u_dbu", "password=dbpw", "database=u_dbn",
					"admin[user]=admin", "admin[password]=pw1", "admin[email]=e@x.org"),
			},
		},
		{
			name: "wordpress", id: "wordpress", php: "8.4",
			opts: map[string]string{"site_name": "My Blog", "username": "wpadmin", "email": "e@x.org", "password": "pw1", "language": "de_DE"},
			calls: []string{
				gWP,
				gPHP("8.4", "/home/u/.wp-cli/wp", "config", "create", "--dbname=u_dbn", "--dbuser=u_dbu", "--dbpass=dbpw",
					"--dbhost=localhost", "--dbprefix=wp_RAND_", "--dbcharset=utf8mb4", "--locale=de_DE", "--path="+gRoot),
			},
			posts: []string{"203.0.113.5:80 /wp-admin/install.php?step=2 " + gBody("weblog_title", "My Blog", "user_name", "wpadmin",
				"admin_password", "pw1", "admin_password2", "pw1", "admin_email", "e@x.org")},
		},
		{
			name: "osticket", id: "osticket", php: "8.4", ssl: true,
			opts: map[string]string{"helpdesk_name": "Help Desk", "email": "h@x.org", "first_name": "Ann", "last_name": "Lee",
				"username": "osAdmin", "admin_email": "a@x.org", "password": "pw1"},
			calls: []string{
				"v-copy-fs-directory u " + gRoot + "/upload/. " + gRoot,
				"v-move-fs-file u " + gRoot + "/include/ost-sampleconfig.php " + gRoot + "/include/ost-config.php",
				"v-delete-fs-directory u " + gRoot + "/upload",
				"v-delete-fs-directory u " + gRoot + "/setup",
			},
			posts: []string{"203.0.113.5:443 /setup/install.php " + gBody("s", "install", "name", "Help Desk", "email", "h@x.org",
				"lang_id", "en_US", "fname", "Ann", "lname", "Lee", "admin_email", "a@x.org", "username", "osAdmin", "passwd", "pw1",
				"passwd2", "pw1", "prefix", "ost_", "dbhost", "localhost", "dbname", "u_dbn", "dbuser", "u_dbu", "dbpass", "dbpw")},
		},
	}

	seen := map[string]bool{}
	for _, c := range cases {
		seen[c.id] = true
	}
	for _, inst := range All() {
		if id := inst.Info().ID; id != "namelessmc" && !seen[id] {
			t.Errorf("no golden case for %s", id)
		}
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			inst, _ := Get(c.id)
			r := &recorder{reads: c.reads, out: map[string]string{"v-list-sys-php": `["` + c.php + `"]`}}
			plain, tlsSite := newStubSites()
			defer plain.srv.Close()
			defer tlsSite.srv.Close()
			h := &Host{R: r, User: "u", TmpDir: t.TempDir(),
				Sleep:     func(context.Context, time.Duration) error { return nil },
				TLSConfig: &tls.Config{InsecureSkipVerify: true},
				DialContext: func(ctx context.Context, n, addr string) (net.Conn, error) {
					site := plain
					if strings.HasSuffix(addr, ":443") {
						site = tlsSite
					}
					site.dial(addr)
					return (&net.Dialer{}).DialContext(ctx, n, site.srv.Listener.Addr().String())
				}}
			opts := map[string]string{"php_version": c.php}
			for k, v := range c.opts {
				opts[k] = v
			}
			tg := &Target{Domain: "d.example", Home: "/home/u", IP: "203.0.113.5", SSL: c.ssl}
			if inst.Info().Database {
				tg.DB = Database{Host: "localhost", Name: "u_dbn", User: "u_dbu", Password: "dbpw"}
			}
			if err := Install(context.Background(), inst, tg, opts, h, Env{WebBackend: "php-fpm"}, nil); err != nil {
				t.Fatalf("install: %v", err)
			}

			backend := "v-change-web-domain-backend-tpl u d.example PHP-" + strings.ReplaceAll(c.php, ".", "_")
			at := -1
			for i, call := range r.calls {
				if call == backend {
					at = i
				}
			}
			if at < 0 {
				t.Fatalf("no backend call in %q", r.calls)
			}
			got := make([]string, 0, len(r.calls))
			for _, call := range r.calls[at+1:] {
				// the listings that record what the install writes (Host.writing)
				if call == "v-run-cli-cmd u ls "+strings.Join(trackFlags, " ")+" "+gRoot {
					continue
				}
				got = append(got, regexp.MustCompile(`wp_[A-Za-z0-9]{5}_|cp_[A-Za-z0-9]{5}_`).ReplaceAllStringFunc(call, func(m string) string { return m[:3] + "RAND_" }))
			}
			if strings.Join(got, "\n") != strings.Join(c.calls, "\n") {
				t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(c.calls, "\n"))
			}

			for dst, want := range c.files {
				have, ok := r.files[dst]
				if !ok {
					t.Errorf("no file written to %s", dst)
					continue
				}
				have = regexp.MustCompile(`analytics\.salt=[A-Za-z0-9]{64}`).ReplaceAllString(have, "analytics.salt=SALT")
				if m := regexp.MustCompile(`hashed_password: '(\$2[ab]\$[^']+)'`).FindStringSubmatch(have); m != nil {
					if bcrypt.CompareHashAndPassword([]byte(m[1]), []byte(opts["password"])) != nil {
						t.Errorf("hash does not match the password")
					}
					have = strings.Replace(have, m[1], "HASH", 1)
				}
				if have != want {
					t.Errorf("%s:\n%q\nwant:\n%q", dst, have, want)
				}
			}

			var posts []string
			for _, s := range []*stubSite{plain, tlsSite} {
				posts = append(posts, s.requests()...)
			}
			var gotPosts []string
			for _, p := range posts {
				gotPosts = append(gotPosts, regexp.MustCompile(`tablePrefix=fl[A-Za-z0-9]{5}`).ReplaceAllString(p, "tablePrefix=flRAND"))
			}
			if !sameSet(gotPosts, c.posts) {
				t.Errorf("requests:\n%q\nwant:\n%q", gotPosts, c.posts)
			}
		})
	}
}

func prestaCalls(ssl string) []string {
	return []string{
		"v-delete-fs-file u " + gRoot + "/index.php",
		"v-delete-fs-file u " + gRoot + "/Install_PrestaShop.html",
		"v-extract-fs-archive u " + gRoot + "/prestashop.zip " + gRoot + "/  1",
		gPHP("8.4", gRoot+"/install/index_cli.php", "--db_server=localhost", "--db_user=u_dbu", "--db_password=dbpw",
			"--db_name=u_dbn", "--name=My Shop", "--firstname=Ann", "--lastname=Lee", "--language=en", "--password=pw1",
			"--email=e@x.org", "--domain=d.example", ssl),
		"v-delete-fs-directory u " + gRoot + "/install",
		"v-delete-fs-file u " + gRoot + "/prestashop.zip",
	}
}

func shopwareEnv() map[string]string {
	return map[string]string{gRoot + "/.env": "APP_URL=http://127.0.0.1:8000\nDATABASE_URL=mysql://root:root@localhost/shopware\n"}
}

func shopwareFiles(t *testing.T) map[string]string {
	return map[string]string{
		gRoot + "/.env":      "APP_URL=http://d.example\nDATABASE_URL=mysql://u_dbu:dbpw@localhost:3306/u_dbn\n",
		gRoot + "/.htaccess": gHtaccess(t, "Shopware"),
	}
}

func shopwareCalls(locale string, translation bool) []string {
	console := gRoot + "/bin/console"
	calls := []string{
		"v-copy-fs-file u <T> " + gRoot + "/.htaccess",
		"v-open-fs-file u " + gRoot + "/.env",
		"v-copy-fs-file u <T> " + gRoot + "/.env",
		gPHP("8.4", console, "system:install", "-n", "--shop-name=My Shop", "--shop-email=s@x.org", "--shop-locale="+locale,
			"--shop-currency=USD", "--basic-setup"),
	}
	if translation {
		calls = append(calls, gPHP("8.4", console, "translation:install", "-n", "--locales="+locale))
	}
	return append(calls, gPHP("8.4", console, "user:create", "shopadmin", "-a", "--firstName=Ann", "--lastName=Lee",
		"--password=pw1", "--email=e@x.org", "-n"))
}

// A stub site records each POST as "<dialed address> <request URI> <body>".
type stubSite struct {
	srv  *httptest.Server
	mu   sync.Mutex
	addr string
	reqs []string
}

func (s *stubSite) dial(addr string) {
	s.mu.Lock()
	s.addr = addr
	s.mu.Unlock()
}

func (s *stubSite) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reqs...)
}

func newStubSites() (plain, secure *stubSite) {
	mk := func(tlsOn bool) *stubSite {
		s := &stubSite{}
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				return // WaitSite's probe before the first POST (TestPostWaitsForThePool)
			}
			b, _ := io.ReadAll(r.Body)
			s.mu.Lock()
			s.reqs = append(s.reqs, s.addr+" "+r.URL.RequestURI()+" "+string(b))
			s.mu.Unlock()
		})
		if tlsOn {
			s.srv = httptest.NewTLSServer(h)
		} else {
			s.srv = httptest.NewServer(h)
		}
		return s
	}
	return mk(false), mk(true)
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	// Requests of one site come in order; plain and TLS sites are listed plain first,
	// and every case uses one scheme except ClassicPress/Dolibarr, which are all TLS.
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
