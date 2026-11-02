package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/masiur/taka-poisa/internal/ledger"
)

func askBody(name, email string, principal int64, firstDue ledger.Date) map[string]any {
	return map[string]any{
		"name": name, "email": email, "phone": "01712-345678", "locale": "en",
		"principal": principal, "started_on": firstDue.AddDays(-30).String(), "purpose": "Medical bill",
		"message":  "Need it for my mother's treatment.",
		"payout":   map[string]any{"method": "bkash", "account_number": "01712345678"},
		"schedule": map[string]any{"mode": "generate", "count": 2, "first_due": firstDue.String(), "frequency": "monthly"},
	}
}

func createLinkFor(t *testing.T, c *http.Client, url string, body map[string]any) map[string]any {
	t.Helper()
	code, l := call(t, c, "POST", url+"/api/request-links", body)
	if code != 201 {
		t.Fatalf("create link: %d %v", code, l)
	}
	return l
}

func tokenOf(link map[string]any) string {
	u := link["url"].(string)
	return u[strings.LastIndex(u, "/")+1:]
}

func countRows(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRequestLinkFlow(t *testing.T) {
	srv, pool := newTestEnv(t, nil)
	c := ownerClient(t, srv.URL)
	today := ledger.Today("Asia/Dhaka")
	link := createLinkFor(t, c, srv.URL, map[string]any{"label": "Everyone", "max_amount": 50000_00})
	if !strings.Contains(link["url"].(string), "/ask/") {
		t.Fatalf("link url: %v", link)
	}
	token := tokenOf(link)
	anon := newClient(t)

	code, info := call(t, anon, "GET", srv.URL+"/api/public/ask/"+token, nil)
	if code != 200 || info["owner_name"] != "Masiur" || info["currency"] != "BDT" || num(info["max_amount"]) != 50000_00 {
		t.Fatalf("link info: %d %v", code, info)
	}
	if code, _ := call(t, anon, "POST", srv.URL+"/api/public/schedule/preview", map[string]any{
		"principal": 100_00, "schedule": map[string]any{"mode": "generate", "count": 1, "first_due": today.String()}}); code != 200 {
		t.Fatalf("public preview: %d", code)
	}

	if code, body := call(t, anon, "POST", srv.URL+"/api/public/ask/"+token, askBody("Rahim", "Rahim@Example.com", 60000_00, today.AddDays(30))); code != 422 ||
		body["fields"].(map[string]any)["principal"] != "above_limit" {
		t.Fatalf("above limit: %d %v", code, body)
	}
	if code, body := call(t, anon, "POST", srv.URL+"/api/public/ask/"+token, askBody("Rahim", "Rahim@Example.com", 10000_00, today.AddDays(30))); code != 201 {
		t.Fatalf("submit: %d %v", code, body)
	}

	// Rahim is a contact right away; the request waits as a lent agreement and owes nothing yet.
	_, list := call(t, c, "GET", srv.URL+"/api/contacts", nil)
	contacts := list["contacts"].([]any)
	if len(contacts) != 1 || contacts[0].(map[string]any)["email"] != "rahim@example.com" || contacts[0].(map[string]any)["phone"] != "+8801712345678" {
		t.Fatalf("contact from request: %v", contacts)
	}
	_, reqs := call(t, c, "GET", srv.URL+"/api/agreements?status=requested", nil)
	id := num(reqs["agreements"].([]any)[0].(map[string]any)["id"])
	loanURL := fmt.Sprintf("%s/api/agreements/%d", srv.URL, id)
	_, a := call(t, c, "GET", loanURL, nil)
	payout, _ := a["payout"].(map[string]any)
	if a["origin"] != "link" || a["direction"] != "lent" || payout["method"] != "bkash" || payout["account_number"] != "+8801712345678" {
		t.Fatalf("link request: %v", a)
	}
	if _, o := call(t, c, "GET", srv.URL+"/api/overview", nil); num(o["receivable"]) != 0 {
		t.Fatalf("an unsent request counts as owed: %v", o)
	}

	mailer := &fakeMailer{}
	w := newWorker(pool, mailer)
	w.RunOnce(context.Background())
	sent := mailer.all()
	if len(sent) != 1 || sent[0].To != "owner@example.com" || sent[0].Subject != "Rahim asks to borrow ৳10,000" || sent[0].ReplyTo != "rahim@example.com" {
		t.Fatalf("owner notice: %+v", sent)
	}
	if strings.Contains(sent[0].Text+sent[0].HTML, "1712345678") {
		t.Fatal("payout details leaked into an email")
	}

	if code, _ := call(t, c, "POST", loanURL+"/approve", map[string]any{"note": "Sending tonight"}); code != 204 {
		t.Fatalf("approve: %d", code)
	}
	// Terms can still be adjusted before sending.
	terms := map[string]any{"principal": 8000_00, "started_on": today.String(), "purpose": "Medical bill",
		"schedule": map[string]any{"mode": "generate", "count": 1, "first_due": today.AddDays(45).String()}}
	if code, body := call(t, c, "PUT", loanURL, terms); code != 204 {
		t.Fatalf("adjust approved request: %d %v", code, body)
	}

	if code, body := call(t, c, "POST", loanURL+"/mark-sent", map[string]any{"method": "bkash", "ref": "TX9KL", "sent_on": today.String()}); code != 204 {
		t.Fatalf("mark sent: %d %v", code, body)
	}
	if code, _ := call(t, c, "POST", loanURL+"/mark-sent", map[string]any{"method": "bkash", "sent_on": today.String()}); code != 409 {
		t.Fatalf("mark sent twice: %d", code)
	}
	_, a = call(t, c, "GET", loanURL, nil)
	if a["status"] != "active" || a["payout"] != nil || a["sent_ref"] != "TX9KL" || a["started_on"] != today.String() || num(a["principal"]) != 8000_00 {
		t.Fatalf("after sent: %v", a)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM payout_details`); n != 0 {
		t.Fatalf("%d payout rows kept after sending", n)
	}
	if _, o := call(t, c, "GET", srv.URL+"/api/overview", nil); num(o["receivable"]) != 8000_00 {
		t.Fatalf("sent loan not receivable: %v", o)
	}

	w.RunOnce(context.Background())
	sent = mailer.all()
	// Emails are written when sent, so they show the adjusted amount.
	approved, sentMail := sent[1], sent[2]
	if approved.To != "rahim@example.com" || approved.Subject != "Masiur approved your request for ৳8,000" || !strings.Contains(approved.Text, "Sending tonight") ||
		strings.Contains(approved.HTML, `href=""`) || strings.Contains(approved.Text, "Open in Taka Poisa") {
		t.Fatalf("approved email: %+v", approved)
	}
	if sentMail.Subject != "Masiur sent you ৳8,000" || !strings.Contains(sentMail.Text, "by bKash (transaction TX9KL)") || !strings.Contains(sentMail.Text, "/s/") {
		t.Fatalf("sent email: %+v", sentMail)
	}

	// Honeypot: looks fine to a bot, stores nothing.
	before := countRows(t, pool, `SELECT count(*) FROM agreements`)
	bot := askBody("Bot", "bot@example.com", 100_00, today.AddDays(30))
	bot["website"] = "http://spam.example"
	if code, _ := call(t, anon, "POST", srv.URL+"/api/public/ask/"+token, bot); code != 201 {
		t.Fatalf("honeypot: %d", code)
	}
	if after := countRows(t, pool, `SELECT count(*) FROM agreements`); after != before {
		t.Fatal("honeypot submission was stored")
	}

	// Revoked links stop working.
	if code, _ := call(t, c, "DELETE", fmt.Sprintf("%s/api/request-links/%d", srv.URL, num(link["id"])), nil); code != 204 {
		t.Fatalf("revoke: %d", code)
	}
	if code, _ := call(t, anon, "GET", srv.URL+"/api/public/ask/"+token, nil); code != 404 {
		t.Fatalf("revoked link info: %d", code)
	}

	// Another account cannot act on this request.
	other := createSecondAccount(t, pool)
	call(t, other, "POST", srv.URL+"/api/login", map[string]any{"email": "other@example.com", "password": "other-pass-1"})
	for _, action := range []string{"/approve", "/decline", "/block"} {
		if code, _ := call(t, other, "POST", loanURL+action, map[string]any{}); code != 404 {
			t.Errorf("other account %s: %d", action, code)
		}
	}
}

func TestRequestLinkGrantedToContactBothWays(t *testing.T) {
	srv, pool := newTestEnv(t, nil)
	c := ownerClient(t, srv.URL)
	today := ledger.Today("Asia/Dhaka")
	rahim := mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": "Rahim", "email": "rahim@example.com"})

	l1 := createLinkFor(t, c, srv.URL, map[string]any{"contact_id": rahim})
	l2 := createLinkFor(t, c, srv.URL, map[string]any{"contact_id": rahim})
	if l1["id"] != l2["id"] || l1["contact_name"] != "Rahim" {
		t.Fatalf("granting twice should return the same link: %v %v", l1, l2)
	}
	_, info := call(t, newClient(t), "GET", srv.URL+"/api/public/ask/"+tokenOf(l1), nil)
	if info["name"] != "Rahim" || info["email"] != "rahim@example.com" {
		t.Fatalf("prefill: %v", info)
	}

	// Rahim has a verified account here, so it works both ways.
	requester := createUser(t, pool, srv.URL, "rahim@example.com", true)
	if code, body := call(t, newClient(t), "POST", srv.URL+"/api/public/ask/"+tokenOf(l1), askBody("Rahim", "rahim@example.com", 3000_00, today.AddDays(20))); code != 201 {
		t.Fatalf("submit: %d %v", code, body)
	}
	_, mine := call(t, c, "GET", srv.URL+"/api/contacts", nil)
	if n := len(mine["contacts"].([]any)); n != 1 {
		t.Fatalf("owner has %d contacts, want the same Rahim", n)
	}
	_, theirs := call(t, requester, "GET", srv.URL+"/api/contacts", nil)
	tc := theirs["contacts"].([]any)
	if len(tc) != 1 || tc[0].(map[string]any)["name"] != "Masiur" {
		t.Fatalf("owner not added to the requester's contacts: %v", theirs)
	}

	// Mark as sent without approving first; the requester gets a borrowed copy.
	_, reqs := call(t, c, "GET", srv.URL+"/api/agreements?status=requested", nil)
	id := num(reqs["agreements"].([]any)[0].(map[string]any)["id"])
	if code, _ := call(t, c, "POST", fmt.Sprintf("%s/api/agreements/%d/mark-sent", srv.URL, id), map[string]any{"method": "cash", "sent_on": today.String()}); code != 204 {
		t.Fatalf("mark sent: %d", code)
	}
	_, copies := call(t, requester, "GET", srv.URL+"/api/agreements", nil)
	cp := copies["agreements"].([]any)
	if len(cp) != 1 || cp[0].(map[string]any)["direction"] != "borrowed" || num(cp[0].(map[string]any)["principal"]) != 3000_00 || cp[0].(map[string]any)["contact_name"] != "Masiur" {
		t.Fatalf("borrower copy: %v", copies)
	}
}

func TestRequestLinkLimitsBlockDeclineExpiry(t *testing.T) {
	srv, pool := newTestEnv(t, nil)
	c := ownerClient(t, srv.URL)
	today := ledger.Today("Asia/Dhaka")
	token := tokenOf(createLinkFor(t, c, srv.URL, map[string]any{}))
	ask := func(email string) (int, map[string]any) {
		return call(t, newClient(t), "POST", srv.URL+"/api/public/ask/"+token, askBody("Karim", email, 1000_00, today.AddDays(20)))
	}
	requestID := func(email string) int64 {
		var id int64
		pool.QueryRow(context.Background(), `SELECT a.id FROM agreements a JOIN contacts c ON c.id = a.contact_id
			WHERE c.email = $1 ORDER BY a.id DESC LIMIT 1`, email).Scan(&id)
		return id
	}

	for range 3 {
		if code, _ := ask("karim@example.com"); code != 201 {
			t.Fatalf("submit: %d", code)
		}
	}
	if code, body := ask("karim@example.com"); code != 429 || body["error"] != "too_many_requests" {
		t.Fatalf("4th from same email: %d %v", code, body)
	}

	// Decline with a note drops the payout details.
	karim := requestID("karim@example.com")
	if code, _ := call(t, c, "POST", fmt.Sprintf("%s/api/agreements/%d/decline", srv.URL, karim), map[string]any{"note": "Not this month"}); code != 204 {
		t.Fatalf("decline: %d", code)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM payout_details WHERE agreement_id = $1`, karim); n != 0 {
		t.Fatal("payout kept after decline")
	}

	// Expiry closes the request and the worker sweep deletes its payout details.
	if code, _ := ask("salma@example.com"); code != 201 {
		t.Fatalf("submit salma: %d", code)
	}
	salma := requestID("salma@example.com")
	pool.Exec(context.Background(), `UPDATE agreements SET request_expires_at = now() - interval '1 minute' WHERE id = $1`, salma)
	newWorker(pool, nil).RunOnce(context.Background())
	if _, a := call(t, c, "GET", fmt.Sprintf("%s/api/agreements/%d", srv.URL, salma), nil); a["status"] != "expired" || a["payout"] != nil {
		t.Fatalf("expired: %v %v", a["status"], a["payout"])
	}

	// Per-IP limit: the 6th submission within the hour is refused, valid or not.
	if code, _ := ask("someone@example.com"); code != 429 {
		t.Fatalf("6th submission from one IP: %d", code)
	}
}

func TestRequestLinkBlock(t *testing.T) {
	srv, pool := newTestEnv(t, nil)
	c := ownerClient(t, srv.URL)
	today := ledger.Today("Asia/Dhaka")
	token := tokenOf(createLinkFor(t, c, srv.URL, map[string]any{}))
	call(t, newClient(t), "POST", srv.URL+"/api/public/ask/"+token, askBody("Spammer", "spam@example.com", 1000_00, today.AddDays(20)))
	var id int64
	pool.QueryRow(context.Background(), `SELECT id FROM agreements WHERE origin = 'link'`).Scan(&id)

	if code, _ := call(t, c, "POST", fmt.Sprintf("%s/api/agreements/%d/block", srv.URL, id), nil); code != 204 {
		t.Fatalf("block: %d", code)
	}
	_, list := call(t, c, "GET", srv.URL+"/api/contacts", nil)
	if n := len(list["contacts"].([]any)); n != 0 {
		t.Fatalf("blocked requester still in contacts: %d", n)
	}
	if code, body := call(t, newClient(t), "POST", srv.URL+"/api/public/ask/"+token, askBody("Spammer", "SPAM@example.com", 1000_00, today.AddDays(20))); code != 422 || body["error"] != "cannot_request" {
		t.Fatalf("blocked email submits again: %d %v", code, body)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM outbox WHERE kind = 'link.declined'`); n != 0 {
		t.Fatal("blocking sent a decline email")
	}
}
