package http

import (
	"encoding/json"
	"errors"
	"hash/maphash"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"orbitjob/internal/admin/app/account"
	tenantquery "orbitjob/internal/admin/app/tenant/query"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/domain/resource"
)

// AccountHTTP handles human credentials independently of automation keys.
type AccountHTTP struct {
	Service       *account.Service
	Security      middleware.ConsoleSecurity
	passwordSlots chan struct{}
	limitMu       sync.Mutex
	seed          maphash.Seed
	ipLimits      [2048]credentialWindow
	accountLimits [2048]credentialWindow
}
type credentialWindow struct {
	start time.Time
	count int
}

func NewAccountHTTP(service *account.Service, security middleware.ConsoleSecurity) *AccountHTTP {
	return &AccountHTTP{Service: service, Security: security, passwordSlots: make(chan struct{}, 2), seed: maphash.MakeSeed()}
}
func (h *Handler) SetAccountHTTP(accounts *AccountHTTP) { h.accounts = accounts }

type SessionSnapshot struct {
	Authenticated   bool                `json:"authenticated"`
	Initialized     bool                `json:"initialized"`
	User            *account.User       `json:"user,omitempty"`
	ExpiresAt       *time.Time          `json:"expires_at,omitempty"`
	CSRFToken       string              `json:"csrf_token,omitempty"`
	Workspaces      []account.Workspace `json:"workspaces"`
	RecentAuthUntil *time.Time          `json:"recent_auth_until,omitempty"`
}
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Remember bool   `json:"remember"`
}
type SetupAccountRequest struct {
	Token       string `json:"token"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name,omitempty"`
}

// AcceptInvitationRequest describes the conditional credentials accepted by the
// invitation endpoint. The decoder uses the same fields as SetupAccountRequest.
type AcceptInvitationRequest SetupAccountRequest

type PasswordRequest struct {
	Password string `json:"password"`
}
type PasswordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	Password        string `json:"password"`
}
type ResetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}
type MemberChangeRequest struct {
	TenantID         string   `json:"tenant_id"`
	Status           *string  `json:"status,omitempty"`
	Role             *string  `json:"role,omitempty"`
	PolicyIDs        []string `json:"policy_ids,omitempty"`
	BoundaryPolicyID *string  `json:"boundary_policy_id,omitempty"`
}
type PasswordResetRequest struct {
	TenantID string `json:"tenant_id,omitempty"`
}
type SessionListItem struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Current   bool      `json:"current"`
	Remember  bool      `json:"remember"`
}
type SessionListResponse struct {
	Items []SessionListItem `json:"items"`
}
type MemberListResponse struct {
	Items []account.Membership `json:"items"`
}
type UserListResponse struct {
	Items []account.User `json:"items"`
}

func accountError(c *gin.Context, err error) {
	status, code, message := http.StatusServiceUnavailable, apperror.CodeServiceUnavailable, "account service is temporarily unavailable"
	switch {
	case errors.Is(err, account.ErrUnauthenticated):
		status, code, message = http.StatusUnauthorized, apperror.CodeUnauthorized, "invalid credentials or session"
	case errors.Is(err, account.ErrRecentAuth):
		status, code, message = http.StatusForbidden, "recent_auth_required", "recent password authentication required"
	case errors.Is(err, account.ErrForbidden):
		status, code, message = http.StatusForbidden, apperror.CodeForbidden, "account operation forbidden"
	case errors.Is(err, account.ErrInvalid):
		status, code, message = http.StatusBadRequest, apperror.CodeValidation, "invalid account input or capability"
	case errors.Is(err, account.ErrConflict):
		status, code, message = http.StatusConflict, apperror.CodeConflict, "account operation conflicts with current state"
	case errors.Is(err, account.ErrLastAdmin):
		status, code, message = http.StatusConflict, apperror.CodeConflict, "the last active installation administrator must remain"
	}
	apperror.Write(c, status, apperror.APIError{Code: code, Message: message})
}
func (a *AccountHTTP) session(c *gin.Context, required bool) (*account.Session, bool) {
	// Browser account endpoints never reinterpret an explicit automation credential.
	if len(c.Request.Header.Values("Authorization")) > 0 {
		accountError(c, account.ErrUnauthenticated)
		return nil, false
	}
	token, ok := a.Security.SessionSecret(c.Request)
	if !ok {
		if required {
			accountError(c, account.ErrUnauthenticated)
			return nil, false
		}
		return nil, true
	}
	s, err := a.Service.Authenticate(c.Request.Context(), token)
	if errors.Is(err, account.ErrUnauthenticated) && !required {
		return nil, true
	}
	if err != nil {
		accountError(c, err)
		return nil, false
	}
	if !middleware.SafeMethod(c.Request.Method) && !a.Security.CheckCSRF(c.Request, s.CSRFToken) {
		accountError(c, account.ErrForbidden)
		return nil, false
	}
	return s, true
}
func (a *AccountHTTP) snapshot(c *gin.Context, s *account.Session) {
	initialized, err := a.Service.Initialized(c.Request.Context())
	if err != nil {
		accountError(c, err)
		return
	}
	out := SessionSnapshot{Initialized: initialized, Workspaces: []account.Workspace{}}
	if s != nil {
		workspaces, err := a.Service.Workspaces(c.Request.Context(), s.User.ID)
		if err != nil {
			accountError(c, err)
			return
		}
		until := s.RecentAuthAt.Add(account.RecentAuthLifetime)
		out.Authenticated = true
		out.User = &s.User
		out.ExpiresAt = &s.ExpiresAt
		out.CSRFToken = s.CSRFToken
		out.Workspaces = workspaces
		out.RecentAuthUntil = &until
	}
	c.JSON(http.StatusOK, out)
}
func (a *AccountHTTP) cookie(c *gin.Context, s account.Session, secret string) {
	cookie := &http.Cookie{Name: a.Security.CookieName(c.Request), Value: secret, Path: "/", HttpOnly: true, Secure: !a.Security.Insecure(c.Request), SameSite: http.SameSiteStrictMode}
	if s.Remember {
		cookie.Expires = s.ExpiresAt
		cookie.MaxAge = int(time.Until(s.ExpiresAt).Seconds())
	}
	http.SetCookie(c.Writer, cookie)
}
func (a *AccountHTTP) clearCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: a.Security.CookieName(c.Request), Value: "", Path: "/", HttpOnly: true, Secure: !a.Security.Insecure(c.Request), SameSite: http.SameSiteStrictMode, MaxAge: -1})
}
func (a *AccountHTTP) beginCredential(c *gin.Context) bool {
	if len(c.Request.Header.Values("Authorization")) > 0 {
		accountError(c, account.ErrUnauthenticated)
		return false
	}
	if !a.Security.OriginAllowed(c.Request) {
		accountError(c, account.ErrForbidden)
		return false
	}
	ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		ip = c.Request.RemoteAddr
	}
	a.limitMu.Lock()
	bucket := &a.ipLimits[maphash.String(a.seed, ip)%uint64(len(a.ipLimits))]
	now := time.Now()
	if now.Sub(bucket.start) > time.Minute {
		*bucket = credentialWindow{start: now}
	}
	bucket.count++
	allowed := bucket.count <= 20
	a.limitMu.Unlock()
	if !allowed {
		a.limited(c)
		return false
	}
	select {
	case a.passwordSlots <- struct{}{}:
		return true
	default:
		a.limited(c)
		return false
	}
}
func (a *AccountHTTP) accountLimit(c *gin.Context, username string) bool {
	a.limitMu.Lock()
	bucket := &a.accountLimits[maphash.String(a.seed, strings.ToLower(strings.TrimSpace(username)))%uint64(len(a.accountLimits))]
	now := time.Now()
	if now.Sub(bucket.start) > time.Minute {
		*bucket = credentialWindow{start: now}
	}
	bucket.count++
	ok := bucket.count <= 10
	a.limitMu.Unlock()
	if !ok {
		a.limited(c)
	}
	return ok
}
func (a *AccountHTTP) limited(c *gin.Context) {
	c.Header("Retry-After", "60")
	apperror.Write(c, http.StatusTooManyRequests, apperror.APIError{Code: apperror.CodeRateLimited, Message: "credential attempts are temporarily limited"})
}
func decodeAccount(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		accountError(c, account.ErrInvalid)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		accountError(c, account.ErrInvalid)
		return false
	}
	return true
}
func (a *AccountHTTP) getSession(c *gin.Context) {
	s, ok := a.session(c, false)
	if ok {
		a.snapshot(c, s)
	}
}
func (a *AccountHTTP) login(c *gin.Context) {
	if !a.beginCredential(c) {
		return
	}
	defer func() { <-a.passwordSlots }()
	var in LoginRequest
	if !decodeAccount(c, &in) || !a.accountLimit(c, in.Username) {
		return
	}
	s, secret, err := a.Service.Login(c.Request.Context(), in.Username, in.Password, in.Remember)
	if err != nil {
		accountError(c, err)
		return
	}
	a.cookie(c, s, secret)
	a.snapshot(c, &s)
}
func (a *AccountHTTP) logout(c *gin.Context) {
	s, ok := a.session(c, true)
	if !ok {
		return
	}
	if err := a.Service.Logout(c.Request.Context(), s.ID); err != nil {
		accountError(c, err)
		return
	}
	a.clearCookie(c)
	c.Status(http.StatusNoContent)
}
func (a *AccountHTTP) reauth(c *gin.Context) {
	if !a.beginCredential(c) {
		return
	}
	defer func() { <-a.passwordSlots }()
	s, ok := a.session(c, true)
	if !ok {
		return
	}
	var in PasswordRequest
	if !decodeAccount(c, &in) || !a.accountLimit(c, s.User.Username) {
		return
	}
	if err := a.Service.Reauthenticate(c.Request.Context(), s.ID, in.Password); err != nil {
		accountError(c, err)
		return
	}
	s, ok = a.session(c, true)
	if ok {
		a.snapshot(c, s)
	}
}
func (a *AccountHTTP) setup(c *gin.Context) {
	if !a.beginCredential(c) {
		return
	}
	defer func() { <-a.passwordSlots }()
	var in SetupAccountRequest
	if !decodeAccount(c, &in) || !a.accountLimit(c, in.Username) {
		return
	}
	user, err := a.Service.Setup(c.Request.Context(), in.Token, in.Username, in.Password, in.DisplayName)
	if err != nil {
		accountError(c, err)
		return
	}
	s, secret, err := a.Service.Login(c.Request.Context(), user.Username, in.Password, false)
	if err != nil {
		accountError(c, err)
		return
	}
	a.cookie(c, s, secret)
	a.snapshot(c, &s)
}
func (a *AccountHTTP) acceptInvitation(c *gin.Context) {
	if !a.beginCredential(c) {
		return
	}
	defer func() { <-a.passwordSlots }()
	var in SetupAccountRequest
	if !decodeAccount(c, &in) {
		return
	}
	// net/http omits malformed cookie values; their presence still requires authentication.
	sessionCookies := 0
	for _, header := range c.Request.Header.Values("Cookie") {
		for _, part := range strings.Split(header, ";") {
			name, _, _ := strings.Cut(strings.TrimSpace(part), "=")
			if strings.TrimSpace(name) == a.Security.CookieName(c.Request) {
				sessionCookies++
			}
		}
	}
	if sessionCookies > 1 {
		accountError(c, account.ErrUnauthenticated)
		return
	}
	s, ok := a.session(c, sessionCookies == 1)
	if !ok {
		return
	}
	sessionID := ""
	if s != nil {
		sessionID = s.ID
		if in.Username != "" {
			username, err := account.NormalizeUsername(in.Username)
			if err != nil {
				accountError(c, err)
				return
			}
			if username != s.User.Username {
				accountError(c, account.ErrUnauthenticated)
				return
			}
		}
		in.Username = s.User.Username
	}
	if !a.accountLimit(c, in.Username) {
		return
	}
	user, err := a.Service.AcceptInvitation(c.Request.Context(), sessionID, in.Token, in.Username, in.Password, in.DisplayName)
	if err != nil {
		accountError(c, err)
		return
	}
	if s != nil {
		a.snapshot(c, s)
		return
	}
	created, secret, err := a.Service.Login(c.Request.Context(), user.Username, in.Password, false)
	if err != nil {
		accountError(c, err)
		return
	}
	a.cookie(c, created, secret)
	a.snapshot(c, &created)
}
func (a *AccountHTTP) resetPassword(c *gin.Context) {
	if !a.beginCredential(c) {
		return
	}
	defer func() { <-a.passwordSlots }()
	var in ResetPasswordRequest
	if !decodeAccount(c, &in) {
		return
	}
	if err := a.Service.ResetPassword(c.Request.Context(), in.Token, in.Password); err != nil {
		accountError(c, err)
		return
	}
	a.clearCookie(c)
	c.Status(http.StatusNoContent)
}
func (a *AccountHTTP) sessions(c *gin.Context) {
	s, ok := a.session(c, true)
	if !ok {
		return
	}
	list, err := a.Service.Sessions(c.Request.Context(), s.ID)
	if err != nil {
		accountError(c, err)
		return
	}
	out := SessionListResponse{Items: []SessionListItem{}}
	for _, item := range list {
		out.Items = append(out.Items, SessionListItem{ID: item.ID, CreatedAt: item.CreatedAt, ExpiresAt: item.ExpiresAt, Current: item.ID == s.ID, Remember: item.Remember})
	}
	c.JSON(http.StatusOK, out)
}
func (a *AccountHTTP) revokeSession(c *gin.Context) {
	s, ok := a.session(c, true)
	if !ok {
		return
	}
	if err := a.Service.RevokeSession(c.Request.Context(), s.ID, c.Param("id")); err != nil {
		accountError(c, err)
		return
	}
	if s.ID == c.Param("id") {
		a.clearCookie(c)
	}
	c.Status(http.StatusNoContent)
}
func (a *AccountHTTP) members(c *gin.Context) {
	s, ok := a.session(c, true)
	if !ok {
		return
	}
	items, err := a.Service.Members(c.Request.Context(), s.ID, c.Query("tenant_id"))
	if err != nil {
		accountError(c, err)
		return
	}
	c.JSON(http.StatusOK, MemberListResponse{Items: items})
}
func (a *AccountHTTP) recent(c *gin.Context, s *account.Session) bool {
	if time.Since(s.RecentAuthAt) > account.RecentAuthLifetime {
		accountError(c, account.ErrRecentAuth)
		return false
	}
	return true
}
func (a *AccountHTTP) invite(c *gin.Context) {
	s, ok := a.session(c, true)
	if !ok || !a.recent(c, s) {
		return
	}
	var in account.Grant
	if !decodeAccount(c, &in) {
		return
	}
	out, err := a.Service.Invite(c.Request.Context(), s.ID, in)
	if err != nil {
		accountError(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}
func (a *AccountHTTP) updateMember(c *gin.Context) {
	s, ok := a.session(c, true)
	if !ok || !a.recent(c, s) {
		return
	}
	var in MemberChangeRequest
	if !decodeAccount(c, &in) {
		return
	}
	if err := a.Service.UpdateMember(c.Request.Context(), s.ID, c.Param("user_id"), in.TenantID, account.MemberChange{Status: in.Status, Role: in.Role, PolicyIDs: in.PolicyIDs, BoundaryPolicyID: in.BoundaryPolicyID}); err != nil {
		accountError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
func (a *AccountHTTP) issuePasswordReset(c *gin.Context) {
	s, ok := a.session(c, true)
	if !ok || !a.recent(c, s) {
		return
	}
	var in PasswordResetRequest
	if !decodeAccount(c, &in) {
		return
	}
	out, err := a.Service.IssuePasswordReset(c.Request.Context(), s.ID, c.Param("user_id"), in.TenantID)
	if err != nil {
		accountError(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}
func (a *AccountHTTP) users(c *gin.Context) {
	s, ok := a.session(c, true)
	if !ok {
		return
	}
	items, err := a.Service.Users(c.Request.Context(), s.ID)
	if err != nil {
		accountError(c, err)
		return
	}
	c.JSON(http.StatusOK, UserListResponse{Items: items})
}
func (a *AccountHTTP) updateUser(c *gin.Context) {
	s, ok := a.session(c, true)
	if !ok || !a.recent(c, s) {
		return
	}
	var in account.UserChange
	if !decodeAccount(c, &in) {
		return
	}
	if err := a.Service.UpdateUser(c.Request.Context(), s.ID, c.Param("id"), in); err != nil {
		accountError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) listHumanTenants(c *gin.Context, p middleware.Principal) {
	if h.accounts == nil || !p.InstallationAdmin {
		accountError(c, account.ErrForbidden)
		return
	}
	var in ListTenantsRequest
	if err := c.ShouldBindQuery(&in); err != nil {
		accountError(c, account.ErrInvalid)
		return
	}
	items, err := h.accounts.Service.WorkspaceDirectory(c.Request.Context(), p.SessionID)
	if err != nil {
		accountError(c, err)
		return
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset := in.Offset
	if offset < 0 {
		offset = 0
	}
	out := []tenantquery.TenantListItem{}
	for i := offset; i < len(items) && len(out) < limit; i++ {
		item := items[i]
		out = append(out, tenantquery.TenantListItem{ID: item.ID, Name: item.Name, Slug: item.Slug, Status: item.Status})
	}
	c.JSON(http.StatusOK, tenantListResponse{Items: out})
}
func (h *Handler) getHumanTenant(c *gin.Context, p middleware.Principal) {
	items, err := h.accounts.Service.WorkspaceDirectory(c.Request.Context(), p.SessionID)
	if err != nil {
		accountError(c, err)
		return
	}
	for _, item := range items {
		if item.ID == c.Param("id") {
			c.JSON(http.StatusOK, tenantquery.TenantGetResult{ID: item.ID, Name: item.Name, Slug: item.Slug, Status: item.Status})
			return
		}
	}
	writeAPIError(c, &resource.NotFoundError{Resource: "tenant", ID: c.Param("id")})
}

func (a *AccountHTTP) changePassword(c *gin.Context) {
	if !a.beginCredential(c) {
		return
	}
	defer func() { <-a.passwordSlots }()
	session, ok := a.session(c, true)
	if !ok {
		return
	}
	var in PasswordChangeRequest
	if !decodeAccount(c, &in) || !a.accountLimit(c, session.User.Username) {
		return
	}
	if err := a.Service.ChangePassword(c.Request.Context(), session.ID, in.CurrentPassword, in.Password); err != nil {
		accountError(c, err)
		return
	}
	a.clearCookie(c)
	c.Status(http.StatusNoContent)
}
