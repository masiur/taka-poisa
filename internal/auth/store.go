// Package auth owns users, the first-run owner setup, passwords and sessions.
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
)

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
	// The account's ledger settings every money request needs.
	Timezone string
	Currency string
}

// OwnerInput creates the first user and their ledger account.
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

// createOwner is the single owner-creation path; a Google owner has no password hash
// and a linked identity instead.
func (s *Store) createOwner(ctx context.Context, in OwnerInput, hash *string, google *GoogleClaims) (int64, error) {
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
	if google != nil {
		if err := linkGoogle(ctx, tx, userID, *google); err != nil {
			return 0, err
		}
	}
	return userID, tx.Commit(ctx)
}

// GoogleSignIn resolves a verified Google account to a user:
//  1. a user already linked to this Google subject;
//  2. otherwise a user whose email equals the Google-verified email (the identity is linked);
//  3. otherwise, when setup is given and no user exists yet, a new owner.
//
// Any other case is ErrNoAccount: sign-up through Google is not open on self-hosted servers.
func (s *Store) GoogleSignIn(ctx context.Context, c GoogleClaims, setup *OwnerInput) (int64, error) {
	if !c.EmailVerified {
		return 0, ErrEmailUnverified
	}
	var userID int64
	err := s.db.QueryRow(ctx,
		`SELECT user_id FROM user_identities WHERE provider = 'google' AND subject = $1`, c.Subject).Scan(&userID)
	if err == nil {
		return userID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}

	email := validate.NormalizeEmail(c.Email)
	err = s.db.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if err == nil {
		return userID, linkGoogle(ctx, s.db, userID, c)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}

	if setup == nil {
		return 0, ErrNoAccount
	}
	in := *setup
	in.Email = email
	in.Name = googleDisplayName(c)
	return s.createOwner(ctx, in, nil, &c)
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func linkGoogle(ctx context.Context, db execer, userID int64, c GoogleClaims) error {
	_, err := db.Exec(ctx, `
		INSERT INTO user_identities (provider, subject, user_id, email) VALUES ('google', $1, $2, $3)
		ON CONFLICT (provider, subject) DO NOTHING`,
		c.Subject, userID, validate.NormalizeEmail(c.Email))
	return err
}

// googleDisplayName is the Google profile name, or the email's local part, cut to the 100-character limit.
func googleDisplayName(c GoogleClaims) string {
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
// Users without a password (Google-only) can never pass.
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
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
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
		SELECT u.id, u.name, u.email, u.locale, m.account_id, m.role, a.timezone, a.currency
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		JOIN account_members m ON m.user_id = u.id
		JOIN accounts a ON a.id = m.account_id
		WHERE s.id_hash = $1 AND s.expires_at > now()
		ORDER BY m.created_at
		LIMIT 1`, hashToken(token)).
		Scan(&p.UserID, &p.Name, &p.Email, &p.Locale, &p.AccountID, &p.Role, &p.Timezone, &p.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrNoSession
	}
	return p, err
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE id_hash = $1`, hashToken(token))
	return err
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
