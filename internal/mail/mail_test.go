package mail

import (
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

func TestBuildMessage(t *testing.T) {
	s, err := NewSMTP(SMTPConfig{Host: "smtp.example.com", From: "reminders@takapoisa.example", FromName: "Taka Poisa"})
	if err != nil {
		t.Fatal(err)
	}
	id, raw, err := s.build(Message{
		To: "rahim@example.com", ToName: "রহিম\r\nBcc: evil@example.com",
		ReplyTo: "owner@example.com", ReplyToName: "Masiur",
		FromName: "Masiur via Taka Poisa",
		Subject:  "মনে করিয়ে দেওয়া: ৳১০,০০০",
		Text:     "plain body", HTML: "<p>html body</p>",
		UnsubscribeURL: "https://app.example/api/public/unsubscribe/tok",
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("Message-ID") != id || !strings.HasSuffix(id, "@takapoisa.example>") {
		t.Errorf("message id %q / %q", id, msg.Header.Get("Message-ID"))
	}
	if bcc := msg.Header.Get("Bcc"); bcc != "" {
		t.Errorf("header injection: Bcc %q", bcc)
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subject != "মনে করিয়ে দেওয়া: ৳১০,০০০" {
		t.Errorf("subject %q", subject)
	}
	if msg.Header.Get("List-Unsubscribe-Post") != "List-Unsubscribe=One-Click" || !strings.Contains(msg.Header.Get("List-Unsubscribe"), "/unsubscribe/tok") {
		t.Errorf("unsubscribe headers missing")
	}
	from, _ := mail.ParseAddress(msg.Header.Get("From"))
	if from.Address != "reminders@takapoisa.example" || from.Name != "Masiur via Taka Poisa" {
		t.Errorf("from %+v", from)
	}

	_, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var parts []string
	for {
		p, err := mr.NextPart() // decodes quoted-printable
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(p)
		parts = append(parts, string(b))
	}
	if len(parts) != 2 || parts[0] != "plain body" || parts[1] != "<p>html body</p>" {
		t.Errorf("parts %q", parts)
	}
}

func TestNewSMTPDefaults(t *testing.T) {
	s, _ := NewSMTP(SMTPConfig{Host: "h", From: "a@b.c", Port: 465})
	if s.cfg.TLS != "tls" {
		t.Errorf("port 465 should default to implicit TLS, got %s", s.cfg.TLS)
	}
	s, _ = NewSMTP(SMTPConfig{Host: "h", From: "a@b.c"})
	if s.cfg.TLS != "starttls" || s.cfg.Port != 587 {
		t.Errorf("defaults %+v", s.cfg)
	}
	if _, err := NewSMTP(SMTPConfig{Host: "h", From: "a@b.c", TLS: "maybe"}); err == nil {
		t.Error("bad SMTP_TLS accepted")
	}
}
