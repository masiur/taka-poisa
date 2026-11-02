package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/masiur/taka-poisa/internal/auth"
	"github.com/masiur/taka-poisa/internal/reminders"
	"github.com/masiur/taka-poisa/internal/validate"
)

// postSignup creates a user with their own ledger when SIGNUP=open, signs them in and emails a
// verification link. The account works before verification; only features that trust an email
// match (incoming loan requests) wait for it.
func (s *server) postSignup(w http.ResponseWriter, r *http.Request) {
	if !s.signup {
		writeError(w, http.StatusForbidden, "signup_closed", nil)
		return
	}
	key := clientIP(r)
	if s.signupLimit.Blocked(key) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", nil)
		return
	}
	var in auth.OwnerInput
	if !decode(w, r, &in) {
		return
	}
	in.AccountName = "" // every sign-up starts with the default Personal ledger
	in.Normalize()
	if errs := in.Validate(); len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", errs)
		return
	}
	s.signupLimit.Fail(key) // counts attempts that reach the database
	userID, err := s.auth.Signup(r.Context(), in)
	switch {
	case errors.Is(err, auth.ErrEmailTaken):
		writeError(w, http.StatusUnprocessableEntity, "validation", validate.Errors{"email": "taken"})
		return
	case errors.Is(err, auth.ErrSetupNeeded):
		writeError(w, http.StatusConflict, "setup_needed", nil)
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	s.sendVerification(r.Context(), userID)
	s.startSession(w, r, userID)
}

// sendVerification emails a verification link. Failures are logged, not returned: the account
// works without it and the user can ask for another link.
func (s *server) sendVerification(ctx context.Context, userID int64) bool {
	if s.mailer == nil {
		return false
	}
	v, ok, err := s.auth.CreateVerification(ctx, userID)
	if err != nil || !ok {
		if err != nil {
			s.log.Error("create verification", "user", userID, "err", err)
		}
		return false
	}
	link := s.baseURL + "/api/verify-email?token=" + url.QueryEscape(v.Token)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := s.mailer.Send(ctx, reminders.ComposeVerification(v.Email, v.Name, v.Locale, link)); err != nil {
		s.log.Error("send verification email", "user", userID, "err", err)
		return false
	}
	return true
}

func (s *server) resendVerification(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if p.EmailVerified {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if s.mailer == nil {
		writeError(w, http.StatusConflict, "email_disabled", nil)
		return
	}
	key := "u" + p.Email
	if s.verifyLimit.Blocked(key) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", nil)
		return
	}
	s.verifyLimit.Fail(key)
	if !s.sendVerification(r.Context(), p.UserID) {
		writeError(w, http.StatusBadGateway, "send_failed", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// verifyEmail is the link in the verification email. It lands on Home, or on the login page
// with an error when the link is no good.
func (s *server) verifyEmail(w http.ResponseWriter, r *http.Request) {
	err := s.auth.VerifyEmail(r.Context(), r.URL.Query().Get("token"))
	if errors.Is(err, auth.ErrBadVerification) {
		http.Redirect(w, r, "/login?error=link_expired", http.StatusSeeOther)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	http.Redirect(w, r, "/?verified=1", http.StatusSeeOther)
}
