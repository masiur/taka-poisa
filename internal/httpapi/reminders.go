package httpapi

import (
	"errors"
	"net/http"

	"github.com/masiur/taka-poisa/internal/auth"
	"github.com/masiur/taka-poisa/internal/ledger"
	"github.com/masiur/taka-poisa/internal/validate"
)

func (s *server) reminderRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agreements/{id}/reminders", s.requireAuth(s.getReminders))
	mux.HandleFunc("PUT /api/agreements/{id}/reminders", s.requireAuth(s.putReminders))
	mux.HandleFunc("POST /api/agreements/{id}/reminders/send-now", s.requireAuth(s.sendNow))
	mux.HandleFunc("POST /api/agreements/{id}/payments/{pid}/confirm", s.requireAuth(s.resolveClaim(true)))
	mux.HandleFunc("POST /api/agreements/{id}/payments/{pid}/reject", s.requireAuth(s.resolveClaim(false)))
	mux.HandleFunc("POST /api/contacts/{id}/statement-link", s.requireAuth(s.statementLink))
	mux.HandleFunc("GET /api/account/reminder-defaults", s.requireAuth(s.getReminderDefaults))
	mux.HandleFunc("PUT /api/account/reminder-defaults", s.requireAuth(s.putReminderDefaults))

	// Public: reached from reminder emails by people without an account. The secret token is the
	// only credential, so these never reveal anything beyond that contact's own loans.
	mux.HandleFunc("GET /api/public/statement/{token}", s.publicStatement)
	mux.HandleFunc("POST /api/public/statement/{token}/claims", s.publicClaim)
	mux.HandleFunc("POST /api/public/statement/{token}/optout", s.publicOptOut)
	mux.HandleFunc("POST /api/public/unsubscribe/{token}", s.publicOptOut) // RFC 8058 one-click (email)
}

func (s *server) getReminders(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	v, err := s.ledger.Reminders(r.Context(), p.AccountID, id)
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reminders": v, "email_enabled": s.emailEnabled, "sms_enabled": s.smsEnabled})
}

func decodeRules(w http.ResponseWriter, r *http.Request) ([]ledger.Rule, bool) {
	var in struct {
		Rules []ledger.Rule `json:"rules"`
	}
	if !decode(w, r, &in) {
		return nil, false
	}
	if in.Rules == nil {
		in.Rules = []ledger.Rule{}
	}
	if errs := ledger.ValidateRules(in.Rules); len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", errs)
		return nil, false
	}
	return in.Rules, true
}

func (s *server) putReminders(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	rules, ok := decodeRules(w, r)
	if !ok {
		return
	}
	if err := s.ledger.SetRules(r.Context(), p.AccountID, p.UserID, id, rules); err != nil {
		s.ledgerError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) sendNow(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	var in struct {
		Recipient string `json:"recipient"`
		Channel   string `json:"channel"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Channel == "" {
		in.Channel = "email"
	}
	if (in.Recipient != "contact" && in.Recipient != "self") || (in.Channel != "email" && in.Channel != "sms") {
		writeError(w, http.StatusUnprocessableEntity, "validation", validate.Errors{"recipient": validate.Invalid})
		return
	}
	if err := s.ledger.SendNow(r.Context(), p.AccountID, p.UserID, id, in.Recipient, in.Channel); err != nil {
		s.ledgerError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *server) resolveClaim(confirm bool) func(http.ResponseWriter, *http.Request, auth.Principal) {
	return func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		id, ok1 := pathID(r, "id")
		pid, ok2 := pathID(r, "pid")
		if !ok1 || !ok2 {
			writeError(w, http.StatusNotFound, "not_found", nil)
			return
		}
		if err := s.ledger.ResolveClaim(r.Context(), p.AccountID, p.UserID, id, pid, confirm); err != nil {
			s.ledgerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *server) statementLink(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	token, err := s.ledger.CreateStatementLink(r.Context(), p.AccountID, id)
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"url": s.baseURL + "/s/" + token})
}

func (s *server) getReminderDefaults(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	rules, err := s.ledger.DefaultRulesFor(r.Context(), p.AccountID)
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules, "sms_enabled": s.smsEnabled})
}

func (s *server) putReminderDefaults(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if p.Role != "owner" {
		writeError(w, http.StatusForbidden, "forbidden", nil)
		return
	}
	rules, ok := decodeRules(w, r)
	if !ok {
		return
	}
	if err := s.ledger.SetDefaultRules(r.Context(), p.AccountID, rules); err != nil {
		s.ledgerError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) publicStatement(w http.ResponseWriter, r *http.Request) {
	st, err := s.ledger.Statement(r.Context(), r.PathValue("token"))
	if errors.Is(err, ledger.ErrNotFound) {
		writeError(w, http.StatusNotFound, "link_expired", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *server) publicClaim(w http.ResponseWriter, r *http.Request) {
	var in ledger.ClaimInput
	if !decode(w, r, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", errs)
		return
	}
	err := s.ledger.SubmitClaim(r.Context(), r.PathValue("token"), in)
	if errors.Is(err, ledger.ErrNotFound) {
		writeError(w, http.StatusNotFound, "link_expired", nil)
		return
	}
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *server) publicOptOut(w http.ResponseWriter, r *http.Request) {
	// ?channel=sms stops SMS; anything else (including the email one-click) stops email.
	channel := "email"
	if r.URL.Query().Get("channel") == "sms" {
		channel = "sms"
	}
	err := s.ledger.OptOut(r.Context(), r.PathValue("token"), channel)
	if errors.Is(err, ledger.ErrNotFound) {
		writeError(w, http.StatusNotFound, "link_expired", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
