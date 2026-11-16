package hestia

// DNSDomain is one zone from `v-list-dns-domains`; the object key becomes Name.
type DNSDomain struct {
	Name      string `json:"NAME"`
	IP        string `json:"IP"`
	TPL       string `json:"TPL"`
	TTL       string `json:"TTL"`
	EXP       string `json:"EXP"`
	SOA       string `json:"SOA"`
	SERIAL    string `json:"SERIAL"`
	DNSSEC    string `json:"DNSSEC"`
	SRC       string `json:"SRC"`
	RECORDS   string `json:"RECORDS"`
	SUSPENDED string `json:"SUSPENDED"`
	TIME      string `json:"TIME"`
	DATE      string `json:"DATE"`
}

func (d *DNSDomain) SetName(name string) { d.Name = name }

// DNSRecord is one record from `v-list-dns-records`; the object key is its ID.
type DNSRecord struct {
	RECORD    string `json:"RECORD"`
	TYPE      string `json:"TYPE"`
	PRIORITY  string `json:"PRIORITY"`
	VALUE     string `json:"VALUE"`
	ID        string `json:"ID"`
	SUSPENDED string `json:"SUSPENDED"`
	TIME      string `json:"TIME"`
	DATE      string `json:"DATE"`
	TTL       string `json:"TTL"`
}

func (r *DNSRecord) SetName(id string) { r.ID = id }

// DNSSECKey is the public key from `v-list-dnssec-public-key`; all empty when the
// zone has none.
type DNSSECKey struct {
	RECORD    string `json:"RECORD"`
	KEYTAG    string `json:"KEYTAG"`
	FLAG      string `json:"FLAG"`
	ALGORITHM string `json:"ALGORITHM"`
	KEY       string `json:"KEY"`
	DS        string `json:"DS"`
}

// UserIP is one address from `v-list-user-ips`; the object key becomes IP.
type UserIP struct {
	IP     string `json:"IP"`
	OWNER  string `json:"OWNER"`
	STATUS string `json:"STATUS"`
	NAME   string `json:"NAME"`
	NAT    string `json:"NAT"`
}

func (u *UserIP) SetName(ip string) { u.IP = ip }

// Display is the address to show users: the NAT address if there is one.
func (u UserIP) Display() string {
	if u.NAT != "" {
		return u.NAT
	}
	return u.IP
}
