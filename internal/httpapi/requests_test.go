package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/masiur/taka-poisa/internal/auth"
	"github.com/masiur/taka-poisa/internal/ledger"
)

func loanRequest(contactID int64, principal int64, firstDue ledger.Date) map[string]any {
	return map[string]any{
		"contact_id": contactID, "principal": principal, "started_on": firstDue.AddDays(-30).String(),
		"purpose": "Shop rent", "message": "I'll pay back in two months, promise.", "expires_in_days": 14,
		"schedule": map[string]any{"mode": "generate", "count": 2, "first_due": firstDue.String(), "frequency": "monthly"},
	}
}

// tokenFrom extracts the /r/<token> from a request email.
func tokenFrom(t *testing.T, text string) string {
	t.Helper()
	i := strings.Index(text, "/r/")
	if i < 0 {
		t.Fatalf("no request link in %q", text)
	}
	tok := text[i+3:]
	if j := strings.IndexAny(tok, "?\n \""); j >= 0 {
		tok = tok[:j]
	}
	return tok
}

func TestLoanRequestByEmailLink(t *testing.T) {
	srv, pool := newTestEnv(t, nil)
	c := ownerClient(t, srv.URL) // the borrower: Masiur
	today := ledger.Today("Asia/Dhaka")
	lender := mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": "Karim", "email": "karim@example.com", "locale": "en"})
	mailer := &fakeMailer{}
	w := newWorker(pool, mailer)

	req := mustCreate(t, c, srv.URL+"/api/requests", loanRequest(lender, 20000_00, today.AddDays(30)))
	reqURL := fmt.Sprintf("%s/api/agreements/%d", srv.URL, req)
	_, a := call(t, c, "GET", reqURL, nil)
	if a["status"] != "requested" || a["direction"] != "borrowed" || a["origin"] != "request" {
		t.Fatalf("request: %v", a)
	}
	if _, o := call(t, c, "GET", srv.URL+"/api/overview", nil); num(o["payable"]) != 0 {
		t.Fatalf("an unanswered request counts as owed: %v", o)
	}

	w.RunOnce(context.Background())
	sent := mailer.all()
	if len(sent) != 1 || sent[0].To != "karim@example.com" || sent[0].Subject != "Masiur asks to borrow ৳20,000" || sent[0].ReplyTo != "owner@example.com" {
		t.Fatalf("request email: %+v", sent)
	}
	token := tokenFrom(t, sent[0].Text)

	code, v := call(t, newClient(t), "GET", srv.URL+"/api/public/request/"+token, nil)
	if code != 200 || v["borrower_name"] != "Masiur" || v["status"] != "requested" || len(v["installments"].([]any)) != 2 ||
		!strings.Contains(v["message"].(string), "promise") {
		t.Fatalf("public request: %d %v", code, v)
	}

	if code, _ := call(t, newClient(t), "POST", srv.URL+"/api/public/request/"+token+"/accept", map[string]any{"note": "OK, pay on time"}); code != 204 {
		t.Fatalf("accept: %d", code)
	}
	if code, _ := call(t, newClient(t), "POST", srv.URL+"/api/public/request/"+token+"/decline", map[string]any{}); code != 409 {
		t.Fatalf("second answer: %d", code)
	}
	_, a = call(t, c, "GET", reqURL, nil)
	if a["status"] != "active" || a["decision_note"] != "OK, pay on time" || !hasActivity(a, "request.accepted") {
		t.Fatalf("after accept: %v", a)
	}
	if _, o := call(t, c, "GET", srv.URL+"/api/overview", nil); num(o["payable"]) != 20000_00 {
		t.Fatalf("accepted loan not owed: %v", o)
	}
	// The borrower's own reminders start; none go to the lender.
	_, rem := call(t, c, "GET", reqURL+"/reminders", nil)
	upcoming := rem["reminders"].(map[string]any)["upcoming"].([]any)
	if len(upcoming) == 0 {
		t.Fatal("no reminders planned after accept")
	}
	for _, u := range upcoming {
		if u.(map[string]any)["recipient"] != "self" {
			t.Fatalf("borrowed loan reminds the lender: %v", u)
		}
	}

	w.RunOnce(context.Background())
	sent = mailer.all()
	if last := sent[len(sent)-1]; last.To != "owner@example.com" || last.Subject != "Karim accepted your request for ৳20,000" || !strings.Contains(last.Text, "OK, pay on time") {
		t.Fatalf("decision email: %+v", last)
	}

	// Decline, withdraw and expiry close requests too.
	declined := mustCreate(t, c, srv.URL+"/api/requests", loanRequest(lender, 500_00, today.AddDays(30)))
	w.RunOnce(context.Background())
	tok2 := tokenFrom(t, mailer.all()[len(mailer.all())-1].Text)
	call(t, newClient(t), "POST", srv.URL+"/api/public/request/"+tok2+"/decline", map[string]any{"note": "Not now"})
	if _, a := call(t, c, "GET", fmt.Sprintf("%s/api/agreements/%d", srv.URL, declined), nil); a["status"] != "declined" {
		t.Fatalf("declined: %v", a["status"])
	}

	other := mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": "Salma", "email": "salma@example.com"})
	withdrawn := mustCreate(t, c, srv.URL+"/api/requests", loanRequest(other, 100_00, today.AddDays(10)))
	if code, _ := call(t, c, "POST", fmt.Sprintf("%s/api/agreements/%d/withdraw", srv.URL, withdrawn), nil); code != 204 {
		t.Fatalf("withdraw: %d", code)
	}
	if code, _ := call(t, c, "POST", fmt.Sprintf("%s/api/agreements/%d/withdraw", srv.URL, withdrawn), nil); code != 409 {
		t.Fatalf("withdraw twice: %d", code)
	}
	w.RunOnce(context.Background()) // the queued email to Salma is skipped: request closed
	for _, m := range mailer.all() {
		if m.To == "salma@example.com" {
			t.Fatal("email sent for a withdrawn request")
		}
	}

	expiring := mustCreate(t, c, srv.URL+"/api/requests", loanRequest(other, 100_00, today.AddDays(10)))
	pool.Exec(context.Background(), `UPDATE agreements SET request_expires_at = now() - interval '1 minute' WHERE id = $1`, expiring)
	w.RunOnce(context.Background())
	if _, a := call(t, c, "GET", fmt.Sprintf("%s/api/agreements/%d", srv.URL, expiring), nil); a["status"] != "expired" {
		t.Fatalf("expired: %v", a["status"])
	}
}

func TestLoanRequestLimitsAndBlock(t *testing.T) {
	srv, pool := newTestEnv(t, nil)
	c := ownerClient(t, srv.URL)
	today := ledger.Today("Asia/Dhaka")
	lender := mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": "Karim", "email": "karim@example.com"})
	noEmail := mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": "No Email"})

	if code, body := call(t, c, "POST", srv.URL+"/api/requests", loanRequest(noEmail, 100_00, today.AddDays(5))); code != 422 || body["error"] != "cannot_request" {
		t.Fatalf("request to contact without email: %d %v", code, body)
	}
	bad := loanRequest(lender, 100_00, today.AddDays(5))
	bad["expires_in_days"] = 90
	if code, _ := call(t, c, "POST", srv.URL+"/api/requests", bad); code != 422 {
		t.Fatalf("90-day expiry accepted: %d", code)
	}

	for i := range 3 {
		mustCreate(t, c, srv.URL+"/api/requests", loanRequest(lender, int64(100_00*(i+1)), today.AddDays(5)))
	}
	if code, body := call(t, c, "POST", srv.URL+"/api/requests", loanRequest(lender, 100_00, today.AddDays(5))); code != 429 || body["error"] != "too_many_requests" {
		t.Fatalf("4th request to the same lender in a week: %d %v", code, body)
	}

	// The lender blocks this account from the link; it also declines the open request.
	mailer := &fakeMailer{}
	newWorker(pool, mailer).RunOnce(context.Background())
	token := tokenFrom(t, mailer.all()[0].Text)
	if code, _ := call(t, newClient(t), "POST", srv.URL+"/api/public/request/"+token+"/block", nil); code != 204 {
		t.Fatalf("block: %d", code)
	}
	pool.Exec(context.Background(), `UPDATE agreements SET created_at = now() - interval '8 days' WHERE origin = 'request'`)
	if code, body := call(t, c, "POST", srv.URL+"/api/requests", loanRequest(lender, 100_00, today.AddDays(5))); code != 422 || body["error"] != "cannot_request" {
		t.Fatalf("request after block: %d %v", code, body)
	}
	if code, _ := call(t, newClient(t), "GET", srv.URL+"/api/public/request/garbage", nil); code != 404 {
		t.Fatalf("bad token: %d", code)
	}
}

// createUser adds an owner with their own account; verified marks their email as proven.
func createUser(t *testing.T, pool *pgxpool.Pool, url, email string, verified bool) *http.Client {
	t.Helper()
	ctx := context.Background()
	hash, _ := auth.HashPassword("user-pass-1")
	var accountID, userID int64
	pool.QueryRow(ctx, `INSERT INTO accounts (name) VALUES ('Lender book') RETURNING id`).Scan(&accountID)
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, name, email_verified_at)
		VALUES ($1, $2, 'Karim', CASE WHEN $3 THEN now() END) RETURNING id`, email, hash, verified).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	pool.Exec(ctx, `INSERT INTO account_members (account_id, user_id, role) VALUES ($1, $2, 'owner')`, accountID, userID)
	cl := newClient(t)
	if code, _ := call(t, cl, "POST", url+"/api/login", map[string]any{"email": email, "password": "user-pass-1"}); code != 204 {
		t.Fatalf("login %s: %d", email, code)
	}
	return cl
}

func TestIncomingRequestInApp(t *testing.T) {
	srv, pool := newTestEnv(t, nil)
	borrower := ownerClient(t, srv.URL)
	today := ledger.Today("Asia/Dhaka")
	lenderContact := mustCreate(t, borrower, srv.URL+"/api/contacts", map[string]any{"name": "Karim", "email": "karim@example.com"})
	req := mustCreate(t, borrower, srv.URL+"/api/requests", loanRequest(lenderContact, 8000_00, today.AddDays(20)))

	// Someone who merely signed up with the lender's email (not verified) sees nothing.
	impostor := createUser(t, pool, srv.URL, "karim@example.com", false)
	if _, body := call(t, impostor, "GET", srv.URL+"/api/requests/incoming", nil); len(body["requests"].([]any)) != 0 {
		t.Fatal("unverified email sees incoming requests")
	}
	if code, _ := call(t, impostor, "POST", fmt.Sprintf("%s/api/requests/incoming/%d/accept", srv.URL, req), map[string]any{}); code != 404 {
		t.Fatalf("unverified accept: %d", code)
	}
	pool.Exec(context.Background(), `DELETE FROM users WHERE email = 'karim@example.com'`)

	lender := createUser(t, pool, srv.URL, "karim@example.com", true)
	_, body := call(t, lender, "GET", srv.URL+"/api/requests/incoming", nil)
	list := body["requests"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["borrower_name"] != "Masiur" {
		t.Fatalf("incoming: %v", body)
	}
	code, res := call(t, lender, "POST", fmt.Sprintf("%s/api/requests/incoming/%d/accept", srv.URL, req), map[string]any{"note": ""})
	if code != 200 || num(res["agreement_id"]) == 0 {
		t.Fatalf("accept from inbox: %d %v", code, res)
	}
	copyID := num(res["agreement_id"])

	_, mine := call(t, lender, "GET", fmt.Sprintf("%s/api/agreements/%d", srv.URL, copyID), nil)
	if mine["direction"] != "lent" || num(mine["principal"]) != 8000_00 || mine["contact_name"] != "Masiur" || len(mine["installments"].([]any)) != 2 {
		t.Fatalf("lender's copy: %v", mine)
	}
	if _, a := call(t, borrower, "GET", fmt.Sprintf("%s/api/agreements/%d", srv.URL, req), nil); a["status"] != "active" {
		t.Fatalf("borrower's request after accept: %v", a["status"])
	}
	// The lender's account cannot read the borrower's agreement directly.
	if code, _ := call(t, lender, "GET", fmt.Sprintf("%s/api/agreements/%d", srv.URL, req), nil); code != 404 {
		t.Fatalf("lender reads borrower's agreement: %d", code)
	}
	if _, body := call(t, lender, "GET", srv.URL+"/api/requests/incoming", nil); len(body["requests"].([]any)) != 0 {
		t.Fatal("answered request still in the inbox")
	}
}
