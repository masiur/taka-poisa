package reminders

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/masiur/taka-poisa/internal/ledger"
	"github.com/masiur/taka-poisa/internal/mail"
	"github.com/masiur/taka-poisa/internal/sms"
)

const (
	batchSize   = 20
	maxAttempts = 3
	// A send claimed for longer than this was interrupted (crash, deploy). It is marked failed,
	// never retried: reminders are delivered at most once rather than risk a duplicate.
	stuckAfter = 10 * time.Minute
	// Sends more than this late (server was down, email not configured yet) are skipped,
	// so a backlog never turns into a burst of stale emails.
	staleAfter = 24 * time.Hour
)

type Worker struct {
	DB      *pgxpool.Pool
	Ledger  *ledger.Store
	Mailer  mail.Mailer // nil when SMTP is not configured
	SMS     sms.Sender  // nil when no SMS gateway is configured
	BaseURL string
	Log     *slog.Logger
}

// Run delivers due reminders every 30 seconds until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			w.Log.Error("reminders: run failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type claimed struct {
	id, accountID, agreementID int64
	recipient, channel, tone   string
	scheduledFor               time.Time
	attempts                   int
}

// RunOnce claims up to batchSize due sends and delivers them; it returns how many it handled.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	if _, err := w.DB.Exec(ctx, `
		UPDATE reminder_sends SET status = 'failed', last_error = 'interrupted', updated_at = now()
		WHERE status = 'sending' AND updated_at < now() - make_interval(secs => $1)`, stuckAfter.Seconds()); err != nil {
		return 0, err
	}

	rows, err := w.DB.Query(ctx, `
		UPDATE reminder_sends SET status = 'sending', attempts = attempts + 1, updated_at = now()
		WHERE id IN (
			SELECT id FROM reminder_sends WHERE status = 'planned' AND scheduled_for <= now()
			ORDER BY scheduled_for LIMIT $1 FOR UPDATE SKIP LOCKED)
		RETURNING id, account_id, agreement_id, recipient, channel, tone, scheduled_for, attempts`, batchSize)
	if err != nil {
		return 0, err
	}
	var batch []claimed
	for rows.Next() {
		var c claimed
		if err := rows.Scan(&c.id, &c.accountID, &c.agreementID, &c.recipient, &c.channel, &c.tone, &c.scheduledFor, &c.attempts); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, c := range batch {
		w.deliver(ctx, c)
	}
	n, err := w.runOutbox(ctx)
	return len(batch) + n, err
}

// runOutbox expires stale loan requests and delivers request emails, with the same
// at-most-once and retry rules as reminders.
func (w *Worker) runOutbox(ctx context.Context) (int, error) {
	if _, err := w.Ledger.ExpireRequests(ctx); err != nil {
		return 0, err
	}
	if _, err := w.DB.Exec(ctx, `
		UPDATE outbox SET status = 'failed', last_error = 'interrupted', updated_at = now()
		WHERE status = 'sending' AND updated_at < now() - make_interval(secs => $1)`, stuckAfter.Seconds()); err != nil {
		return 0, err
	}
	rows, err := w.DB.Query(ctx, `
		UPDATE outbox SET status = 'sending', attempts = attempts + 1, updated_at = now()
		WHERE id IN (
			SELECT id FROM outbox WHERE status = 'planned' AND scheduled_for <= now()
			ORDER BY scheduled_for LIMIT $1 FOR UPDATE SKIP LOCKED)
		RETURNING id, kind, account_id, agreement_id, scheduled_for, attempts`, batchSize)
	if err != nil {
		return 0, err
	}
	type item struct {
		id, accountID, agreementID int64
		kind                       string
		scheduledFor               time.Time
		attempts                   int
	}
	var batch []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.kind, &it.accountID, &it.agreementID, &it.scheduledFor, &it.attempts); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	finish := func(id int64, status, reason, messageID, lastError string) {
		if _, err := w.DB.Exec(ctx, `
			UPDATE outbox SET status = $2, skip_reason = nullif($3, ''), provider_message_id = nullif($4, ''),
				last_error = nullif($5, ''), sent_at = CASE WHEN $2 = 'sent' THEN now() END, updated_at = now()
			WHERE id = $1`, id, status, reason, messageID, lastError); err != nil {
			w.Log.Error("outbox: update failed", "id", id, "err", err)
		}
	}
	retry := func(it item, cause error) {
		w.Log.Warn("outbox: send failed", "id", it.id, "attempt", it.attempts, "err", cause)
		if it.attempts >= maxAttempts {
			finish(it.id, "failed", "", "", cause.Error())
			return
		}
		if _, err := w.DB.Exec(ctx, `
			UPDATE outbox SET status = 'planned', last_error = $2, updated_at = now(),
				scheduled_for = now() + make_interval(mins => 5 * $3) WHERE id = $1`, it.id, cause.Error(), it.attempts); err != nil {
			w.Log.Error("outbox: requeue failed", "id", it.id, "err", err)
		}
	}

	for _, it := range batch {
		switch {
		case time.Since(it.scheduledFor) > staleAfter:
			finish(it.id, "skipped", "missed", "", "")
			continue
		case w.Mailer == nil:
			finish(it.id, "skipped", "email_not_configured", "", "")
			continue
		}
		msg, skip, err := w.composeOutbox(ctx, it.kind, it.accountID, it.agreementID)
		if err != nil {
			retry(it, err)
			continue
		}
		if skip != "" {
			finish(it.id, "skipped", skip, "", "")
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, time.Minute)
		id, err := w.Mailer.Send(sendCtx, msg)
		cancel()
		if err != nil {
			retry(it, err)
			continue
		}
		finish(it.id, "sent", "", id, "")
	}
	return len(batch), nil
}

func (w *Worker) deliver(ctx context.Context, c claimed) {
	if time.Since(c.scheduledFor) > staleAfter {
		w.finish(ctx, c, "skipped", "missed", "", "")
		return
	}
	if c.channel == "sms" && w.SMS == nil {
		w.finish(ctx, c, "skipped", "sms_not_configured", "", "")
		return
	}
	if c.channel != "sms" && w.Mailer == nil {
		w.finish(ctx, c, "skipped", "email_not_configured", "", "")
		return
	}
	rc, err := w.Ledger.ReminderContext(ctx, c.accountID, c.agreementID)
	if errors.Is(err, ledger.ErrNotFound) {
		w.finish(ctx, c, "skipped", "loan_gone", "", "")
		return
	}
	if err != nil {
		w.retry(ctx, c, err)
		return
	}
	// Re-check at send time: payments or opt-outs may have happened since planning.
	switch {
	case rc.Status != "active" || rc.Remaining <= 0:
		w.finish(ctx, c, "skipped", "paid", "", "")
		return
	case c.channel == "sms" && (rc.Direction != "lent" || rc.ContactPhone == ""):
		w.finish(ctx, c, "skipped", "no_phone", "", "")
		return
	case c.channel == "sms" && rc.SMSOptedOut:
		w.finish(ctx, c, "skipped", "opted_out", "", "")
		return
	case c.channel != "sms" && c.recipient == "contact" && (rc.Direction != "lent" || rc.ContactEmail == ""):
		w.finish(ctx, c, "skipped", "no_email", "", "")
		return
	case c.channel != "sms" && c.recipient == "contact" && rc.OptedOut:
		w.finish(ctx, c, "skipped", "opted_out", "", "")
		return
	}

	if c.channel == "sms" {
		token, err := w.Ledger.CreateStatementLink(ctx, c.accountID, rc.ContactID)
		if err != nil {
			w.retry(ctx, c, err)
			return
		}
		sendCtx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		id, err := w.SMS.Send(sendCtx, sms.Message{To: rc.ContactPhone, Text: composeSMS(rc, c.tone, w.BaseURL+"/s/"+token)})
		if err != nil {
			w.retry(ctx, c, err)
			return
		}
		w.finish(ctx, c, "sent", "", id, "")
		return
	}

	var msg mail.Message
	if c.recipient == "contact" {
		token, err := w.Ledger.CreateStatementLink(ctx, c.accountID, rc.ContactID)
		if err != nil {
			w.retry(ctx, c, err)
			return
		}
		msg = compose(rc, c.recipient, c.tone,
			w.BaseURL+"/s/"+token,
			w.BaseURL+"/s/"+token+"?unsubscribe=1",
			w.BaseURL+"/api/public/unsubscribe/"+token)
	} else {
		msg = compose(rc, c.recipient, c.tone, w.BaseURL+"/loans/"+strconv.FormatInt(c.agreementID, 10), "", "")
	}

	sendCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	id, err := w.Mailer.Send(sendCtx, msg)
	if err != nil {
		w.retry(ctx, c, err)
		return
	}
	w.finish(ctx, c, "sent", "", id, "")
}

// retry puts a failed send back in the queue with backoff, or gives up after maxAttempts.
func (w *Worker) retry(ctx context.Context, c claimed, cause error) {
	w.Log.Warn("reminders: send failed", "send_id", c.id, "attempt", c.attempts, "err", cause)
	if c.attempts >= maxAttempts {
		w.finish(ctx, c, "failed", "", "", cause.Error())
		return
	}
	if _, err := w.DB.Exec(ctx, `
		UPDATE reminder_sends SET status = 'planned', last_error = $2, updated_at = now(),
			scheduled_for = now() + make_interval(mins => 5 * $3)
		WHERE id = $1`, c.id, cause.Error(), c.attempts); err != nil {
		w.Log.Error("reminders: requeue failed", "send_id", c.id, "err", err)
	}
}

func (w *Worker) finish(ctx context.Context, c claimed, status, reason, messageID, lastError string) {
	if _, err := w.DB.Exec(ctx, `
		UPDATE reminder_sends SET status = $2, skip_reason = nullif($3, ''), provider_message_id = nullif($4, ''),
			last_error = nullif($5, ''), sent_at = CASE WHEN $2 = 'sent' THEN now() END, updated_at = now()
		WHERE id = $1`, c.id, status, reason, messageID, lastError); err != nil {
		w.Log.Error("reminders: update failed", "send_id", c.id, "err", err)
		return
	}
	if status == "sent" {
		if _, err := w.DB.Exec(ctx, `
			INSERT INTO activity (account_id, agreement_id, action, data)
			VALUES ($1, $2, 'reminder.sent', jsonb_build_object('recipient', $3::text, 'channel', $4::text))`,
			c.accountID, c.agreementID, c.recipient, c.channel); err != nil {
			w.Log.Error("reminders: activity failed", "send_id", c.id, "err", err)
		}
	}
}

// composeOutbox writes the email for one outbox item, or returns why it should be skipped.
func (w *Worker) composeOutbox(ctx context.Context, kind string, accountID, agreementID int64) (mail.Message, string, error) {
	detail, err := w.Ledger.GetAgreement(ctx, accountID, agreementID, ledger.Today("UTC"))
	if errors.Is(err, ledger.ErrNotFound) {
		return mail.Message{}, "loan_gone", nil
	}
	if err != nil {
		return mail.Message{}, "", err
	}
	installments := make([]ledger.InstallmentInput, len(detail.Installments))
	for i, x := range detail.Installments {
		installments[i] = ledger.InstallmentInput{DueOn: x.DueOn, Amount: x.Amount}
	}
	loanURL := w.BaseURL + "/loans/" + strconv.FormatInt(agreementID, 10)

	if strings.HasPrefix(kind, "link.") {
		n, err := w.Ledger.LinkNotice(ctx, accountID, agreementID)
		if errors.Is(err, ledger.ErrNotFound) {
			return mail.Message{}, "loan_gone", nil
		}
		if err != nil {
			return mail.Message{}, "", err
		}
		n.Kind = kind
		if kind == "link.received" {
			return composeLink(n, installments, loanURL), "", nil
		}
		if n.RequesterEmail == "" {
			return mail.Message{}, "no_email", nil
		}
		link := ""
		if kind == "link.sent" {
			token, err := w.Ledger.CreateStatementLink(ctx, accountID, n.ContactID)
			if err != nil {
				return mail.Message{}, "", err
			}
			link = w.BaseURL + "/s/" + token
		}
		return composeLink(n, installments, link), "", nil
	}

	n, err := w.Ledger.RequestNotice(ctx, accountID, agreementID)
	if errors.Is(err, ledger.ErrNotFound) {
		return mail.Message{}, "loan_gone", nil
	}
	if err != nil {
		return mail.Message{}, "", err
	}
	n.Kind = kind
	if kind != "request.created" {
		return composeRequest(n, installments, loanURL), "", nil
	}
	if n.Status != "requested" {
		return mail.Message{}, "closed", nil
	}
	if n.LenderEmail == "" {
		return mail.Message{}, "no_email", nil
	}
	token, err := w.Ledger.CreateRequestLink(ctx, accountID, agreementID)
	if err != nil {
		return mail.Message{}, "", err
	}
	return composeRequest(n, installments, w.BaseURL+"/r/"+token), "", nil
}
