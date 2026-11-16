package hestia

import (
	"strings"
	"testing"
)

func TestValidCronField(t *testing.T) {
	long := strings.Repeat("1,", 32) + "1" // 65 characters
	cases := []struct {
		kind, v string
		ok      bool
	}{
		{"min", "*", true}, {"min", "*/5", true}, {"min", "0,6", true}, {"min", "0-59", true},
		{"min", "5-10/2", true}, {"min", "0", true}, {"min", "59", true}, {"min", "1,2,3-5,*/10", true},
		{"hour", "23", true}, {"hour", "*/6", true}, {"hour", "0", true},
		{"day", "1-31/2", true}, {"day", "1", true}, {"day", "31", true},
		{"month", "1-11/2", true}, {"month", "12", true}, {"month", "*/3", true},
		{"wday", "7", true}, {"wday", "0", true}, {"wday", "1,2,3,4,5", true}, {"wday", "0,6", true},
		{"min", "60", false}, {"min", "99", false}, {"hour", "24", false},
		{"day", "0", false}, {"day", "32", false}, {"month", "0", false}, {"month", "13", false},
		{"wday", "8", false},
		{"min", "1\n* * * * * x", false}, {"min", "1 foo", false}, {"min", "mon", false},
		{"month", "jan", false}, {"wday", "mon", false}, {"min", "@reboot", false},
		{"min", "5-1", false}, {"min", "*/0", false}, {"min", "*/59", true}, {"min", "*/60", false}, {"hour", "*/24", false}, {"hour", "*/23", true}, {"day", "*/32", false}, {"month", "1-11/12", true}, {"month", "*/13", false}, {"wday", "*/8", false}, {"wday", "*/7", true}, {"min", "1-5/0", false},
		{"min", long, false}, {"min", "1 ", false}, {"min", " 1", false}, {"min", "", false},
		{"min", "1,", false}, {"min", ",1", false}, {"min", "1,,2", false}, {"min", "-1", false},
		{"min", "+1", false}, {"min", "1-", false}, {"min", "*/", false}, {"min", "5/2", false},
		{"min", "*-5", false}, {"min", "1\r", false}, {"min", "1\x00", false}, {"min", "1\t", false},
		{"min", "١", false}, {"min", "*/5/2", false}, {"min", "1-2-3", false}, {"min", "1000", false},
		{"nope", "1", false},
	}
	for _, c := range cases {
		if got := ValidCronField(c.kind, c.v); got != c.ok {
			t.Errorf("ValidCronField(%q, %q) = %v, want %v", c.kind, c.v, got, c.ok)
		}
	}
	if !ValidCronField("min", strings.Repeat("1,", 31)+"1") { // 63 characters
		t.Error("63 characters should pass")
	}
}

func TestValidCronCommand(t *testing.T) {
	cases := []struct {
		v  string
		ok bool
	}{
		{"php /home/a/cron.php >/dev/null 2>&1", true},
		{"find /tmp -name '*.tmp' -exec rm {} \\;", true},
		{"echo \"hi\" | mail -s x a@b.c", true},
		{"-x", true}, {"é", true},
		{"", false}, {"a`b`", false}, {"a\nb", false}, {"a\rb", false}, {"a\x00b", false},
		{"a\x07b", false}, {"a\tb", false}, {"a\x7fb", false},
		{"-e", false}, {"-ne", false}, {"-neE", false}, {"-nn", false}, {"-", true}, {"-nx", true}, {"- e", true}, {"-n", false}, {"-E", false}, {"a\xffb", false},
		{strings.Repeat("a", maxCronCommand+1), false},
	}
	for _, c := range cases {
		if got := ValidCronCommand(c.v); got != c.ok {
			t.Errorf("ValidCronCommand(%q) = %v, want %v", c.v, got, c.ok)
		}
	}
}

func TestDecodeCronPlain(t *testing.T) {
	in := "1\t0\t*/6\t*\t*\t*\tfind . -exec rm {} \\;\tno\t12:00:00\t2026-01-10\n" +
		"2\t30\t3\t*\t*\t1\ta\tb\tc\tyes\t14:01:09\t2026-04-18\n\n"
	jobs, err := DecodeCronPlain([]byte(in))
	if err != nil || len(jobs) != 2 {
		t.Fatalf("%v %+v", err, jobs)
	}
	j := jobs[0]
	if j.JOB != "1" || j.MIN != "0" || j.HOUR != "*/6" || j.WDAY != "*" || j.CMD != "find . -exec rm {} \\;" ||
		j.SUSPENDED != "no" || j.TIME != "12:00:00" || j.DATE != "2026-01-10" {
		t.Fatalf("job = %+v", j)
	}
	if jobs[1].CMD != "a\tb\tc" || jobs[1].SUSPENDED != "yes" {
		t.Fatalf("job = %+v", jobs[1])
	}
	q, err := DecodeCronPlain([]byte("3\t0\t1\t*\t*\t*\tmysqldump x > \\\"/b/db-$(date +\\%F).sql\\\"\tno\t01:02:03\t2026-01-01\n"))
	if err != nil || q[0].CMD != `mysqldump x > "/b/db-$(date +\%F).sql"` || q[0].JOB != "3" {
		t.Fatalf("quotes: %v %+v", err, q)
	}
	if _, err := DecodeCronPlain([]byte("1\t2\t3\n")); err == nil {
		t.Error("short line should fail")
	}
	if jobs, err := DecodeCronPlain(nil); err != nil || len(jobs) != 0 || jobs == nil {
		t.Errorf("empty: %v %v", jobs, err)
	}
}

func TestDecodeCronPlainJob(t *testing.T) {
	in := "0\t*/6\t*\t*\t*\tfind . -exec rm {} \\;\ta\tb\tno\t12:00:00\t2026-01-10\n"
	j, err := DecodeCronPlainJob([]byte(in), "7")
	if err != nil || j.JOB != "7" || j.MIN != "0" || j.HOUR != "*/6" || j.WDAY != "*" ||
		j.CMD != "find . -exec rm {} \\;\ta\tb" || j.SUSPENDED != "no" || j.TIME != "12:00:00" || j.DATE != "2026-01-10" {
		t.Fatalf("%v %+v", err, j)
	}
	j, err = DecodeCronPlainJob([]byte("0\t1\t*\t*\t*\techo \\\"x\\\" \\%F\tyes\t01:02:03\t2026-01-01\n"), "2")
	if err != nil || j.CMD != `echo "x" \%F` || j.SUSPENDED != "yes" {
		t.Fatalf("%v %+v", err, j)
	}
	if _, err := DecodeCronPlainJob([]byte("1\t2\n"), "1"); err == nil {
		t.Error("short line should fail")
	}
}
