package ledger

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Date is a calendar day with no time or zone, "2006-01-02" in JSON and a Postgres date.
type Date struct {
	t time.Time // always midnight UTC
}

const dateLayout = "2006-01-02"

func NewDate(year int, month time.Month, day int) Date {
	return Date{time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return Date{}, err
	}
	return Date{t}, nil
}

// Today is the current day in the given IANA timezone (the account's).
func Today(tz string) Date {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	y, m, d := time.Now().In(loc).Date()
	return NewDate(y, m, d)
}

func (d Date) IsZero() bool            { return d.t.IsZero() }
func (d Date) String() string          { return d.t.Format(dateLayout) }
func (d Date) Before(o Date) bool      { return d.t.Before(o.t) }
func (d Date) AddDays(n int) Date      { return Date{d.t.AddDate(0, 0, n)} }
func (d Date) Year() int               { return d.t.Year() }
func (d Date) Time() time.Time         { return d.t }

// AddMonthsClamped adds n months keeping day as the target day, clamped to the month's
// last day: Jan 31 + 1 month = Feb 28 (or 29), + 2 months = Mar 31.
func (d Date) AddMonthsClamped(n, day int) Date {
	first := time.Date(d.t.Year(), d.t.Month()+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return NewDate(first.Year(), first.Month(), min(day, last))
}

func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(d.String())
}

func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == "" {
		*d = Date{}
		return nil
	}
	parsed, err := ParseDate(s)
	if err != nil {
		return errors.New("date must be YYYY-MM-DD")
	}
	*d = parsed
	return nil
}

// ScanDate and DateValue let pgx read and write Date as a Postgres date.
func (d *Date) ScanDate(v pgtype.Date) error {
	if !v.Valid {
		*d = Date{}
		return nil
	}
	y, m, day := v.Time.Date()
	*d = NewDate(y, m, day)
	return nil
}

func (d Date) DateValue() (pgtype.Date, error) {
	return pgtype.Date{Time: d.t, Valid: !d.IsZero()}, nil
}
