// Package account owns ledger-book settings: name, currency, timezone, reminder hour, email language.
package account

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // timezone validation must not depend on the host's zoneinfo

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/masiur/taka-poisa/internal/validate"
)

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

type Settings struct {
	Name     string `json:"name"`
	Currency string `json:"currency"`
	Timezone string `json:"timezone"`
	SendHour int    `json:"send_hour"`
	Locale   string `json:"locale"`
}

func (s *Settings) Normalize() {
	s.Name = strings.TrimSpace(s.Name)
	s.Currency = strings.ToUpper(strings.TrimSpace(s.Currency))
	s.Timezone = strings.TrimSpace(s.Timezone)
}

func (s Settings) Validate() validate.Errors {
	e := validate.Errors{}
	e.Text("name", s.Name, 100)
	if !currencyRe.MatchString(s.Currency) {
		e.Add("currency", validate.Invalid)
	}
	// LoadLocation accepts "" and "Local" as host-dependent zones; only IANA names are allowed.
	if s.Timezone == "" || s.Timezone == "Local" {
		e.Add("timezone", validate.Required)
	} else if _, err := time.LoadLocation(s.Timezone); err != nil {
		e.Add("timezone", validate.Invalid)
	}
	if s.SendHour < 0 || s.SendHour > 23 {
		e.Add("send_hour", validate.Invalid)
	}
	e.Locale("locale", s.Locale)
	return e
}

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

func (st *Store) Get(ctx context.Context, accountID int64) (Settings, error) {
	var s Settings
	err := st.db.QueryRow(ctx,
		`SELECT name, currency, timezone, send_hour, locale FROM accounts WHERE id = $1`, accountID).
		Scan(&s.Name, &s.Currency, &s.Timezone, &s.SendHour, &s.Locale)
	return s, err
}

// ErrCurrencyLocked: the currency cannot change once the account has loans, so balances
// never add up amounts in different currencies.
var ErrCurrencyLocked = errors.New("currency is locked once loans exist")

// Update replaces the settings; s must be normalized and valid.
func (st *Store) Update(ctx context.Context, accountID int64, s Settings) error {
	tag, err := st.db.Exec(ctx, `
		UPDATE accounts SET name = $2, currency = $3, timezone = $4, send_hour = $5, locale = $6
		WHERE id = $1
		  AND (currency = $3 OR NOT EXISTS (SELECT 1 FROM agreements WHERE account_id = $1))`,
		accountID, s.Name, s.Currency, s.Timezone, s.SendHour, s.Locale)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrCurrencyLocked
	}
	return err
}
