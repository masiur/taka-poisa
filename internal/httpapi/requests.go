package httpapi

import (
	"errors"
	"net/http"

	"github.com/masiur/taka-poisa/internal/auth"
	"github.com/masiur/taka-poisa/internal/ledger"
	"github.com/masiur/taka-poisa/internal/validate"
)

func (s *server) requestRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/requests", s.requireAuth(s.createRequest))
	mux.HandleFunc("POST /api/agreements/{id}/withdraw", s.requireAuth(s.withdrawRequest))
	mux.HandleFunc("GET /api/requests/incoming", s.requireAuth(s.incomingRequests))
	mux.HandleFunc("POST /api/requests/incoming/{id}/accept", s.requireAuth(s.decideIncoming(true)))
	mux.HandleFunc("POST /api/requests/incoming/{id}/decline", s.requireAuth(s.decideIncoming(false)))

	// Public: the lender's secret link from the request email.
	mux.HandleFunc("GET /api/public/request/{token}", s.publicRequest)
	mux.HandleFunc("POST /api/public/request/{token}/accept", s.publicDecide(true))
	mux.HandleFunc("POST /api/public/request/{token}/decline", s.publicDecide(false))
	mux.HandleFunc("POST /api/public/request/{token}/block", s.publicBlock)
}

func (s *server) createRequest(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var in ledger.RequestInput
	if !decode(w, r, &in) {
		return
	}
	in.Normalize()
	items, errs := in.Build()
	if len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", errs)
		return
	}
	id, err := s.ledger.CreateRequest(r.Context(), p.AccountID, p.UserID, p.Currency, in, items)
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

func (s *server) withdrawRequest(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	if err := s.ledger.WithdrawRequest(r.Context(), p.AccountID, p.UserID, id); err != nil {
		s.ledgerError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) incomingRequests(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	list, err := s.ledger.Incoming(r.Context(), p.UserID, p.AccountID)
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": list})
}

type decisionInput struct {
	Note string `json:"note"`
}

func (s *server) decideIncoming(accept bool) func(http.ResponseWriter, *http.Request, auth.Principal) {
	return func(w http.ResponseWriter, r *http.Request, p auth.Principal) {
		id, ok := pathID(r, "id")
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", nil)
			return
		}
		var in decisionInput
		if !decode(w, r, &in) {
			return
		}
		copyID, err := s.ledger.DecideIncoming(r.Context(), p.UserID, p.AccountID, id, accept, in.Note)
		if err != nil {
			s.ledgerError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int64{"agreement_id": copyID})
	}
}

func (s *server) publicRequest(w http.ResponseWriter, r *http.Request) {
	v, err := s.ledger.RequestByToken(r.Context(), r.PathValue("token"))
	if errors.Is(err, ledger.ErrNotFound) {
		writeError(w, http.StatusNotFound, "link_expired", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *server) publicDecide(accept bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in decisionInput
		if !decode(w, r, &in) {
			return
		}
		err := s.ledger.DecideByToken(r.Context(), r.PathValue("token"), accept, in.Note)
		if errors.Is(err, ledger.ErrNotFound) {
			writeError(w, http.StatusNotFound, "link_expired", nil)
			return
		}
		if err != nil {
			s.ledgerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *server) publicBlock(w http.ResponseWriter, r *http.Request) {
	err := s.ledger.BlockByToken(r.Context(), r.PathValue("token"))
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
