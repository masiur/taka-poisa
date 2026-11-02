package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/masiur/taka-poisa/internal/auth"
	"github.com/masiur/taka-poisa/internal/ledger"
)

// ownerClient sets the server up and returns a logged-in client.
func ownerClient(t *testing.T, url string) *http.Client {
	t.Helper()
	c := newClient(t)
	if code, body := call(t, c, "POST", url+"/api/setup", owner); code != 204 {
		t.Fatalf("setup: %d %v", code, body)
	}
	return c
}

func mustCreate(t *testing.T, c *http.Client, url string, body any) int64 {
	t.Helper()
	code, res := call(t, c, "POST", url, body)
	if code != 201 {
		t.Fatalf("POST %s: %d %v", url, code, res)
	}
	return int64(res["id"].(float64))
}

func num(v any) int64 { return int64(v.(float64)) }

func monthlyLoan(contactID int64, principal int64, count int, firstDue ledger.Date) map[string]any {
	return map[string]any{
		"contact_id": contactID, "direction": "lent", "principal": principal,
		"started_on": firstDue.AddDays(-30).String(), "purpose": "Shop stock",
		"schedule": map[string]any{"mode": "generate", "count": count, "first_due": firstDue.String(), "frequency": "monthly"},
	}
}

func TestLoanLifecycle(t *testing.T) {
	srv := newTestServer(t)
	c := ownerClient(t, srv.URL)
	today := ledger.Today("Asia/Dhaka")

	rahim := mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": " Rahim ", "phone": "01712-345678", "email": "Rahim@Example.com"})
	_, body := call(t, c, "GET", fmt.Sprintf("%s/api/contacts/%d", srv.URL, rahim), nil)
	contact := body["contact"].(map[string]any)
	if contact["name"] != "Rahim" || contact["phone"] != "+8801712345678" || contact["email"] != "rahim@example.com" {
		t.Fatalf("contact normalization: %v", contact)
	}

	// 30,000 taka in 3 monthly installments, the first one 45 days ago (overdue).
	loan := mustCreate(t, c, srv.URL+"/api/agreements", monthlyLoan(rahim, 30000_00, 3, today.AddDays(-45)))
	loanURL := fmt.Sprintf("%s/api/agreements/%d", srv.URL, loan)

	_, a := call(t, c, "GET", loanURL, nil)
	inst := a["installments"].([]any)
	if len(inst) != 3 || num(a["overdue"]) < 10000_00 || a["status"] != "active" {
		t.Fatalf("new loan: %v", a)
	}
	if first := inst[0].(map[string]any); first["status"] != "overdue" || num(first["remaining"]) != 10000_00 {
		t.Fatalf("first installment: %v", first)
	}

	// Partial payment covers the first installment and half the second.
	pay := func(amount int64) (int, map[string]any) {
		return call(t, c, "POST", loanURL+"/payments", map[string]any{
			"amount": amount, "paid_on": today.String(), "method": "bkash", "note": "TrxID 9X8Y",
		})
	}
	code, _ := pay(15000_00)
	if code != 201 {
		t.Fatalf("payment: %d", code)
	}
	_, a = call(t, c, "GET", loanURL, nil)
	inst = a["installments"].([]any)
	if num(a["paid"]) != 15000_00 || num(a["remaining"]) != 15000_00 || inst[0].(map[string]any)["status"] != "paid" ||
		num(inst[1].(map[string]any)["paid"]) != 5000_00 {
		t.Fatalf("after partial payment: %v", a)
	}

	if code, body := pay(15000_01); code != 422 || body["fields"].(map[string]any)["amount"] != "exceeds_balance" {
		t.Fatalf("overpayment: %d %v", code, body)
	}
	if code, _ := pay(15000_00); code != 201 {
		t.Fatalf("final payment: %d", code)
	}
	_, a = call(t, c, "GET", loanURL, nil)
	if a["status"] != "settled" || num(a["remaining"]) != 0 || !hasActivity(a, "agreement.settled") {
		t.Fatalf("settled: %v", a)
	}
	if code, _ := pay(1); code != 409 {
		t.Fatalf("payment on settled loan: %d", code)
	}

	// Deleting a payment reopens the loan.
	lastPayment := num(a["payments"].([]any)[0].(map[string]any)["id"])
	if code, _ := call(t, c, "DELETE", fmt.Sprintf("%s/payments/%d", loanURL, lastPayment), nil); code != 204 {
		t.Fatalf("delete payment: %d", code)
	}
	_, a = call(t, c, "GET", loanURL, nil)
	if a["status"] != "active" || num(a["remaining"]) != 15000_00 || !hasActivity(a, "agreement.reopened") {
		t.Fatalf("reopened: %v", a)
	}

	// Overview and contact list agree with the loan.
	_, o := call(t, c, "GET", srv.URL+"/api/overview", nil)
	if num(o["receivable"]) != 15000_00 || num(o["payable"]) != 0 || num(o["open_loans"]) != 1 || o["currency"] != "BDT" {
		t.Fatalf("overview: %v", o)
	}
	_, list := call(t, c, "GET", srv.URL+"/api/contacts?q=rah", nil)
	contacts := list["contacts"].([]any)
	if len(contacts) != 1 || num(contacts[0].(map[string]any)["receivable"]) != 15000_00 {
		t.Fatalf("contact list: %v", list)
	}

	// Terms: principal can't go below what's paid; a valid change replaces the schedule.
	terms := map[string]any{"principal": 10000_00, "started_on": today.String(), "purpose": "",
		"schedule": map[string]any{"mode": "generate", "count": 1, "first_due": today.String()}}
	if code, body := call(t, c, "PUT", loanURL, terms); code != 422 || body["fields"].(map[string]any)["principal"] != "below_paid" {
		t.Fatalf("principal below paid: %d %v", code, body)
	}
	terms["principal"] = 20000_00
	terms["schedule"] = map[string]any{"mode": "custom", "items": []any{
		map[string]any{"due_on": today.AddDays(10).String(), "amount": 5000_00},
		map[string]any{"due_on": today.AddDays(-60).String(), "amount": 15000_00},
	}}
	if code, body := call(t, c, "PUT", loanURL, terms); code != 204 {
		t.Fatalf("update terms: %d %v", code, body)
	}
	_, a = call(t, c, "GET", loanURL, nil)
	if num(a["principal"]) != 20000_00 || len(a["installments"].([]any)) != 2 || num(a["remaining"]) != 5000_00 {
		t.Fatalf("after terms update: %v", a)
	}

	// A contact with an open loan can't be deleted; after cancelling, it can.
	contactURL := fmt.Sprintf("%s/api/contacts/%d", srv.URL, rahim)
	if code, _ := call(t, c, "DELETE", contactURL, nil); code != 409 {
		t.Fatalf("delete contact with open loan: %d", code)
	}
	if code, _ := call(t, c, "POST", loanURL+"/cancel", nil); code != 204 {
		t.Fatalf("cancel: %d", code)
	}
	if code, _ := call(t, c, "DELETE", contactURL, nil); code != 204 {
		t.Fatalf("delete contact: %d", code)
	}
	if code, _ := call(t, c, "GET", contactURL, nil); code != 404 {
		t.Fatalf("deleted contact still visible: %d", code)
	}
}

func hasActivity(agreement map[string]any, action string) bool {
	for _, a := range agreement["activity"].([]any) {
		if a.(map[string]any)["action"] == action {
			return true
		}
	}
	return false
}

func TestAgreementValidation(t *testing.T) {
	srv := newTestServer(t)
	c := ownerClient(t, srv.URL)
	today := ledger.Today("Asia/Dhaka")
	contact := mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": "Karim"})

	if code, body := call(t, c, "POST", srv.URL+"/api/contacts", map[string]any{"name": "", "phone": "12", "email": "x"}); code != 422 {
		t.Fatalf("bad contact: %d %v", code, body)
	}

	bad := monthlyLoan(contact, 0, 3, today)
	bad["direction"] = "gifted"
	code, body := call(t, c, "POST", srv.URL+"/api/agreements", bad)
	fields, _ := body["fields"].(map[string]any)
	if code != 422 || fields["principal"] == nil || fields["direction"] == nil {
		t.Fatalf("bad agreement: %d %v", code, body)
	}
	if code, body := call(t, c, "POST", srv.URL+"/api/agreements", monthlyLoan(999999, 100, 1, today)); code != 422 || body["fields"].(map[string]any)["contact_id"] != "invalid" {
		t.Fatalf("unknown contact: %d %v", code, body)
	}

	code, body = call(t, c, "POST", srv.URL+"/api/schedule/preview", map[string]any{
		"principal": 1000_01, "schedule": map[string]any{"mode": "generate", "count": 3, "first_due": "2027-01-31", "frequency": "monthly"},
	})
	items, _ := body["installments"].([]any)
	if code != 200 || len(items) != 3 || items[1].(map[string]any)["due_on"] != "2027-02-28" || num(items[2].(map[string]any)["amount"]) != 333_35 {
		t.Fatalf("preview: %d %v", code, body)
	}
}

func TestConcurrentPaymentsCannotOverpay(t *testing.T) {
	srv := newTestServer(t)
	c := ownerClient(t, srv.URL)
	today := ledger.Today("Asia/Dhaka")
	contact := mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": "Salma"})
	loan := mustCreate(t, c, srv.URL+"/api/agreements", monthlyLoan(contact, 1000_00, 1, today))

	var wg sync.WaitGroup
	codes := make(chan int, 5)
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := call(t, c, "POST", fmt.Sprintf("%s/api/agreements/%d/payments", srv.URL, loan), map[string]any{
				"amount": 1000_00, "paid_on": today.String(), "method": "cash",
			})
			codes <- code
		}()
	}
	wg.Wait()
	close(codes)
	ok := 0
	for code := range codes {
		if code == 201 {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("%d payments of the full amount accepted, want 1", ok)
	}
}

// createSecondAccount adds another account with its own owner, as on a hosted server.
func createSecondAccount(t *testing.T, pool *pgxpool.Pool) *http.Client {
	t.Helper()
	ctx := context.Background()
	hash, _ := auth.HashPassword("other-pass-1")
	var accountID, userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO accounts (name) VALUES ('Other') RETURNING id`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, name) VALUES ('other@example.com', $1, 'Other') RETURNING id`, hash).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO account_members (account_id, user_id, role) VALUES ($1, $2, 'owner')`, accountID, userID); err != nil {
		t.Fatal(err)
	}
	return newClient(t)
}

func TestAccountsAreIsolated(t *testing.T) {
	srv, pool := newTestEnv(t, nil)
	c := ownerClient(t, srv.URL)
	today := ledger.Today("Asia/Dhaka")
	contact := mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": "Secret Contact"})
	loan := mustCreate(t, c, srv.URL+"/api/agreements", monthlyLoan(contact, 500_00, 1, today))

	other := createSecondAccount(t, pool)
	if code, _ := call(t, other, "POST", srv.URL+"/api/login", map[string]any{"email": "other@example.com", "password": "other-pass-1"}); code != 204 {
		t.Fatalf("other login: %d", code)
	}
	checks := []struct {
		method, path string
		body         any
		want         int
	}{
		{"GET", fmt.Sprintf("/api/contacts/%d", contact), nil, 404},
		{"PUT", fmt.Sprintf("/api/contacts/%d", contact), map[string]any{"name": "Hijack"}, 404},
		{"DELETE", fmt.Sprintf("/api/contacts/%d", contact), nil, 404},
		{"GET", fmt.Sprintf("/api/agreements/%d", loan), nil, 404},
		{"POST", fmt.Sprintf("/api/agreements/%d/payments", loan), map[string]any{"amount": 1, "paid_on": today.String(), "method": "cash"}, 404},
		{"POST", fmt.Sprintf("/api/agreements/%d/cancel", loan), nil, 404},
		{"POST", "/api/agreements", monthlyLoan(contact, 100, 1, today), 422},
	}
	for _, ch := range checks {
		if code, body := call(t, other, ch.method, srv.URL+ch.path, ch.body); code != ch.want {
			t.Errorf("%s %s by other account: %d %v, want %d", ch.method, ch.path, code, body, ch.want)
		}
	}
	_, list := call(t, other, "GET", srv.URL+"/api/contacts", nil)
	if n := len(list["contacts"].([]any)); n != 0 {
		t.Errorf("other account sees %d contacts", n)
	}
	res, err := other.Get(srv.URL + "/api/export/contacts")
	if err != nil {
		t.Fatal(err)
	}
	csvBody, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if strings.Contains(string(csvBody), "Secret Contact") {
		t.Error("other account's export contains our contact")
	}
}

func TestCurrencyLockedOnceLoansExist(t *testing.T) {
	srv := newTestServer(t)
	c := ownerClient(t, srv.URL)
	settings := map[string]any{"name": "My ledger", "currency": "USD", "timezone": "Asia/Dhaka", "send_hour": 9, "locale": "en"}
	if code, _ := call(t, c, "PUT", srv.URL+"/api/account", settings); code != 204 {
		t.Fatalf("currency change before loans: %d", code)
	}
	contact := mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": "Rahim"})
	mustCreate(t, c, srv.URL+"/api/agreements", monthlyLoan(contact, 100, 1, ledger.Today("Asia/Dhaka")))
	settings["currency"] = "BDT"
	if code, body := call(t, c, "PUT", srv.URL+"/api/account", settings); code != 422 || body["fields"].(map[string]any)["currency"] != "locked" {
		t.Fatalf("currency change with loans: %d %v", code, body)
	}
	settings["currency"] = "USD"
	settings["send_hour"] = 18
	if code, _ := call(t, c, "PUT", srv.URL+"/api/account", settings); code != 204 {
		t.Fatalf("other settings with loans: %d", code)
	}
}

func TestExportNeutralisesFormulas(t *testing.T) {
	srv := newTestServer(t)
	c := ownerClient(t, srv.URL)
	mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": `=HYPERLINK("http://evil","x")`, "notes": "রহিমের নোট"})
	res, err := c.Get(srv.URL + "/api/export/contacts")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	text := string(body)
	if res.StatusCode != 200 || !strings.HasPrefix(text, "\xef\xbb\xbfid,name,") || !strings.Contains(text, `"'=HYPERLINK(`) || !strings.Contains(text, "রহিমের নোট") {
		t.Fatalf("export: %d %q", res.StatusCode, text)
	}
	if code, _ := call(t, c, "GET", srv.URL+"/api/export/users", nil); code != 404 {
		t.Fatalf("unknown export kind: %d", code)
	}
}

func TestContactTags(t *testing.T) {
	srv := newTestServer(t)
	c := ownerClient(t, srv.URL)
	rahim := mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": "Rahim", "tags": []string{" Office  colleague ", "office colleague", "", "Family"}})
	mustCreate(t, c, srv.URL+"/api/contacts", map[string]any{"name": "Karim"})

	_, body := call(t, c, "GET", fmt.Sprintf("%s/api/contacts/%d", srv.URL, rahim), nil)
	if got := fmt.Sprint(body["contact"].(map[string]any)["tags"]); got != "[Office colleague Family]" {
		t.Fatalf("tags not normalised: %s", got)
	}
	_, list := call(t, c, "GET", srv.URL+"/api/contacts?tag=Family", nil)
	if contacts := list["contacts"].([]any); len(contacts) != 1 {
		t.Fatalf("tag filter: %v", list)
	}
	if _, tags := call(t, c, "GET", srv.URL+"/api/contacts/tags", nil); fmt.Sprint(tags["tags"]) != "[Family Office colleague]" {
		t.Fatalf("tags: %v", tags)
	}

	tooMany := make([]string, 11)
	for i := range tooMany {
		tooMany[i] = fmt.Sprint("t", i)
	}
	if code, body := call(t, c, "PUT", fmt.Sprintf("%s/api/contacts/%d", srv.URL, rahim), map[string]any{"name": "Rahim", "tags": tooMany}); code != 422 || body["fields"].(map[string]any)["tags"] == nil {
		t.Fatalf("too many tags: %d %v", code, body)
	}
}

func TestSetupDefaultsLedgerName(t *testing.T) {
	srv := newTestServer(t)
	c := newClient(t)
	in := map[string]any{"name": "U", "email": "u@example.com", "password": "password1", "locale": "en"}
	if code, body := call(t, c, "POST", srv.URL+"/api/setup", in); code != 204 {
		t.Fatalf("setup without ledger name: %d %v", code, body)
	}
	if _, body := call(t, c, "GET", srv.URL+"/api/account", nil); body["name"] != "Personal" {
		t.Fatalf("ledger name: %v", body)
	}
}
