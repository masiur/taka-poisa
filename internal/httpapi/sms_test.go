package httpapi

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/masiur/taka-poisa/internal/ledger"
	"github.com/masiur/taka-poisa/internal/sms"
)

type fakeSMS struct {
	mu   sync.Mutex
	sent []sms.Message
}

func (f *fakeSMS) Send(_ context.Context, m sms.Message) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return "sms-1", nil
}

func (f *fakeSMS) all() []sms.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sms.Message(nil), f.sent...)
}

func TestSMSReminders(t *testing.T) {
	srv, pool := newTestEnv(t, nil)
	c := ownerClient(t, srv.URL)
	today := ledger.Today("Asia/Dhaka")
	rahim := mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": "Rahim", "phone": "01712-345678", "locale": "en"})
	loan := mustCreate(t, c, srv.URL+"/api/agreements", monthlyLoan(rahim, 2000_00, 1, today.AddDays(5)))
	loanURL := fmt.Sprintf("%s/api/agreements/%d", srv.URL, loan)

	if code, _ := call(t, c, "PUT", loanURL+"/reminders", map[string]any{"rules": []any{
		map[string]any{"offset_days": 0, "recipient": "self", "channel": "sms", "tone": "polite"},
	}}); code != 422 {
		t.Fatalf("SMS to self accepted: %d", code)
	}
	if code, _ := call(t, c, "PUT", loanURL+"/reminders", map[string]any{"rules": []any{
		map[string]any{"offset_days": -1, "recipient": "contact", "channel": "sms", "tone": "polite"},
		map[string]any{"offset_days": -1, "recipient": "contact", "channel": "email", "tone": "polite"},
	}}); code != 204 {
		t.Fatalf("set SMS rule: %d", code)
	}
	_, body := call(t, c, "GET", loanURL+"/reminders", nil)
	view := body["reminders"].(map[string]any)
	upcoming := view["upcoming"].([]any)
	// Rahim has no email, so only the SMS is planned.
	if len(upcoming) != 1 || upcoming[0].(map[string]any)["channel"] != "sms" || view["contact_phone"] != "+8801712345678" {
		t.Fatalf("planned: %v", view)
	}

	// No gateway configured: skipped with a reason the owner can see.
	first := makeDue(t, pool, loan, "contact", "1 minute")
	newWorker(pool, nil).RunOnce(context.Background())
	if status, reason := sendStatus(t, pool, first); status != "skipped" || reason != "sms_not_configured" {
		t.Fatalf("no gateway: %s %s", status, reason)
	}

	// Send now through a gateway.
	gw := &fakeSMS{}
	w := newWorker(pool, nil)
	w.SMS = gw
	if code, body := call(t, c, "POST", loanURL+"/reminders/send-now", map[string]any{"recipient": "contact", "channel": "sms"}); code != 202 {
		t.Fatalf("send now SMS: %d %v", code, body)
	}
	if code, _ := call(t, c, "POST", loanURL+"/reminders/send-now", map[string]any{"recipient": "contact", "channel": "sms"}); code != 409 {
		t.Fatalf("second SMS today: %d", code)
	}
	if code, _ := call(t, c, "POST", loanURL+"/reminders/send-now", map[string]any{"recipient": "contact", "channel": "email"}); code != 422 {
		t.Fatalf("email to a contact without email: %d", code)
	}
	w.RunOnce(context.Background())
	sent := gw.all()
	if len(sent) != 1 || sent[0].To != "+8801712345678" || !strings.Contains(sent[0].Text, "Masiur") || !strings.Contains(sent[0].Text, "https://app.test/s/") {
		t.Fatalf("SMS: %+v", sent)
	}

	// "Stop SMS" from the statement page.
	link := sent[0].Text[strings.Index(sent[0].Text, "https://app.test/s/"):]
	token := strings.TrimPrefix(strings.Fields(link)[0], "https://app.test/s/")
	if code, _ := call(t, newClient(t), "POST", srv.URL+"/api/public/statement/"+token+"/optout?channel=sms", nil); code != 204 {
		t.Fatalf("stop SMS: %d", code)
	}
	_, st := call(t, newClient(t), "GET", srv.URL+"/api/public/statement/"+token, nil)
	if st["sms_opted_out"] != true || st["opted_out"] != false {
		t.Fatalf("statement after stop SMS: %v", st)
	}
	_, body = call(t, c, "GET", loanURL+"/reminders", nil)
	if n := len(body["reminders"].(map[string]any)["upcoming"].([]any)); n != 0 {
		t.Fatalf("%d SMS still planned after opting out", n)
	}
	if code, _ := call(t, c, "POST", loanURL+"/reminders/send-now", map[string]any{"recipient": "contact", "channel": "sms"}); code != 422 {
		t.Fatalf("send now to opted-out SMS: %d", code)
	}
}

func TestPlanEmailAndSMSSameDay(t *testing.T) {
	items := []ledger.InstallmentState{{Seq: 1, DueOn: ledger.Today("UTC").AddDays(10), Amount: 1, Remaining: 1}}
	rules := []ledger.Rule{
		{ID: 1, Recipient: "contact", Channel: "email", Tone: "polite"},
		{ID: 2, Recipient: "contact", Channel: "sms", Tone: "polite"},
	}
	got := ledger.PlanSends(items, rules, timeNowUTC(), utc, 9, map[string]bool{"contact/email": true, "contact/sms": true})
	if len(got) != 2 || got[0].Channel == got[1].Channel {
		t.Fatalf("want one email and one SMS on the day: %+v", got)
	}
}

var utc = time.UTC

func timeNowUTC() time.Time { return time.Now().UTC() }
