package reminders

import (
	"strings"
	"testing"

	"github.com/masiur/taka-poisa/internal/ledger"
)

func TestFormatMoney(t *testing.T) {
	cases := []struct {
		minor         int64
		currency, loc string
		want          string
	}{
		{30000_00, "BDT", "en", "৳30,000"},
		{1_00_000_00, "BDT", "en", "৳1,00,000"},
		{1_23_45_678_50, "BDT", "en", "৳1,23,45,678.50"},
		{1_00_000_00, "BDT", "bn", "৳১,০০,০০০"},
		{1_000_000_00, "USD", "en", "$1,000,000"},
		{999, "JPY", "en", "JPY 999"},
		{5, "BDT", "en", "৳0.05"},
	}
	for _, c := range cases {
		if got := formatMoney(c.minor, c.currency, c.loc); got != c.want {
			t.Errorf("formatMoney(%d, %s, %s) = %q, want %q", c.minor, c.currency, c.loc, got, c.want)
		}
	}
}

func TestComposeContactAndSelf(t *testing.T) {
	d := func(s string) ledger.Date { v, _ := ledger.ParseDate(s); return v }
	rc := ledger.ReminderContext{
		Direction: "lent", Currency: "BDT", OwnerName: "Masiur", OwnerEmail: "owner@example.com", OwnerLocale: "en",
		ContactName: "রহিম", ContactEmail: "rahim@example.com", ContactLocale: "bn",
		Today: d("2026-11-20"), Remaining: 20000_00, Overdue: 10000_00, OverdueSince: d("2026-11-10"),
	}
	m := compose(rc, "contact", "firm", "https://app.test/s/T", "https://app.test/s/T?unsubscribe=1", "https://app.test/api/public/unsubscribe/T")
	if m.Subject != "বকেয়া: Masiurকে ৳১০,০০০, ১০ দিন দেরি" {
		t.Errorf("bn firm subject %q", m.Subject)
	}
	if m.ReplyTo != "owner@example.com" || m.FromName != "Masiur via Taka Poisa" || !strings.Contains(m.HTML, "https://app.test/s/T") {
		t.Errorf("contact email: %+v", m)
	}
	if !strings.Contains(m.Text, "মোট বাকি: ৳২০,০০০") {
		t.Errorf("total missing: %q", m.Text)
	}

	// Names are HTML-escaped in the HTML part.
	rc.ContactName = `<script>x</script>`
	rc.ContactLocale = "en"
	m = compose(rc, "contact", "polite", "https://app.test/s/T", "u", "u")
	if strings.Contains(m.HTML, "<script>") || m.Subject != "Friendly reminder: ৳10,000 still due to Masiur" {
		t.Errorf("escaping or polite subject: %q", m.Subject)
	}

	rc.Overdue, rc.NextDueOn, rc.NextAmount = 0, d("2026-11-25"), 5000_00
	m = compose(rc, "self", "polite", "https://app.test/loans/1", "", "")
	if m.To != "owner@example.com" || m.Subject != "<script>x</script> pays you ৳5,000 on 25 Nov 2026" || m.UnsubscribeURL != "" {
		t.Errorf("self email: %+v", m)
	}
}
