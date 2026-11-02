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
	maxOpenLinkRequests  = 20 // per account
	maxLinkRequestsEmail = 3  // per requester email per 7 days
	linkRequestTTL       = 14 * 24 * time.Hour
)

var ErrAboveLimit = errors.New("amount above the link's limit")

// RequestLink lets anyone (or one granted contact) ask this account to lend them money.
type RequestLink struct {
	ID          int64     `json:"id"`
	Token       string    `json:"token"`
	Label       string    `json:"label"`
	ContactID   *int64    `json:"contact_id"`
	ContactName string    `json:"contact_name"`
	MaxAmount   *int64    `json:"max_amount"`
	OpenCount   int       `json:"open_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type LinkInput struct {
	Label     string `json:"label"`
	ContactID *int64 `json:"contact_id"`
	MaxAmount *int64 `json:"max_amount"`
}

func (in *LinkInput) Normalize() { in.Label = strings.TrimSpace(in.Label) }

func (in LinkInput) Validate() validate.Errors {
	e := validate.Errors{}
	if len([]rune(in.Label)) > 100 {
		e.Add("label", validate.TooLong)
	}
	if in.MaxAmount != nil && (*in.MaxAmount <= 0 || *in.MaxAmount > MaxAmount) {
		e.Add("max_amount", validate.Invalid)
	}
	return e
}

const linkCols = `l.id, l.token, l.label, l.contact_id, coalesce(c.name, ''), l.max_amount,
	(SELECT count(*) FROM agreements a WHERE a.request_link_id = l.id AND a.status IN ('requested', 'approved')),
	l.created_at`

func scanLink(row pgx.Row) (RequestLink, error) {
	var l RequestLink
	err := row.Scan(&l.ID, &l.Token, &l.Label, &l.ContactID, &l.ContactName, &l.MaxAmount, &l.OpenCount, &l.CreatedAt)
	return l, err
}

// CreateLink makes a new link. A contact link is returned as-is if one is already active,
// so "grant a link" on a contact is idempotent.
func (s *Store) CreateLink(ctx context.Context, accountID int64, in LinkInput) (RequestLink, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return RequestLink{}, err
	}
	defer tx.Rollback(ctx)
	if in.ContactID != nil {
		var live bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM contacts WHERE account_id = $1 AND id = $2 AND deleted_at IS NULL)`,
			accountID, *in.ContactID).Scan(&live); err != nil {
			return RequestLink{}, err
		}
		if !live {
			return RequestLink{}, ErrNotFound
		}
		l, err := scanLink(tx.QueryRow(ctx, `SELECT `+linkCols+` FROM request_links l LEFT JOIN contacts c ON c.id = l.contact_id
			WHERE l.account_id = $1 AND l.contact_id = $2 AND l.revoked_at IS NULL ORDER BY l.id LIMIT 1`, accountID, *in.ContactID))
		if err == nil {
			return l, tx.Commit(ctx)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return RequestLink{}, err
		}
	}
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return RequestLink{}, err
	}
	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO request_links (account_id, token, label, contact_id, max_amount) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		accountID, base64.RawURLEncoding.EncodeToString(raw), in.Label, in.ContactID, in.MaxAmount).Scan(&id); err != nil {
		return RequestLink{}, err
	}
	l, err := scanLink(tx.QueryRow(ctx, `SELECT `+linkCols+` FROM request_links l LEFT JOIN contacts c ON c.id = l.contact_id WHERE l.id = $1`, id))
	if err != nil {
		return l, err
	}
	return l, tx.Commit(ctx)
}

func (s *Store) ListLinks(ctx context.Context, accountID int64) ([]RequestLink, error) {
	rows, err := s.db.Query(ctx, `SELECT `+linkCols+` FROM request_links l LEFT JOIN contacts c ON c.id = l.contact_id
		WHERE l.account_id = $1 AND l.revoked_at IS NULL ORDER BY l.contact_id NULLS FIRST, l.id`, accountID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (RequestLink, error) { return scanLink(r) })
}

func (s *Store) RevokeLink(ctx context.Context, accountID, id int64) error {
	tag, err := s.db.Exec(ctx, `UPDATE request_links SET revoked_at = now() WHERE account_id = $1 AND id = $2 AND revoked_at IS NULL`, accountID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// LinkInfo is what the public form shows before anyone submits.
type LinkInfo struct {
	OwnerName   string `json:"owner_name"`
	AccountName string `json:"account_name"`
	Currency    string `json:"currency"`
	Locale      string `json:"locale"`
	MaxAmount   *int64 `json:"max_amount"`
	// Pre-filled for a link granted to one contact.
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

type linkRow struct {
	id, accountID int64
	contactID     *int64
	maxAmount     *int64
}

func resolveRequestLinkToken(ctx context.Context, q querier, token string, lock bool) (linkRow, error) {
	var l linkRow
	sql := `SELECT id, account_id, contact_id, max_amount FROM request_links WHERE token = $1 AND revoked_at IS NULL`
	if lock {
		sql += ` FOR UPDATE`
	}
	err := q.QueryRow(ctx, sql, token).Scan(&l.id, &l.accountID, &l.contactID, &l.maxAmount)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrNotFound
	}
	return l, err
}

func (s *Store) LinkInfo(ctx context.Context, token string) (LinkInfo, error) {
	var info LinkInfo
	l, err := resolveRequestLinkToken(ctx, s.db, token, false)
	if err != nil {
		return info, err
	}
	info.MaxAmount = l.maxAmount
	if err := s.db.QueryRow(ctx, `
		SELECT acc.name, acc.currency, acc.locale,
			coalesce((SELECT u.name FROM account_members m JOIN users u ON u.id = m.user_id
			          WHERE m.account_id = acc.id AND m.role = 'owner' ORDER BY m.created_at LIMIT 1), acc.name)
		FROM accounts acc WHERE acc.id = $1`, l.accountID).Scan(&info.AccountName, &info.Currency, &info.Locale, &info.OwnerName); err != nil {
		return info, err
	}
	if l.contactID != nil {
		s.db.QueryRow(ctx, `SELECT name, coalesce(email, ''), coalesce(phone_e164, ''), locale FROM contacts WHERE id = $1 AND deleted_at IS NULL`,
			*l.contactID).Scan(&info.Name, &info.Email, &info.Phone, &info.Locale)
	}
	return info, nil
}

// Payout is where the requester wants the money sent.
type Payout struct {
	Method        string `json:"method"` // bkash | nagad | rocket | bank | cash
	AccountName   string `json:"account_name"`
	AccountNumber string `json:"account_number"`
	BankName      string `json:"bank_name"`
	Branch        string `json:"branch"`
	RoutingNumber string `json:"routing_number"`
}

func (p *Payout) Normalize() {
	for _, f := range []*string{&p.AccountName, &p.AccountNumber, &p.BankName, &p.Branch, &p.RoutingNumber} {
		*f = strings.TrimSpace(*f)
	}
	switch p.Method {
	case "bkash", "nagad", "rocket":
		p.AccountNumber = NormalizePhone(p.AccountNumber)
		p.BankName, p.Branch, p.RoutingNumber = "", "", ""
	case "cash":
		*p = Payout{Method: "cash"}
	}
}

func (p Payout) validate(e validate.Errors) {
	switch p.Method {
	case "bkash", "nagad", "rocket":
		if !e164Re.MatchString(p.AccountNumber) {
			e.Add("payout.account_number", validate.Invalid)
		}
	case "bank":
		e.Text("payout.account_name", p.AccountName, 100)
		e.Text("payout.account_number", p.AccountNumber, 40)
		e.Text("payout.bank_name", p.BankName, 100)
		if len([]rune(p.Branch)) > 100 {
			e.Add("payout.branch", validate.TooLong)
		}
		if len(p.RoutingNumber) > 20 {
			e.Add("payout.routing_number", validate.TooLong)
		}
	case "cash":
	default:
		e.Add("payout.method", validate.Invalid)
	}
}

// LinkRequestInput is what a requester submits through a link.
type LinkRequestInput struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Locale  string `json:"locale"`
	Message string `json:"message"`
	Payout  Payout `json:"payout"`
	Website string `json:"website"` // honeypot: hidden from people, filled by bots
	Terms
}

func (in *LinkRequestInput) Normalize() {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = validate.NormalizeEmail(in.Email)
	in.Phone = NormalizePhone(in.Phone)
	in.Message = strings.TrimSpace(in.Message)
	if in.Locale == "" {
		in.Locale = "bn"
	}
	in.Payout.Normalize()
	in.Terms.Normalize()
}

func (in LinkRequestInput) Build() ([]InstallmentInput, validate.Errors) {
	e := validate.Errors{}
	e.Text("name", in.Name, 100)
	e.Email("email", in.Email)
	if in.Phone != "" && !e164Re.MatchString(in.Phone) {
		e.Add("phone", validate.Invalid)
	}
	e.Locale("locale", in.Locale)
	if len([]rune(in.Message)) > 1000 {
		e.Add("message", validate.TooLong)
	}
	in.Payout.validate(e)
	items := in.Terms.Build(e)
	return items, e
}

// IsBot reports a filled honeypot; the caller pretends to succeed and stores nothing.
func (in LinkRequestInput) IsBot() bool { return in.Website != "" }

// SubmitLinkRequest records a request from a link: the requester becomes (or matches) a contact
// at once, and the request waits for the owner as a 'requested' lent agreement.
func (s *Store) SubmitLinkRequest(ctx context.Context, token string, in LinkRequestInput, items []InstallmentInput) (int64, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	l, err := resolveRequestLinkToken(ctx, tx, token, true) // the row lock serialises the limits below
	if err != nil {
		return 0, err
	}
	if l.maxAmount != nil && in.Principal > *l.maxAmount {
		return 0, ErrAboveLimit
	}
	var blocked bool
	var open, fromEmail int
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM link_blocks WHERE account_id = $1 AND email = $2),
			(SELECT count(*) FROM agreements WHERE account_id = $1 AND origin = 'link' AND status IN ('requested', 'approved')),
			(SELECT count(*) FROM agreements a JOIN contacts c ON c.id = a.contact_id
			 WHERE a.account_id = $1 AND a.origin = 'link' AND c.email = $2 AND a.created_at > now() - interval '7 days')`,
		l.accountID, in.Email).Scan(&blocked, &open, &fromEmail); err != nil {
		return 0, err
	}
	if blocked {
		return 0, ErrCannotRequest
	}
	if open >= maxOpenLinkRequests || fromEmail >= maxLinkRequestsEmail {
		return 0, ErrTooManyRequests
	}

	// The requester becomes a contact now: the granted contact, a contact with this email, or a new one.
	var contactID int64
	if l.contactID != nil {
		contactID = *l.contactID
		if _, err := tx.Exec(ctx, `UPDATE contacts SET email = coalesce(email, $2), phone_e164 = coalesce(phone_e164, $3), updated_at = now()
			WHERE id = $1`, contactID, in.Email, nullIfEmpty(in.Phone)); err != nil {
			return 0, err
		}
	} else {
		err = tx.QueryRow(ctx, `SELECT id FROM contacts WHERE account_id = $1 AND email = $2 AND deleted_at IS NULL ORDER BY id LIMIT 1`,
			l.accountID, in.Email).Scan(&contactID)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `INSERT INTO contacts (account_id, name, email, phone_e164, locale) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
				l.accountID, in.Name, in.Email, nullIfEmpty(in.Phone), in.Locale).Scan(&contactID)
		}
		if err != nil {
			return 0, err
		}
	}

	var currency string
	if err := tx.QueryRow(ctx, `SELECT currency FROM accounts WHERE id = $1`, l.accountID).Scan(&currency); err != nil {
		return 0, err
	}
	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO agreements (account_id, contact_id, direction, principal, currency, started_on, purpose,
			status, origin, request_message, request_expires_at, request_link_id)
		VALUES ($1, $2, 'lent', $3, $4, $5, $6, 'requested', 'link', nullif($7, ''), $8, $9) RETURNING id`,
		l.accountID, contactID, in.Principal, currency, in.StartedOn, in.Purpose, in.Message,
		time.Now().Add(linkRequestTTL), l.id).Scan(&id); err != nil {
		return 0, err
	}
	if err := insertInstallments(ctx, tx, id, items); err != nil {
		return 0, err
	}
	if err := copyDefaultRules(ctx, tx, l.accountID, id); err != nil {
		return 0, err
	}
	p := in.Payout
	if _, err := tx.Exec(ctx, `
		INSERT INTO payout_details (agreement_id, method, account_name, account_number, bank_name, branch, routing_number)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, p.Method, p.AccountName, p.AccountNumber, p.BankName, p.Branch, p.RoutingNumber); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox (kind, account_id, agreement_id) VALUES ('link.received', $1, $2)`, l.accountID, id); err != nil {
		return 0, err
	}
	if err := logActivity(ctx, tx, l.accountID, id, contactID, 0, "link.received", map[string]any{"principal": in.Principal}); err != nil {
		return 0, err
	}
	if err := addOwnerToRequesterContacts(ctx, tx, l.accountID, in.Email); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// addOwnerToRequesterContacts adds this account's owner to the requester's own contacts, when the
// requester has a verified account on this server ("both ways"). An unverified email match is
// ignored, so nobody can fill a stranger's contact list by typing their email.
func addOwnerToRequesterContacts(ctx context.Context, tx pgx.Tx, ownerAccountID int64, requesterEmail string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO contacts (account_id, name, email, locale)
		SELECT rm.account_id, ou.name, ou.email, ou.locale
		FROM users ru
		JOIN LATERAL (SELECT account_id FROM account_members WHERE user_id = ru.id ORDER BY created_at LIMIT 1) rm ON true
		JOIN LATERAL (SELECT u.name, u.email, u.locale FROM account_members m JOIN users u ON u.id = m.user_id
		              WHERE m.account_id = $1 AND m.role = 'owner' ORDER BY m.created_at LIMIT 1) ou ON true
		WHERE ru.email = $2 AND ru.email_verified_at IS NOT NULL AND rm.account_id <> $1
		  AND NOT EXISTS (SELECT 1 FROM contacts c WHERE c.account_id = rm.account_id AND c.email = ou.email AND c.deleted_at IS NULL)`,
		ownerAccountID, requesterEmail)
	return err
}

// lockLinkRequest locks an open link request of this account.
func lockLinkRequest(ctx context.Context, tx pgx.Tx, accountID, id int64) (lockedAgreement, error) {
	a, err := lockAgreement(ctx, tx, accountID, id)
	if err != nil {
		return a, err
	}
	if a.origin != "link" {
		return a, ErrNotFound
	}
	if a.status != "requested" && a.status != "approved" {
		return a, ErrRequestClosed
	}
	return a, nil
}

func (s *Store) ApproveLinkRequest(ctx context.Context, accountID, userID, id int64, note string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	a, err := lockLinkRequest(ctx, tx, accountID, id)
	if err != nil {
		return err
	}
	if a.status != "requested" {
		return ErrRequestClosed
	}
	if _, err := tx.Exec(ctx, `UPDATE agreements SET status = 'approved', decided_at = now(), decision_note = nullif($2, ''), updated_at = now() WHERE id = $1`,
		id, clip(note, 500)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox (kind, account_id, agreement_id) VALUES ('link.approved', $1, $2)`, accountID, id); err != nil {
		return err
	}
	if err := logActivity(ctx, tx, accountID, id, a.contactID, userID, "link.approved", nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) DeclineLinkRequest(ctx context.Context, accountID, userID, id int64, note string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	a, err := lockLinkRequest(ctx, tx, accountID, id)
	if err != nil {
		return err
	}
	if err := closeLinkRequest(ctx, tx, accountID, userID, id, a.contactID, note, true); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// closeLinkRequest declines and drops the payout details; notify=false is used when blocking.
func closeLinkRequest(ctx context.Context, tx pgx.Tx, accountID, userID, id, contactID int64, note string, notify bool) error {
	if _, err := tx.Exec(ctx, `UPDATE agreements SET status = 'declined', decided_at = now(), decision_note = nullif($2, ''), updated_at = now() WHERE id = $1`,
		id, clip(note, 500)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM payout_details WHERE agreement_id = $1`, id); err != nil {
		return err
	}
	if notify {
		if _, err := tx.Exec(ctx, `INSERT INTO outbox (kind, account_id, agreement_id) VALUES ('link.declined', $1, $2)`, accountID, id); err != nil {
			return err
		}
	}
	return logActivity(ctx, tx, accountID, id, contactID, userID, "link.declined", nil)
}

type SentInput struct {
	Method string `json:"method"`
	Ref    string `json:"ref"` // transaction ID
	SentOn Date   `json:"sent_on"`
}

func (in *SentInput) Normalize() { in.Ref = strings.TrimSpace(in.Ref) }

func (in SentInput) Validate() validate.Errors {
	e := validate.Errors{}
	if !paymentMethods[in.Method] {
		e.Add("method", validate.Invalid)
	}
	if len(in.Ref) > 100 {
		e.Add("ref", validate.TooLong)
	}
	if !saneDate(in.SentOn) {
		e.Add("sent_on", validate.Invalid)
	}
	return e
}

// MarkSent records that the owner sent the money: the request becomes an active loan starting on
// the day it was sent, the payout details are deleted, reminders start, and a requester with a
// verified account gets their own "borrowed" copy.
func (s *Store) MarkSent(ctx context.Context, accountID, userID, id int64, in SentInput) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	a, err := lockLinkRequest(ctx, tx, accountID, id)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE agreements SET status = 'active', started_on = $2, sent_at = now(), sent_method = $3, sent_ref = nullif($4, ''),
			decided_at = coalesce(decided_at, now()), updated_at = now()
		WHERE id = $1`, id, in.SentOn, in.Method, in.Ref); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM payout_details WHERE agreement_id = $1`, id); err != nil {
		return err
	}
	if err := logActivity(ctx, tx, accountID, id, a.contactID, userID, "link.sent", map[string]any{
		"amount": a.principal, "method": in.Method,
	}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox (kind, account_id, agreement_id) VALUES ('link.sent', $1, $2)`, accountID, id); err != nil {
		return err
	}
	if err := replan(ctx, tx, accountID, id); err != nil {
		return err
	}
	if err := createBorrowerCopy(ctx, tx, accountID, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// createBorrowerCopy mirrors a sent loan into the requester's own ledger as "borrowed", if their
// email belongs to a verified user here with the same currency.
func createBorrowerCopy(ctx context.Context, tx pgx.Tx, accountID, id int64) error {
	var borrowerAccount, ownerContact int64
	err := tx.QueryRow(ctx, `
		SELECT rm.account_id
		FROM agreements a
		JOIN contacts c ON c.id = a.contact_id
		JOIN users ru ON ru.email = c.email AND ru.email_verified_at IS NOT NULL
		JOIN LATERAL (SELECT account_id FROM account_members WHERE user_id = ru.id ORDER BY created_at LIMIT 1) rm ON true
		JOIN accounts racc ON racc.id = rm.account_id AND racc.currency = a.currency
		WHERE a.id = $1 AND rm.account_id <> $2`, id, accountID).Scan(&borrowerAccount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// The owner as a contact in the borrower's book (added at submit time, or now).
	err = tx.QueryRow(ctx, `
		SELECT c.id FROM contacts c
		JOIN LATERAL (SELECT u.email FROM account_members m JOIN users u ON u.id = m.user_id
		              WHERE m.account_id = $2 AND m.role = 'owner' ORDER BY m.created_at LIMIT 1) ou ON c.email = ou.email
		WHERE c.account_id = $1 AND c.deleted_at IS NULL ORDER BY c.id LIMIT 1`, borrowerAccount, accountID).Scan(&ownerContact)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			INSERT INTO contacts (account_id, name, email, locale)
			SELECT $1, u.name, u.email, u.locale FROM account_members m JOIN users u ON u.id = m.user_id
			WHERE m.account_id = $2 AND m.role = 'owner' ORDER BY m.created_at LIMIT 1
			RETURNING id`, borrowerAccount, accountID).Scan(&ownerContact)
	}
	if err != nil {
		return err
	}
	var copyID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO agreements (account_id, contact_id, direction, principal, currency, started_on, purpose)
		SELECT $1, $2, 'borrowed', principal, currency, started_on, purpose FROM agreements WHERE id = $3
		RETURNING id`, borrowerAccount, ownerContact, id).Scan(&copyID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO installments (agreement_id, seq, due_on, amount)
		SELECT $1, seq, due_on, amount FROM installments WHERE agreement_id = $2`, copyID, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE agreements SET borrower_copy_id = $2 WHERE id = $1`, id, copyID); err != nil {
		return err
	}
	if err := copyDefaultRules(ctx, tx, borrowerAccount, copyID); err != nil {
		return err
	}
	if err := logActivity(ctx, tx, borrowerAccount, copyID, ownerContact, 0, "agreement.created", map[string]any{"from_request": true}); err != nil {
		return err
	}
	return replan(ctx, tx, borrowerAccount, copyID)
}

// BlockLinkRequester stops this email from asking again, closes the request without telling them,
// and removes the contact if this request was their only record.
func (s *Store) BlockLinkRequester(ctx context.Context, accountID, userID, id int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	a, err := lockAgreement(ctx, tx, accountID, id)
	if err != nil {
		return err
	}
	if a.origin != "link" {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO link_blocks (account_id, email) SELECT $1, email FROM contacts WHERE id = $2 AND email IS NOT NULL
		ON CONFLICT DO NOTHING`, accountID, a.contactID); err != nil {
		return err
	}
	if a.status == "requested" || a.status == "approved" {
		if err := closeLinkRequest(ctx, tx, accountID, userID, id, a.contactID, "", false); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE contacts SET deleted_at = now() WHERE id = $1 AND NOT EXISTS (
			SELECT 1 FROM agreements WHERE contact_id = $1 AND id <> $2)`, a.contactID, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PayoutFor returns the payout details of an open link request (nil when there are none).
func (s *Store) PayoutFor(ctx context.Context, accountID, agreementID int64) (*Payout, error) {
	var p Payout
	err := s.db.QueryRow(ctx, `
		SELECT p.method, p.account_name, p.account_number, p.bank_name, p.branch, p.routing_number
		FROM payout_details p JOIN agreements a ON a.id = p.agreement_id
		WHERE a.account_id = $1 AND a.id = $2`, accountID, agreementID).
		Scan(&p.Method, &p.AccountName, &p.AccountNumber, &p.BankName, &p.Branch, &p.RoutingNumber)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// LinkNotice is what the worker needs for link emails.
type LinkNotice struct {
	Kind           string
	AgreementID    int64
	ContactID      int64
	Status         string
	OwnerName      string
	OwnerEmail     string
	OwnerLocale    string
	RequesterName  string
	RequesterEmail string
	RequesterLoc   string
	Currency       string
	Principal      int64
	Message        string
	DecisionNote   string
	SentMethod     string
	SentRef        string
}

func (s *Store) LinkNotice(ctx context.Context, accountID, agreementID int64) (LinkNotice, error) {
	n := LinkNotice{AgreementID: agreementID}
	var email *string
	err := s.db.QueryRow(ctx, `
		SELECT a.contact_id, a.status, u.name, u.email, u.locale, c.name, c.email, c.locale, a.currency, a.principal,
			coalesce(a.request_message, ''), coalesce(a.decision_note, ''), coalesce(a.sent_method, ''), coalesce(a.sent_ref, '')
		FROM agreements a
		JOIN contacts c ON c.id = a.contact_id
		JOIN LATERAL (SELECT u.* FROM account_members m JOIN users u ON u.id = m.user_id
		              WHERE m.account_id = a.account_id AND m.role = 'owner' ORDER BY m.created_at LIMIT 1) u ON true
		WHERE a.account_id = $1 AND a.id = $2 AND a.origin = 'link'`, accountID, agreementID).
		Scan(&n.ContactID, &n.Status, &n.OwnerName, &n.OwnerEmail, &n.OwnerLocale, &n.RequesterName, &email, &n.RequesterLoc,
			&n.Currency, &n.Principal, &n.Message, &n.DecisionNote, &n.SentMethod, &n.SentRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return n, ErrNotFound
	}
	if email != nil {
		n.RequesterEmail = *email
	}
	return n, err
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
