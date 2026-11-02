package httpapi

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/masiur/taka-poisa/internal/auth"
)

const testClientID = "test-client.apps.googleusercontent.com"

// fakeGoogle is a minimal OIDC provider: a JWKS endpoint and a token endpoint that
// checks PKCE and returns an RS256-signed ID token with the claims the test sets.
type fakeGoogle struct {
	srv       *httptest.Server
	mu        sync.Mutex
	claims    map[string]any
	challenge string
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeGoogle{}
	mux := http.NewServeMux()
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		claims, challenge := f.claims, f.challenge
		f.mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if r.Form.Get("code") != "good-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}}, nil)
		if err != nil {
			t.Error(err)
			return
		}
		payload, _ := json.Marshal(claims)
		jws, err := signer.Sign(payload)
		if err != nil {
			t.Error(err)
			return
		}
		idToken, _ := jws.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": idToken,
		})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGoogle) client() *auth.Google {
	return auth.NewGoogle(testClientID, "secret", "http://app.test/api/auth/google/callback", auth.GoogleEndpoints{
		AuthURL:  f.srv.URL + "/auth",
		TokenURL: f.srv.URL + "/token",
		JWKSURL:  f.srv.URL + "/jwks",
		Issuers:  []string{"https://accounts.google.com", "accounts.google.com"},
	})
}

func googleClaims(sub, email string, verified bool) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss": "https://accounts.google.com", "aud": testClientID, "sub": sub,
		"email": email, "email_verified": verified, "name": "Google User",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}
}

// noRedirectClient keeps cookies but stops at each redirect so the test can follow the flow.
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

// googleFlow runs start → (Google) → callback and returns where the app redirected.
// claims get the nonce from the auth request unless they already carry one.
func googleFlow(t *testing.T, c *http.Client, app string, f *fakeGoogle, startQuery string, claims map[string]any) string {
	t.Helper()
	authURL, err := url.Parse(location(t, c, app+"/api/auth/google/start?"+startQuery))
	if err != nil || !strings.HasPrefix(authURL.String(), f.srv.URL+"/auth") {
		t.Fatalf("start redirected to %q", authURL)
	}
	q := authURL.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("client_id") != testClientID {
		t.Fatalf("auth request missing PKCE or client id: %v", q)
	}
	if _, ok := claims["nonce"]; !ok {
		claims["nonce"] = q.Get("nonce")
	}
	f.mu.Lock()
	f.claims, f.challenge = claims, q.Get("code_challenge")
	f.mu.Unlock()
	return location(t, c, app+"/api/auth/google/callback?code=good-code&state="+url.QueryEscape(q.Get("state")))
}

func TestGoogleSetupThenLogin(t *testing.T) {
	f := newFakeGoogle(t)
	srv := newTestServerWith(t, f.client())

	if _, body := call(t, newClient(t), "GET", srv.URL+"/api/setup", nil); body["google"] != true {
		t.Fatalf("google not advertised: %v", body)
	}

	c := noRedirectClient(t)
	if loc := googleFlow(t, c, srv.URL, f, "mode=setup&account_name=My+ledger&locale=bn", googleClaims("g-1", "Owner@Gmail.com", true)); loc != "/" {
		t.Fatalf("setup via google redirected to %q", loc)
	}
	code, body := call(t, c, "GET", srv.URL+"/api/me", nil)
	user, _ := body["user"].(map[string]any)
	if code != 200 || user["email"] != "owner@gmail.com" || user["name"] != "Google User" || user["locale"] != "bn" {
		t.Fatalf("me: %d %v", code, body)
	}

	// Same Google account signs in again from a fresh browser.
	c2 := noRedirectClient(t)
	if loc := googleFlow(t, c2, srv.URL, f, "mode=login", googleClaims("g-1", "owner@gmail.com", true)); loc != "/" {
		t.Fatalf("login redirected to %q", loc)
	}
	if code, _ := call(t, c2, "GET", srv.URL+"/api/me", nil); code != 200 {
		t.Fatalf("me after google login: %d", code)
	}

	// A Google-only user has no password to log in with.
	if code, _ := call(t, newClient(t), "POST", srv.URL+"/api/login", map[string]any{"email": "owner@gmail.com", "password": ""}); code != 401 {
		t.Fatalf("password login for google-only user: %d", code)
	}

	// Setup cannot run twice through Google either.
	if loc := googleFlow(t, noRedirectClient(t), srv.URL, f, "mode=setup&account_name=X&locale=en", googleClaims("g-2", "other@gmail.com", true)); loc != "/setup?error=already_set_up" {
		t.Fatalf("second google setup redirected to %q", loc)
	}
}

func TestGoogleLinksExistingPasswordUser(t *testing.T) {
	f := newFakeGoogle(t)
	srv := newTestServerWith(t, f.client())
	call(t, newClient(t), "POST", srv.URL+"/api/setup", owner) // owner@example.com, password

	c := noRedirectClient(t)
	if loc := googleFlow(t, c, srv.URL, f, "mode=login", googleClaims("g-9", "owner@example.com", true)); loc != "/" {
		t.Fatalf("link by verified email redirected to %q", loc)
	}
	_, body := call(t, c, "GET", srv.URL+"/api/me", nil)
	if user, _ := body["user"].(map[string]any); user["name"] != "Masiur" {
		t.Fatalf("linked to wrong user: %v", body)
	}
	// Password login keeps working after linking.
	if code, _ := call(t, newClient(t), "POST", srv.URL+"/api/login", map[string]any{"email": "owner@example.com", "password": "s3cret-pass"}); code != 204 {
		t.Fatalf("password login after link: %d", code)
	}

	// An unverified Google email never links.
	if loc := googleFlow(t, noRedirectClient(t), srv.URL, f, "mode=login", googleClaims("g-10", "owner@example.com", false)); loc != "/login?error=google_unverified" {
		t.Fatalf("unverified email redirected to %q", loc)
	}
}

func TestGoogleRejections(t *testing.T) {
	f := newFakeGoogle(t)
	srv := newTestServerWith(t, f.client())
	call(t, newClient(t), "POST", srv.URL+"/api/setup", owner)

	if loc := googleFlow(t, noRedirectClient(t), srv.URL, f, "mode=login", googleClaims("g-20", "stranger@gmail.com", true)); loc != "/login?error=no_account" {
		t.Fatalf("unknown account redirected to %q", loc)
	}

	bad := googleClaims("g-21", "owner@example.com", true)
	bad["nonce"] = "not-the-nonce"
	if loc := googleFlow(t, noRedirectClient(t), srv.URL, f, "mode=login", bad); loc != "/login?error=google_failed" {
		t.Fatalf("nonce mismatch redirected to %q", loc)
	}

	wrongAud := googleClaims("g-22", "owner@example.com", true)
	wrongAud["aud"] = "someone-else"
	if loc := googleFlow(t, noRedirectClient(t), srv.URL, f, "mode=login", wrongAud); loc != "/login?error=google_failed" {
		t.Fatalf("wrong audience redirected to %q", loc)
	}

	// Callback with a forged state (login CSRF) is refused and creates no session.
	c := noRedirectClient(t)
	location(t, c, srv.URL+"/api/auth/google/start?mode=login")
	if loc := location(t, c, srv.URL+"/api/auth/google/callback?code=good-code&state=forged"); loc != "/login?error=google_failed" {
		t.Fatalf("forged state redirected to %q", loc)
	}
	if code, _ := call(t, c, "GET", srv.URL+"/api/me", nil); code != 401 {
		t.Fatalf("session after forged state: %d", code)
	}

	// User pressed cancel on Google's screen.
	c = noRedirectClient(t)
	authURL, _ := url.Parse(location(t, c, srv.URL+"/api/auth/google/start?mode=login"))
	if loc := location(t, c, srv.URL+"/api/auth/google/callback?error=access_denied&state="+url.QueryEscape(authURL.Query().Get("state"))); loc != "/login?error=google_cancelled" {
		t.Fatalf("cancel redirected to %q", loc)
	}
}

func TestGoogleSetupValidatesInput(t *testing.T) {
	f := newFakeGoogle(t)
	srv := newTestServerWith(t, f.client())
	if loc := location(t, noRedirectClient(t), srv.URL+"/api/auth/google/start?mode=setup&locale=xx"); loc != "/setup?error=invalid" {
		t.Fatalf("bad locale redirected to %q", loc)
	}
}

func TestGoogleDisabled(t *testing.T) {
	srv := newTestServer(t)
	if _, body := call(t, newClient(t), "GET", srv.URL+"/api/setup", nil); body["google"] != false {
		t.Fatalf("google advertised while disabled: %v", body)
	}
	if code, _ := call(t, newClient(t), "GET", srv.URL+"/api/auth/google/start", nil); code != 404 {
		t.Fatalf("start while disabled: %d", code)
	}
}
