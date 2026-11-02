package reminders

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/masiur/taka-poisa/internal/ledger"
	"github.com/masiur/taka-poisa/internal/mail"
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
	recipient, tone            string
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
		RETURNING id, account_id, agreement_id, recipient, tone, scheduled_for, attempts`, batchSize)
	if err != nil {
		return 0, err
	}
	var batch []claimed
	for rows.Next() {
		var c claimed
		if err := rows.Scan(&c.id, &c.accountID, &c.agreementID, &c.recipient, &c.tone, &c.scheduledFor, &c.attempts); err != nil {
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
	return len(batch), nil
}

func (w *Worker) deliver(ctx context.Context, c claimed) {
	if time.Since(c.scheduledFor) > staleAfter {
		w.finish(ctx, c, "skipped", "missed", "", "")
		return
	}
	if w.Mailer == nil {
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
	case c.recipient == "contact" && (rc.Direction != "lent" || rc.ContactEmail == ""):
		w.finish(ctx, c, "skipped", "no_email", "", "")
		return
	case c.recipient == "contact" && rc.OptedOut:
		w.finish(ctx, c, "skipped", "opted_out", "", "")
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
			VALUES ($1, $2, 'reminder.sent', jsonb_build_object('recipient', $3::text))`,
			c.accountID, c.agreementID, c.recipient); err != nil {
			w.Log.Error("reminders: activity failed", "send_id", c.id, "err", err)
		}
	}
}
