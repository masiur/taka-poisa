// Package auth owns users, the first-run owner setup, sign-up, email verification, passwords and sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/masiur/taka-poisa/internal/validate"
)

const SessionTTL = 30 * 24 * time.Hour

// setupLockKey serialises concurrent first-run setups (pg_advisory_xact_lock key).
const setupLockKey = 7_301_001

var (
	ErrAlreadySetUp       = errors.New("already set up")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrNoSession          = errors.New("no session")
	ErrNoAccount          = errors.New("no account for this sign-in")
	ErrEmailUnverified    = errors.New("email not verified by provider")
	ErrEmailTaken         = errors.New("email already registered")
	ErrSetupNeeded        = errors.New("first-run setup not done")
	ErrBadVerification    = errors.New("verification link expired or not valid")
)

// VerificationTTL is how long an email verification link works.
const VerificationTTL = 7 * 24 * time.Hour

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// Principal is the signed-in user and the account they are acting in.
type Principal struct {
	UserID    int64
	Name      string
	Email     string
	Locale    string
	AccountID int64
	Role      string
	// EmailVerified: the user proved they own Email (a verification link or an outside sign-in).
	EmailVerified bool
	// The account's ledger settings every money request needs.
	Timezone string
	Currency string
}

// OwnerInput creates a user and their ledger account: the first-run owner or a sign-up.
type OwnerInput struct {
	Name        string `json:"name"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	AccountName string `json:"account_name"`
	Locale      string `json:"locale"`
}

func (in *OwnerInput) Normalize() {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = validate.NormalizeEmail(in.Email)
	in.AccountName = strings.TrimSpace(in.AccountName)
	if in.AccountName == "" {
		in.AccountName = DefaultAccountName(in.Locale)
	}
}

// DefaultAccountName names the first ledger. People group contacts with tags, not separate ledgers.
func DefaultAccountName(locale string) string {
	if locale == "en" {
		return "Personal"
	}
	return "ব্যক্তিগত"
}

func (in OwnerInput) Validate() validate.Errors {
	e := validate.Errors{}
	e.Text("name", in.Name, 100)
	e.Email("email", in.Email)
	switch {
	case len(in.Password) < 8:
		e.Add("password", validate.TooShort)
	case len(in.Password) > 200:
		e.Add("password", validate.TooLong)
	}
	e.Text("account_name", in.AccountName, 100)
	e.Locale("locale", in.Locale)
	return e
}

func (s *Store) SetupNeeded(ctx context.Context) (bool, error) {
	var exists bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists)
	return !exists, err
}

// CreateOwner creates the first user as owner of a new account. It fails with
// ErrAlreadySetUp once any user exists. in must be normalized and valid.
func (s *Store) CreateOwner(ctx context.Context, in OwnerInput) (int64, error) {
	hash, err := HashPassword(in.Password)
	if err != nil {
		return 0, err
	}
	return s.createOwner(ctx, in, &hash, nil)
}

// createOwner is the single owner-creation path; an owner from an outside sign-in has no
// password hash and a linked identity instead.
func (s *Store) createOwner(ctx context.Context, in OwnerInput, hash *string, ext *linkedIdentity) (int64, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, setupLockKey); err != nil {
		return 0, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists); err != nil {
		return 0, err
	}
	if exists {
		return 0, ErrAlreadySetUp
	}
	userID, err := insertUser(ctx, tx, in, hash)
	if err != nil {
		return 0, err
	}
	if ext != nil {
		if err := linkIdentity(ctx, tx, userID, *ext); err != nil {
			return 0, err
		}
	}
	return userID, tx.Commit(ctx)
}

// Signup creates a password user as owner of their own new ledger. The email stays unverified
// until they open a verification link. in must be normalized and valid.
func (s *Store) Signup(ctx context.Context, in OwnerInput) (int64, error) {
	hash, err := HashPassword(in.Password)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// Sign-up opens only after the first-run owner exists; the lock orders it after a setup in flight.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, setupLockKey); err != nil {
		return 0, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists); err != nil {
		return 0, err
	}
	if !exists {
		return 0, ErrSetupNeeded
	}
	userID, err := insertUser(ctx, tx, in, &hash)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // users_email_key
		return 0, ErrEmailTaken
	}
	if err != nil {
		return 0, err
	}
	return userID, tx.Commit(ctx)
}

// insertUser adds a user, a new ledger account named in.AccountName, and the owner membership.
func insertUser(ctx context.Context, tx pgx.Tx, in OwnerInput, hash *string) (int64, error) {
	var accountID, userID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO accounts (name, locale) VALUES ($1, $2) RETURNING id`,
		in.AccountName, in.Locale).Scan(&accountID); err != nil {
		return 0, err
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, locale) VALUES ($1, $2, $3, $4) RETURNING id`,
		in.Email, hash, in.Name, in.Locale).Scan(&userID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO account_members (account_id, user_id, role) VALUES ($1, $2, 'owner')`,
		accountID, userID); err != nil {
		return 0, err
	}
	return userID, nil
}

// ExternalSignIn resolves an identity verified by an outside provider to a user:
//  1. a user already linked to this provider subject;
//  2. otherwise a user whose email equals the provider-verified email (the identity is linked; see claimUser);
//  3. otherwise, when setup is given and no user exists yet, a new owner.
//
// Any other case is ErrNoAccount: an outside sign-in never opens sign-up by itself.
func (s *Store) ExternalSignIn(ctx context.Context, provider string, c ExternalIdentity, setup *OwnerInput) (int64, error) {
	if !c.EmailVerified {
		return 0, ErrEmailUnverified
	}
	id := linkedIdentity{provider, c}
	var userID int64
	err := s.db.QueryRow(ctx,
		`SELECT user_id FROM user_identities WHERE provider = $1 AND subject = $2`, provider, c.Subject).Scan(&userID)
	if err == nil {
		return userID, markVerified(ctx, s.db, userID, c)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}

	email := validate.NormalizeEmail(c.Email)
	err = s.db.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if err == nil {
		return userID, s.claimUser(ctx, userID, id)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}

	if setup == nil {
		return 0, ErrNoAccount
	}
	in := *setup
	in.Email = email
	in.Name = displayName(c)
	return s.createOwner(ctx, in, nil, &id)
}

// claimUser links an outside identity to an existing user with the same email. If that user
// signed up and never verified the email, whoever signed up may not own it: their password and
// sessions are dropped, so the provider-verified owner of the email is the only one who can sign
// in. The first-run owner (the first user) set up this server and keeps their password.
func (s *Store) claimUser(ctx context.Context, userID int64, id linkedIdentity) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx,
		`UPDATE users SET password_hash = NULL
		WHERE id = $1 AND email_verified_at IS NULL AND id <> (SELECT min(id) FROM users)`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
			return err
		}
	}
	if err := linkIdentity(ctx, tx, userID, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Verification is a new email verification link's token and who to send it to.
type Verification struct {
	Token  string
	Email  string
	Name   string
	Locale string
}

// CreateVerification issues an email verification token for the user. ok is false when the
// email is already verified (nothing to send).
func (s *Store) CreateVerification(ctx context.Context, userID int64) (v Verification, ok bool, err error) {
	var verified bool
	if err := s.db.QueryRow(ctx,
		`SELECT email, name, locale, email_verified_at IS NOT NULL FROM users WHERE id = $1`, userID).
		Scan(&v.Email, &v.Name, &v.Locale, &verified); err != nil || verified {
		return v, false, err
	}
	if v.Token, err = randomToken(); err != nil {
		return v, false, err
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO email_verifications (token_hash, user_id, email, expires_at) VALUES ($1, $2, $3, $4)`,
		hashToken(v.Token), userID, v.Email, time.Now().Add(VerificationTTL)); err != nil {
		return v, false, err
	}
	return v, true, nil
}

// VerifyEmail marks the user's email verified if the token is valid, unexpired and was issued
// for their current email. The user's verification tokens are then used up.
func (s *Store) VerifyEmail(ctx context.Context, token string) error {
	var userID int64
	err := s.db.QueryRow(ctx, `
		UPDATE users u SET email_verified_at = coalesce(u.email_verified_at, now())
		FROM email_verifications v
		WHERE v.token_hash = $1 AND v.expires_at > now() AND v.user_id = u.id AND v.email = u.email
		RETURNING u.id`, hashToken(token)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBadVerification
	}
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `DELETE FROM email_verifications WHERE user_id = $1`, userID)
	return err
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type linkedIdentity struct {
	provider string
	ExternalIdentity
}

func linkIdentity(ctx context.Context, db execer, userID int64, id linkedIdentity) error {
	_, err := db.Exec(ctx, `
		INSERT INTO user_identities (provider, subject, user_id, email) VALUES ($1, $2, $3, $4)
		ON CONFLICT (provider, subject) DO NOTHING`,
		id.provider, id.Subject, userID, validate.NormalizeEmail(id.Email))
	if err != nil {
		return err
	}
	return markVerified(ctx, db, userID, id.ExternalIdentity)
}

// markVerified records that the user proved they own their account email (the provider verified it).
// Features that trust an email match, such as incoming loan requests, require this.
func markVerified(ctx context.Context, db execer, userID int64, c ExternalIdentity) error {
	_, err := db.Exec(ctx, `
		UPDATE users SET email_verified_at = now()
		WHERE id = $1 AND email = $2 AND email_verified_at IS NULL`, userID, validate.NormalizeEmail(c.Email))
	return err
}

// displayName is the provider's profile name, or the email's local part, cut to the 100-character limit.
func displayName(c ExternalIdentity) string {
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name, _, _ = strings.Cut(c.Email, "@")
	}
	if r := []rune(name); len(r) > 100 {
		name = string(r[:100])
	}
	return name
}

// Authenticate returns the user id for a correct email and password.
// Users without a password (outside sign-in only) can never pass.
func (s *Store) Authenticate(ctx context.Context, email, password string) (int64, error) {
	var id int64
	var hash *string
	err := s.db.QueryRow(ctx, `SELECT id, password_hash FROM users WHERE email = $1`,
		validate.NormalizeEmail(email)).Scan(&id, &hash)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && hash == nil) {
		_, _ = VerifyPassword(password, dummyHash())
		return 0, ErrInvalidCredentials
	}
	if err != nil {
		return 0, err
	}
	ok, err := VerifyPassword(password, *hash)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrInvalidCredentials
	}
	return id, nil
}

// CreateSession issues a new random session token; only its hash is stored.
func (s *Store) CreateSession(ctx context.Context, userID int64) (token string, expires time.Time, err error) {
	if token, err = randomToken(); err != nil {
		return "", time.Time{}, err
	}
	expires = time.Now().Add(SessionTTL)

	if _, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1 AND expires_at < now()`, userID); err != nil {
		return "", time.Time{}, err
	}
	if _, err := s.db.Exec(ctx,
		`INSERT INTO sessions (id_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		hashToken(token), userID, expires); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// Principal resolves a session token to the user and their account.
func (s *Store) Principal(ctx context.Context, token string) (Principal, error) {
	var p Principal
	err := s.db.QueryRow(ctx, `
		SELECT u.id, u.name, u.email, u.locale, u.email_verified_at IS NOT NULL, m.account_id, m.role, a.timezone, a.currency
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		JOIN account_members m ON m.user_id = u.id
		JOIN accounts a ON a.id = m.account_id
		WHERE s.id_hash = $1 AND s.expires_at > now()
		ORDER BY m.created_at
		LIMIT 1`, hashToken(token)).
		Scan(&p.UserID, &p.Name, &p.Email, &p.Locale, &p.EmailVerified, &p.AccountID, &p.Role, &p.Timezone, &p.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrNoSession
	}
	return p, err
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE id_hash = $1`, hashToken(token))
	return err
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
