package terminal

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hostisan/hostisan/internal/hestia"
)

// The refusals the terminal sends back as a FrameClose reason (Hestia's texts, WT33).
var (
	ErrNotAllowed = errors.New("You are not allowed to access this server.")
	ErrNoShell    = errors.New("You have no shell access.")
)

// Default locations of the OS and Hestia user databases.
const (
	PasswdFile  = "/etc/passwd"
	HestiaUsers = "/usr/local/hestia/data/users"
)

// Account is the OS account a shell runs as.
type Account struct {
	Name, Home, Shell string
	UID, GID          uint32
	// Groups are the supplementary groups (initgroups); nil until filled by
	// LookupAccount.
	Groups []uint32
}

// CheckAccount applies the terminal's own account rules (parity decision 9): a valid
// Hestia user name, an /etc/passwd entry with uid and gid not 0, a
// <hestiaUsers>/<name>/user.conf file, and an absolute shell not ending in nologin.
// A bad name is refused before passwd is read. Groups is left nil.
func CheckAccount(name string, passwd io.Reader, hestiaUsers string) (Account, error) {
	if !hestia.ValidUsername(name) {
		return Account{}, ErrNotAllowed
	}
	a, ok, err := findPasswd(name, passwd)
	if err != nil {
		return Account{}, fmt.Errorf("%w (%v)", ErrNotAllowed, err)
	}
	if !ok || a.UID == 0 || a.GID == 0 {
		return Account{}, ErrNotAllowed
	}
	fi, err := os.Stat(filepath.Join(hestiaUsers, name, "user.conf"))
	if err != nil || !fi.Mode().IsRegular() {
		return Account{}, ErrNotAllowed
	}
	if !filepath.IsAbs(a.Shell) || strings.HasSuffix(a.Shell, "nologin") {
		return Account{}, ErrNoShell
	}
	if !filepath.IsAbs(a.Home) {
		return Account{}, ErrNotAllowed
	}
	return a, nil
}

// findPasswd returns the first passwd entry named name, like getpwnam.
func findPasswd(name string, passwd io.Reader) (Account, bool, error) {
	sc := bufio.NewScanner(passwd)
	for sc.Scan() {
		f := strings.Split(sc.Text(), ":")
		if f[0] != name {
			continue
		}
		if len(f) != 7 {
			return Account{}, false, nil
		}
		uid, err1 := strconv.ParseUint(f[2], 10, 32)
		gid, err2 := strconv.ParseUint(f[3], 10, 32)
		if err1 != nil || err2 != nil {
			return Account{}, false, nil
		}
		return Account{Name: name, Home: f[5], Shell: f[6], UID: uint32(uid), GID: uint32(gid)}, true, nil
	}
	return Account{}, false, sc.Err()
}

// LookupAccount checks name against /etc/passwd and Hestia's users directory and
// fills its supplementary groups from the OS group database.
func LookupAccount(name string) (Account, error) {
	f, err := os.Open(PasswdFile)
	if err != nil {
		return Account{}, fmt.Errorf("%w (%v)", ErrNotAllowed, err)
	}
	defer f.Close()
	a, err := CheckAccount(name, f, HestiaUsers)
	if err != nil {
		return Account{}, err
	}
	if a.Groups, err = groupsOf(a); err != nil {
		return Account{}, fmt.Errorf("%w (groups: %v)", ErrNotAllowed, err)
	}
	return a, nil
}

// groupsOf lists the account's groups (primary included), as initgroups would.
func groupsOf(a Account) ([]uint32, error) {
	u := &user.User{Uid: strconv.FormatUint(uint64(a.UID), 10), Gid: strconv.FormatUint(uint64(a.GID), 10), Username: a.Name}
	ids, err := u.GroupIds()
	if err != nil {
		return nil, err
	}
	out := []uint32{a.GID}
	for _, id := range ids {
		g, err := strconv.ParseUint(id, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("group id %q", id)
		}
		if g == 0 {
			continue // never hand root's group to a shell
		}
		if uint32(g) != a.GID {
			out = append(out, uint32(g))
		}
	}
	return out, nil
}
