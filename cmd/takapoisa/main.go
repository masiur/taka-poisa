// Command takapoisa runs the Taka Poisa server: migrations, API and web UI in one binary.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/masiur/taka-poisa/internal/auth"
	"github.com/masiur/taka-poisa/internal/db"
	"github.com/masiur/taka-poisa/internal/httpapi"
	"github.com/masiur/taka-poisa/internal/ledger"
	"github.com/masiur/taka-poisa/internal/mail"
	"github.com/masiur/taka-poisa/internal/reminders"
	"github.com/masiur/taka-poisa/internal/webui"
)

type config struct {
	DatabaseURL        string
	BaseURL            *url.URL
	ListenAddr         string
	GoogleClientID     string
	GoogleClientSecret string
	SMTP               mail.SMTPConfig
}

func loadConfig() (config, error) {
	cfg := config{
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		ListenAddr:         envOr("LISTEN_ADDR", ":8080"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	}
	if cfg.DatabaseURL == "" {
		return cfg, errors.New("DATABASE_URL is required")
	}
	base, err := url.Parse(strings.TrimRight(envOr("BASE_URL", "http://localhost:8080"), "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return cfg, errors.New("BASE_URL must be an absolute http(s) URL")
	}
	cfg.BaseURL = base
	if (cfg.GoogleClientID == "") != (cfg.GoogleClientSecret == "") {
		return cfg, errors.New("set both GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET, or neither")
	}
	port, err := strconv.Atoi(envOr("SMTP_PORT", "0"))
	if err != nil {
		return cfg, errors.New("SMTP_PORT must be a number")
	}
	cfg.SMTP = mail.SMTPConfig{
		Host:     os.Getenv("SMTP_HOST"),
		Port:     port,
		Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     os.Getenv("SMTP_FROM"),
		FromName: envOr("SMTP_FROM_NAME", "Taka Poisa"),
		TLS:      os.Getenv("SMTP_TLS"),
	}
	return cfg, nil
}

// mailer returns the SMTP mailer, or nil when SMTP_HOST is unset (reminders are then skipped).
func (c config) mailer() (mail.Mailer, error) {
	if c.SMTP.Host == "" {
		return nil, nil
	}
	return mail.NewSMTP(c.SMTP)
}

// google returns the Google sign-in client, or nil when it is not configured.
func (c config) google() *auth.Google {
	if c.GoogleClientID == "" {
		return nil
	}
	return auth.NewGoogle(c.GoogleClientID, c.GoogleClientSecret,
		c.BaseURL.String()+"/api/auth/google/callback", auth.GoogleProduction)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	mailer, err := cfg.mailer()
	if err != nil {
		return err
	}
	ledgerStore := ledger.NewStore(pool)
	worker := &reminders.Worker{DB: pool, Ledger: ledgerStore, Mailer: mailer, BaseURL: cfg.BaseURL.String(), Log: log}
	go func() {
		if n, err := ledgerStore.ReplanAll(ctx); err != nil {
			log.Error("replan at startup failed", "err", err)
		} else {
			log.Info("reminders replanned", "loans", n)
		}
		worker.Run(ctx)
	}()

	srv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: httpapi.New(httpapi.Config{
			DB:            pool,
			SecureCookies: cfg.BaseURL.Scheme == "https",
			Static:        webui.FS(),
			Google:        cfg.google(),
			BaseURL:       cfg.BaseURL.String(),
			EmailEnabled:  mailer != nil,
			Log:           log,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.ListenAddr, "base_url", cfg.BaseURL.String(), "google_login", cfg.GoogleClientID != "", "email", mailer != nil)
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
