package reminders

import (
	"bytes"
	"html/template"
	"strings"

	"github.com/masiur/taka-poisa/internal/ledger"
	"github.com/masiur/taka-poisa/internal/mail"
)

// strings per language; {placeholders} are filled by fill().
var text = map[string]map[string]string{
	"en": {
		"c_subj_before":         "Reminder: {amount} due to {owner} on {date}",
		"c_subj_due":            "Due today: {amount} to {owner}",
		"c_subj_overdue":        "Friendly reminder: {amount} still due to {owner}",
		"c_subj_overdue_firm":   "Overdue: {amount} to {owner}, {days} days late",
		"c_greeting":            "Hi {contact},",
		"c_body_before":         "This is a reminder that {amount} is due to {owner} on {date}.",
		"c_body_due":            "{amount} is due to {owner} today.",
		"c_body_overdue":        "{amount} that was due to {owner} on {date} is still unpaid. If you have already paid, please let {owner} know.",
		"c_body_overdue_firm":   "{amount} is now {days} days overdue (it was due on {date}). Please pay {owner} as soon as you can.",
		"c_cta":                 "See details or tell {owner} you paid",
		"c_footer":              "Sent by Taka Poisa on behalf of {owner}. Reply to this email to reach {owner}.",
		"c_unsubscribe":         "Don't want these emails? Unsubscribe",
		"lent_subj_before":      "{contact} pays you {amount} on {date}",
		"lent_subj_due":         "{contact} owes you {amount} today",
		"lent_subj_overdue":     "{contact} is {days} days late with {amount}",
		"borrowed_subj_before":  "You pay {contact} {amount} on {date}",
		"borrowed_subj_due":     "Pay {contact} {amount} today",
		"borrowed_subj_overdue": "You are {days} days late paying {contact} {amount}",
		"s_cta":                 "Open in Taka Poisa",
		"s_footer":              "You get this because of the reminder settings on this loan.",
		"total":                 "Total left: {total}",
	},
	"bn": {
		"c_subj_before":         "মনে করিয়ে দেওয়া: {date} তারিখে {owner}কে {amount} দেওয়ার কথা",
		"c_subj_due":            "আজ {owner}কে {amount} দেওয়ার দিন",
		"c_subj_overdue":        "{owner}কে {amount} দেওয়া এখনও বাকি",
		"c_subj_overdue_firm":   "বকেয়া: {owner}কে {amount}, {days} দিন দেরি",
		"c_greeting":            "প্রিয় {contact},",
		"c_body_before":         "মনে করিয়ে দিচ্ছি, {date} তারিখে {owner}কে {amount} দেওয়ার কথা।",
		"c_body_due":            "আজ {owner}কে {amount} দেওয়ার দিন।",
		"c_body_overdue":        "{date} তারিখে {owner}কে যে {amount} দেওয়ার কথা ছিল, তা এখনও বাকি। যদি দিয়ে থাকেন, {owner}কে জানিয়ে দিন।",
		"c_body_overdue_firm":   "{amount} এখন {days} দিন বকেয়া ({date} তারিখে দেওয়ার কথা ছিল)। যত দ্রুত সম্ভব {owner}কে পরিশোধ করুন।",
		"c_cta":                 "বিস্তারিত দেখুন বা পরিশোধ জানান",
		"c_footer":              "{owner}এর পক্ষ থেকে টাকা পয়সা এই ইমেইল পাঠিয়েছে। {owner}এর সাথে যোগাযোগ করতে এই ইমেইলের উত্তর দিন।",
		"c_unsubscribe":         "এমন ইমেইল চান না? বন্ধ করুন",
		"lent_subj_before":      "{date} তারিখে {contact} আপনাকে {amount} দেবেন",
		"lent_subj_due":         "আজ {contact} আপনাকে {amount} দেবেন",
		"lent_subj_overdue":     "{contact} {amount} দিতে {days} দিন দেরি করছেন",
		"borrowed_subj_before":  "{date} তারিখে {contact}কে আপনার {amount} দেওয়ার কথা",
		"borrowed_subj_due":     "আজ {contact}কে {amount} দিন",
		"borrowed_subj_overdue": "{contact}কে {amount} দিতে আপনার {days} দিন দেরি",
		"s_cta":                 "টাকা পয়সায় খুলুন",
		"s_footer":              "এই ধারের রিমাইন্ডার সেটিংসের জন্য আপনি এটি পাচ্ছেন।",
		"total":                 "মোট বাকি: {total}",
	},
}

func fill(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

// kind is what the reminder is about, decided at send time from the latest balances.
func kind(rc ledger.ReminderContext) string {
	switch {
	case rc.Overdue > 0:
		return "overdue"
	case rc.NextDueOn == rc.Today:
		return "due"
	default:
		return "before"
	}
}

var htmlTmpl = template.Must(template.New("email").Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"></head>
<body style="margin:0;padding:24px 12px;background:#f0f4f9;font-family:Roboto,'Noto Sans Bengali',Arial,sans-serif;color:#1f1f1f">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr><td align="center">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:520px;background:#ffffff;border-radius:24px">
<tr><td style="padding:32px 28px">
<p style="margin:0 0 16px;color:#0b57d0;font-size:15px;font-weight:500">{{.Brand}}</p>
{{if .Greeting}}<p style="margin:0 0 12px;font-size:16px;line-height:24px">{{.Greeting}}</p>{{end}}
<p style="margin:0 0 16px;font-size:16px;line-height:24px">{{.Body}}</p>
<p style="margin:0 0 24px;font-size:22px;line-height:28px;font-weight:500">{{.Total}}</p>
<a href="{{.Link}}" style="display:inline-block;padding:10px 24px;border-radius:999px;background:#0b57d0;color:#ffffff;text-decoration:none;font-size:14px;font-weight:500">{{.CTA}}</a>
<p style="margin:32px 0 0;color:#444746;font-size:12px;line-height:16px">{{.Footer}}</p>
{{if .Unsubscribe}}<p style="margin:8px 0 0;font-size:12px"><a href="{{.UnsubscribeLink}}" style="color:#444746">{{.Unsubscribe}}</a></p>{{end}}
</td></tr></table></td></tr></table></body></html>`))

type view struct {
	Lang, Brand, Greeting, Body, Total, Link, CTA, Footer, Unsubscribe, UnsubscribeLink string
}

// compose writes one reminder. link is the statement page (contact) or the loan page (owner);
// unsubscribeURL is set for contacts only.
func compose(rc ledger.ReminderContext, recipient, tone, link, unsubscribeLink, unsubscribeURL string) mail.Message {
	locale := rc.OwnerLocale
	if recipient == "contact" {
		locale = rc.ContactLocale
	}
	tr := text[locale]
	if tr == nil {
		tr = text["en"]
	}
	k := kind(rc)
	vars := map[string]string{
		"owner":   rc.OwnerName,
		"contact": rc.ContactName,
		"total":   formatMoney(rc.Remaining, rc.Currency, locale),
	}
	switch k {
	case "overdue":
		vars["amount"] = formatMoney(rc.Overdue, rc.Currency, locale)
		vars["date"] = formatDate(rc.OverdueSince, locale)
		vars["days"] = formatInt(rc.OverdueSince.DaysBefore(rc.Today), locale)
	default:
		vars["amount"] = formatMoney(rc.NextAmount, rc.Currency, locale)
		vars["date"] = formatDate(rc.NextDueOn, locale)
	}

	v := view{Lang: locale, Brand: map[string]string{"bn": "টাকা পয়সা", "en": "Taka Poisa"}[locale], Link: link, Total: fill(tr["total"], vars)}
	var subject string
	m := mail.Message{}
	if recipient == "contact" {
		suffix := ""
		if k == "overdue" && tone == "firm" {
			suffix = "_firm"
		}
		subject = fill(tr["c_subj_"+k+suffix], vars)
		v.Greeting = fill(tr["c_greeting"], vars)
		v.Body = fill(tr["c_body_"+k+suffix], vars)
		v.CTA = fill(tr["c_cta"], vars)
		v.Footer = fill(tr["c_footer"], vars)
		v.Unsubscribe, v.UnsubscribeLink = tr["c_unsubscribe"], unsubscribeLink
		m = mail.Message{
			To: rc.ContactEmail, ToName: rc.ContactName,
			ReplyTo: rc.OwnerEmail, ReplyToName: rc.OwnerName,
			FromName:       rc.OwnerName + " via Taka Poisa",
			UnsubscribeURL: unsubscribeURL,
		}
	} else {
		subject = fill(tr[rc.Direction+"_subj_"+k], vars)
		v.Body = subject
		v.CTA = tr["s_cta"]
		v.Footer = tr["s_footer"]
		m = mail.Message{To: rc.OwnerEmail, ToName: rc.OwnerName, FromName: "Taka Poisa"}
	}
	m.Subject = subject

	var html bytes.Buffer
	htmlTmpl.Execute(&html, v) // the template is fixed and the view only holds strings
	m.HTML = html.String()

	var plain strings.Builder
	for _, line := range []string{v.Greeting, v.Body, v.Total, v.CTA + ": " + v.Link, v.Footer} {
		if line != "" {
			plain.WriteString(line + "\n\n")
		}
	}
	if v.Unsubscribe != "" {
		plain.WriteString(v.Unsubscribe + ": " + v.UnsubscribeLink + "\n")
	}
	m.Text = plain.String()
	return m
}
