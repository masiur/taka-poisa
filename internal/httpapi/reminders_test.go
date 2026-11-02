package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/masiur/taka-poisa/internal/ledger"
	"github.com/masiur/taka-poisa/internal/mail"
	"github.com/masiur/taka-poisa/internal/reminders"
)

type fakeMailer struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (f *fakeMailer) Send(_ context.Context, m mail.Message) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return fmt.Sprintf("<fake-%d@test>", len(f.sent)), nil
}

func (f *fakeMailer) all() []mail.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]mail.Message(nil), f.sent...)
}

func newWorker(pool *pgxpool.Pool, m mail.Mailer) *reminders.Worker {
	w := &reminders.Worker{DB: pool, Ledger: ledger.NewStore(pool), BaseURL: "https://app.test", Log: slog.New(slog.DiscardHandler)}
	if m != nil {
		w.Mailer = m
	}
	return w
}

// makeDue moves planned sends for a recipient to the past so the worker picks them up.
func makeDue(t *testing.T, pool *pgxpool.Pool, agreementID int64, recipient, ago string) int64 {
	t.Helper()
	var n int64
	err := pool.QueryRow(context.Background(), `
		WITH first AS (
			SELECT id FROM reminder_sends WHERE agreement_id = $1 AND recipient = $2 AND status = 'planned'
			ORDER BY scheduled_for LIMIT 1)
		UPDATE reminder_sends SET scheduled_for = now() - $3::interval,
			send_day = ((now() - $3::interval) AT TIME ZONE 'Asia/Dhaka')::date
		WHERE id IN (SELECT id FROM first)
		RETURNING id`, agreementID, recipient, ago).Scan(&n)
	if err != nil {
		t.Fatalf("make due: %v", err)
	}
	return n
}

func sendStatus(t *testing.T, pool *pgxpool.Pool, id int64) (status, reason string) {
	t.Helper()
	pool.QueryRow(context.Background(), `SELECT status, coalesce(skip_reason, '') FROM reminder_sends WHERE id = $1`, id).Scan(&status, &reason)
	return
}

func TestReminderEndToEnd(t *testing.T) {
	srv, pool := newTestEnv(t, nil)
	c := ownerClient(t, srv.URL)
	today := ledger.Today("Asia/Dhaka")
	rahim := mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": "Rahim", "email": "rahim@example.com", "locale": "en"})
	loan := mustCreate(t, c, srv.URL+"/api/agreements", monthlyLoan(rahim, 3000_00, 3, today.AddDays(5)))
	loanURL := fmt.Sprintf("%s/api/agreements/%d", srv.URL, loan)

	// Default rules are copied onto the loan and planned for both the contact and the owner.
	_, body := call(t, c, "GET", loanURL+"/reminders", nil)
	view := body["reminders"].(map[string]any)
	if len(view["rules"].([]any)) != 4 || len(view["upcoming"].([]any)) == 0 || body["email_enabled"] != false {
		t.Fatalf("reminders view: %v", body)
	}

	mailer := &fakeMailer{}
	w := newWorker(pool, mailer)
	sendID := makeDue(t, pool, loan, "contact", "1 minute")
	if n, err := w.RunOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("run: %d %v", n, err)
	}
	sent := mailer.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d emails", len(sent))
	}
	m := sent[0]
	if m.To != "rahim@example.com" || m.ReplyTo != "owner@example.com" || !strings.Contains(m.Subject, "Masiur") ||
		!strings.Contains(m.Text, "https://app.test/s/") || !strings.HasPrefix(m.UnsubscribeURL, "https://app.test/api/public/unsubscribe/") {
		t.Fatalf("email: %+v", m)
	}
	if status, _ := sendStatus(t, pool, sendID); status != "sent" {
		t.Fatalf("send status %s", status)
	}
	// The same send is never delivered twice.
	if n, _ := w.RunOnce(context.Background()); n != 0 || len(mailer.all()) != 1 {
		t.Fatalf("re-run delivered again")
	}

	// The contact opens the statement from the email.
	token := m.UnsubscribeURL[strings.LastIndex(m.UnsubscribeURL, "/")+1:]
	code, st := call(t, newClient(t), "GET", srv.URL+"/api/public/statement/"+token, nil)
	if code != 200 || st["contact_name"] != "Rahim" || st["owner_name"] != "Masiur" || len(st["loans"].([]any)) != 1 {
		t.Fatalf("statement: %d %v", code, st)
	}
	if code, _ := call(t, newClient(t), "GET", srv.URL+"/api/public/statement/not-a-token", nil); code != 404 {
		t.Fatalf("bad token: %d", code)
	}

	// "I've paid": pending until the owner confirms.
	claim := map[string]any{"agreement_id": loan, "amount": 1000_00, "paid_on": today.String(), "method": "bkash", "note": "TrxID 1"}
	if code, body := call(t, newClient(t), "POST", srv.URL+"/api/public/statement/"+token+"/claims", claim); code != 201 {
		t.Fatalf("claim: %d %v", code, body)
	}
	_, a := call(t, c, "GET", loanURL, nil)
	if num(a["paid"]) != 0 {
		t.Fatalf("unconfirmed claim changed the balance: %v", a["paid"])
	}
	claimID := num(a["payments"].([]any)[0].(map[string]any)["id"])
	if code, _ := call(t, c, "POST", fmt.Sprintf("%s/payments/%d/confirm", loanURL, claimID), nil); code != 204 {
		t.Fatalf("confirm: %d", code)
	}
	_, a = call(t, c, "GET", loanURL, nil)
	if num(a["paid"]) != 1000_00 || !hasActivity(a, "claim.confirmed") {
		t.Fatalf("after confirm: %v", a)
	}
	// A claim for someone else's loan through this token is refused.
	other := mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": "Other"})
	otherLoan := mustCreate(t, c, srv.URL+"/api/agreements", monthlyLoan(other, 100_00, 1, today))
	claim["agreement_id"] = otherLoan
	if code, _ := call(t, newClient(t), "POST", srv.URL+"/api/public/statement/"+token+"/claims", claim); code != 404 {
		t.Fatalf("claim on another contact's loan: %d", code)
	}

	// Send now: once per recipient per day.
	if code, _ := call(t, c, "POST", loanURL+"/reminders/send-now", map[string]any{"recipient": "contact"}); code != 409 {
		t.Fatalf("second contact email today: %d", code) // the rule-based one went out today
	}
	if code, body := call(t, c, "POST", loanURL+"/reminders/send-now", map[string]any{"recipient": "self"}); code != 202 {
		t.Fatalf("send now to self: %d %v", code, body)
	}
	if code, _ := call(t, c, "POST", loanURL+"/reminders/send-now", map[string]any{"recipient": "self"}); code != 409 {
		t.Fatalf("second self email today: %d", code)
	}
	w.RunOnce(context.Background())
	if last := mailer.all()[len(mailer.all())-1]; last.To != "owner@example.com" || !strings.Contains(last.Text, "https://app.test/loans/") {
		t.Fatalf("self reminder: %+v", last)
	}

	// One-click unsubscribe removes all planned emails to the contact.
	res, err := http.Post(srv.URL+"/api/public/unsubscribe/"+token, "application/x-www-form-urlencoded", strings.NewReader("List-Unsubscribe=One-Click"))
	if err != nil || res.StatusCode != 204 {
		t.Fatalf("unsubscribe: %v %v", err, res.StatusCode)
	}
	_, body = call(t, c, "GET", loanURL+"/reminders", nil)
	view = body["reminders"].(map[string]any)
	if view["contact_opted_out"] != true {
		t.Fatalf("not opted out: %v", view)
	}
	for _, u := range view["upcoming"].([]any) {
		if u.(map[string]any)["recipient"] == "contact" {
			t.Fatalf("contact still has planned emails after opting out")
		}
	}
	if code, _ := call(t, c, "POST", loanURL+"/reminders/send-now", map[string]any{"recipient": "contact"}); code != 422 {
		t.Fatalf("send now to opted-out contact: %d", code)
	}

	// Paying in full clears every planned reminder.
	call(t, c, "POST", loanURL+"/payments", map[string]any{"amount": 2000_00, "paid_on": today.String(), "method": "cash"})
	_, body = call(t, c, "GET", loanURL+"/reminders", nil)
	if n := len(body["reminders"].(map[string]any)["upcoming"].([]any)); n != 0 {
		t.Fatalf("%d reminders still planned after settling", n)
	}
}

func TestReminderRulesAndSkips(t *testing.T) {
	srv, pool := newTestEnv(t, nil)
	c := ownerClient(t, srv.URL)
	today := ledger.Today("Asia/Dhaka")
	contact := mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": "Karim", "email": "karim@example.com"})
	loan := mustCreate(t, c, srv.URL+"/api/agreements", monthlyLoan(contact, 1000_00, 1, today.AddDays(10)))
	loanURL := fmt.Sprintf("%s/api/agreements/%d", srv.URL, loan)

	if code, _ := call(t, c, "PUT", loanURL+"/reminders", map[string]any{"rules": []any{map[string]any{"offset_days": 99, "recipient": "contact", "tone": "polite"}}}); code != 422 {
		t.Fatalf("invalid rule: %d", code)
	}
	rules := []any{
		map[string]any{"offset_days": -7, "recipient": "contact", "tone": "polite"},
		map[string]any{"offset_days": -1, "recipient": "contact", "tone": "polite", "send_hour": 20},
		map[string]any{"offset_days": 1, "recipient": "contact", "tone": "firm", "repeat_every_days": 2, "repeat_count": 3},
	}
	if code, _ := call(t, c, "PUT", loanURL+"/reminders", map[string]any{"rules": rules}); code != 204 {
		t.Fatalf("set rules: %d", code)
	}
	_, body := call(t, c, "GET", loanURL+"/reminders", nil)
	if n := len(body["reminders"].(map[string]any)["upcoming"].([]any)); n != 6 {
		t.Fatalf("planned %d sends, want 6 (1 + 1 + 1 + 3 repeats)", n)
	}

	// Stale (server was down) → skipped, not sent.
	stale := makeDue(t, pool, loan, "contact", "2 days")
	mailer := &fakeMailer{}
	newWorker(pool, mailer).RunOnce(context.Background())
	if status, reason := sendStatus(t, pool, stale); status != "skipped" || reason != "missed" || len(mailer.all()) != 0 {
		t.Fatalf("stale send: %s %s, %d emails", status, reason, len(mailer.all()))
	}

	// No SMTP configured → skipped with a reason the owner can see.
	noMail := makeDue(t, pool, loan, "contact", "1 minute")
	newWorker(pool, nil).RunOnce(context.Background())
	if status, reason := sendStatus(t, pool, noMail); status != "skipped" || reason != "email_not_configured" {
		t.Fatalf("no mailer: %s %s", status, reason)
	}

	// Two workers racing for the same due send deliver it once.
	due := makeDue(t, pool, loan, "contact", "1 minute")
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); newWorker(pool, mailer).RunOnce(context.Background()) }()
	}
	wg.Wait()
	if status, _ := sendStatus(t, pool, due); status != "sent" || len(mailer.all()) != 1 {
		t.Fatalf("racing workers: %s, %d emails", status, len(mailer.all()))
	}

	// Account defaults apply to new loans only.
	defaults := []any{map[string]any{"offset_days": 0, "recipient": "self", "tone": "polite"}}
	if code, _ := call(t, c, "PUT", srv.URL+"/api/account/reminder-defaults", map[string]any{"rules": defaults}); code != 204 {
		t.Fatalf("set defaults: %d", code)
	}
	loan2 := mustCreate(t, c, srv.URL+"/api/agreements", monthlyLoan(contact, 500_00, 1, today.AddDays(3)))
	_, body = call(t, c, "GET", fmt.Sprintf("%s/api/agreements/%d/reminders", srv.URL, loan2), nil)
	if n := len(body["reminders"].(map[string]any)["rules"].([]any)); n != 1 {
		t.Fatalf("new loan has %d rules, want the 1 default", n)
	}
	_, body = call(t, c, "GET", loanURL+"/reminders", nil)
	if n := len(body["reminders"].(map[string]any)["rules"].([]any)); n != 3 {
		t.Fatalf("existing loan changed: %d rules", n)
	}

	// Borrowed loans never email the lender, only the owner.
	borrowed := monthlyLoan(contact, 100_00, 1, today.AddDays(3))
	borrowed["direction"] = "borrowed"
	b := mustCreate(t, c, srv.URL+"/api/agreements", borrowed)
	call(t, c, "PUT", fmt.Sprintf("%s/api/agreements/%d/reminders", srv.URL, b), map[string]any{"rules": rules})
	_, body = call(t, c, "GET", fmt.Sprintf("%s/api/agreements/%d/reminders", srv.URL, b), nil)
	if n := len(body["reminders"].(map[string]any)["upcoming"].([]any)); n != 0 {
		t.Fatalf("borrowed loan planned %d contact emails", n)
	}

	// Another account cannot see or change these reminders.
	otherClient := createSecondAccount(t, pool)
	call(t, otherClient, "POST", srv.URL+"/api/login", map[string]any{"email": "other@example.com", "password": "other-pass-1"})
	for _, ch := range []struct{ method, path string }{
		{"GET", "/reminders"}, {"PUT", "/reminders"}, {"POST", "/reminders/send-now"},
	} {
		var b any = map[string]any{"rules": []any{}, "recipient": "self"}
		if ch.method == "GET" {
			b = nil
		}
		if ch.method == "POST" {
			b = map[string]any{"recipient": "self"}
		}
		if ch.method == "PUT" {
			b = map[string]any{"rules": []any{}}
		}
		if code, _ := call(t, otherClient, ch.method, loanURL+ch.path, b); code != 404 {
			t.Errorf("other account %s %s: %d", ch.method, ch.path, code)
		}
	}
	if code, _ := call(t, otherClient, "POST", fmt.Sprintf("%s/api/contacts/%d/statement-link", srv.URL, contact), nil); code != 404 {
		t.Errorf("other account statement link: %d", code)
	}
}
