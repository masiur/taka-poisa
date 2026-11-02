// Package reminders writes and delivers reminder emails from the planned sends in the database.
package reminders

import (
	"fmt"
	"strings"

	"github.com/masiur/taka-poisa/internal/ledger"
)

var bnDigits = strings.NewReplacer("0", "০", "1", "১", "2", "২", "3", "৩", "4", "৪", "5", "৫", "6", "৬", "7", "৭", "8", "৮", "9", "৯")

var symbols = map[string]string{"BDT": "৳", "INR": "₹", "USD": "$", "EUR": "€", "GBP": "£", "PKR": "Rs ", "NPR": "Rs "}

// zeroDecimal are currencies without a minor unit (ISO 4217 exponent 0) that the app supports.
var zeroDecimal = map[string]bool{"JPY": true, "KRW": true, "VND": true}

// formatMoney renders minor units the way the web UI does: South Asian grouping for
// BDT/INR/PKR/NPR (1,00,000), decimals only when non-zero, Bangla digits for "bn".
func formatMoney(minor int64, currency, locale string) string {
	scale := int64(100)
	if zeroDecimal[currency] {
		scale = 1
	}
	whole, frac := minor/scale, minor%scale
	digits := fmt.Sprint(whole)
	southAsian := currency == "BDT" || currency == "INR" || currency == "PKR" || currency == "NPR"
	var grouped string
	if len(digits) <= 3 {
		grouped = digits
	} else {
		head, tail := digits[:len(digits)-3], digits[len(digits)-3:]
		size := 3
		if southAsian {
			size = 2
		}
		var parts []string
		for len(head) > size {
			parts = append([]string{head[len(head)-size:]}, parts...)
			head = head[:len(head)-size]
		}
		parts = append([]string{head}, parts...)
		grouped = strings.Join(parts, ",") + "," + tail
	}
	if frac != 0 {
		grouped += fmt.Sprintf(".%02d", frac)
	}
	sym, ok := symbols[currency]
	if !ok {
		sym = currency + " "
	}
	out := sym + grouped
	if locale == "bn" {
		out = bnDigits.Replace(out)
	}
	return out
}

var monthsBN = []string{"জানুয়ারি", "ফেব্রুয়ারি", "মার্চ", "এপ্রিল", "মে", "জুন", "জুলাই", "আগস্ট", "সেপ্টেম্বর", "অক্টোবর", "নভেম্বর", "ডিসেম্বর"}

func formatDate(d ledger.Date, locale string) string {
	t := d.Time()
	if locale == "bn" {
		return bnDigits.Replace(fmt.Sprint(t.Day())) + " " + monthsBN[t.Month()-1] + " " + bnDigits.Replace(fmt.Sprint(t.Year()))
	}
	return t.Format("2 Jan 2006")
}

func formatInt(n int, locale string) string {
	if locale == "bn" {
		return bnDigits.Replace(fmt.Sprint(n))
	}
	return fmt.Sprint(n)
}
