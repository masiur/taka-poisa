package httpapi

import (
	"strings"
	"testing"
)

// verifyLink returns the verification link from the last email sent to addr.
func verifyLink(t *testing.T, m *fakeMailer, addr string) string {
	t.Helper()
	msgs := m.all()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].To != addr {
			continue
		}
		_, rest, ok := strings.Cut(msgs[i].Text, "http://app.test")
		if !ok {
			break
		}
		return strings.Fields(rest)[0]
	}
	t.Fatalf("no verification email to %s in %v", addr, msgs)
	return ""
}

func TestSignup(t *testing.T) {
	mailer := &fakeMailer{}
	srv, _ := newTestEnvWith(t, func(c *Config) { c.Signup, c.Mailer = true, mailer })
	nina := map[string]any{"name": "Nina", "email": " Nina@Example.com ", "password": "nina-pass-1", "locale": "en"}

	if code, body := call(t, newClient(t), "POST", srv.URL+"/api/signup", nina); code != 409 || body["error"] != "setup_needed" {
		t.Fatalf("sign-up before setup: %d %v", code, body)
	}
	o := ownerClient(t, srv.URL)
	mustCreate(t, o, srv.URL+"/api/contacts", map[string]any{"name": "Owner's contact"})
	if _, body := call(t, newClient(t), "GET", srv.URL+"/api/setup", nil); body["signup"] != true {
		t.Fatalf("sign-up not advertised: %v", body)
	}

	c := newClient(t)
	if code, body := call(t, c, "POST", srv.URL+"/api/signup", nina); code != 204 {
		t.Fatalf("sign-up: %d %v", code, body)
	}
	_, me := call(t, c, "GET", srv.URL+"/api/me", nil)
	user := me["user"].(map[string]any)
	if user["email"] != "nina@example.com" || user["email_verified"] != false || me["email_enabled"] != true {
		t.Fatalf("me after sign-up: %v", me)
	}
	if _, acc := call(t, c, "GET", srv.URL+"/api/account", nil); acc["name"] != "Personal" {
		t.Fatalf("new ledger: %v", acc)
	}
	if _, list := call(t, c, "GET", srv.URL+"/api/contacts", nil); len(list["contacts"].([]any)) != 0 {
		t.Fatalf("sign-up sees another ledger's contacts: %v", list)
	}

	link := verifyLink(t, mailer, "nina@example.com")
	if loc := location(t, noRedirectClient(t), srv.URL+link); loc != "/?verified=1" {
		t.Fatalf("verify redirected to %q", loc)
	}
	if _, me := call(t, c, "GET", srv.URL+"/api/me", nil); me["user"].(map[string]any)["email_verified"] != true {
		t.Fatalf("not verified: %v", me)
	}
	if loc := location(t, noRedirectClient(t), srv.URL+link); loc != "/login?error=link_expired" {
		t.Fatalf("reused link redirected to %q", loc)
	}
	if code, _ := call(t, c, "POST", srv.URL+"/api/verify-email/resend", nil); code != 204 || len(mailer.all()) != 1 {
		t.Fatalf("resend when verified: %d, %d emails", code, len(mailer.all()))
	}

	nina["password"] = "another-pass"
	if code, body := call(t, newClient(t), "POST", srv.URL+"/api/signup", nina); code != 422 || body["fields"].(map[string]any)["email"] != "taken" {
		t.Fatalf("duplicate email: %d %v", code, body)
	}
	if code, body := call(t, newClient(t), "POST", srv.URL+"/api/signup", map[string]any{"name": "", "email": "x", "password": "short", "locale": "en"}); code != 422 || len(body["fields"].(map[string]any)) != 3 {
		t.Fatalf("invalid sign-up: %d %v", code, body)
	}
}

func TestSignupResendAndClosed(t *testing.T) {
	mailer := &fakeMailer{}
	srv, _ := newTestEnvWith(t, func(c *Config) { c.Signup, c.Mailer = true, mailer })
	ownerClient(t, srv.URL)
	c := newClient(t)
	call(t, c, "POST", srv.URL+"/api/signup", map[string]any{"name": "Raju", "email": "raju@example.com", "password": "raju-pass-1", "locale": "bn"})
	first := verifyLink(t, mailer, "raju@example.com")
	for i := range 3 {
		if code, _ := call(t, c, "POST", srv.URL+"/api/verify-email/resend", nil); code != 204 {
			t.Fatalf("resend %d: %d", i, code)
		}
	}
	if code, _ := call(t, c, "POST", srv.URL+"/api/verify-email/resend", nil); code != 429 {
		t.Fatalf("fourth resend: %d", code)
	}
	if msgs := mailer.all(); len(msgs) != 4 || !strings.Contains(msgs[3].Subject, "ইমেইল") {
		t.Fatalf("emails: %d %v", len(msgs), msgs[len(msgs)-1].Subject)
	}
	// Any unused link works; using one uses them all up.
	if loc := location(t, noRedirectClient(t), srv.URL+first); loc != "/?verified=1" {
		t.Fatalf("older link redirected to %q", loc)
	}
	if loc := location(t, noRedirectClient(t), srv.URL+verifyLink(t, mailer, "raju@example.com")); loc != "/login?error=link_expired" {
		t.Fatalf("newer link after verifying redirected to %q", loc)
	}

	closed := newTestServer(t)
	ownerClient(t, closed.URL)
	if code, body := call(t, newClient(t), "POST", closed.URL+"/api/signup", map[string]any{"name": "A", "email": "a@example.com", "password": "password1", "locale": "en"}); code != 403 || body["error"] != "signup_closed" {
		t.Fatalf("closed sign-up: %d %v", code, body)
	}
	if _, body := call(t, newClient(t), "GET", closed.URL+"/api/setup", nil); body["signup"] != false {
		t.Fatalf("closed sign-up advertised: %v", body)
	}
}

// Someone signs up with another person's email and a password they know. When the real owner of
// the email signs in through the outside provider, the squatter loses access.
func TestOutsideSignInClaimsUnverifiedSignup(t *testing.T) {
	f := newFakeProvider()
	srv, _ := newTestEnvWith(t, func(c *Config) { c.Signup, c.Identity = true, f })
	ownerClient(t, srv.URL)
	victim := "victim@example.com"
	squatter := newClient(t)
	if code, body := call(t, squatter, "POST", srv.URL+"/api/signup", map[string]any{"name": "Squatter", "email": victim, "password": "squat-pass-1", "locale": "en"}); code != 204 {
		t.Fatalf("squat sign-up: %d %v", code, body)
	}

	f.willSignIn("v", googleLike("s-v", victim, true))
	c := noRedirectClient(t)
	if loc := signIn(t, c, srv.URL, f, "mode=login", "v"); loc != "/" {
		t.Fatalf("victim sign-in redirected to %q", loc)
	}
	if _, me := call(t, c, "GET", srv.URL+"/api/me", nil); me["user"].(map[string]any)["email_verified"] != true {
		t.Fatalf("victim me: %v", me)
	}
	if code, _ := call(t, squatter, "GET", srv.URL+"/api/me", nil); code != 401 {
		t.Fatalf("squatter session survived: %d", code)
	}
	if code, _ := call(t, newClient(t), "POST", srv.URL+"/api/login", map[string]any{"email": victim, "password": "squat-pass-1"}); code != 401 {
		t.Fatalf("squatter password still works: %d", code)
	}
}
