package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"golang.org/x/oauth2"

	"github.com/takapoisa/takapoisa/internal/auth"
	"github.com/takapoisa/takapoisa/internal/validate"
)

const (
	oauthCookie     = "tp_oauth"
	oauthCookiePath = "/api/auth/google"
)

// oauthState lives in a short-lived HttpOnly cookie between start and callback.
// State guards against login CSRF, the nonce binds the ID token to this browser,
// and the PKCE verifier binds the code exchange.
type oauthState struct {
	State       string `json:"s"`
	Nonce       string `json:"n"`
	Verifier    string `json:"v"`
	Setup       bool   `json:"setup,omitempty"`
	AccountName string `json:"an,omitempty"`
	Locale      string `json:"l,omitempty"`
}

// googleStart sends the browser to Google. With mode=setup (first run only) it carries
// the ledger name and language chosen on the setup page.
func (s *server) googleStart(w http.ResponseWriter, r *http.Request) {
	if s.google == nil {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	q := r.URL.Query()
	st := oauthState{State: randomToken(), Nonce: randomToken(), Verifier: oauth2.GenerateVerifier()}
	if q.Get("mode") == "setup" {
		in := auth.OwnerInput{AccountName: q.Get("account_name"), Locale: q.Get("locale")}
		in.Normalize()
		errs := validate.Errors{}
		errs.Text("account_name", in.AccountName, 100)
		errs.Locale("locale", in.Locale)
		if len(errs) > 0 {
			http.Redirect(w, r, "/setup?error=invalid", http.StatusSeeOther)
			return
		}
		st.Setup, st.AccountName, st.Locale = true, in.AccountName, in.Locale
	}

	raw, _ := json.Marshal(st)
	http.SetCookie(w, &http.Cookie{
		Name:     oauthCookie,
		Value:    base64.RawURLEncoding.EncodeToString(raw),
		Path:     oauthCookiePath,
		MaxAge:   600,
		HttpOnly: true,
		Secure:   s.secureCookies,
		// Lax is sent on the top-level GET redirect back from Google.
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, s.google.AuthCodeURL(st.State, st.Nonce, st.Verifier), http.StatusFound)
}

func (s *server) googleCallback(w http.ResponseWriter, r *http.Request) {
	if s.google == nil {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	st, ok := readOAuthState(r)
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Path: oauthCookiePath, MaxAge: -1,
		HttpOnly: true, Secure: s.secureCookies, SameSite: http.SameSiteLaxMode,
	})

	back := "/login"
	if st.Setup {
		back = "/setup"
	}
	fail := func(code string) {
		http.Redirect(w, r, back+"?error="+url.QueryEscape(code), http.StatusSeeOther)
	}

	q := r.URL.Query()
	if !ok || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(st.State)) != 1 {
		fail("google_failed")
		return
	}
	if q.Get("error") != "" {
		fail("google_cancelled")
		return
	}

	claims, err := s.google.Exchange(r.Context(), q.Get("code"), st.Verifier, st.Nonce)
	if err != nil {
		s.log.Warn("google sign-in failed", "err", err)
		fail("google_failed")
		return
	}

	var setup *auth.OwnerInput
	if st.Setup {
		setup = &auth.OwnerInput{AccountName: st.AccountName, Locale: st.Locale}
	}
	userID, err := s.auth.GoogleSignIn(r.Context(), claims, setup)
	switch {
	case errors.Is(err, auth.ErrEmailUnverified):
		fail("google_unverified")
		return
	case errors.Is(err, auth.ErrNoAccount):
		fail("no_account")
		return
	case errors.Is(err, auth.ErrAlreadySetUp):
		fail("already_set_up")
		return
	case err != nil:
		s.log.Error("google sign-in failed", "err", err)
		fail("google_failed")
		return
	}

	if err := s.setSessionCookie(w, r, userID); err != nil {
		s.log.Error("google sign-in: create session", "err", err)
		fail("google_failed")
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func readOAuthState(r *http.Request) (oauthState, bool) {
	var st oauthState
	c, err := r.Cookie(oauthCookie)
	if err != nil {
		return st, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || json.Unmarshal(raw, &st) != nil || st.State == "" {
		return oauthState{}, false
	}
	return st, true
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
