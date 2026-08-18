package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wayy01/vps-dashboard/backend/internal/store"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrAccountLocked      = errors.New("account temporarily locked after repeated failed logins")
	ErrAccountDisabled    = errors.New("account disabled")
	ErrInvalidTOTP        = errors.New("invalid verification code")
	ErrNotFound           = errors.New("not found")
	ErrLastAdmin          = errors.New("cannot remove the last enabled admin")
)

const (
	maxFailedLogins = 5
	lockoutWindow   = 15 * time.Minute
	totpIssuer      = "VPS Dashboard"
)

type Service struct {
	st         *store.Store
	sealer     *Sealer
	sessionTTL time.Duration
	idleTTL    time.Duration
	require2FA bool
}

func NewService(st *store.Store, sealer *Sealer, sessionTTL, idleTTL time.Duration, require2FA bool) *Service {
	return &Service{st: st, sealer: sealer, sessionTTL: sessionTTL, idleTTL: idleTTL, require2FA: require2FA}
}

func (s *Service) Require2FA() bool { return s.require2FA }

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Role         Role      `json:"role"`
	TOTPEnabled  bool      `json:"totpEnabled"`
	Disabled     bool      `json:"disabled"`
	MustChangePW bool      `json:"mustChangePassword"`
	LastLoginAt  time.Time `json:"lastLoginAt"`
	CreatedAt    time.Time `json:"createdAt"`
}

func scanUser(row interface{ Scan(...any) error }) (*User, string, string, int, int64, error) {
	var (
		u          User
		role       string
		pwHash     string
		totpSecret string
		totpOK     int
		disabled   int
		mustChange int
		failed     int
		locked     int64
		lastLogin  int64
		created    int64
	)
	err := row.Scan(&u.ID, &u.Username, &pwHash, &role, &totpSecret, &totpOK, &disabled, &mustChange, &failed, &locked, &lastLogin, &created)
	if err != nil {
		return nil, "", "", 0, 0, err
	}
	u.Role = Role(role)
	u.TOTPEnabled = totpOK == 1
	u.Disabled = disabled == 1
	u.MustChangePW = mustChange == 1
	if lastLogin > 0 {
		u.LastLoginAt = time.Unix(lastLogin, 0).UTC()
	}
	u.CreatedAt = time.Unix(created, 0).UTC()
	return &u, pwHash, totpSecret, failed, locked, nil
}

const userCols = `id, username, password_hash, role, totp_secret, totp_enabled, disabled, must_change_pw, failed_count, locked_until, last_login_at, created_at`

func (s *Service) UserByID(ctx context.Context, id int64) (*User, error) {
	row := s.st.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id)
	u, _, _, _, _, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *Service) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.st.DB.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*User{}
	for rows.Next() {
		u, _, _, _, _, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Service) CreateUser(ctx context.Context, username, password string, role Role, mustChange bool) (*User, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	if username == "" {
		return nil, errors.New("username required")
	}
	if !role.Valid() {
		return nil, fmt.Errorf("unknown role %q", role)
	}
	if err := ValidatePasswordStrength(password); err != nil {
		return nil, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	mc := 0
	if mustChange {
		mc = 1
	}
	res, err := s.st.DB.ExecContext(ctx,
		`INSERT INTO users(username, password_hash, role, must_change_pw, created_at) VALUES(?,?,?,?,?)`,
		username, hash, string(role), mc, time.Now().Unix())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("user %q already exists", username)
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.UserByID(ctx, id)
}

func ValidatePasswordStrength(pw string) error {
	if len(pw) < 12 {
		return errors.New("password must be at least 12 characters")
	}
	var hasUpper, hasLower, hasDigit, hasOther bool
	for _, r := range pw {
		switch {
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= '0' && r <= '9':
			hasDigit = true
		default:
			hasOther = true
		}
	}
	classes := 0
	for _, ok := range []bool{hasUpper, hasLower, hasDigit, hasOther} {
		if ok {
			classes++
		}
	}
	if classes < 3 {
		return errors.New("password must mix at least three of: uppercase, lowercase, digits, symbols")
	}
	return nil
}

func (s *Service) SetPassword(ctx context.Context, userID int64, password string) error {
	if err := ValidatePasswordStrength(password); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.st.DB.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, must_change_pw = 0 WHERE id = ?`, hash, userID)
	return err
}

func (s *Service) VerifyUserPassword(ctx context.Context, userID int64, password string) bool {
	var hash string
	if err := s.st.DB.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash); err != nil {
		return false
	}
	return VerifyPassword(password, hash)
}

func (s *Service) SetRole(ctx context.Context, userID int64, role Role) error {
	if !role.Valid() {
		return fmt.Errorf("unknown role %q", role)
	}
	if role != RoleAdmin {
		if err := s.guardLastAdmin(ctx, userID); err != nil {
			return err
		}
	}
	_, err := s.st.DB.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ?`, string(role), userID)
	return err
}

func (s *Service) SetDisabled(ctx context.Context, userID int64, disabled bool) error {
	if disabled {
		if err := s.guardLastAdmin(ctx, userID); err != nil {
			return err
		}
	}
	v := 0
	if disabled {
		v = 1
	}
	if _, err := s.st.DB.ExecContext(ctx, `UPDATE users SET disabled = ? WHERE id = ?`, v, userID); err != nil {
		return err
	}
	if disabled {
		return s.RevokeAllSessions(ctx, userID)
	}
	return nil
}

func (s *Service) DeleteUser(ctx context.Context, userID int64) error {
	if err := s.guardLastAdmin(ctx, userID); err != nil {
		return err
	}
	_, err := s.st.DB.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	return err
}

// guardLastAdmin refuses changes that would leave the dashboard with no way in.
func (s *Service) guardLastAdmin(ctx context.Context, userID int64) error {
	var role string
	var disabled int
	err := s.st.DB.QueryRowContext(ctx, `SELECT role, disabled FROM users WHERE id = ?`, userID).Scan(&role, &disabled)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if Role(role) != RoleAdmin || disabled == 1 {
		return nil
	}
	var others int
	if err := s.st.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled = 0 AND id != ?`, userID).Scan(&others); err != nil {
		return err
	}
	if others == 0 {
		return ErrLastAdmin
	}
	return nil
}

// --- login ---

type LoginResult struct {
	User        *User
	Token       string
	SessionID   string
	NeedsTOTP   bool
	NeedsEnroll bool
	ExpiresAt   time.Time
}

func (s *Service) Login(ctx context.Context, username, password, ip, userAgent string) (*LoginResult, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	row := s.st.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE username = ?`, username)
	u, pwHash, _, failed, lockedUntil, err := scanUser(row)
	if err == sql.ErrNoRows {
		// Spend comparable time on unknown users so the response does not
		// disclose which usernames exist.
		HashPassword(password)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if lockedUntil > now.Unix() {
		return nil, ErrAccountLocked
	}
	if u.Disabled {
		return nil, ErrAccountDisabled
	}
	if !VerifyPassword(password, pwHash) {
		failed++
		var locked int64
		if failed >= maxFailedLogins {
			locked = now.Add(lockoutWindow).Unix()
			failed = 0
		}
		s.st.DB.ExecContext(ctx, `UPDATE users SET failed_count = ?, locked_until = ? WHERE id = ?`, failed, locked, u.ID)
		if locked > 0 {
			return nil, ErrAccountLocked
		}
		return nil, ErrInvalidCredentials
	}
	s.st.DB.ExecContext(ctx, `UPDATE users SET failed_count = 0, locked_until = 0 WHERE id = ?`, u.ID)

	// A session always starts un-elevated. Nothing beyond the 2FA endpoints
	// accepts it until the second factor is proved.
	sess, token, err := s.newSession(ctx, u.ID, ip, userAgent, !s.require2FA && !u.TOTPEnabled)
	if err != nil {
		return nil, err
	}
	res := &LoginResult{User: u, Token: token, SessionID: sess.ID, ExpiresAt: sess.ExpiresAt}
	switch {
	case u.TOTPEnabled:
		res.NeedsTOTP = true
	case s.require2FA:
		res.NeedsEnroll = true
	default:
		s.st.DB.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, now.Unix(), u.ID)
	}
	return res, nil
}

// --- TOTP ---

type EnrollmentSecret struct {
	Secret string `json:"secret"`
	URL    string `json:"otpauthUrl"`
}

func (s *Service) BeginTOTPEnrollment(ctx context.Context, userID int64) (*EnrollmentSecret, error) {
	u, err := s.UserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: totpIssuer, AccountName: u.Username})
	if err != nil {
		return nil, err
	}
	sealed, err := s.sealer.Seal(key.Secret())
	if err != nil {
		return nil, err
	}
	// Stored but not yet enabled: enrollment only completes once the user
	// proves they can generate a code from it.
	if _, err := s.st.DB.ExecContext(ctx,
		`UPDATE users SET totp_secret = ?, totp_enabled = 0 WHERE id = ?`, sealed, userID); err != nil {
		return nil, err
	}
	return &EnrollmentSecret{Secret: key.Secret(), URL: key.URL()}, nil
}

func (s *Service) ConfirmTOTPEnrollment(ctx context.Context, userID int64, code string) ([]string, error) {
	secret, err := s.totpSecret(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !totp.Validate(code, secret) {
		return nil, ErrInvalidTOTP
	}
	if _, err := s.st.DB.ExecContext(ctx, `UPDATE users SET totp_enabled = 1 WHERE id = ?`, userID); err != nil {
		return nil, err
	}
	return s.regenerateRecoveryCodes(ctx, userID)
}

func (s *Service) totpSecret(ctx context.Context, userID int64) (string, error) {
	var sealed string
	err := s.st.DB.QueryRowContext(ctx, `SELECT totp_secret FROM users WHERE id = ?`, userID).Scan(&sealed)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if sealed == "" {
		return "", errors.New("no enrollment in progress")
	}
	return s.sealer.Open(sealed)
}

// VerifySecondFactor accepts either a live TOTP code or a single-use recovery
// code, and elevates the session on success.
func (s *Service) VerifySecondFactor(ctx context.Context, sessionID string, userID int64, code string) error {
	code = strings.TrimSpace(strings.ReplaceAll(code, " ", ""))
	if secret, err := s.totpSecret(ctx, userID); err == nil {
		// Skew of one step tolerates ordinary clock drift between the server
		// and the authenticator without widening the window meaningfully.
		ok, err := totp.ValidateCustom(code, secret, time.Now(), totp.ValidateOpts{
			Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
		})
		if err == nil && ok {
			return s.elevate(ctx, sessionID, userID)
		}
	}
	if s.consumeRecoveryCode(ctx, userID, code) {
		return s.elevate(ctx, sessionID, userID)
	}
	return ErrInvalidTOTP
}

func (s *Service) elevate(ctx context.Context, sessionID string, userID int64) error {
	if _, err := s.st.DB.ExecContext(ctx,
		`UPDATE sessions SET twofa_passed = 1 WHERE id = ?`, sessionID); err != nil {
		return err
	}
	_, err := s.st.DB.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, time.Now().Unix(), userID)
	return err
}

func (s *Service) DisableTOTP(ctx context.Context, userID int64) error {
	if s.require2FA {
		return errors.New("two-factor authentication is mandatory and cannot be disabled")
	}
	_, err := s.st.DB.ExecContext(ctx, `UPDATE users SET totp_enabled = 0, totp_secret = '' WHERE id = ?`, userID)
	return err
}

// ResetTOTP clears an enrollment so a locked-out user can re-enroll. Admin only.
func (s *Service) ResetTOTP(ctx context.Context, userID int64) error {
	if _, err := s.st.DB.ExecContext(ctx,
		`UPDATE users SET totp_enabled = 0, totp_secret = '' WHERE id = ?`, userID); err != nil {
		return err
	}
	if _, err := s.st.DB.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return s.RevokeAllSessions(ctx, userID)
}

func (s *Service) regenerateRecoveryCodes(ctx context.Context, userID int64) ([]string, error) {
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	codes := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		c := RandomToken(6)
		codes = append(codes, c)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO recovery_codes(user_id, code_hash) VALUES(?, ?)`, userID, HashToken(c)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return codes, nil
}

func (s *Service) RegenerateRecoveryCodes(ctx context.Context, userID int64) ([]string, error) {
	return s.regenerateRecoveryCodes(ctx, userID)
}

func (s *Service) consumeRecoveryCode(ctx context.Context, userID int64, code string) bool {
	res, err := s.st.DB.ExecContext(ctx,
		`UPDATE recovery_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at = 0`,
		time.Now().Unix(), userID, HashToken(code))
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}
