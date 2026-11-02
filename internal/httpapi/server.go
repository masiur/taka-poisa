// Package httpapi is the JSON API and the static UI server.
package httpapi

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/masiur/taka-poisa/internal/account"
	"github.com/masiur/taka-poisa/internal/auth"
	"github.com/masiur/taka-poisa/internal/ledger"
	"github.com/masiur/taka-poisa/internal/validate"
)

const sessionCookie = "tp_session"

type Config struct {
	DB            *pgxpool.Pool
	SecureCookies bool                  // true when BASE_URL is https
	Static        fs.FS                 // built web UI
	Identity      auth.IdentityProvider // outside sign-in; nil on the open-source server
	BaseURL       string                // public URL, for links handed out (statement pages)
	EmailEnabled  bool                  // SMTP configured; shown to the owner next to reminders
	SMSEnabled    bool                  // an SMS gateway is configured
	Log           *slog.Logger
}

type server struct {
	db            *pgxpool.Pool
	auth          *auth.Store
	accounts      *account.Store
	ledger        *ledger.Store
	identity      auth.IdentityProvider
	baseURL       string
	emailEnabled  bool
	smsEnabled    bool
	secureCookies bool
	loginLimit    *limiter
	askLimit      *limiter
	log           *slog.Logger
}

func New(cfg Config) http.Handler {
	s := &server{
		db:            cfg.DB,
		auth:          auth.NewStore(cfg.DB),
		accounts:      account.NewStore(cfg.DB),
		ledger:        ledger.NewStore(cfg.DB),
		identity:      cfg.Identity,
		baseURL:       cfg.BaseURL,
		emailEnabled:  cfg.EmailEnabled,
		smsEnabled:    cfg.SMSEnabled,
		secureCookies: cfg.SecureCookies,
		loginLimit:    newLimiter(10, 15*time.Minute),
		askLimit:      newLimiter(5, askLimitWindow),
		log:           cfg.Log,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /api/setup", s.getSetup)
	mux.HandleFunc("POST /api/setup", s.postSetup)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/auth/{provider}/start", s.identityStart)
	mux.HandleFunc("GET /api/auth/{provider}/callback", s.identityCallback)
	mux.HandleFunc("GET /api/me", s.requireAuth(s.me))
	mux.HandleFunc("GET /api/account", s.requireAuth(s.getAccount))
	mux.HandleFunc("PUT /api/account", s.requireAuth(s.putAccount))
	s.ledgerRoutes(mux)
	s.reminderRoutes(mux)
	s.requestRoutes(mux)
	s.linkRoutes(mux)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", nil)
	})
	mux.Handle("/", spaHandler(cfg.Static))

	// CrossOriginProtection rejects cross-site state-changing requests (CSRF) using
	// Sec-Fetch-Site / Origin, so no CSRF token is needed.
	return http.NewCrossOriginProtection().Handler(securityHeaders(mux))
}

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Ping(r.Context()); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok"))
}

func (s *server) getSetup(w http.ResponseWriter, r *http.Request) {
	needed, err := s.auth.SetupNeeded(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	sso := ""
	if s.identity != nil {
		sso = s.identity.Name()
	}
	writeJSON(w, http.StatusOK, map[string]any{"needed": needed, "sso": sso})
}

func (s *server) postSetup(w http.ResponseWriter, r *http.Request) {
	var in auth.OwnerInput
	if !decode(w, r, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", errs)
		return
	}
	userID, err := s.auth.CreateOwner(r.Context(), in)
	if errors.Is(err, auth.ErrAlreadySetUp) {
		writeError(w, http.StatusConflict, "already_set_up", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.startSession(w, r, userID)
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	key := clientIP(r) + "|" + validate.NormalizeEmail(in.Email)
	if s.loginLimit.Blocked(key) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", nil)
		return
	}
	userID, err := s.auth.Authenticate(r.Context(), in.Email, in.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		s.loginLimit.Fail(key)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.loginLimit.Reset(key)
	s.startSession(w, r, userID)
}

func (s *server) startSession(w http.ResponseWriter, r *http.Request, userID int64) {
	if err := s.setSessionCookie(w, r, userID); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) setSessionCookie(w http.ResponseWriter, r *http.Request, userID int64) error {
	token, expires, err := s.auth.CreateSession(r.Context(), userID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.auth.DeleteSession(r.Context(), c.Value); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.secureCookies, SameSite: http.SameSiteLaxMode,
	})
}

func (s *server) requireAuth(h func(http.ResponseWriter, *http.Request, auth.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", nil)
			return
		}
		p, err := s.auth.Principal(r.Context(), c.Value)
		if errors.Is(err, auth.ErrNoSession) {
			s.clearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, "unauthorized", nil)
			return
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		h(w, r, p)
	}
}

type meResponse struct {
	User struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Email  string `json:"email"`
		Locale string `json:"locale"`
	} `json:"user"`
	Account struct {
		ID   int64  `json:"id"`
		Role string `json:"role"`
	} `json:"account"`
}

func (s *server) me(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	var res meResponse
	res.User.ID, res.User.Name, res.User.Email, res.User.Locale = p.UserID, p.Name, p.Email, p.Locale
	res.Account.ID, res.Account.Role = p.AccountID, p.Role
	writeJSON(w, http.StatusOK, res)
}

func (s *server) getAccount(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	settings, err := s.accounts.Get(r.Context(), p.AccountID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *server) putAccount(w http.ResponseWriter, r *http.Request, p auth.Principal) {
	if p.Role != "owner" {
		writeError(w, http.StatusForbidden, "forbidden", nil)
		return
	}
	var in account.Settings
	if !decode(w, r, &in) {
		return
	}
	in.Normalize()
	if errs := in.Validate(); len(errs) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", errs)
		return
	}
	err := s.accounts.Update(r.Context(), p.AccountID, in)
	if errors.Is(err, account.ErrCurrencyLocked) {
		writeError(w, http.StatusUnprocessableEntity, "validation", validate.Errors{"currency": "locked"})
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	// Timezone or send hour may have changed: move planned reminders.
	if err := s.ledger.ReplanAccount(r.Context(), p.AccountID); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal", nil)
}

// decode reads a JSON body of at most 64 KiB, rejecting unknown fields.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", nil)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string, fields validate.Errors) {
	body := map[string]any{"error": code}
	if len(fields) > 0 {
		body["fields"] = fields
	}
	writeJSON(w, status, body)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// clientIP is the TCP peer address. Behind a reverse proxy this is the proxy;
// the login limiter keys on IP+email, so that only coarsens the limit per email.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
