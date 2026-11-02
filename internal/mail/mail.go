// Package mail sends email over SMTP, so self-hosters can use any provider
// (Mailjet, Brevo, Gmail, Amazon SES, …).
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

type Message struct {
	To, ToName     string
	ReplyTo        string
	ReplyToName    string
	FromName       string // display name; the address is the server's From
	Subject        string
	Text, HTML     string
	UnsubscribeURL string // List-Unsubscribe, one-click (RFC 8058)
}

// Mailer sends one message and returns its Message-ID.
type Mailer interface {
	Send(ctx context.Context, m Message) (string, error)
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // address all mail is sent from
	FromName string
	TLS      string // starttls (default) | tls (implicit, port 465) | none (local relays only)
}

type SMTP struct {
	cfg SMTPConfig
}

func NewSMTP(cfg SMTPConfig) (*SMTP, error) {
	if cfg.Host == "" || cfg.From == "" {
		return nil, errors.New("SMTP_HOST and SMTP_FROM are required")
	}
	if _, err := mail.ParseAddress(cfg.From); err != nil {
		return nil, fmt.Errorf("SMTP_FROM: %w", err)
	}
	switch cfg.TLS {
	case "":
		cfg.TLS = "starttls"
		if cfg.Port == 465 {
			cfg.TLS = "tls"
		}
	case "starttls", "tls", "none":
	default:
		return nil, errors.New("SMTP_TLS must be starttls, tls or none")
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	return &SMTP{cfg: cfg}, nil
}

func (s *SMTP) Send(ctx context.Context, m Message) (string, error) {
	id, raw, err := s.build(m)
	if err != nil {
		return "", err
	}
	addr := net.JoinHostPort(s.cfg.Host, fmt.Sprint(s.cfg.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	if s.cfg.TLS == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: s.cfg.Host})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return "", err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(60 * time.Second)
	}
	conn.SetDeadline(deadline)

	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		conn.Close()
		return "", err
	}
	defer c.Close()
	if s.cfg.TLS == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return "", errors.New("smtp: server does not offer STARTTLS (set SMTP_TLS=none only for a local relay)")
		}
		if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host}); err != nil {
			return "", err
		}
	}
	if s.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return "", err
		}
	}
	if err := c.Mail(s.cfg.From); err != nil {
		return "", err
	}
	if err := c.Rcpt(m.To); err != nil {
		return "", err
	}
	w, err := c.Data()
	if err != nil {
		return "", err
	}
	if _, err := w.Write(raw); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return id, c.Quit()
}

// oneLine strips CR/LF so user-supplied names can never inject headers.
func oneLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

func address(name, addr string) string {
	return (&mail.Address{Name: oneLine(name), Address: oneLine(addr)}).String()
}

// build renders a multipart/alternative (text + HTML) UTF-8 message.
func (s *SMTP) build(m Message) (string, []byte, error) {
	if _, err := mail.ParseAddress(m.To); err != nil {
		return "", nil, fmt.Errorf("bad recipient: %w", err)
	}
	idBytes := make([]byte, 16)
	rand.Read(idBytes)
	domain := s.cfg.From[strings.LastIndex(s.cfg.From, "@")+1:]
	messageID := fmt.Sprintf("<%s@%s>", hex.EncodeToString(idBytes), domain)

	fromName := m.FromName
	if fromName == "" {
		fromName = s.cfg.FromName
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, part := range []struct{ typ, content string }{{"text/plain", m.Text}, {"text/html", m.HTML}} {
		pw, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.typ + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return "", nil, err
		}
		qp := quotedprintable.NewWriter(pw)
		qp.Write([]byte(part.content))
		qp.Close()
	}
	mw.Close()

	var h bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&h, "%s: %s\r\n", k, v) }
	header("From", address(fromName, s.cfg.From))
	header("To", address(m.ToName, m.To))
	if m.ReplyTo != "" {
		header("Reply-To", address(m.ReplyToName, m.ReplyTo))
	}
	header("Subject", mime.QEncoding.Encode("utf-8", oneLine(m.Subject)))
	header("Date", time.Now().Format(time.RFC1123Z))
	header("Message-ID", messageID)
	header("MIME-Version", "1.0")
	if m.UnsubscribeURL != "" {
		header("List-Unsubscribe", "<"+oneLine(m.UnsubscribeURL)+">")
		header("List-Unsubscribe-Post", "List-Unsubscribe=One-Click")
	}
	header("Content-Type", `multipart/alternative; boundary="`+mw.Boundary()+`"`)
	h.WriteString("\r\n")
	h.Write(body.Bytes())
	return messageID, h.Bytes(), nil
}
