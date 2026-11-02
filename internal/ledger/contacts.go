// Package ledger owns contacts, agreements (loans), installments, payments and balances.
// Every query is scoped to an account id.
package ledger

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/masiur/taka-poisa/internal/validate"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrHasOpenLoans   = errors.New("contact has open agreements")
	ErrExceedsBalance = errors.New("payment exceeds remaining balance")
	ErrBelowPaid      = errors.New("principal below amount already paid")
	ErrNotActive      = errors.New("agreement is not active")
)

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

type ContactInput struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	Phone  string `json:"phone"`
	Locale string `json:"locale"`
	Notes  string `json:"notes"`
}

var (
	e164Re       = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)
	phoneStripRe = regexp.MustCompile(`[\s\-().]`)
)

// NormalizePhone returns E.164. Bangladeshi local numbers (01XXXXXXXXX) get +880.
func NormalizePhone(v string) string {
	v = phoneStripRe.ReplaceAllString(strings.TrimSpace(v), "")
	switch {
	case strings.HasPrefix(v, "00"):
		v = "+" + v[2:]
	case strings.HasPrefix(v, "880"):
		v = "+" + v
	case len(v) == 11 && strings.HasPrefix(v, "01"):
		v = "+880" + v[1:]
	}
	return v
}

func (in *ContactInput) Normalize() {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = validate.NormalizeEmail(in.Email)
	in.Phone = NormalizePhone(in.Phone)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Locale == "" {
		in.Locale = "bn"
	}
}

func (in ContactInput) Validate() validate.Errors {
	e := validate.Errors{}
	e.Text("name", in.Name, 100)
	if in.Email != "" {
		e.Email("email", in.Email)
	}
	if in.Phone != "" && !e164Re.MatchString(in.Phone) {
		e.Add("phone", validate.Invalid)
	}
	e.Locale("locale", in.Locale)
	if len([]rune(in.Notes)) > 2000 {
		e.Add("notes", validate.TooLong)
	}
	return e
}

type Contact struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	Phone         string    `json:"phone"`
	Locale        string    `json:"locale"`
	Notes         string    `json:"notes"`
	EmailOptedOut bool      `json:"email_opted_out"`
	CreatedAt     time.Time `json:"created_at"`
}

// ContactSummary is a contact with balances over their open agreements.
type ContactSummary struct {
	Contact
	Receivable int64 `json:"receivable"` // they owe me
	Payable    int64 `json:"payable"`    // I owe them
	Overdue    int64 `json:"overdue"`
	OpenLoans  int   `json:"open_loans"`
}

const contactCols = `c.id, c.name, coalesce(c.email, ''), coalesce(c.phone_e164, ''), c.locale, c.notes,
	c.email_opted_out_at IS NOT NULL, c.created_at`

func scanContact(row pgx.Row, c *Contact, extra ...any) error {
	return row.Scan(append([]any{&c.ID, &c.Name, &c.Email, &c.Phone, &c.Locale, &c.Notes, &c.EmailOptedOut, &c.CreatedAt}, extra...)...)
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *Store) CreateContact(ctx context.Context, accountID, userID int64, in ContactInput) (int64, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO contacts (account_id, name, email, phone_e164, locale, notes)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		accountID, in.Name, nullIfEmpty(in.Email), nullIfEmpty(in.Phone), in.Locale, in.Notes).Scan(&id); err != nil {
		return 0, err
	}
	if err := logActivity(ctx, tx, accountID, 0, id, userID, "contact.created", nil); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// UpdateContact saves the contact and replans their reminders (the email may have changed).
func (s *Store) UpdateContact(ctx context.Context, accountID, id int64, in ContactInput) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE contacts SET name = $3, email = $4, phone_e164 = $5, locale = $6, notes = $7, updated_at = now()
		WHERE account_id = $1 AND id = $2 AND deleted_at IS NULL`,
		accountID, id, in.Name, nullIfEmpty(in.Email), nullIfEmpty(in.Phone), in.Locale, in.Notes)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := replanContact(ctx, tx, accountID, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteContact hides a contact. It is refused while they have open (active or requested) agreements,
// so no balance silently disappears.
func (s *Store) DeleteContact(ctx context.Context, accountID, id int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var open bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM agreements WHERE account_id = $1 AND contact_id = $2 AND status IN ('active', 'requested'))
		FROM contacts WHERE account_id = $1 AND id = $2 AND deleted_at IS NULL FOR UPDATE`, accountID, id).Scan(&open)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if open {
		return ErrHasOpenLoans
	}
	if _, err := tx.Exec(ctx, `UPDATE contacts SET deleted_at = now() WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetContact(ctx context.Context, accountID, id int64) (Contact, error) {
	var c Contact
	err := scanContact(s.db.QueryRow(ctx,
		`SELECT `+contactCols+` FROM contacts c WHERE c.account_id = $1 AND c.id = $2 AND c.deleted_at IS NULL`,
		accountID, id), &c)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// ListContacts returns contacts with balances, optionally filtered by a name/email/phone search.
func (s *Store) ListContacts(ctx context.Context, accountID int64, today Date, q string) ([]ContactSummary, error) {
	rows, err := s.db.Query(ctx, `
		WITH `+summaryCTE+`
		SELECT `+contactCols+`,
			coalesce(sum(s.remaining) FILTER (WHERE s.direction = 'lent'), 0),
			coalesce(sum(s.remaining) FILTER (WHERE s.direction = 'borrowed'), 0),
			coalesce(sum(s.overdue), 0),
			count(s.id)
		FROM contacts c
		LEFT JOIN summary s ON s.contact_id = c.id AND s.status = 'active'
		WHERE c.account_id = $1 AND c.deleted_at IS NULL
		  AND ($3 = '' OR c.name ILIKE '%' || $3 || '%' OR c.email ILIKE '%' || $3 || '%' OR c.phone_e164 LIKE '%' || $3 || '%')
		GROUP BY c.id
		ORDER BY lower(c.name), c.id`, accountID, today, strings.TrimSpace(q))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContactSummary{}
	for rows.Next() {
		var c ContactSummary
		if err := scanContact(rows, &c.Contact, &c.Receivable, &c.Payable, &c.Overdue, &c.OpenLoans); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
