package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/masiur/taka-poisa/internal/auth"
	"github.com/masiur/taka-poisa/internal/ledger"
	"github.com/masiur/taka-poisa/internal/validate"
)

func (s *server) linkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/request-links", s.requireAuth(s.listLinks))
	mux.HandleFunc("POST /api/request-links", s.requireAuth(s.createLink))
	mux.HandleFunc("DELETE /api/request-links/{id}", s.requireAuth(s.revokeLink))
	mux.HandleFunc("POST /api/agreements/{id}/approve", s.requireAuth(s.approveLinkRequest))
	mux.HandleFunc("POST /api/agreements/{id}/decline", s.requireAuth(s.declineLinkRequest))
	mux.HandleFunc("POST /api/agreements/{id}/mark-sent", s.requireAuth(s.markSent))
	mux.HandleFunc("POST /api/agreements/{id}/block", s.requireAuth(s.blockRequester))

	// Public: the request form behind a link, for people without an account.
	mux.HandleFunc("GET /api/public/ask/{token}", s.publicLinkInfo)
	mux.HandleFunc("POST /api/public/ask/{token}", s.publicLinkSubmit)
	mux.HandleFunc("POST /api/public/schedule/preview", func(w http.ResponseWriter, r *http.Request) {
		s.previewSchedule(w, r, auth.Principal{})
	})
}

type linkResponse struct {
	ledger.RequestLink
	URL string `json:"url"`
}

func (s *server) withURL(l ledger.RequestLink) linkResponse {
	return linkResponse{l, s.baseURL + "/ask/" + l.Token}
}

func (s *server) listLinks(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	links, err := s.ledger.ListLinks(r.Context(), p.AccountID)
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	out := make([]linkResponse, len(links))
	for i, l := range links {
		out[i] = s.withURL(l)
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": out})
}

func (s *server) createLink(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var in ledger.LinkInput
	if !decode(w, r, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", errs)
		return
	}
	l, err := s.ledger.CreateLink(r.Context(), p.AccountID, in)
	if err != nil {
		s.ledgerError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.withURL(l))
}

func (s *server) revokeLink(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	if err := s.ledger.RevokeLink(r.Context(), p.AccountID, id); err != nil {
		s.ledgerError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) approveLinkRequest(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	s.withNote(w, r, func(id int64, note string) error {
		return s.ledger.ApproveLinkRequest(r.Context(), p.AccountID, p.UserID, id, note)
	})
}

func (s *server) declineLinkRequest(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	s.withNote(w, r, func(id int64, note string) error {
		return s.ledger.DeclineLinkRequest(r.Context(), p.AccountID, p.UserID, id, note)
	})
}

func (s *server) withNote(w http.ResponseWriter, r *http.Request, do func(id int64, note string) error) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	var in decisionInput
	if !decode(w, r, &in) {
		return
	}
	if err := do(id, in.Note); err != nil {
		s.ledgerError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) markSent(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	var in ledger.SentInput
	if !decode(w, r, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", errs)
		return
	}
	if err := s.ledger.MarkSent(r.Context(), p.AccountID, p.UserID, id, in); err != nil {
		s.ledgerError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) blockRequester(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", nil)
		return
	}
	if err := s.ledger.BlockLinkRequester(r.Context(), p.AccountID, p.UserID, id); err != nil {
		s.ledgerError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) publicLinkInfo(w http.ResponseWriter, r *http.Request) {
	info, err := s.ledger.LinkInfo(r.Context(), r.PathValue("token"))
	if errors.Is(err, ledger.ErrNotFound) {
		writeError(w, http.StatusNotFound, "link_expired", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *server) publicLinkSubmit(w http.ResponseWriter, r *http.Request) {
	// Every submission counts, valid or not: a public form must not be a free spam cannon.
	key := "ask|" + clientIP(r)
	if s.askLimit.Blocked(key) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", nil)
		return
	}
	s.askLimit.Fail(key)

	var in ledger.LinkRequestInput
	if !decode(w, r, &in) {
		return
	}
	if in.IsBot() {
		w.WriteHeader(http.StatusCreated) // look successful, store nothing
		return
	}
	in.Normalize()
	items, errs := in.Build()
	if len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", errs)
		return
	}
	_, err := s.ledger.SubmitLinkRequest(r.Context(), r.PathValue("token"), in, items)
	switch {
	case errors.Is(err, ledger.ErrNotFound):
		writeError(w, http.StatusNotFound, "link_expired", nil)
	case errors.Is(err, ledger.ErrAboveLimit):
		writeError(w, http.StatusUnprocessableEntity, "validation", validate.Errors{"principal": "above_limit"})
	case err != nil:
		s.ledgerError(w, r, err)
	default:
		w.WriteHeader(http.StatusCreated)
	}
}

// askLimitWindow: at most 5 submissions per IP per hour through any link.
const askLimitWindow = time.Hour
