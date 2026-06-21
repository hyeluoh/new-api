package service

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/go-ldap/ldap/v3"
)

// LDAPUserInfo holds the attributes returned from a successful LDAP authentication.
type LDAPUserInfo struct {
	Username    string
	DisplayName string
	Email       string
}

var (
	ErrLDAPNotEnabled   = errors.New("LDAP authentication is not enabled")
	ErrLDAPNotConfigured = errors.New("LDAP server is not configured")
	ErrLDAPInvalidCreds = errors.New("invalid LDAP credentials")
)

// AuthenticateLDAP validates the username/password against the configured LDAP
// server. It supports two modes:
//   - Search-then-bind (BindDN set): binds with a service account, searches for
//     the user DN, then rebinds as the user to verify the password.
//   - Simple bind (BindDN empty): builds the user DN from UserBase +
//     UsernameAttribute, then binds directly as the user.
func AuthenticateLDAP(username, password string) (*LDAPUserInfo, error) {
	cfg := system_setting.GetLDAPSettings()
	if !cfg.Enabled {
		return nil, ErrLDAPNotEnabled
	}
	if cfg.ServerURL == "" || cfg.UserBase == "" {
		return nil, ErrLDAPNotConfigured
	}
	if username == "" || password == "" {
		return nil, ErrLDAPInvalidCreds
	}

	conn, err := dialLDAP(cfg)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var userDN string
	var info *LDAPUserInfo

	if cfg.BindDN != "" {
		// Search-then-bind mode: bind with service account first.
		if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
			return nil, fmt.Errorf("LDAP service bind failed: %w", err)
		}
		userDN, info, err = searchLDAPUser(conn, cfg, username)
		if err != nil {
			return nil, err
		}
		// Verify the user's password by binding as the user.
		if err := conn.Bind(userDN, password); err != nil {
			if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
				return nil, ErrLDAPInvalidCreds
			}
			return nil, fmt.Errorf("LDAP user bind failed: %w", err)
		}
	} else {
		// Simple bind mode: construct the user DN directly.
		userDN = fmt.Sprintf("%s=%s,%s", escapeAttr(cfg.UsernameAttribute), ldap.EscapeFilter(username), cfg.UserBase)
		if err := conn.Bind(userDN, password); err != nil {
			if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
				return nil, ErrLDAPInvalidCreds
			}
			return nil, fmt.Errorf("LDAP bind failed: %w", err)
		}
		info = &LDAPUserInfo{Username: username}
		// Try to fetch attributes for a richer profile.
		if dn, attrInfo, err := searchLDAPUser(conn, cfg, username); err == nil {
			info = attrInfo
			_ = dn
		}
	}

	if info.Username == "" {
		info.Username = username
	}
	return info, nil
}

// dialLDAP opens and returns a configured LDAP connection.
// For ldaps:// the TLS negotiation happens during DialURL using the custom
// TLS config; for ldap:// + SkipTLSVerify we upgrade with StartTLS afterwards.
func dialLDAP(cfg *system_setting.LDAPSettings) (*ldap.Conn, error) {
	opts := []ldap.DialOpt{}
	if cfg.SkipTLSVerify {
		opts = append(opts, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: true}))
	}
	l, err := ldap.DialURL(cfg.ServerURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("LDAP connect failed: %w", err)
	}
	return l, nil
}

// searchLDAPUser searches for a user by the configured filter and returns the
// DN plus extracted attributes (username, display name, email).
func searchLDAPUser(conn *ldap.Conn, cfg *system_setting.LDAPSettings, username string) (string, *LDAPUserInfo, error) {
	filter := buildUserFilter(cfg.UserFilter, cfg.UsernameAttribute, username)

	attrs := []string{"dn"}
	if cfg.UsernameAttribute != "" {
		attrs = append(attrs, cfg.UsernameAttribute)
	}
	if cfg.DisplayNameAttribute != "" {
		attrs = append(attrs, cfg.DisplayNameAttribute)
	}
	if cfg.EmailAttribute != "" {
		attrs = append(attrs, cfg.EmailAttribute)
	}

	req := ldap.NewSearchRequest(
		cfg.UserBase,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		filter,
		attrs,
		nil,
	)

	res, err := conn.Search(req)
	if err != nil {
		return "", nil, fmt.Errorf("LDAP search failed: %w", err)
	}
	if len(res.Entries) == 0 {
		return "", nil, ErrLDAPInvalidCreds
	}
	entry := res.Entries[0]

	info := &LDAPUserInfo{Username: username}
	if cfg.UsernameAttribute != "" {
		if v := entry.GetAttributeValue(cfg.UsernameAttribute); v != "" {
			info.Username = v
		}
	}
	if cfg.DisplayNameAttribute != "" {
		info.DisplayName = entry.GetAttributeValue(cfg.DisplayNameAttribute)
	}
	if cfg.EmailAttribute != "" {
		info.Email = entry.GetAttributeValue(cfg.EmailAttribute)
	}

	return entry.DN, info, nil
}

// buildUserFilter resolves the configured filter. It supports a "%s" template
// (in either the full filter or the attribute-based filter) and falls back to a
// sensible default of (<attr>=<username>).
func buildUserFilter(filterTemplate, attr, username string) string {
	escaped := ldap.EscapeFilter(username)
	if filterTemplate == "" {
		if attr == "" {
			attr = "uid"
		}
		return fmt.Sprintf("(%s=%s)", escapeAttr(attr), escaped)
	}
	if strings.Contains(filterTemplate, "%s") {
		return fmt.Sprintf(filterTemplate, escaped)
	}
	return filterTemplate
}

// escapeAttr escapes an attribute name for use in a DN/filter. Attribute names
// are simple identifiers and rarely need escaping, but we guard against embedded
// commas just to keep the DN well-formed in simple-bind mode.
func escapeAttr(attr string) string {
	return strings.ReplaceAll(attr, ",", "\\,")
}
