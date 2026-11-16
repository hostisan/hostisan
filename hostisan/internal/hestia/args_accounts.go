package hestia

import "regexp"

// Path-parameter validators for the account pages (Plan 3). Each keeps argv safe
// before any command runs; Hestia's own checks still apply after.

// Hestia's is_object_format_valid, ^[[:alnum:]][-|\.|_[:alnum:]]{0,64}[[:alnum:]]$,
// without the "|" its bracket expression lets through by accident.
var objectRe = regexp.MustCompile(`^[A-Za-z0-9][-._A-Za-z0-9]{0,64}[A-Za-z0-9]$`)

// ValidPackageArg reports whether s is a package name: 2 to 66 characters of
// [A-Za-z0-9._-], a letter or digit at both ends.
func ValidPackageArg(s string) bool { return objectRe.MatchString(s) }

// ValidBackupName reports whether s is a backup file name, by the same rule as
// packages (Hestia checks backups with is_object_format_valid too). It has no "/", so
// filepath.Join(dir, s) stays in dir.
func ValidBackupName(s string) bool { return objectRe.MatchString(s) }

var snapshotRe = regexp.MustCompile(`^[0-9a-f]{8,64}$`)

// ValidSnapshotID reports whether s is a restic snapshot ID: 8 to 64 lower-case hex
// characters (the short or the full ID).
func ValidSnapshotID(s string) bool { return snapshotRe.MatchString(s) }

var accessKeyRe = regexp.MustCompile(`^[A-Za-z0-9]{20}$`)

// ValidAccessKeyID reports whether s is an access key ID: exactly 20 ASCII letters or
// digits, as v-list-access-keys requires of its file names.
func ValidAccessKeyID(s string) bool { return accessKeyRe.MatchString(s) }

// ValidSSHKeyID reports whether s can be an SSH key ID (its comment, URL-decoded):
// 1 to 255 bytes, no control characters (C0, DEL or C1) and no leading "-". Spaces
// and punctuation are allowed, as comments hold them; the slice also requires the ID
// to be in the current list before deleting.
func ValidSSHKeyID(s string) bool {
	if s == "" || len(s) > 255 || s[0] == '-' {
		return false
	}
	for _, r := range s {
		if r < ' ' || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == '�' {
			return false
		}
	}
	return true
}
