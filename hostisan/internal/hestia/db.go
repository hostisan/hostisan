package hestia

// Database is one entry from `v-list-databases`; the object key becomes DATABASE.
type Database struct {
	DATABASE  string `json:"DATABASE"`
	DBUSER    string `json:"DBUSER"`
	HOST      string `json:"HOST"`
	TYPE      string `json:"TYPE"`
	CHARSET   string `json:"CHARSET"`
	U_DISK    string `json:"U_DISK"`
	SUSPENDED string `json:"SUSPENDED"`
	TIME      string `json:"TIME"`
	DATE      string `json:"DATE"`
}

func (d *Database) SetName(name string) { d.DATABASE = name }

// DatabaseHost is one element of the `v-list-database-hosts` array.
type DatabaseHost struct {
	HOST        string `json:"HOST"`
	PORT        string `json:"PORT"`
	TYPE        string `json:"TYPE"`
	CHARSETS    string `json:"CHARSETS"`
	MAX_DB      string `json:"MAX_DB"`
	U_SYS_USERS string `json:"U_SYS_USERS"`
	U_DB_BASES  string `json:"U_DB_BASES"`
	TPL         string `json:"TPL"`
	SUSPENDED   string `json:"SUSPENDED"`
	TIME        string `json:"TIME"`
	DATE        string `json:"DATE"`
}
