package hestia

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// maxCronField is the longest schedule field accepted (Parity 23).
const maxCronField = 64

// maxCronCommand bounds the command so it always fits in one argv entry.
const maxCronCommand = 8192

var cronRanges = map[string][2]int{
	"min": {0, 59}, "hour": {0, 23}, "day": {1, 31}, "month": {1, 12}, "wday": {0, 7},
}

// ValidCronField reports whether v is a strict cron schedule field of the given kind
// (min, hour, day, month or wday): "*", "N", "A-B", "*/N", "A-B/N" or a comma list of
// these, within the kind's range, A <= B, steps of 1 or more, at most 64 characters.
// Anything else (spaces, control characters, names) is refused.
func ValidCronField(kind, v string) bool {
	rg, ok := cronRanges[kind]
	if !ok || v == "" || len(v) > maxCronField {
		return false
	}
	for _, item := range strings.Split(v, ",") {
		if !validCronItem(item, rg[0], rg[1]) {
			return false
		}
	}
	return true
}

func validCronItem(item string, lo, hi int) bool {
	base, step, hasStep := strings.Cut(item, "/")
	if hasStep {
		if n, ok := cronNum(step); !ok || n < 1 || n > hi {
			return false
		}
	}
	if base == "*" {
		return true
	}
	a, b, isRange := strings.Cut(base, "-")
	x, ok := cronNum(a)
	if !ok || x < lo || x > hi {
		return false
	}
	if !isRange {
		return !hasStep // "N/S" is not an accepted form
	}
	y, ok := cronNum(b)
	return ok && y >= x && y <= hi
}

// cronNum parses 1 to 3 ASCII digits.
func cronNum(s string) (int, bool) {
	if s == "" || len(s) > 3 {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, true
}

// ValidCronCommand reports whether cmd may be stored as a cron command (Parity 24): not
// empty, valid UTF-8, no backtick or control character (so no newline, carriage return,
// NUL or tab), at most 8192 bytes, and not made only of the echo flags n, e, E after a dash (-n, -e, -neE ...), which the bin's
// echo would swallow.
func ValidCronCommand(cmd string) bool {
	if cmd == "" || len(cmd) > maxCronCommand || !utf8.ValidString(cmd) {
		return false
	}
	if cmd[0] == '-' && strings.Trim(cmd[1:], "neE") == "" && len(cmd) > 1 {
		return false // echo flags, alone or combined
	}
	for _, r := range cmd {
		if r == '`' || r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// cronPlainCmd undoes the bins' sed 's/"/\\"/g', which shows " as \" in plain output.
func cronPlainCmd(fields []string) string {
	return strings.ReplaceAll(strings.Join(fields, "\t"), `\"`, `"`)
}

// DecodeCronPlain parses `v-list-cron-jobs <user> plain`: one tab-separated line per
// job, JOB MIN HOUR DAY MONTH WDAY CMD SUSPENDED TIME DATE. The command is everything
// between the first six and the last three fields, so a backslash or tab in it
// survives. Used when the json output is invalid (Parity 25).
func DecodeCronPlain(stdout []byte) ([]CronJob, error) {
	jobs := []CronJob{}
	for _, line := range strings.Split(string(stdout), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 10 {
			return nil, errors.New("hestia: short cron line")
		}
		n := len(f)
		jobs = append(jobs, CronJob{
			JOB: f[0], MIN: f[1], HOUR: f[2], DAY: f[3], MONTH: f[4], WDAY: f[5],
			CMD: cronPlainCmd(f[6 : n-3]), SUSPENDED: f[n-3], TIME: f[n-2], DATE: f[n-1],
		})
	}
	return jobs, nil
}

// DecodeCronPlainJob parses `v-list-cron-job <user> <job> plain`, which has no JOB
// column: MIN HOUR DAY MONTH WDAY CMD SUSPENDED TIME DATE. job comes from the path.
func DecodeCronPlainJob(stdout []byte, job string) (CronJob, error) {
	line := strings.TrimRight(string(stdout), "\n")
	f := strings.Split(line, "\t")
	if len(f) < 9 {
		return CronJob{}, errors.New("hestia: short cron line")
	}
	n := len(f)
	return CronJob{
		JOB: job, MIN: f[0], HOUR: f[1], DAY: f[2], MONTH: f[3], WDAY: f[4],
		CMD: cronPlainCmd(f[5 : n-3]), SUSPENDED: f[n-3], TIME: f[n-2], DATE: f[n-1],
	}, nil
}
