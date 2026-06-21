package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// LdapLogin authenticates a user against the configured LDAP server.
// On success it finds or creates a local shadow account and sets up the
// login session, mirroring the password login flow (including 2FA support).
func LdapLogin(c *gin.Context) {
	cfg := system_setting.GetLDAPSettings()
	if !cfg.Enabled {
		common.ApiErrorI18n(c, i18n.MsgLDAPNotEnabled)
		return
	}

	var req LoginRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.Username == "" || req.Password == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	ldapUser, err := service.AuthenticateLDAP(req.Username, req.Password)
	if err != nil {
		common.SysLog("[LDAP] login failed for " + req.Username + ": " + err.Error())
		switch {
		case errors.Is(err, service.ErrLDAPNotEnabled):
			common.ApiErrorI18n(c, i18n.MsgLDAPNotEnabled)
		case errors.Is(err, service.ErrLDAPNotConfigured):
			common.ApiErrorI18n(c, i18n.MsgLDAPNotConfigured)
		case errors.Is(err, service.ErrLDAPInvalidCreds):
			common.ApiErrorI18n(c, i18n.MsgLDAPInvalidCredentials)
		default:
			common.ApiErrorI18n(c, i18n.MsgLDAPAuthFailed)
		}
		return
	}

	user, err := findOrCreateLDAPUser(ldapUser, cfg)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Banned accounts must not log in.
	if user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
		return
	}

	// 2FA: mirror the password login pending-session flow.
	if model.IsTwoFAEnabled(user.Id) {
		session := sessions.Default(c)
		session.Set("pending_username", user.Username)
		session.Set("pending_user_id", user.Id)
		if err := session.Save(); err != nil {
			common.ApiErrorI18n(c, i18n.MsgUserSessionSaveFailed)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message": i18n.T(c, i18n.MsgUserRequire2FA),
			"success": true,
			"data": map[string]interface{}{
				"require_2fa": true,
			},
		})
		return
	}

	setupLogin(user, c)
}

// findOrCreateLDAPUser resolves an authenticated LDAP user to a local account.
// It reuses an existing local user with the same username (e.g. one an admin
// pre-created); otherwise it provisions a new shadow user when AutoRegister is on.
func findOrCreateLDAPUser(ldapUser *service.LDAPUserInfo, cfg *system_setting.LDAPSettings) (*model.User, error) {
	// Look up an existing (non-deleted) local user by username first.
	var existing model.User
	result := model.DB.Where("username = ?", ldapUser.Username).First(&existing)
	if result.Error == nil {
		return &existing, nil
	}
	if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, result.Error
	}

	// No local user yet. Auto-provision only when enabled.
	if !cfg.AutoRegister {
		return nil, errors.New("LDAP auto-registration is disabled; ask an administrator to create the account")
	}

	// Truncate to the DB username length limit.
	username := truncate(ldapUser.Username, model.UserNameMaxLength)
	displayName := truncate(defaultIfEmpty(ldapUser.DisplayName, ldapUser.Username), model.UserNameMaxLength)

	group := cfg.DefaultGroup
	if group == "" {
		group = "default"
	}

	newUser := model.User{
		Username:    username,
		DisplayName: displayName,
		Email:       ldapUser.Email,
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       group,
		// Password stays empty: common.Password2Hash turns "" into a random hash,
		// so the shadow account cannot be used for normal password login.
	}
	if err := newUser.Insert(0); err != nil {
		return nil, err
	}
	common.SysLog("[LDAP] auto-registered user: " + newUser.Username)
	return &newUser, nil
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

func defaultIfEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
