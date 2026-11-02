package reminders

import (
	"bytes"
	"strings"

	"github.com/masiur/taka-poisa/internal/mail"
)

var verifyText = map[string]map[string]string{
	"en": {
		"subj":     "Confirm your email for Taka Poisa",
		"greeting": "Hi {name},",
		"body":     "Confirm that this is your email to finish setting up your Taka Poisa account. People can then send you loan requests inside the app.",
		"cta":      "Confirm email",
		"footer":   "The link works for 7 days. If you didn't create this account, ignore this email.",
	},
	"bn": {
		"subj":     "টাকা পয়সায় আপনার ইমেইল নিশ্চিত করুন",
		"greeting": "প্রিয় {name},",
		"body":     "টাকা পয়সা অ্যাকাউন্ট চালু করা শেষ করতে নিশ্চিত করুন যে এটি আপনার ইমেইল। এরপর অন্যরা অ্যাপের ভেতরেই আপনাকে ধারের অনুরোধ পাঠাতে পারবেন।",
		"cta":      "ইমেইল নিশ্চিত করুন",
		"footer":   "লিংকটি ৭ দিন কাজ করবে। আপনি এই অ্যাকাউন্ট না খুলে থাকলে এই ইমেইলটি উপেক্ষা করুন।",
	},
}

// ComposeVerification is the email that confirms a new user owns their email address.
func ComposeVerification(to, name, locale, link string) mail.Message {
	tr := verifyText[locale]
	if tr == nil {
		locale, tr = "en", verifyText["en"]
	}
	vars := map[string]string{"name": name}
	v := view{
		Lang:     locale,
		Brand:    map[string]string{"bn": "টাকা পয়সা", "en": "Taka Poisa"}[locale],
		Greeting: fill(tr["greeting"], vars),
		Body:     tr["body"],
		Link:     link,
		CTA:      tr["cta"],
		Footer:   tr["footer"],
	}
	var html bytes.Buffer
	htmlTmpl.Execute(&html, v) // the template is fixed and the view only holds strings
	var plain strings.Builder
	for _, line := range []string{v.Greeting, v.Body, ctaLine(v), v.Footer} {
		plain.WriteString(line + "\n\n")
	}
	return mail.Message{To: to, ToName: name, FromName: "Taka Poisa", Subject: tr["subj"], HTML: html.String(), Text: plain.String()}
}
