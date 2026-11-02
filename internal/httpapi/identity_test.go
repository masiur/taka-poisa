package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"testing"

	"github.com/masiur/taka-poisa/internal/auth"
)

// fakeProvider is an IdentityProvider for tests. A code maps to the identity it signs in as;
// Exchange enforces what a real provider guarantees: the code is known, PKCE was sent, and the
// identity is bound to the nonce from the authorization request.
type fakeProvider struct {
	mu         sync.Mutex
	identities map[string]auth.ExternalIdentity // code -> identity
	nonces     map[string]string                // state -> nonce seen in AuthCodeURL
	lastNonce  string
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{identities: map[string]auth.ExternalIdentity{}, nonces: map[string]string{}}
}

func (f *fakeProvider) Name() string { return "testidp" }

func (f *fakeProvider) AuthCodeURL(state, nonce, verifier, redirect string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nonces[state] = nonce
	f.lastNonce = nonce
	q := url.Values{"state": {state}, "redirect_uri": {redirect}, "has_pkce": {map[bool]string{true: "1"}[verifier != ""]}}
	return "https://idp.test/auth?" + q.Encode()
}

func (f *fakeProvider) Exchange(_ context.Context, code, verifier, nonce, _ string) (auth.ExternalIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.identities[code]
	if !ok || verifier == "" || nonce != f.lastNonce {
		return auth.ExternalIdentity{}, errors.New("rejected by provider")
	}
	return id, nil
}

func (f *fakeProvider) willSignIn(code string, id auth.ExternalIdentity) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.identities[code] = id
}

func noRedirectClient(t *testing.T) *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func location(t *testing.T, c *http.Client, u string) string {
	t.Helper()
	res, err := c.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.Header.Get("Location")
}

// signIn runs start → provider → callback and returns where the app redirected.
func signIn(t *testing.T, c *http.Client, app string, f *fakeProvider, startQuery, code string) string {
	t.Helper()
	authURL, err := url.Parse(location(t, c, app+"/api/auth/testidp/start?"+startQuery))
	if err != nil || authURL.Host != "idp.test" {
		t.Fatalf("start redirected to %q", authURL)
	}
	q := authURL.Query()
	if q.Get("has_pkce") != "1" || q.Get("redirect_uri") != "http://app.test/api/auth/testidp/callback" {
		t.Fatalf("auth request: %v", q)
	}
	return location(t, c, app+"/api/auth/testidp/callback?code="+code+"&state="+url.QueryEscape(q.Get("state")))
}

var googleLike = func(sub, email string, verified bool) auth.ExternalIdentity {
	return auth.ExternalIdentity{Subject: sub, Email: email, EmailVerified: verified, Name: "Outside User"}
}

func TestOutsideSignInSetupThenLogin(t *testing.T) {
	f := newFakeProvider()
	srv := newTestServerWith(t, f)
	if _, body := call(t, newClient(t), "GET", srv.URL+"/api/setup", nil); body["sso"] != "testidp" {
		t.Fatalf("provider not advertised: %v", body)
	}

	f.willSignIn("c1", googleLike("s-1", "Owner@Mail.test", true))
	c := noRedirectClient(t)
	if loc := signIn(t, c, srv.URL, f, "mode=setup&account_name=My+ledger&locale=bn", "c1"); loc != "/" {
		t.Fatalf("setup redirected to %q", loc)
	}
	code, body := call(t, c, "GET", srv.URL+"/api/me", nil)
	user, _ := body["user"].(map[string]any)
	if code != 200 || user["email"] != "owner@mail.test" || user["name"] != "Outside User" || user["locale"] != "bn" {
		t.Fatalf("me: %d %v", code, body)
	}

	if loc := signIn(t, noRedirectClient(t), srv.URL, f, "mode=login", "c1"); loc != "/" {
		t.Fatalf("login redirected to %q", loc)
	}
	if code, _ := call(t, newClient(t), "POST", srv.URL+"/api/login", map[string]any{"email": "owner@mail.test", "password": ""}); code != 401 {
		t.Fatalf("password login for an outside-only user: %d", code)
	}
	f.willSignIn("c2", googleLike("s-2", "other@mail.test", true))
	if loc := signIn(t, noRedirectClient(t), srv.URL, f, "mode=setup&account_name=X&locale=en", "c2"); loc != "/setup?error=already_set_up" {
		t.Fatalf("second setup redirected to %q", loc)
	}
}

func TestOutsideSignInLinksAndRefuses(t *testing.T) {
	f := newFakeProvider()
	srv := newTestServerWith(t, f)
	call(t, newClient(t), "POST", srv.URL+"/api/setup", owner) // owner@example.com with a password

	f.willSignIn("link", googleLike("s-9", "owner@example.com", true))
	c := noRedirectClient(t)
	if loc := signIn(t, c, srv.URL, f, "mode=login", "link"); loc != "/" {
		t.Fatalf("link by verified email redirected to %q", loc)
	}
	if _, body := call(t, c, "GET", srv.URL+"/api/me", nil); body["user"].(map[string]any)["name"] != "Masiur" {
		t.Fatalf("linked to the wrong user: %v", body)
	}

	f.willSignIn("unverified", googleLike("s-10", "owner@example.com", false))
	f.willSignIn("stranger", googleLike("s-11", "stranger@example.com", true))
	cases := map[string]string{"unverified": "/login?error=sso_unverified", "stranger": "/login?error=no_account", "unknown-code": "/login?error=sso_failed"}
	for code, want := range cases {
		if loc := signIn(t, noRedirectClient(t), srv.URL, f, "mode=login", code); loc != want {
			t.Errorf("%s redirected to %q, want %q", code, loc, want)
		}
	}

	// Forged state (login CSRF) is refused and creates no session.
	c = noRedirectClient(t)
	location(t, c, srv.URL+"/api/auth/testidp/start?mode=login")
	if loc := location(t, c, srv.URL+"/api/auth/testidp/callback?code=link&state=forged"); loc != "/login?error=sso_failed" {
		t.Fatalf("forged state redirected to %q", loc)
	}
	if code, _ := call(t, c, "GET", srv.URL+"/api/me", nil); code != 401 {
		t.Fatalf("session after forged state: %d", code)
	}

	// Cancelled on the provider's screen.
	c = noRedirectClient(t)
	authURL, _ := url.Parse(location(t, c, srv.URL+"/api/auth/testidp/start?mode=login"))
	if loc := location(t, c, srv.URL+"/api/auth/testidp/callback?error=access_denied&state="+url.QueryEscape(authURL.Query().Get("state"))); loc != "/login?error=sso_cancelled" {
		t.Fatalf("cancel redirected to %q", loc)
	}

	if loc := location(t, noRedirectClient(t), srv.URL+"/api/auth/testidp/start?mode=setup&locale=fr"); loc != "/setup?error=invalid" {
		t.Fatalf("invalid language redirected to %q", loc)
	}
	if code, _ := call(t, newClient(t), "GET", srv.URL+"/api/auth/otheridp/start", nil); code != 404 {
		t.Fatalf("unknown provider: %d", code)
	}
}

func TestNoOutsideSignInByDefault(t *testing.T) {
	srv := newTestServer(t)
	if _, body := call(t, newClient(t), "GET", srv.URL+"/api/setup", nil); body["sso"] != "" {
		t.Fatalf("sign-in provider advertised on the open-source server: %v", body)
	}
	if code, _ := call(t, newClient(t), "GET", srv.URL+"/api/auth/google/start", nil); code != 404 {
		t.Fatalf("google start on the open-source server: %d", code)
	}
}
