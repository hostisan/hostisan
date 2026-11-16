package hestia

// SysIP is one address from `v-list-sys-ips json` or `v-list-sys-ip <ip> json`; the
// object key becomes IP.
type SysIP struct {
	IP            string `json:"IP"`
	OWNER         string `json:"OWNER"`
	STATUS        string `json:"STATUS"`
	NAME          string `json:"NAME"`
	U_SYS_USERS   string `json:"U_SYS_USERS"`
	U_WEB_DOMAINS string `json:"U_WEB_DOMAINS"`
	INTERFACE     string `json:"INTERFACE"`
	NETMASK       string `json:"NETMASK"`
	NAT           string `json:"NAT"`
	TIME          string `json:"TIME"`
	DATE          string `json:"DATE"`
}

func (s *SysIP) SetName(ip string) { s.IP = ip }

// Display is the address to show users: the NAT address if there is one.
func (s SysIP) Display() string {
	if s.NAT != "" {
		return s.NAT
	}
	return s.IP
}
