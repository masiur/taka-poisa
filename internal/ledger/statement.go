package ledger

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/masiur/taka-poisa/internal/validate"
)

var ErrTooManyClaims = errors.New("too many pending claims")

const maxClaimsPerDay = 5

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// CreateStatementLink issues a secret link to a contact's statement page. Each reminder email
// gets a fresh one; they expire after 90 days.
func (s *Store) CreateStatementLink(ctx context.Context, accountID, contactID int64) (string, error) {
	return createStatementLink(ctx, s.db, accountID, contactID)
}

func createStatementLink(ctx context.Context, q querier, accountID, contactID int64) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	tag, err := q.Exec(ctx, `
		INSERT INTO links (token_hash, account_id, kind, contact_id, expires_at)
		SELECT $1, $2, 'statement', id, $4
		FROM contacts WHERE account_id = $2 AND id = $3 AND deleted_at IS NULL`,
		hashToken(token), accountID, contactID, time.Now().Add(reminderLinkTTL))
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	return token, nil
}

type linkTarget struct {
	accountID, contactID int64
}

func resolveLink(ctx context.Context, q querier, token string) (linkTarget, error) {
	var l linkTarget
	err := q.QueryRow(ctx, `
		SELECT l.account_id, l.contact_id FROM links l JOIN contacts c ON c.id = l.contact_id
		WHERE l.token_hash = $1 AND l.kind = 'statement' AND l.expires_at > now()
		  AND l.revoked_at IS NULL AND c.deleted_at IS NULL`, hashToken(token)).Scan(&l.accountID, &l.contactID)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrNotFound
	}
	return l, err
}

type StatementLoan struct {
	AgreementSummary
	Installments []InstallmentState `json:"installments"`
	Payments     []Payment          `json:"payments"`
}

// Statement is what a contact sees from a link: their loans with this account, nothing else.
type Statement struct {
	AccountName string          `json:"account_name"`
	OwnerName   string          `json:"owner_name"`
	ContactName string          `json:"contact_name"`
	Locale      string          `json:"locale"`
	Currency    string          `json:"currency"`
	OptedOut    bool            `json:"opted_out"`
	HasEmail    bool            `json:"has_email"`
	SMSOptedOut bool            `json:"sms_opted_out"`
	HasPhone    bool            `json:"has_phone"`
	Loans       []StatementLoan `json:"loans"`
}

func (s *Store) Statement(ctx context.Context, token string) (Statement, error) {
	var st Statement
	l, err := resolveLink(ctx, s.db, token)
	if err != nil {
		return st, err
	}
	var tz string
	if err := s.db.QueryRow(ctx, `
		SELECT acc.name, acc.currency, acc.timezone, c.name, c.locale, c.email_opted_out_at IS NOT NULL, c.email IS NOT NULL,
			c.sms_opted_out_at IS NOT NULL, c.phone_e164 IS NOT NULL,
			coalesce((SELECT u.name FROM account_members m JOIN users u ON u.id = m.user_id
			          WHERE m.account_id = acc.id AND m.role = 'owner' ORDER BY m.created_at LIMIT 1), acc.name)
		FROM accounts acc JOIN contacts c ON c.account_id = acc.id
		WHERE acc.id = $1 AND c.id = $2`, l.accountID, l.contactID).
		Scan(&st.AccountName, &st.Currency, &tz, &st.ContactName, &st.Locale, &st.OptedOut, &st.HasEmail,
			&st.SMSOptedOut, &st.HasPhone, &st.OwnerName); err != nil {
		return st, err
	}
	today := Today(tz)
	list, err := s.ListAgreements(ctx, l.accountID, today, AgreementFilter{ContactID: l.contactID})
	if err != nil {
		return st, err
	}
	st.Loans = []StatementLoan{}
	for _, a := range list {
		if a.Status != "active" && a.Status != "settled" {
			continue
		}
		d, err := s.GetAgreement(ctx, l.accountID, a.ID, today)
		if err != nil {
			return st, err
		}
		payments := []Payment{}
		for _, p := range d.Payments {
			if p.Status != "rejected" {
				payments = append(payments, p)
			}
		}
		st.Loans = append(st.Loans, StatementLoan{AgreementSummary: d.AgreementSummary, Installments: d.Installments, Payments: payments})
	}
	return st, nil
}

type ClaimInput struct {
	AgreementID int64 `json:"agreement_id"`
	PaymentInput
}

// SubmitClaim records the contact's "I've paid" as a pending payment. It changes no balance
// until the owner confirms it.
func (s *Store) SubmitClaim(ctx context.Context, token string, in ClaimInput) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	l, err := resolveLink(ctx, tx, token)
	if err != nil {
		return err
	}
	var contactID int64
	var status string
	var principal int64
	err = tx.QueryRow(ctx, `
		SELECT contact_id, status, principal FROM agreements WHERE account_id = $1 AND id = $2 FOR UPDATE`,
		l.accountID, in.AgreementID).Scan(&contactID, &status, &principal)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && contactID != l.contactID) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "active" {
		return ErrNotActive
	}
	paid, err := paidTotal(ctx, tx, in.AgreementID)
	if err != nil {
		return err
	}
	if in.Amount > principal-paid {
		return ErrExceedsBalance
	}
	var recent int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM payments p JOIN agreements a ON a.id = p.agreement_id
		WHERE a.contact_id = $1 AND p.source = 'contact_claim' AND p.created_at > now() - interval '1 day'`,
		l.contactID).Scan(&recent); err != nil {
		return err
	}
	if recent >= maxClaimsPerDay {
		return ErrTooManyClaims
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO payments (agreement_id, amount, paid_on, method, note, source, status)
		VALUES ($1, $2, $3, $4, $5, 'contact_claim', 'pending')`,
		in.AgreementID, in.Amount, in.PaidOn, in.Method, strings.TrimSpace(in.Note)); err != nil {
		return err
	}
	if err := logActivity(ctx, tx, l.accountID, in.AgreementID, l.contactID, 0, "claim.submitted", map[string]any{
		"amount": in.Amount, "method": in.Method,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ResolveClaim confirms (it becomes a real payment) or rejects a contact's claim.
func (s *Store) ResolveClaim(ctx context.Context, accountID, userID, agreementID, paymentID int64, confirm bool) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	a, err := lockAgreement(ctx, tx, accountID, agreementID)
	if err != nil {
		return err
	}
	var amount int64
	err = tx.QueryRow(ctx, `
		SELECT amount FROM payments WHERE id = $1 AND agreement_id = $2 AND status = 'pending' FOR UPDATE`,
		paymentID, agreementID).Scan(&amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !confirm {
		if _, err := tx.Exec(ctx, `UPDATE payments SET status = 'rejected' WHERE id = $1`, paymentID); err != nil {
			return err
		}
		if err := logActivity(ctx, tx, accountID, agreementID, a.contactID, userID, "claim.rejected", map[string]any{"amount": amount}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if a.status != "active" {
		return ErrNotActive
	}
	paid, err := paidTotal(ctx, tx, agreementID)
	if err != nil {
		return err
	}
	if amount > a.principal-paid {
		return ErrExceedsBalance
	}
	if _, err := tx.Exec(ctx, `UPDATE payments SET status = 'confirmed', confirmed_at = now(), created_by = $2 WHERE id = $1`, paymentID, userID); err != nil {
		return err
	}
	if err := logActivity(ctx, tx, accountID, agreementID, a.contactID, userID, "claim.confirmed", map[string]any{"amount": amount}); err != nil {
		return err
	}
	if err := recomputeStatus(ctx, tx, accountID, userID, agreementID, a); err != nil {
		return err
	}
	if err := replan(ctx, tx, accountID, agreementID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// OptOut stops reminders to the link's contact on one channel ("email" or "sms").
func (s *Store) OptOut(ctx context.Context, token, channel string) error {
	column := "email_opted_out_at"
	if channel == "sms" {
		column = "sms_opted_out_at"
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	l, err := resolveLink(ctx, tx, token)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE contacts SET `+column+` = now() WHERE id = $1 AND `+column+` IS NULL`, l.contactID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if err := logActivity(ctx, tx, l.accountID, 0, l.contactID, 0, "contact.opted_out", map[string]any{"channel": channel}); err != nil {
			return err
		}
	}
	if err := replanContact(ctx, tx, l.accountID, l.contactID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReminderContext is everything the sender needs to write one reminder, read at send time
// so the email reflects the latest payments.
type ReminderContext struct {
	AccountID     int64
	AgreementID   int64
	ContactID     int64
	Status        string
	Direction     string
	Currency      string
	AccountName   string
	OwnerName     string
	OwnerEmail    string
	OwnerLocale   string
	ContactName   string
	ContactEmail  string
	ContactLocale string
	OptedOut      bool
	ContactPhone  string
	SMSOptedOut   bool
	Today         Date
	Remaining     int64
	Overdue       int64
	OverdueSince  Date // earliest unpaid due date before today
	NextDueOn     Date
	NextAmount    int64
}

func (s *Store) ReminderContext(ctx context.Context, accountID, agreementID int64) (ReminderContext, error) {
	rc := ReminderContext{AccountID: accountID, AgreementID: agreementID}
	var tz string
	var email *string
	err := s.db.QueryRow(ctx, `
		SELECT a.contact_id, a.status, a.direction, a.currency, acc.name, acc.timezone,
			u.name, u.email, u.locale, c.name, c.email, c.locale, c.email_opted_out_at IS NOT NULL,
			coalesce(c.phone_e164, ''), c.sms_opted_out_at IS NOT NULL
		FROM agreements a
		JOIN accounts acc ON acc.id = a.account_id
		JOIN contacts c ON c.id = a.contact_id
		JOIN LATERAL (SELECT u.* FROM account_members m JOIN users u ON u.id = m.user_id
		              WHERE m.account_id = acc.id AND m.role = 'owner' ORDER BY m.created_at LIMIT 1) u ON true
		WHERE a.account_id = $1 AND a.id = $2`, accountID, agreementID).
		Scan(&rc.ContactID, &rc.Status, &rc.Direction, &rc.Currency, &rc.AccountName, &tz,
			&rc.OwnerName, &rc.OwnerEmail, &rc.OwnerLocale, &rc.ContactName, &email, &rc.ContactLocale, &rc.OptedOut,
			&rc.ContactPhone, &rc.SMSOptedOut)
	if errors.Is(err, pgx.ErrNoRows) {
		return rc, ErrNotFound
	}
	if err != nil {
		return rc, err
	}
	if email != nil {
		rc.ContactEmail = *email
	}
	rc.Today = Today(tz)
	d, err := s.GetAgreement(ctx, accountID, agreementID, rc.Today)
	if err != nil {
		return rc, err
	}
	rc.Remaining, rc.Overdue, rc.NextDueOn, rc.NextAmount = d.Remaining, d.Overdue, d.NextDueOn, d.NextAmount
	for _, it := range d.Installments {
		if it.Status == "overdue" {
			rc.OverdueSince = it.DueOn
			break
		}
	}
	return rc, nil
}

// ValidateClaim checks a claim's fields (the payment rules plus a target agreement).
func (in ClaimInput) Validate() validate.Errors {
	e := in.PaymentInput.Validate()
	if in.AgreementID <= 0 {
		e.Add("agreement_id", validate.Required)
	}
	return e
}
