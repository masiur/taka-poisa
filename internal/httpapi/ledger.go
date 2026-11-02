package httpapi

import (
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"

	"github.com/masiur/taka-poisa/internal/auth"
	"github.com/masiur/taka-poisa/internal/ledger"
	"github.com/masiur/taka-poisa/internal/validate"
)

func (s *server) ledgerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/overview", s.requireAuth(s.overview))
	mux.HandleFunc("GET /api/contacts", s.requireAuth(s.listContacts))
	mux.HandleFunc("POST /api/contacts", s.requireAuth(s.createContact))
	mux.HandleFunc("GET /api/contacts/{id}", s.requireAuth(s.getContact))
	mux.HandleFunc("PUT /api/contacts/{id}", s.requireAuth(s.updateContact))
	mux.HandleFunc("DELETE /api/contacts/{id}", s.requireAuth(s.deleteContact))
	mux.HandleFunc("POST /api/schedule/preview", s.requireAuth(s.previewSchedule))
	mux.HandleFunc("GET /api/agreements", s.requireAuth(s.listAgreements))
	mux.HandleFunc("POST /api/agreements", s.requireAuth(s.createAgreement))
	mux.HandleFunc("GET /api/agreements/{id}", s.requireAuth(s.getAgreement))
	mux.HandleFunc("PUT /api/agreements/{id}", s.requireAuth(s.updateAgreement))
	mux.HandleFunc("POST /api/agreements/{id}/cancel", s.requireAuth(s.cancelAgreement))
	mux.HandleFunc("POST /api/agreements/{id}/payments", s.requireAuth(s.addPayment))
	mux.HandleFunc("DELETE /api/agreements/{id}/payments/{pid}", s.requireAuth(s.deletePayment))
	mux.HandleFunc("GET /api/export/{kind}", s.requireAuth(s.export))
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

// ledgerError maps domain errors to responses; anything else is a 500.
func (s *server) ledgerError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ledger.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", nil)
	case errors.Is(err, ledger.ErrHasOpenLoans):
		writeError(w, http.StatusConflict, "has_open_loans", nil)
	case errors.Is(err, ledger.ErrNotActive):
		writeError(w, http.StatusConflict, "not_active", nil)
	case errors.Is(err, ledger.ErrExceedsBalance):
		writeError(w, http.StatusUnprocessableEntity, "validation", validate.Errors{"amount": "exceeds_balance"})
	case errors.Is(err, ledger.ErrAlreadySentToday):
		writeError(w, http.StatusConflict, "already_sent_today", nil)
	case errors.Is(err, ledger.ErrNotEligible):
		writeError(w, http.StatusUnprocessableEntity, "not_eligible", nil)
	case errors.Is(err, ledger.ErrTooManyClaims):
		writeError(w, http.StatusTooManyRequests, "rate_limited", nil)
	case errors.Is(err, ledger.ErrBelowPaid):
		writeError(w, http.StatusUnprocessableEntity, "validation", validate.Errors{"principal": "below_paid"})
	default:
		s.internalError(w, r, err)
	}
}

func (s *server) overview(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	today := ledger.Today(p.Timezone)
	o, err := s.ledger.Overview(r.Context(), p.AccountID, today)
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		ledger.Overview
		Currency string      `json:"currency"`
		Today    ledger.Date `json:"today"` // the account's today, for due/overdue labels
	}{o, p.Currency, today})
}

func (s *server) listContacts(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	list, err := s.ledger.ListContacts(r.Context(), p.AccountID, ledger.Today(p.Timezone), r.URL.Query().Get("q"))
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contacts": list, "currency": p.Currency})
}

func (s *server) createContact(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var in ledger.ContactInput
	if !decode(w, r, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", errs)
		return
	}
	id, err := s.ledger.CreateContact(r.Context(), p.AccountID, p.UserID, in)
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *server) getContact(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	c, err := s.ledger.GetContact(r.Context(), p.AccountID, id)
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	agreements, err := s.ledger.ListAgreements(r.Context(), p.AccountID, ledger.Today(p.Timezone), ledger.AgreementFilter{ContactID: id})
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contact": c, "agreements": agreements, "currency": p.Currency})
}

func (s *server) updateContact(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	var in ledger.ContactInput
	if !decode(w, r, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", errs)
		return
	}
	if err := s.ledger.UpdateContact(r.Context(), p.AccountID, id, in); err != nil {
		s.ledgerError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) deleteContact(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	if err := s.ledger.DeleteContact(r.Context(), p.AccountID, id); err != nil {
		s.ledgerError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// previewSchedule returns the installments a plan would create, so the UI shows exactly
// what the server will store without re-implementing the maths.
func (s *server) previewSchedule(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var in struct {
		Principal int64                `json:"principal"`
		Schedule  ledger.ScheduleInput `json:"schedule"`
	}
	if !decode(w, r, &in) {
		return
	}
	errs := validate.Errors{}
	if in.Principal <= 0 || in.Principal > ledger.MaxAmount {
		errs.Add("principal", validate.Invalid)
	}
	var items []ledger.InstallmentInput
	if len(errs) == 0 {
		items = in.Schedule.Build(in.Principal, errs)
	}
	if len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", errs)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"installments": items})
}

func (s *server) listAgreements(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	q := r.URL.Query()
	f := ledger.AgreementFilter{Status: q.Get("status")}
	if v := q.Get("contact_id"); v != "" {
		f.ContactID, _ = strconv.ParseInt(v, 10, 64)
	}
	list, err := s.ledger.ListAgreements(r.Context(), p.AccountID, ledger.Today(p.Timezone), f)
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agreements": list, "currency": p.Currency})
}

func (s *server) createAgreement(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var in ledger.AgreementInput
	if !decode(w, r, &in) {
		return
	}
	in.Normalize()
	items, errs := in.Build()
	if len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", errs)
		return
	}
	id, err := s.ledger.CreateAgreement(r.Context(), p.AccountID, p.UserID, p.Currency, in, items)
	if errors.Is(err, ledger.ErrNotFound) {
		writeError(w, http.StatusUnprocessableEntity, "validation", validate.Errors{"contact_id": validate.Invalid})
		return
	}
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *server) getAgreement(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	d, err := s.ledger.GetAgreement(r.Context(), p.AccountID, id, ledger.Today(p.Timezone))
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *server) updateAgreement(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	var t ledger.Terms
	if !decode(w, r, &t) {
		return
	}
	t.Normalize()
	errs := validate.Errors{}
	items := t.Build(errs)
	if len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", errs)
		return
	}
	if err := s.ledger.UpdateTerms(r.Context(), p.AccountID, p.UserID, id, t, items); err != nil {
		s.ledgerError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) cancelAgreement(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	if err := s.ledger.CancelAgreement(r.Context(), p.AccountID, p.UserID, id); err != nil {
		s.ledgerError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) addPayment(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	var in ledger.PaymentInput
	if !decode(w, r, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", errs)
		return
	}
	pid, err := s.ledger.AddPayment(r.Context(), p.AccountID, p.UserID, id, in)
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": pid})
}

func (s *server) deletePayment(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok1 := pathID(r, "id")
	pid, ok2 := pathID(r, "pid")
	if !ok1 || !ok2 {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	if err := s.ledger.DeletePayment(r.Context(), p.AccountID, p.UserID, id, pid); err != nil {
		s.ledgerError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) export(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	kind := r.PathValue("kind")
	if _, ok := ledger.ExportKinds[kind]; !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="taka-poisa-`+kind+`.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	// UTF-8 BOM so Excel shows Bangla text correctly.
	w.Write([]byte("\xef\xbb\xbf"))
	if err := s.ledger.Export(r.Context(), p.AccountID, kind, csv.NewWriter(w)); err != nil {
		// Headers are already sent; log only.
		s.log.Error("export failed", "kind", kind, "err", err)
	}
}
