// Package validate collects per-field input errors as stable codes the UI translates.
package validate

import (
	"net/mail"
	"strings"
	"unicode/utf8"
)

const (
	Required = "required"
	TooShort = "too_short"
	TooLong  = "too_long"
	Invalid  = "invalid"
)

// Errors maps a field name to its first error code.
type Errors map[string]string

func (e Errors) Add(field, code string) {
	if _, ok := e[field]; !ok {
		e[field] = code
	}
}

// Text requires a non-blank value of at most max characters.
func (e Errors) Text(field, v string, max int) {
	switch {
	case strings.TrimSpace(v) == "":
		e.Add(field, Required)
	case utf8.RuneCountInString(v) > max:
		e.Add(field, TooLong)
	}
}

// Email requires a bare address (no display name), already trimmed and lower-cased by the caller.
func (e Errors) Email(field, v string) {
	if v == "" {
		e.Add(field, Required)
		return
	}
	if len(v) > 254 {
		e.Add(field, TooLong)
		return
	}
	addr, err := mail.ParseAddress(v)
	if err != nil || addr.Address != v {
		e.Add(field, Invalid)
	}
}

func (e Errors) Locale(field, v string) {
	if v != "bn" && v != "en" {
		e.Add(field, Invalid)
	}
}

// NormalizeEmail is the single place emails are canonicalised before storing or comparing.
func NormalizeEmail(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}
