package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/masiur/taka-poisa/internal/validate"
)

// summaryCTE computes each agreement's balances for account $1 as of day $2. Payments cover
// installments oldest first, so the overdue amount is what was due before today minus what
// was paid, and the next due installment is the first whose running total exceeds what was paid.
// Allocate in schedule.go applies the same rule per installment.
const summaryCTE = `summary AS (
	SELECT a.id, a.contact_id, a.direction, a.status, a.principal,
		coalesce(p.paid, 0)::bigint AS paid,
		(a.principal - coalesce(p.paid, 0))::bigint AS remaining,
		greatest(coalesce(d.due_before, 0) - coalesce(p.paid, 0), 0)::bigint AS overdue,
		n.due_on AS next_due_on,
		coalesce(n.remaining, 0)::bigint AS next_amount
	FROM agreements a
	LEFT JOIN LATERAL (
		SELECT sum(amount) AS paid FROM payments WHERE agreement_id = a.id AND status = 'confirmed'
	) p ON true
	LEFT JOIN LATERAL (
		SELECT sum(amount) AS due_before FROM installments WHERE agreement_id = a.id AND due_on < $2
	) d ON true
	LEFT JOIN LATERAL (
		SELECT i.due_on, least(i.amount, i.cum - coalesce(p.paid, 0)) AS remaining
		FROM (SELECT seq, due_on, amount, sum(amount) OVER (ORDER BY seq) AS cum
		      FROM installments WHERE agreement_id = a.id) i
		WHERE i.cum > coalesce(p.paid, 0)
		ORDER BY i.seq LIMIT 1
	) n ON true
	WHERE a.account_id = $1
)`

// Terms are the editable money terms of an agreement.
type Terms struct {
	Principal int64         `json:"principal"`
	StartedOn Date          `json:"started_on"`
	Purpose   string        `json:"purpose"`
	Schedule  ScheduleInput `json:"schedule"`
}

func (t *Terms) Normalize() {
	t.Purpose = strings.TrimSpace(t.Purpose)
}

// Build validates the terms and returns the installments they produce.
func (t Terms) Build(errs validate.Errors) []InstallmentInput {
	if t.Principal <= 0 || t.Principal > MaxAmount {
		errs.Add("principal", validate.Invalid)
	}
	if !saneDate(t.StartedOn) {
		errs.Add("started_on", validate.Invalid)
	}
	if len([]rune(t.Purpose)) > 500 {
		errs.Add("purpose", validate.TooLong)
	}
	if errs["principal"] != "" {
		return nil
	}
	return t.Schedule.Build(t.Principal, errs)
}

type AgreementInput struct {
	ContactID int64  `json:"contact_id"`
	Direction string `json:"direction"` // lent | borrowed
	Terms
}

func (in AgreementInput) Build() ([]InstallmentInput, validate.Errors) {
	errs := validate.Errors{}
	if in.ContactID <= 0 {
		errs.Add("contact_id", validate.Required)
	}
	if in.Direction != "lent" && in.Direction != "borrowed" {
		errs.Add("direction", validate.Invalid)
	}
	items := in.Terms.Build(errs)
	return items, errs
}

type AgreementSummary struct {
	ID          int64     `json:"id"`
	ContactID   int64     `json:"contact_id"`
	ContactName string    `json:"contact_name"`
	Direction   string    `json:"direction"`
	Status      string    `json:"status"`
	Currency    string    `json:"currency"`
	Principal   int64     `json:"principal"`
	Paid        int64     `json:"paid"`
	Remaining   int64     `json:"remaining"`
	Overdue     int64     `json:"overdue"`
	StartedOn   Date      `json:"started_on"`
	Purpose     string    `json:"purpose"`
	NextDueOn   Date      `json:"next_due_on"` // null when nothing is left to pay
	NextAmount  int64     `json:"next_amount"`
	CreatedAt   time.Time `json:"created_at"`
}

type Payment struct {
	ID        int64     `json:"id"`
	Amount    int64     `json:"amount"`
	PaidOn    Date      `json:"paid_on"`
	Method    string    `json:"method"`
	Note      string    `json:"note"`
	Source    string    `json:"source"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type Activity struct {
	ID        int64           `json:"id"`
	Action    string          `json:"action"`
	Data      json.RawMessage `json:"data"`
	ActorName string          `json:"actor_name"`
	CreatedAt time.Time       `json:"created_at"`
}

type AgreementDetail struct {
	AgreementSummary
	Installments []InstallmentState `json:"installments"`
	Payments     []Payment          `json:"payments"`
	Activity     []Activity         `json:"activity"`
}

type AgreementFilter struct {
	ContactID int64
	Status    string // "" = any
	ID        int64
}

func (s *Store) ListAgreements(ctx context.Context, accountID int64, today Date, f AgreementFilter) ([]AgreementSummary, error) {
	rows, err := s.db.Query(ctx, `
		WITH `+summaryCTE+`
		SELECT s.id, s.contact_id, c.name, s.direction, s.status, a.currency, s.principal, s.paid,
			s.remaining, s.overdue, a.started_on, a.purpose, s.next_due_on, s.next_amount, a.created_at
		FROM summary s
		JOIN agreements a ON a.id = s.id
		JOIN contacts c ON c.id = s.contact_id
		WHERE ($3 = 0 OR s.contact_id = $3) AND ($4 = '' OR s.status = $4) AND ($5 = 0 OR s.id = $5)
		ORDER BY s.status <> 'active', s.next_due_on NULLS LAST, s.id DESC`,
		accountID, today, f.ContactID, f.Status, f.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgreementSummary{}
	for rows.Next() {
		var a AgreementSummary
		if err := rows.Scan(&a.ID, &a.ContactID, &a.ContactName, &a.Direction, &a.Status, &a.Currency,
			&a.Principal, &a.Paid, &a.Remaining, &a.Overdue, &a.StartedOn, &a.Purpose,
			&a.NextDueOn, &a.NextAmount, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetAgreement(ctx context.Context, accountID, id int64, today Date) (AgreementDetail, error) {
	list, err := s.ListAgreements(ctx, accountID, today, AgreementFilter{ID: id})
	if err != nil {
		return AgreementDetail{}, err
	}
	if len(list) == 0 {
		return AgreementDetail{}, ErrNotFound
	}
	d := AgreementDetail{AgreementSummary: list[0], Payments: []Payment{}, Activity: []Activity{}}

	rows, err := s.db.Query(ctx, `SELECT id, seq, due_on, amount FROM installments WHERE agreement_id = $1 ORDER BY seq`, id)
	if err != nil {
		return d, err
	}
	var items []InstallmentState
	for rows.Next() {
		var it InstallmentState
		if err := rows.Scan(&it.ID, &it.Seq, &it.DueOn, &it.Amount); err != nil {
			rows.Close()
			return d, err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return d, err
	}
	d.Installments = Allocate(items, d.Paid, today)

	rows, err = s.db.Query(ctx, `
		SELECT id, amount, paid_on, method, note, source, status, created_at
		FROM payments WHERE agreement_id = $1 ORDER BY paid_on DESC, id DESC`, id)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var p Payment
		if err := rows.Scan(&p.ID, &p.Amount, &p.PaidOn, &p.Method, &p.Note, &p.Source, &p.Status, &p.CreatedAt); err != nil {
			rows.Close()
			return d, err
		}
		d.Payments = append(d.Payments, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return d, err
	}

	rows, err = s.db.Query(ctx, `
		SELECT ac.id, ac.action, ac.data, coalesce(u.name, ''), ac.created_at
		FROM activity ac LEFT JOIN users u ON u.id = ac.actor_user_id
		WHERE ac.account_id = $1 AND ac.agreement_id = $2
		ORDER BY ac.created_at DESC, ac.id DESC LIMIT 100`, accountID, id)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.ID, &a.Action, &a.Data, &a.ActorName, &a.CreatedAt); err != nil {
			return d, err
		}
		d.Activity = append(d.Activity, a)
	}
	return d, rows.Err()
}

// CreateAgreement records a loan with its schedule. The contact must be live and in the same account.
func (s *Store) CreateAgreement(ctx context.Context, accountID, userID int64, currency string, in AgreementInput, items []InstallmentInput) (int64, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var live bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM contacts WHERE account_id = $1 AND id = $2 AND deleted_at IS NULL FOR SHARE)`,
		accountID, in.ContactID).Scan(&live); err != nil {
		return 0, err
	}
	if !live {
		return 0, ErrNotFound
	}

	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO agreements (account_id, contact_id, direction, principal, currency, started_on, purpose, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		accountID, in.ContactID, in.Direction, in.Principal, currency, in.StartedOn, in.Purpose, userID).Scan(&id); err != nil {
		return 0, err
	}
	if err := insertInstallments(ctx, tx, id, items); err != nil {
		return 0, err
	}
	if err := copyDefaultRules(ctx, tx, accountID, id); err != nil {
		return 0, err
	}
	if err := replan(ctx, tx, accountID, id); err != nil {
		return 0, err
	}
	if err := logActivity(ctx, tx, accountID, id, in.ContactID, userID, "agreement.created", map[string]any{
		"principal": in.Principal, "installments": len(items), "direction": in.Direction,
	}); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// UpdateTerms replaces principal, dates, purpose and schedule. The principal cannot drop
// below what has already been paid.
func (s *Store) UpdateTerms(ctx context.Context, accountID, userID, id int64, t Terms, items []InstallmentInput) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	a, err := lockAgreement(ctx, tx, accountID, id)
	if err != nil {
		return err
	}
	if a.status != "active" && a.status != "settled" {
		return ErrNotActive
	}
	paid, err := paidTotal(ctx, tx, id)
	if err != nil {
		return err
	}
	if t.Principal < paid {
		return ErrBelowPaid
	}
	if _, err := tx.Exec(ctx, `
		UPDATE agreements SET principal = $2, started_on = $3, purpose = $4, updated_at = now() WHERE id = $1`,
		id, t.Principal, t.StartedOn, t.Purpose); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM installments WHERE agreement_id = $1`, id); err != nil {
		return err
	}
	if err := insertInstallments(ctx, tx, id, items); err != nil {
		return err
	}
	if err := logActivity(ctx, tx, accountID, id, a.contactID, userID, "agreement.updated", map[string]any{
		"principal": t.Principal, "installments": len(items),
	}); err != nil {
		return err
	}
	if err := recomputeStatus(ctx, tx, accountID, userID, id, a); err != nil {
		return err
	}
	if err := replan(ctx, tx, accountID, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CancelAgreement marks a loan as cancelled (kept for history, excluded from balances).
func (s *Store) CancelAgreement(ctx context.Context, accountID, userID, id int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	a, err := lockAgreement(ctx, tx, accountID, id)
	if err != nil {
		return err
	}
	if a.status != "active" && a.status != "settled" {
		return ErrNotActive
	}
	if _, err := tx.Exec(ctx, `UPDATE agreements SET status = 'cancelled', updated_at = now() WHERE id = $1`, id); err != nil {
		return err
	}
	if err := logActivity(ctx, tx, accountID, id, a.contactID, userID, "agreement.cancelled", nil); err != nil {
		return err
	}
	if err := replan(ctx, tx, accountID, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var paymentMethods = map[string]bool{"cash": true, "bkash": true, "nagad": true, "rocket": true, "bank": true, "card": true, "other": true}

type PaymentInput struct {
	Amount int64  `json:"amount"`
	PaidOn Date   `json:"paid_on"`
	Method string `json:"method"`
	Note   string `json:"note"`
}

func (in *PaymentInput) Normalize() {
	in.Note = strings.TrimSpace(in.Note)
}

func (in PaymentInput) Validate() validate.Errors {
	e := validate.Errors{}
	if in.Amount <= 0 || in.Amount > MaxAmount {
		e.Add("amount", validate.Invalid)
	}
	if !saneDate(in.PaidOn) {
		e.Add("paid_on", validate.Invalid)
	}
	if !paymentMethods[in.Method] {
		e.Add("method", validate.Invalid)
	}
	if len([]rune(in.Note)) > 500 {
		e.Add("note", validate.TooLong)
	}
	return e
}

// AddPayment records a confirmed payment. The agreement row is locked, so concurrent
// payments cannot together exceed the remaining balance.
func (s *Store) AddPayment(ctx context.Context, accountID, userID, agreementID int64, in PaymentInput) (int64, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	a, err := lockAgreement(ctx, tx, accountID, agreementID)
	if err != nil {
		return 0, err
	}
	if a.status != "active" {
		return 0, ErrNotActive
	}
	paid, err := paidTotal(ctx, tx, agreementID)
	if err != nil {
		return 0, err
	}
	if in.Amount > a.principal-paid {
		return 0, ErrExceedsBalance
	}
	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO payments (agreement_id, amount, paid_on, method, note, confirmed_at, created_by)
		VALUES ($1, $2, $3, $4, $5, now(), $6) RETURNING id`,
		agreementID, in.Amount, in.PaidOn, in.Method, in.Note, userID).Scan(&id); err != nil {
		return 0, err
	}
	if err := logActivity(ctx, tx, accountID, agreementID, a.contactID, userID, "payment.recorded", map[string]any{
		"amount": in.Amount, "method": in.Method, "paid_on": in.PaidOn.String(),
	}); err != nil {
		return 0, err
	}
	if err := recomputeStatus(ctx, tx, accountID, userID, agreementID, a); err != nil {
		return 0, err
	}
	if err := replan(ctx, tx, accountID, agreementID); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

func (s *Store) DeletePayment(ctx context.Context, accountID, userID, agreementID, paymentID int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	a, err := lockAgreement(ctx, tx, accountID, agreementID)
	if err != nil {
		return err
	}
	if a.status != "active" && a.status != "settled" {
		return ErrNotActive
	}
	var amount int64
	err = tx.QueryRow(ctx, `DELETE FROM payments WHERE id = $1 AND agreement_id = $2 RETURNING amount`,
		paymentID, agreementID).Scan(&amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := logActivity(ctx, tx, accountID, agreementID, a.contactID, userID, "payment.deleted", map[string]any{
		"amount": amount,
	}); err != nil {
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

type lockedAgreement struct {
	status    string
	principal int64
	contactID int64
}

func lockAgreement(ctx context.Context, tx pgx.Tx, accountID, id int64) (lockedAgreement, error) {
	var a lockedAgreement
	err := tx.QueryRow(ctx, `
		SELECT status, principal, contact_id FROM agreements WHERE account_id = $1 AND id = $2 FOR UPDATE`,
		accountID, id).Scan(&a.status, &a.principal, &a.contactID)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func paidTotal(ctx context.Context, tx pgx.Tx, agreementID int64) (int64, error) {
	var paid int64
	err := tx.QueryRow(ctx, `
		SELECT coalesce(sum(amount), 0)::bigint FROM payments WHERE agreement_id = $1 AND status = 'confirmed'`,
		agreementID).Scan(&paid)
	return paid, err
}

// recomputeStatus settles an agreement once confirmed payments cover the principal, and
// reopens it if they no longer do (a payment deleted, the principal raised).
func recomputeStatus(ctx context.Context, tx pgx.Tx, accountID, userID, id int64, before lockedAgreement) error {
	var status string
	if err := tx.QueryRow(ctx, `
		UPDATE agreements a SET status = CASE
			WHEN (SELECT coalesce(sum(amount), 0) FROM payments WHERE agreement_id = a.id AND status = 'confirmed') >= a.principal
			THEN 'settled' ELSE 'active' END
		WHERE a.id = $1 AND a.status IN ('active', 'settled')
		RETURNING a.status`, id).Scan(&status); err != nil {
		return err
	}
	switch {
	case before.status == "active" && status == "settled":
		return logActivity(ctx, tx, accountID, id, before.contactID, userID, "agreement.settled", nil)
	case before.status == "settled" && status == "active":
		return logActivity(ctx, tx, accountID, id, before.contactID, userID, "agreement.reopened", nil)
	}
	return nil
}

func insertInstallments(ctx context.Context, tx pgx.Tx, agreementID int64, items []InstallmentInput) error {
	rows := make([][]any, len(items))
	for i, it := range items {
		rows[i] = []any{agreementID, i + 1, it.DueOn, it.Amount}
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"installments"}, []string{"agreement_id", "seq", "due_on", "amount"},
		pgx.CopyFromRows(rows))
	return err
}

func logActivity(ctx context.Context, tx pgx.Tx, accountID, agreementID, contactID, userID int64, action string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO activity (account_id, agreement_id, contact_id, actor_user_id, action, data)
		VALUES ($1, nullif($2, 0), nullif($3, 0), nullif($4, 0), $5, $6)`,
		accountID, agreementID, contactID, userID, action, raw)
	return err
}

// DueItem is one installment that still has money owing, for the overview.
type DueItem struct {
	AgreementID int64  `json:"agreement_id"`
	ContactID   int64  `json:"contact_id"`
	ContactName string `json:"contact_name"`
	Direction   string `json:"direction"`
	Seq         int    `json:"seq"`
	Count       int    `json:"count"`
	DueOn       Date   `json:"due_on"`
	Remaining   int64  `json:"remaining"`
}

type Overview struct {
	Receivable        int64     `json:"receivable"`
	Payable           int64     `json:"payable"`
	OverdueReceivable int64     `json:"overdue_receivable"`
	OverduePayable    int64     `json:"overdue_payable"`
	OpenLoans         int       `json:"open_loans"`
	Due               []DueItem `json:"due"` // overdue and due in the next 30 days, oldest first
}

func (s *Store) Overview(ctx context.Context, accountID int64, today Date) (Overview, error) {
	var o Overview
	if err := s.db.QueryRow(ctx, `
		WITH `+summaryCTE+`
		SELECT coalesce(sum(remaining) FILTER (WHERE direction = 'lent'), 0)::bigint,
		       coalesce(sum(remaining) FILTER (WHERE direction = 'borrowed'), 0)::bigint,
		       coalesce(sum(overdue) FILTER (WHERE direction = 'lent'), 0)::bigint,
		       coalesce(sum(overdue) FILTER (WHERE direction = 'borrowed'), 0)::bigint,
		       count(*)
		FROM summary WHERE status = 'active'`, accountID, today).
		Scan(&o.Receivable, &o.Payable, &o.OverdueReceivable, &o.OverduePayable, &o.OpenLoans); err != nil {
		return o, err
	}

	rows, err := s.db.Query(ctx, `
		WITH paid AS (
			SELECT agreement_id, sum(amount) AS paid FROM payments WHERE status = 'confirmed' GROUP BY agreement_id
		), inst AS (
			SELECT i.agreement_id, i.seq, i.due_on, i.amount,
				sum(i.amount) OVER (PARTITION BY i.agreement_id ORDER BY i.seq) AS cum,
				count(*) OVER (PARTITION BY i.agreement_id) AS n
			FROM installments i JOIN agreements a ON a.id = i.agreement_id
			WHERE a.account_id = $1 AND a.status = 'active'
		)
		SELECT a.id, a.contact_id, c.name, a.direction, inst.seq, inst.n::int, inst.due_on,
			least(inst.amount, greatest(inst.cum - coalesce(p.paid, 0), 0))::bigint AS remaining
		FROM inst
		JOIN agreements a ON a.id = inst.agreement_id
		JOIN contacts c ON c.id = a.contact_id
		LEFT JOIN paid p ON p.agreement_id = inst.agreement_id
		WHERE inst.cum - coalesce(p.paid, 0) > 0 AND inst.due_on <= $2::date + 30
		ORDER BY inst.due_on, a.id, inst.seq
		LIMIT 50`, accountID, today)
	if err != nil {
		return o, err
	}
	defer rows.Close()
	o.Due = []DueItem{}
	for rows.Next() {
		var it DueItem
		if err := rows.Scan(&it.AgreementID, &it.ContactID, &it.ContactName, &it.Direction, &it.Seq, &it.Count, &it.DueOn, &it.Remaining); err != nil {
			return o, err
		}
		o.Due = append(o.Due, it)
	}
	return o, rows.Err()
}
