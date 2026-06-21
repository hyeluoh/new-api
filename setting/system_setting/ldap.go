package system_setting

import "github.com/QuantumNous/new-api/setting/config"

// LDAPSettings holds the configuration for LDAP authentication.
// Registered with config.GlobalConfig under the "ldap" prefix, so options
// are stored as ldap.<json_tag> in the options table.
type LDAPSettings struct {
	Enabled              bool   `json:"enabled"`
	ServerURL            string `json:"server_url"`               // e.g. ldap://host:389 or ldaps://host:636
	BindDN               string `json:"bind_dn"`                  // service/bind account DN (optional; enables search-then-bind mode)
	BindPassword         string `json:"bind_password"`            // bind account password
	UserBase             string `json:"user_base"`                // base DN to search for users
	UserFilter           string `json:"user_filter"`              // search filter, e.g. (uid=%s)
	UsernameAttribute    string `json:"username_attribute"`       // attribute mapped to the login name, e.g. uid / sAMAccountName
	DisplayNameAttribute string `json:"display_name_attribute"`   // attribute for display name, e.g. cn / displayName
	EmailAttribute       string `json:"email_attribute"`          // attribute for email, e.g. mail
	SkipTLSVerify        bool   `json:"skip_tls_verify"`          // skip TLS certificate verification (self-signed)
	AutoRegister         bool   `json:"auto_register"`            // auto-provision local shadow users on first login
	DefaultGroup         string `json:"default_group"`            // group assigned to new LDAP users
}

var defaultLDAPSettings = LDAPSettings{
	Enabled:              false,
	ServerURL:            "",
	BindDN:               "",
	BindPassword:         "",
	UserBase:             "",
	UserFilter:           "(uid=%s)",
	UsernameAttribute:    "uid",
	DisplayNameAttribute: "cn",
	EmailAttribute:       "mail",
	SkipTLSVerify:        false,
	AutoRegister:         true,
	DefaultGroup:         "default",
}

func init() {
	config.GlobalConfig.Register("ldap", &defaultLDAPSettings)
}

func GetLDAPSettings() *LDAPSettings {
	return &defaultLDAPSettings
}
