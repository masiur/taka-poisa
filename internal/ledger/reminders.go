package ledger

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/masiur/taka-poisa/internal/validate"
)

const (
	MaxRules          = 10
	maxPlannedPerLoan = 200
	reminderLinkTTL   = 90 * 24 * time.Hour
)

var (
	ErrAlreadySentToday = errors.New("a reminder was already sent today")
	ErrNotEligible      = errors.New("recipient cannot receive reminders")
)

// Rule is one reminder: offset_days from each unpaid installment's due date (negative = before),
// optionally repeated, sent to the contact or to the owner ("self").
type Rule struct {
	ID              int64  `json:"id"`
	OffsetDays      int    `json:"offset_days"`
	RepeatEveryDays *int   `json:"repeat_every_days"`
	RepeatCount     *int   `json:"repeat_count"`
	SendHour        *int   `json:"send_hour"`
	Recipient       string `json:"recipient"` // contact | self
	Channel         string `json:"channel"`   // email (default) | sms (contact only)
	Tone            string `json:"tone"`      // polite | firm
	Paused          bool   `json:"paused"`
}

// channel is the rule's channel, email when unset.
func (r Rule) channel() string {
	if r.Channel == "" {
		return "email"
	}
	return r.Channel
}

// DefaultRules apply when an account has not set its own defaults: on the due date,
// 3 and 7 days late (the last one firm), and a heads-up to the owner the day before.
func DefaultRules() []Rule {
	return []Rule{
		{OffsetDays: 0, Recipient: "contact", Tone: "polite"},
		{OffsetDays: 3, Recipient: "contact", Tone: "polite"},
		{OffsetDays: 7, Recipient: "contact", Tone: "firm"},
		{OffsetDays: -1, Recipient: "self", Tone: "polite"},
	}
}

func ValidateRules(rules []Rule) validate.Errors {
	e := validate.Errors{}
	if len(rules) > MaxRules {
		e.Add("rules", validate.TooLong)
		return e
	}
	for _, r := range rules {
		switch {
		case r.OffsetDays < -60 || r.OffsetDays > 60,
			(r.RepeatEveryDays == nil) != (r.RepeatCount == nil),
			r.RepeatEveryDays != nil && (*r.RepeatEveryDays < 1 || *r.RepeatEveryDays > 30),
			r.RepeatCount != nil && (*r.RepeatCount < 1 || *r.RepeatCount > 20),
			r.SendHour != nil && (*r.SendHour < 0 || *r.SendHour > 23),
			r.Recipient != "contact" && r.Recipient != "self",
			r.channel() != "email" && r.channel() != "sms",
			r.channel() == "sms" && r.Recipient != "contact",
			r.Tone != "polite" && r.Tone != "firm":
			e.Add("rules", validate.Invalid)
			return e
		}
	}
	return e
}

type PlannedSend struct {
	RuleID    int64
	Recipient string
	Channel   string
	Tone      string
	Day       Date
	At        time.Time
}

// PlanSends lists the future reminders for an agreement. It is pure so it can be tested
// without a database. Rules landing on the same day for the same recipient collapse into one
// send per channel (the firmer tone wins); only "recipient/channel" pairs in eligible are planned
// (for example "contact/sms"); at most maxPlannedPerLoan.
func PlanSends(items []InstallmentState, rules []Rule, now time.Time, loc *time.Location, defaultHour int, eligible map[string]bool) []PlannedSend {
	type key struct {
		recipient, channel string
		day                Date
	}
	byDay := map[key]PlannedSend{}
	for _, it := range items {
		if it.Remaining <= 0 {
			continue
		}
		for _, r := range rules {
			if r.Paused || !eligible[r.Recipient+"/"+r.channel()] {
				continue
			}
			start := it.DueOn.AddDays(r.OffsetDays)
			days := []Date{start}
			if r.RepeatEveryDays != nil && r.RepeatCount != nil {
				for k := 1; k <= *r.RepeatCount; k++ {
					days = append(days, start.AddDays(k**r.RepeatEveryDays))
				}
			}
			hour := defaultHour
			if r.SendHour != nil {
				hour = *r.SendHour
			}
			for _, d := range days {
				y, m, dd := d.Time().Date()
				at := time.Date(y, m, dd, hour, 0, 0, 0, loc)
				if !at.After(now) {
					continue
				}
				k := key{r.Recipient, r.channel(), d}
				prev, seen := byDay[k]
				if !seen || (prev.Tone == "polite" && r.Tone == "firm") || (prev.Tone == r.Tone && at.Before(prev.At)) {
					byDay[k] = PlannedSend{RuleID: r.ID, Recipient: r.Recipient, Channel: r.channel(), Tone: r.Tone, Day: d, At: at}
				}
			}
		}
	}
	out := make([]PlannedSend, 0, len(byDay))
	for _, p := range byDay {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b PlannedSend) int {
		return cmp.Or(a.At.Compare(b.At), cmp.Compare(a.Recipient, b.Recipient), cmp.Compare(a.Channel, b.Channel))
	})
	if len(out) > maxPlannedPerLoan {
		out = out[:maxPlannedPerLoan]
	}
	return out
}

// querier is satisfied by pgx.Tx and *pgxpool.Pool.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func loadRules(ctx context.Context, q querier, accountID int64, agreementID *int64) ([]Rule, error) {
	rows, err := q.Query(ctx, `
		SELECT id, offset_days, repeat_every_days, repeat_count, send_hour, recipient, channel, tone, paused_at IS NOT NULL
		FROM reminder_rules
		WHERE account_id = $1 AND agreement_id IS NOT DISTINCT FROM $2
		ORDER BY position, id`, accountID, agreementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.OffsetDays, &r.RepeatEveryDays, &r.RepeatCount, &r.SendHour, &r.Recipient, &r.Channel, &r.Tone, &r.Paused); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func insertRules(ctx context.Context, tx pgx.Tx, accountID int64, agreementID *int64, rules []Rule) error {
	for i, r := range rules {
		if _, err := tx.Exec(ctx, `
			INSERT INTO reminder_rules (account_id, agreement_id, offset_days, repeat_every_days, repeat_count,
				send_hour, recipient, channel, tone, position, paused_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, CASE WHEN $11 THEN now() END)`,
			accountID, agreementID, r.OffsetDays, r.RepeatEveryDays, r.RepeatCount, r.SendHour,
			r.Recipient, r.channel(), r.Tone, i, r.Paused); err != nil {
			return err
		}
	}
	return nil
}

// copyDefaultRules gives a new agreement the account's default rules (or the built-in set).
func copyDefaultRules(ctx context.Context, tx pgx.Tx, accountID, agreementID int64) error {
	rules, err := loadRules(ctx, tx, accountID, nil)
	if err != nil {
		return err
	}
	if len(rules) == 0 {
		rules = DefaultRules()
	}
	return insertRules(ctx, tx, accountID, &agreementID, rules)
}

// replan regenerates an agreement's planned sends from its current state. It runs in the same
// transaction as every change that can affect reminders, so the plan is never stale.
func replan(ctx context.Context, tx pgx.Tx, accountID, agreementID int64) error {
	var (
		status, direction, tz          string
		sendHour                       int
		contactEmail, contactPhone     *string
		optedOut, smsOptedOut, deleted bool
	)
	err := tx.QueryRow(ctx, `
		SELECT a.status, a.direction, acc.timezone, acc.send_hour, c.email, c.phone_e164,
			c.email_opted_out_at IS NOT NULL, c.sms_opted_out_at IS NOT NULL, c.deleted_at IS NOT NULL
		FROM agreements a
		JOIN accounts acc ON acc.id = a.account_id
		JOIN contacts c ON c.id = a.contact_id
		WHERE a.account_id = $1 AND a.id = $2`, accountID, agreementID).
		Scan(&status, &direction, &tz, &sendHour, &contactEmail, &contactPhone, &optedOut, &smsOptedOut, &deleted)
	if err != nil {
		return err
	}
	// Manual "send now" rows stay queued; the worker re-checks them at send time.
	if _, err := tx.Exec(ctx, `DELETE FROM reminder_sends WHERE agreement_id = $1 AND status = 'planned' AND NOT manual`, agreementID); err != nil {
		return err
	}
	if status != "active" {
		return nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}

	paid, err := paidTotal(ctx, tx, agreementID)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id, seq, due_on, amount FROM installments WHERE agreement_id = $1`, agreementID)
	if err != nil {
		return err
	}
	var items []InstallmentState
	for rows.Next() {
		var it InstallmentState
		if err := rows.Scan(&it.ID, &it.Seq, &it.DueOn, &it.Amount); err != nil {
			rows.Close()
			return err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	rules, err := loadRules(ctx, tx, accountID, &agreementID)
	if err != nil {
		return err
	}

	// Only someone who owes money is reminded: the contact when I lent to them.
	owes := direction == "lent" && !deleted
	eligible := map[string]bool{
		"self/email":    true,
		"contact/email": owes && contactEmail != nil && !optedOut,
		"contact/sms":   owes && contactPhone != nil && !smsOptedOut,
	}
	now := time.Now()
	plan := PlanSends(Allocate(items, paid, Today(tz)), rules, now, loc, sendHour, eligible)
	for _, p := range plan {
		var ruleID *int64
		if p.RuleID != 0 {
			ruleID = &p.RuleID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO reminder_sends (account_id, agreement_id, rule_id, recipient, channel, tone, send_day, scheduled_for)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (agreement_id, recipient, channel, send_day) WHERE status IN ('planned', 'sending', 'sent')
			DO NOTHING`,
			accountID, agreementID, ruleID, p.Recipient, p.Channel, p.Tone, p.Day, p.At); err != nil {
			return err
		}
	}
	return nil
}

// replanContact refreshes every active agreement of a contact (after an email or opt-out change).
func replanContact(ctx context.Context, tx pgx.Tx, accountID, contactID int64) error {
	ids, err := collectIDs(ctx, tx, `SELECT id FROM agreements WHERE account_id = $1 AND contact_id = $2 AND status = 'active'`, accountID, contactID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := replan(ctx, tx, accountID, id); err != nil {
			return err
		}
	}
	return nil
}

// ReplanAll refreshes every active agreement, one transaction each. It runs at startup so plans
// made by an older version (or none at all, right after an upgrade) are brought up to date.
func (s *Store) ReplanAll(ctx context.Context) (int, error) {
	rows, err := s.db.Query(ctx, `SELECT account_id, id FROM agreements WHERE status = 'active' ORDER BY id`)
	if err != nil {
		return 0, err
	}
	type ref struct{ account, id int64 }
	refs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ref, error) {
		var x ref
		return x, r.Scan(&x.account, &x.id)
	})
	if err != nil {
		return 0, err
	}
	for _, r := range refs {
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return 0, err
		}
		err = replan(ctx, tx, r.account, r.id)
		if err == nil {
			err = tx.Commit(ctx)
		}
		tx.Rollback(ctx)
		if err != nil {
			return 0, err
		}
	}
	return len(refs), nil
}

// ReplanAccount refreshes all active agreements, after the timezone or send hour changes.
func (s *Store) ReplanAccount(ctx context.Context, accountID int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	ids, err := collectIDs(ctx, tx, `SELECT id FROM agreements WHERE account_id = $1 AND status = 'active'`, accountID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := replan(ctx, tx, accountID, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func collectIDs(ctx context.Context, q querier, sql string, args ...any) ([]int64, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

type SendRow struct {
	ID           int64      `json:"id"`
	RuleID       *int64     `json:"rule_id"`
	Recipient    string     `json:"recipient"`
	Channel      string     `json:"channel"`
	Tone         string     `json:"tone"`
	Status       string     `json:"status"`
	SkipReason   string     `json:"skip_reason"`
	ScheduledFor time.Time  `json:"scheduled_for"`
	SentAt       *time.Time `json:"sent_at"`
}

type ReminderView struct {
	Rules              []Rule    `json:"rules"`
	Upcoming           []SendRow `json:"upcoming"`
	Recent             []SendRow `json:"recent"`
	ContactEmail       string    `json:"contact_email"`
	ContactOptedOut    bool      `json:"contact_opted_out"`
	ContactPhone       string    `json:"contact_phone"`
	ContactSMSOptedOut bool      `json:"contact_sms_opted_out"`
	Direction          string    `json:"direction"`
}

func (s *Store) Reminders(ctx context.Context, accountID, agreementID int64) (ReminderView, error) {
	v := ReminderView{Upcoming: []SendRow{}, Recent: []SendRow{}}
	err := s.db.QueryRow(ctx, `
		SELECT a.direction, coalesce(c.email, ''), c.email_opted_out_at IS NOT NULL,
			coalesce(c.phone_e164, ''), c.sms_opted_out_at IS NOT NULL
		FROM agreements a JOIN contacts c ON c.id = a.contact_id
		WHERE a.account_id = $1 AND a.id = $2`, accountID, agreementID).
		Scan(&v.Direction, &v.ContactEmail, &v.ContactOptedOut, &v.ContactPhone, &v.ContactSMSOptedOut)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	if v.Rules, err = loadRules(ctx, s.db, accountID, &agreementID); err != nil {
		return v, err
	}
	rows, err := s.db.Query(ctx, `
		(SELECT id, rule_id, recipient, channel, tone, status, coalesce(skip_reason, ''), scheduled_for, sent_at
		 FROM reminder_sends WHERE agreement_id = $1 AND status IN ('planned', 'sending')
		 ORDER BY scheduled_for LIMIT 10)
		UNION ALL
		(SELECT id, rule_id, recipient, channel, tone, status, coalesce(skip_reason, ''), scheduled_for, sent_at
		 FROM reminder_sends WHERE agreement_id = $1 AND status IN ('sent', 'skipped', 'failed')
		 ORDER BY scheduled_for DESC LIMIT 20)`, agreementID)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var r SendRow
		if err := rows.Scan(&r.ID, &r.RuleID, &r.Recipient, &r.Channel, &r.Tone, &r.Status, &r.SkipReason, &r.ScheduledFor, &r.SentAt); err != nil {
			return v, err
		}
		if r.Status == "planned" || r.Status == "sending" {
			v.Upcoming = append(v.Upcoming, r)
		} else {
			v.Recent = append(v.Recent, r)
		}
	}
	return v, rows.Err()
}

// SetRules replaces an agreement's reminder rules and replans.
func (s *Store) SetRules(ctx context.Context, accountID, userID, agreementID int64, rules []Rule) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	a, err := lockAgreement(ctx, tx, accountID, agreementID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM reminder_rules WHERE agreement_id = $1`, agreementID); err != nil {
		return err
	}
	if err := insertRules(ctx, tx, accountID, &agreementID, rules); err != nil {
		return err
	}
	if err := logActivity(ctx, tx, accountID, agreementID, a.contactID, userID, "reminders.updated", map[string]any{"rules": len(rules)}); err != nil {
		return err
	}
	if err := replan(ctx, tx, accountID, agreementID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) DefaultRulesFor(ctx context.Context, accountID int64) ([]Rule, error) {
	rules, err := loadRules(ctx, s.db, accountID, nil)
	if err == nil && len(rules) == 0 {
		rules = DefaultRules()
	}
	return rules, err
}

// SetDefaultRules replaces the account's defaults; existing agreements keep their own rules.
func (s *Store) SetDefaultRules(ctx context.Context, accountID int64, rules []Rule) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM reminder_rules WHERE account_id = $1 AND agreement_id IS NULL`, accountID); err != nil {
		return err
	}
	if err := insertRules(ctx, tx, accountID, nil, rules); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SendNow queues a reminder for immediate delivery. It still counts towards the
// one-message-per-day limit (per channel), so it cannot be used to spam.
func (s *Store) SendNow(ctx context.Context, accountID, userID, agreementID int64, recipient, channel string) error {
	if channel == "" {
		channel = "email"
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	a, err := lockAgreement(ctx, tx, accountID, agreementID)
	if err != nil {
		return err
	}
	if a.status != "active" {
		return ErrNotActive
	}
	var direction, tz string
	var email, phone *string
	var optedOut, smsOptedOut bool
	if err := tx.QueryRow(ctx, `
		SELECT a.direction, acc.timezone, c.email, c.phone_e164, c.email_opted_out_at IS NOT NULL, c.sms_opted_out_at IS NOT NULL
		FROM agreements a JOIN accounts acc ON acc.id = a.account_id JOIN contacts c ON c.id = a.contact_id
		WHERE a.id = $1`, agreementID).Scan(&direction, &tz, &email, &phone, &optedOut, &smsOptedOut); err != nil {
		return err
	}
	switch {
	case recipient == "self" && channel != "email",
		recipient == "contact" && direction != "lent",
		recipient == "contact" && channel == "email" && (email == nil || optedOut),
		recipient == "contact" && channel == "sms" && (phone == nil || smsOptedOut):
		return ErrNotEligible
	}
	// A rule-planned send for today (if any) is replaced by this one; a queued manual one is not.
	if _, err := tx.Exec(ctx, `
		DELETE FROM reminder_sends WHERE agreement_id = $1 AND recipient = $2 AND channel = $3 AND send_day = $4
			AND status = 'planned' AND NOT manual`,
		agreementID, recipient, channel, Today(tz)); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO reminder_sends (account_id, agreement_id, recipient, channel, tone, send_day, scheduled_for, manual)
		VALUES ($1, $2, $3, $4, 'polite', $5, now(), true)
		ON CONFLICT (agreement_id, recipient, channel, send_day) WHERE status IN ('planned', 'sending', 'sent')
		DO NOTHING`, accountID, agreementID, recipient, channel, Today(tz))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadySentToday
	}
	if err := logActivity(ctx, tx, accountID, agreementID, a.contactID, userID, "reminder.queued", map[string]any{"recipient": recipient, "channel": channel}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
