package ledger

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/masiur/taka-poisa/internal/validate"
)

const (
	maxOpenRequests      = 10
	maxRequestsPerLender = 3 // per lender email per 7 days
	requestLinkGrace     = 7 * 24 * time.Hour
)

var (
	ErrTooManyRequests = errors.New("too many loan requests")
	ErrCannotRequest   = errors.New("this person cannot be asked") // no email, or they blocked this account
	ErrRequestClosed   = errors.New("request is no longer open")
)

// RequestInput asks a contact (the lender) for a loan. The proposed terms become the
// agreement's terms if they accept.
type RequestInput struct {
	ContactID     int64  `json:"contact_id"`
	Message       string `json:"message"`
	ExpiresInDays int    `json:"expires_in_days"`
	Terms
}

func (in *RequestInput) Normalize() {
	in.Terms.Normalize()
	in.Message = strings.TrimSpace(in.Message)
	if in.ExpiresInDays == 0 {
		in.ExpiresInDays = 14
	}
}

func (in RequestInput) Build() ([]InstallmentInput, validate.Errors) {
	errs := validate.Errors{}
	if in.ContactID <= 0 {
		errs.Add("contact_id", validate.Required)
	}
	if len([]rune(in.Message)) > 1000 {
		errs.Add("message", validate.TooLong)
	}
	if in.ExpiresInDays < 1 || in.ExpiresInDays > 60 {
		errs.Add("expires_in_days", validate.Invalid)
	}
	items := in.Terms.Build(errs)
	return items, errs
}

// CreateRequest records a loan request and queues the email to the lender.
func (s *Store) CreateRequest(ctx context.Context, accountID, userID int64, currency string, in RequestInput, items []InstallmentInput) (int64, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// Serialise requests per account so the limits below can't be raced past.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM accounts WHERE id = $1 FOR UPDATE`, accountID); err != nil {
		return 0, err
	}
	var email *string
	err = tx.QueryRow(ctx, `
		SELECT email FROM contacts WHERE account_id = $1 AND id = $2 AND deleted_at IS NULL`,
		accountID, in.ContactID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if email == nil {
		return 0, ErrCannotRequest
	}
	var open, toLender int
	var blocked bool
	if err := tx.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM agreements WHERE account_id = $1 AND status = 'requested' AND request_expires_at > now()),
			(SELECT count(*) FROM agreements a JOIN contacts c ON c.id = a.contact_id
			 WHERE a.account_id = $1 AND a.origin = 'request' AND c.email = $2 AND a.created_at > now() - interval '7 days'),
			EXISTS (SELECT 1 FROM request_blocks WHERE account_id = $1 AND email = $2)`,
		accountID, *email).Scan(&open, &toLender, &blocked); err != nil {
		return 0, err
	}
	if blocked {
		return 0, ErrCannotRequest
	}
	if open >= maxOpenRequests || toLender >= maxRequestsPerLender {
		return 0, ErrTooManyRequests
	}

	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO agreements (account_id, contact_id, direction, principal, currency, started_on, purpose,
			status, origin, request_message, request_expires_at, created_by)
		VALUES ($1, $2, 'borrowed', $3, $4, $5, $6, 'requested', 'request', nullif($7, ''), $8, $9)
		RETURNING id`,
		accountID, in.ContactID, in.Principal, currency, in.StartedOn, in.Purpose, in.Message,
		time.Now().Add(time.Duration(in.ExpiresInDays)*24*time.Hour), userID).Scan(&id); err != nil {
		return 0, err
	}
	if err := insertInstallments(ctx, tx, id, items); err != nil {
		return 0, err
	}
	if err := copyDefaultRules(ctx, tx, accountID, id); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox (kind, account_id, agreement_id) VALUES ('request.created', $1, $2)`, accountID, id); err != nil {
		return 0, err
	}
	if err := logActivity(ctx, tx, accountID, id, in.ContactID, userID, "request.sent", map[string]any{"principal": in.Principal}); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// WithdrawRequest lets the borrower cancel a request nobody has answered yet.
func (s *Store) WithdrawRequest(ctx context.Context, accountID, userID, id int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	a, err := lockAgreement(ctx, tx, accountID, id)
	if err != nil {
		return err
	}
	if a.status != "requested" {
		return ErrRequestClosed
	}
	if _, err := tx.Exec(ctx, `UPDATE agreements SET status = 'cancelled', updated_at = now() WHERE id = $1`, id); err != nil {
		return err
	}
	if err := logActivity(ctx, tx, accountID, id, a.contactID, userID, "request.withdrawn", nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ExpireRequests closes requests past their deadline; the worker calls it every tick.
func (s *Store) ExpireRequests(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx, `
		UPDATE agreements SET status = 'expired', updated_at = now()
		WHERE status = 'requested' AND request_expires_at <= now()`)
	return tag.RowsAffected(), err
}

// RequestView is what the lender sees.
type RequestView struct {
	ID           int64              `json:"id"`
	Status       string             `json:"status"`
	BorrowerName string             `json:"borrower_name"`
	AccountName  string             `json:"account_name"`
	Currency     string             `json:"currency"`
	Principal    int64              `json:"principal"`
	Purpose      string             `json:"purpose"`
	Message      string             `json:"message"`
	ExpiresAt    time.Time          `json:"expires_at"`
	Locale       string             `json:"locale"` // the lender's (contact's) language
	Installments []InstallmentInput `json:"installments"`
	DecisionNote string             `json:"decision_note"`
}

const requestViewSQL = `
	SELECT a.id, CASE WHEN a.status = 'requested' AND a.request_expires_at <= now() THEN 'expired' ELSE a.status END,
		coalesce((SELECT u.name FROM account_members m JOIN users u ON u.id = m.user_id
		          WHERE m.account_id = a.account_id AND m.role = 'owner' ORDER BY m.created_at LIMIT 1), acc.name),
		acc.name, a.currency, a.principal, a.purpose, coalesce(a.request_message, ''), a.request_expires_at,
		c.locale, coalesce(a.decision_note, '')
	FROM agreements a JOIN accounts acc ON acc.id = a.account_id JOIN contacts c ON c.id = a.contact_id`

func (s *Store) requestView(ctx context.Context, q querier, where string, args ...any) (RequestView, error) {
	var v RequestView
	err := q.QueryRow(ctx, requestViewSQL+" WHERE "+where, args...).Scan(&v.ID, &v.Status, &v.BorrowerName, &v.AccountName,
		&v.Currency, &v.Principal, &v.Purpose, &v.Message, &v.ExpiresAt, &v.Locale, &v.DecisionNote)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	rows, err := q.Query(ctx, `SELECT due_on, amount FROM installments WHERE agreement_id = $1 ORDER BY seq`, v.ID)
	if err != nil {
		return v, err
	}
	v.Installments, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (InstallmentInput, error) {
		var it InstallmentInput
		return it, r.Scan(&it.DueOn, &it.Amount)
	})
	return v, err
}

// CreateRequestLink issues the lender's secret link, valid until a week after the request expires.
func (s *Store) CreateRequestLink(ctx context.Context, accountID, agreementID int64) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	tag, err := s.db.Exec(ctx, `
		INSERT INTO links (token_hash, account_id, kind, contact_id, agreement_id, expires_at)
		SELECT $1, account_id, 'request', contact_id, id, request_expires_at + make_interval(secs => $3)
		FROM agreements WHERE account_id = $2 AND id = $4 AND origin = 'request'`,
		hashToken(token), accountID, requestLinkGrace.Seconds(), agreementID)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	return token, nil
}

func resolveRequestLink(ctx context.Context, q querier, token string) (accountID, agreementID int64, err error) {
	err = q.QueryRow(ctx, `
		SELECT account_id, agreement_id FROM links
		WHERE token_hash = $1 AND kind = 'request' AND expires_at > now() AND revoked_at IS NULL`,
		hashToken(token)).Scan(&accountID, &agreementID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

func (s *Store) RequestByToken(ctx context.Context, token string) (RequestView, error) {
	accountID, agreementID, err := resolveRequestLink(ctx, s.db, token)
	if err != nil {
		return RequestView{}, err
	}
	return s.requestView(ctx, s.db, "a.account_id = $1 AND a.id = $2", accountID, agreementID)
}

// DecideByToken records the lender's answer from the email link.
func (s *Store) DecideByToken(ctx context.Context, token string, accept bool, note string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	accountID, agreementID, err := resolveRequestLink(ctx, tx, token)
	if err != nil {
		return err
	}
	if err := decide(ctx, tx, accountID, agreementID, accept, note); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// BlockByToken stops future requests from this borrower account to the link's lender.
func (s *Store) BlockByToken(ctx context.Context, token string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	accountID, agreementID, err := resolveRequestLink(ctx, tx, token)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO request_blocks (account_id, email)
		SELECT a.account_id, c.email FROM agreements a JOIN contacts c ON c.id = a.contact_id
		WHERE a.id = $1 AND c.email IS NOT NULL
		ON CONFLICT DO NOTHING`, agreementID); err != nil {
		return err
	}
	// Blocking also declines the open request.
	if err := decide(ctx, tx, accountID, agreementID, false, ""); err != nil && !errors.Is(err, ErrRequestClosed) {
		return err
	}
	return tx.Commit(ctx)
}

// decide accepts or declines an open request and queues the email to the borrower.
func decide(ctx context.Context, tx pgx.Tx, accountID, agreementID int64, accept bool, note string) error {
	var status string
	var contactID int64
	var expired bool
	if err := tx.QueryRow(ctx, `
		SELECT status, contact_id, request_expires_at <= now() FROM agreements
		WHERE account_id = $1 AND id = $2 AND origin = 'request' FOR UPDATE`,
		accountID, agreementID).Scan(&status, &contactID, &expired); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status != "requested" || expired {
		return ErrRequestClosed
	}
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 500 {
		note = string([]rune(note)[:500])
	}
	newStatus, action := "declined", "request.declined"
	if accept {
		newStatus, action = "active", "request.accepted"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE agreements SET status = $2, decided_at = now(), decision_note = nullif($3, ''), updated_at = now()
		WHERE id = $1`, agreementID, newStatus, note); err != nil {
		return err
	}
	if err := logActivity(ctx, tx, accountID, agreementID, contactID, 0, action, nil); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox (kind, account_id, agreement_id) VALUES ('request.decided', $1, $2)`, accountID, agreementID); err != nil {
		return err
	}
	// Accepted: it is now an active borrowed loan, so the owner's own reminders start.
	return replan(ctx, tx, accountID, agreementID)
}

// IncomingRequest is a request addressed to the signed-in user's verified email from another account.
type IncomingRequest struct {
	RequestView
	RequestedAt time.Time `json:"requested_at"`
}

// Incoming lists open requests to this user's email, but only if the user proved they own it
// (email_verified_at); otherwise anyone could read requests meant for someone else.
func (s *Store) Incoming(ctx context.Context, userID, accountID int64) ([]IncomingRequest, error) {
	ids, err := collectIDs(ctx, s.db, `
		SELECT a.id FROM agreements a
		JOIN contacts c ON c.id = a.contact_id
		JOIN users u ON u.id = $1 AND u.email_verified_at IS NOT NULL AND u.email = c.email
		WHERE a.status = 'requested' AND a.request_expires_at > now() AND a.account_id <> $2
		ORDER BY a.created_at DESC LIMIT 50`, userID, accountID)
	if err != nil {
		return nil, err
	}
	out := []IncomingRequest{}
	for _, id := range ids {
		v, err := s.requestView(ctx, s.db, "a.id = $1", id)
		if err != nil {
			return nil, err
		}
		out = append(out, IncomingRequest{RequestView: v})
	}
	return out, nil
}

// DecideIncoming answers a request from the lender's inbox. On accept the lender also gets their
// own "lent" copy, with a contact for the borrower (found by email or created).
func (s *Store) DecideIncoming(ctx context.Context, userID, lenderAccountID, agreementID int64, accept bool, note string) (copyID int64, err error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var borrowerAccount int64
	var borrowerName, borrowerEmail, currency, purpose string
	var principal int64
	var startedOn Date
	err = tx.QueryRow(ctx, `
		SELECT a.account_id, bu.name, bu.email, a.currency, a.principal, a.started_on, a.purpose
		FROM agreements a
		JOIN contacts c ON c.id = a.contact_id
		JOIN users u ON u.id = $1 AND u.email_verified_at IS NOT NULL AND u.email = c.email
		JOIN LATERAL (SELECT u2.name, u2.email FROM account_members m JOIN users u2 ON u2.id = m.user_id
		              WHERE m.account_id = a.account_id AND m.role = 'owner' ORDER BY m.created_at LIMIT 1) bu ON true
		WHERE a.id = $2 AND a.account_id <> $3`, userID, agreementID, lenderAccountID).
		Scan(&borrowerAccount, &borrowerName, &borrowerEmail, &currency, &principal, &startedOn, &purpose)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if err := decide(ctx, tx, borrowerAccount, agreementID, accept, note); err != nil {
		return 0, err
	}
	if !accept {
		return 0, tx.Commit(ctx)
	}

	// The lender's ledger: find or add the borrower as a contact, then copy the agreement as "lent".
	var contactID int64
	err = tx.QueryRow(ctx, `
		SELECT id FROM contacts WHERE account_id = $1 AND email = $2 AND deleted_at IS NULL ORDER BY id LIMIT 1`,
		lenderAccountID, borrowerEmail).Scan(&contactID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO contacts (account_id, name, email) VALUES ($1, $2, $3) RETURNING id`,
			lenderAccountID, borrowerName, borrowerEmail).Scan(&contactID)
	}
	if err != nil {
		return 0, err
	}
	var lenderCurrency string
	if err := tx.QueryRow(ctx, `SELECT currency FROM accounts WHERE id = $1`, lenderAccountID).Scan(&lenderCurrency); err != nil {
		return 0, err
	}
	if lenderCurrency != currency {
		// Balances never mix currencies; the lender can still record it by hand.
		return 0, tx.Commit(ctx)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO agreements (account_id, contact_id, direction, principal, currency, started_on, purpose, created_by)
		VALUES ($1, $2, 'lent', $3, $4, $5, $6, $7) RETURNING id`,
		lenderAccountID, contactID, principal, currency, startedOn, purpose, userID).Scan(&copyID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO installments (agreement_id, seq, due_on, amount)
		SELECT $1, seq, due_on, amount FROM installments WHERE agreement_id = $2`, copyID, agreementID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE agreements SET lender_copy_id = $2 WHERE id = $1`, agreementID, copyID); err != nil {
		return 0, err
	}
	if err := copyDefaultRules(ctx, tx, lenderAccountID, copyID); err != nil {
		return 0, err
	}
	if err := replan(ctx, tx, lenderAccountID, copyID); err != nil {
		return 0, err
	}
	if err := logActivity(ctx, tx, lenderAccountID, copyID, contactID, userID, "agreement.created", map[string]any{
		"principal": principal, "from_request": true,
	}); err != nil {
		return 0, err
	}
	return copyID, tx.Commit(ctx)
}

// RequestNotice is what the worker needs to email about a request.
type RequestNotice struct {
	Kind          string
	AccountID     int64
	AgreementID   int64
	Status        string
	BorrowerName  string
	BorrowerEmail string
	BorrowerLoc   string
	LenderName    string
	LenderEmail   string
	LenderLoc     string
	Currency      string
	Principal     int64
	Message       string
	DecisionNote  string
	ExpiresAt     time.Time
}

func (s *Store) RequestNotice(ctx context.Context, accountID, agreementID int64) (RequestNotice, error) {
	n := RequestNotice{AccountID: accountID, AgreementID: agreementID}
	var lenderEmail *string
	err := s.db.QueryRow(ctx, `
		SELECT a.status, u.name, u.email, u.locale, c.name, c.email, c.locale, a.currency, a.principal,
			coalesce(a.request_message, ''), coalesce(a.decision_note, ''), a.request_expires_at
		FROM agreements a
		JOIN contacts c ON c.id = a.contact_id
		JOIN LATERAL (SELECT u.* FROM account_members m JOIN users u ON u.id = m.user_id
		              WHERE m.account_id = a.account_id AND m.role = 'owner' ORDER BY m.created_at LIMIT 1) u ON true
		WHERE a.account_id = $1 AND a.id = $2 AND a.origin = 'request'`, accountID, agreementID).
		Scan(&n.Status, &n.BorrowerName, &n.BorrowerEmail, &n.BorrowerLoc, &n.LenderName, &lenderEmail, &n.LenderLoc,
			&n.Currency, &n.Principal, &n.Message, &n.DecisionNote, &n.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return n, ErrNotFound
	}
	if lenderEmail != nil {
		n.LenderEmail = *lenderEmail
	}
	return n, err
}
