package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildUserFilter(t *testing.T) {
	tests := []struct {
		name           string
		filterTemplate string
		attr           string
		username       string
		want           string
	}{
		{
			name:           "template with %s placeholder",
			filterTemplate: "(uid=%s)",
			attr:           "uid",
			username:       "johndoe",
			want:           "(uid=johndoe)",
		},
		{
			name:           "complex filter with %s",
			filterTemplate: "(&(objectClass=person)(sAMAccountName=%s))",
			attr:           "sAMAccountName",
			username:       "johndoe",
			want:           "(&(objectClass=person)(sAMAccountName=johndoe))",
		},
		{
			name:           "empty template falls back to attribute-based filter",
			filterTemplate: "",
			attr:           "cn",
			username:       "johndoe",
			want:           "(cn=johndoe)",
		},
		{
			name:           "empty template and empty attr defaults to uid",
			filterTemplate: "",
			attr:           "",
			username:       "johndoe",
			want:           "(uid=johndoe)",
		},
		{
			name:           "special characters in username are escaped",
			filterTemplate: "(uid=%s)",
			attr:           "uid",
			username:       "john*(doe)",
			want:           "(uid=john\\2a\\28doe\\29)",
		},
		{
			name:           "static filter without %s is returned as-is",
			filterTemplate: "(objectClass=*)",
			attr:           "uid",
			username:       "johndoe",
			want:           "(objectClass=*)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildUserFilter(tt.filterTemplate, tt.attr, tt.username)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEscapeAttr(t *testing.T) {
	assert.Equal(t, "uid", escapeAttr("uid"))
	assert.Equal(t, "ou\\,test", escapeAttr("ou,test"))
}

func TestAuthenticateLDAPNotEnabled(t *testing.T) {
	// LDAP is disabled by default in test config.
	_, err := AuthenticateLDAP("user", "pass")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLDAPNotEnabled)
}
