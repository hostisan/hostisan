package session

import "github.com/hostisan/hostisan/internal/hestia"

// ReadOnly ports web/inc/policies.php: a suspended user may look but not change
// anything (when POLICY_USER_VIEW_SUSPENDED allows them in at all), and an admin
// viewing the protected root account may not change it.
func ReadOnly(s *Session, effective hestia.User, cfg hestia.SysConfig) bool {
	if s.UserContext == "user" && effective.Suspended() && cfg["POLICY_USER_VIEW_SUSPENDED"] == "yes" {
		return true
	}
	return s.UserContext == "admin" && s.Look != "" && s.Look == cfg["ROOT_USER"] && cfg["POLICY_SYSTEM_PROTECTED_ADMIN"] == "yes"
}
