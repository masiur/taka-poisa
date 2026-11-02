// Package sms sends text messages through a gateway the operator brings: any HTTP SMS API
// (most Bangladeshi gateways) via a URL/body template, or Twilio.
package sms

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Message is one SMS. To is E.164 (+8801712345678).
type Message struct {
	To   string
	Text string
}

// Sender sends one SMS and returns the gateway's message id (may be empty).
type Sender interface {
	Send(ctx context.Context, m Message) (string, error)
}

var client = &http.Client{Timeout: 30 * time.Second}

// FromEnv builds the sender configured by SMS_PROVIDER, or nil when SMS is off.
//
//	SMS_PROVIDER=http    SMS_HTTP_URL, SMS_HTTP_METHOD (GET|POST), SMS_HTTP_BODY,
//	                     SMS_HTTP_CONTENT_TYPE, SMS_HTTP_SUCCESS (text the response must contain)
//	SMS_PROVIDER=twilio  TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN, TWILIO_FROM
func FromEnv() (Sender, error) {
	switch os.Getenv("SMS_PROVIDER") {
	case "":
		return nil, nil
	case "http":
		return NewHTTPGateway(HTTPGateway{
			URL:         os.Getenv("SMS_HTTP_URL"),
			Method:      os.Getenv("SMS_HTTP_METHOD"),
			Body:        os.Getenv("SMS_HTTP_BODY"),
			ContentType: os.Getenv("SMS_HTTP_CONTENT_TYPE"),
			Success:     os.Getenv("SMS_HTTP_SUCCESS"),
		})
	case "twilio":
		return NewTwilio(os.Getenv("TWILIO_ACCOUNT_SID"), os.Getenv("TWILIO_AUTH_TOKEN"), os.Getenv("TWILIO_FROM"))
	default:
		return nil, errors.New("SMS_PROVIDER must be http or twilio")
	}
}

// HTTPGateway calls an SMS API from templates. Placeholders:
//
//	{to}         +8801712345678 (E.164)
//	{to_digits}  8801712345678
//	{to_local}   01712345678 for Bangladeshi numbers, otherwise the same as {to_digits}
//	{message}    the text
//
// Values are URL-encoded in the URL and in form bodies, and JSON-escaped in JSON bodies
// (put the placeholder inside quotes there: "msg": "{message}").
type HTTPGateway struct {
	URL         string
	Method      string // GET (default without a body) or POST
	Body        string
	ContentType string // default application/x-www-form-urlencoded when Body is set
	Success     string // optional text the response body must contain, for gateways that answer 200 on errors
}

func NewHTTPGateway(g HTTPGateway) (*HTTPGateway, error) {
	if g.URL == "" {
		return nil, errors.New("SMS_HTTP_URL is required")
	}
	if u, err := url.Parse(strings.NewReplacer("{", "", "}", "").Replace(g.URL)); err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("SMS_HTTP_URL must be an http(s) URL")
	}
	g.Method = strings.ToUpper(g.Method)
	if g.Method == "" {
		g.Method = "GET"
		if g.Body != "" {
			g.Method = "POST"
		}
	}
	if g.Method != "GET" && g.Method != "POST" {
		return nil, errors.New("SMS_HTTP_METHOD must be GET or POST")
	}
	if g.Body != "" && g.ContentType == "" {
		g.ContentType = "application/x-www-form-urlencoded"
	}
	return &g, nil
}

func placeholders(m Message) map[string]string {
	digits := strings.TrimPrefix(m.To, "+")
	local := digits
	if strings.HasPrefix(digits, "880") {
		local = "0" + strings.TrimPrefix(digits, "880")
	}
	return map[string]string{"{to}": m.To, "{to_digits}": digits, "{to_local}": local, "{message}": m.Text}
}

func fill(tmpl string, vals map[string]string, escape func(string) string) string {
	pairs := make([]string, 0, len(vals)*2)
	for k, v := range vals {
		pairs = append(pairs, k, escape(v))
	}
	return strings.NewReplacer(pairs...).Replace(tmpl)
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

func (g *HTTPGateway) Send(ctx context.Context, m Message) (string, error) {
	vals := placeholders(m)
	var body io.Reader
	if g.Body != "" {
		escape := url.QueryEscape
		if strings.Contains(g.ContentType, "json") {
			escape = jsonEscape
		}
		body = strings.NewReader(fill(g.Body, vals, escape))
	}
	req, err := http.NewRequestWithContext(ctx, g.Method, fill(g.URL, vals, url.QueryEscape), body)
	if err != nil {
		return "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", g.ContentType)
	}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if res.StatusCode/100 != 2 {
		return "", fmt.Errorf("sms gateway: HTTP %d: %.200s", res.StatusCode, out)
	}
	if g.Success != "" && !strings.Contains(string(out), g.Success) {
		return "", fmt.Errorf("sms gateway: unexpected response: %.200s", out)
	}
	return "", nil
}

// Twilio sends through the Twilio Messages API.
type Twilio struct {
	sid, token, from string
	base             string // API root; overridden in tests
}

func NewTwilio(sid, token, from string) (*Twilio, error) {
	if sid == "" || token == "" || from == "" {
		return nil, errors.New("TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN and TWILIO_FROM are required")
	}
	return &Twilio{sid: sid, token: token, from: from, base: "https://api.twilio.com"}, nil
}

func (t *Twilio) Send(ctx context.Context, m Message) (string, error) {
	form := url.Values{"To": {m.To}, "From": {t.from}, "Body": {m.Text}}
	req, err := http.NewRequestWithContext(ctx, "POST",
		t.base+"/2010-04-01/Accounts/"+url.PathEscape(t.sid)+"/Messages.json", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(t.sid+":"+t.token)))
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var out struct {
		SID     string `json:"sid"`
		Message string `json:"message"`
	}
	json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&out)
	if res.StatusCode/100 != 2 {
		return "", fmt.Errorf("twilio: HTTP %d: %s", res.StatusCode, out.Message)
	}
	return out.SID, nil
}
