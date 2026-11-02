package sms

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

const bangla = "রহিম: ৳১০,০০০ বাকি & 50% off?"

func TestHTTPGatewayGET(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		io.WriteString(w, `{"response_code":202}`)
	}))
	defer srv.Close()
	g, err := NewHTTPGateway(HTTPGateway{URL: srv.URL + "/api?api_key=K&number={to_digits}&local={to_local}&message={message}", Success: "202"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Send(context.Background(), Message{To: "+8801712345678", Text: bangla}); err != nil {
		t.Fatal(err)
	}
	q := got.URL.Query()
	if got.Method != "GET" || q.Get("number") != "8801712345678" || q.Get("local") != "01712345678" || q.Get("message") != bangla || q.Get("api_key") != "K" {
		t.Fatalf("request: %s %v", got.Method, q)
	}
}

func TestHTTPGatewayPOSTBodies(t *testing.T) {
	var body []byte
	var ctype string
	status := 200
	reply := "OK"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		ctype = r.Header.Get("Content-Type")
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	msg := Message{To: "+8801712345678", Text: bangla}

	form, _ := NewHTTPGateway(HTTPGateway{URL: srv.URL, Body: "to={to}&sms={message}"})
	if _, err := form.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	v, _ := url.ParseQuery(string(body))
	if ctype != "application/x-www-form-urlencoded" || v.Get("to") != "+8801712345678" || v.Get("sms") != bangla {
		t.Fatalf("form body %q (%s)", body, ctype)
	}

	js, _ := NewHTTPGateway(HTTPGateway{URL: srv.URL, Body: `{"msisdn":"{to_local}","text":"{message}"}`, ContentType: "application/json"})
	if _, err := js.Send(context.Background(), Message{To: "+8801712345678", Text: "say \"hi\"\nnow"}); err != nil {
		t.Fatal(err)
	}
	var parsed map[string]string
	if err := json.Unmarshal(body, &parsed); err != nil || parsed["text"] != "say \"hi\"\nnow" || parsed["msisdn"] != "01712345678" {
		t.Fatalf("json body %q: %v", body, err)
	}

	status = 500
	if _, err := form.Send(context.Background(), msg); err == nil {
		t.Fatal("HTTP 500 accepted")
	}
	status, reply = 200, `{"error":"low balance"}`
	strict, _ := NewHTTPGateway(HTTPGateway{URL: srv.URL, Body: "x={message}", Success: `"success"`})
	if _, err := strict.Send(context.Background(), msg); err == nil {
		t.Fatal("200 without the success marker accepted")
	}
}

func TestHTTPGatewayConfig(t *testing.T) {
	if _, err := NewHTTPGateway(HTTPGateway{}); err == nil {
		t.Error("empty URL accepted")
	}
	if _, err := NewHTTPGateway(HTTPGateway{URL: "ftp://x"}); err == nil {
		t.Error("ftp URL accepted")
	}
	if _, err := NewHTTPGateway(HTTPGateway{URL: "https://x", Method: "PUT"}); err == nil {
		t.Error("PUT accepted")
	}
}

func TestTwilio(t *testing.T) {
	var got *http.Request
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		r.ParseForm()
		form = r.PostForm
		w.WriteHeader(201)
		io.WriteString(w, `{"sid":"SM123"}`)
	}))
	defer srv.Close()
	tw, err := NewTwilio("AC1", "tok", "+15550001111")
	if err != nil {
		t.Fatal(err)
	}
	tw.base = srv.URL
	id, err := tw.Send(context.Background(), Message{To: "+8801712345678", Text: bangla})
	if err != nil || id != "SM123" {
		t.Fatalf("send: %q %v", id, err)
	}
	user, pass, _ := got.BasicAuth()
	if got.URL.Path != "/2010-04-01/Accounts/AC1/Messages.json" || user != "AC1" || pass != "tok" ||
		form.Get("To") != "+8801712345678" || form.Get("From") != "+15550001111" || form.Get("Body") != bangla {
		t.Fatalf("request %s %v", got.URL.Path, form)
	}
	if _, err := NewTwilio("", "", ""); err == nil {
		t.Error("empty Twilio config accepted")
	}
}
