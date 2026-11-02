package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/masiur/taka-poisa/internal/auth"
	"github.com/masiur/taka-poisa/internal/db"
)

// newTestServer runs the API against a fresh database created on TEST_DATABASE_URL.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newTestServerWith(t, nil)
}

func newTestServerWith(t *testing.T, google *auth.Google) *httptest.Server {
	t.Helper()
	srv, _ := newTestEnv(t, google)
	return srv
}

// newTestEnv also returns the pool, for tests that need to arrange data directly.
func newTestEnv(t *testing.T, google *auth.Google) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("tp_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		admin.Close(ctx)
	})

	cfg, err := pgxpool.ParseConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(New(Config{
		DB:     pool,
		Static: fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}},
		Google: google,
		Log:    slog.New(slog.DiscardHandler),
	}))
	t.Cleanup(srv.Close)
	return srv, pool
}

func newClient(t *testing.T) *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

func call(t *testing.T, c *http.Client, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var r *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, url, r)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

var owner = map[string]any{
	"name": "Masiur", "email": "  Owner@Example.com ", "password": "s3cret-pass",
	"account_name": "My ledger", "locale": "en",
}

func TestSetupLoginLogout(t *testing.T) {
	srv := newTestServer(t)
	c := newClient(t)

	if code, body := call(t, c, "GET", srv.URL+"/api/setup", nil); code != 200 || body["needed"] != true {
		t.Fatalf("setup needed: %d %v", code, body)
	}
	if code, _ := call(t, c, "GET", srv.URL+"/api/me", nil); code != 401 {
		t.Fatalf("me before setup: %d", code)
	}

	bad := map[string]any{"name": "", "email": "nope", "password": "short", "account_name": "x", "locale": "en"}
	code, body := call(t, c, "POST", srv.URL+"/api/setup", bad)
	fields, _ := body["fields"].(map[string]any)
	if code != 422 || fields["name"] != "required" || fields["email"] != "invalid" || fields["password"] != "too_short" {
		t.Fatalf("invalid setup: %d %v", code, body)
	}

	if code, _ := call(t, c, "POST", srv.URL+"/api/setup", owner); code != 204 {
		t.Fatalf("setup: %d", code)
	}
	code, body = call(t, c, "GET", srv.URL+"/api/me", nil)
	user, _ := body["user"].(map[string]any)
	acct, _ := body["account"].(map[string]any)
	if code != 200 || user["email"] != "owner@example.com" || acct["role"] != "owner" {
		t.Fatalf("me after setup: %d %v", code, body)
	}

	if code, _ := call(t, newClient(t), "POST", srv.URL+"/api/setup", owner); code != 409 {
		t.Fatalf("second setup: %d", code)
	}
	if _, body := call(t, c, "GET", srv.URL+"/api/setup", nil); body["needed"] != false {
		t.Fatalf("setup still needed: %v", body)
	}

	if code, _ := call(t, c, "POST", srv.URL+"/api/logout", nil); code != 204 {
		t.Fatalf("logout: %d", code)
	}
	if code, _ := call(t, c, "GET", srv.URL+"/api/me", nil); code != 401 {
		t.Fatalf("me after logout: %d", code)
	}

	if code, body := call(t, c, "POST", srv.URL+"/api/login", map[string]any{"email": "owner@example.com", "password": "wrong-pass"}); code != 401 || body["error"] != "invalid_credentials" {
		t.Fatalf("wrong password: %d %v", code, body)
	}
	if code, _ := call(t, c, "POST", srv.URL+"/api/login", map[string]any{"email": "nobody@example.com", "password": "whatever1"}); code != 401 {
		t.Fatalf("unknown user: %d", code)
	}
	if code, _ := call(t, c, "POST", srv.URL+"/api/login", map[string]any{"email": "OWNER@example.com", "password": "s3cret-pass"}); code != 204 {
		t.Fatalf("login: %d", code)
	}
	if code, _ := call(t, c, "GET", srv.URL+"/api/me", nil); code != 200 {
		t.Fatalf("me after login: %d", code)
	}
}

func TestConcurrentSetupCreatesOneOwner(t *testing.T) {
	srv := newTestServer(t)
	var wg sync.WaitGroup
	codes := make(chan int, 5)
	for i := range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			in := map[string]any{"name": "U", "email": fmt.Sprintf("u%d@example.com", i), "password": "password1", "account_name": "L", "locale": "bn"}
			code, _ := call(t, newClient(t), "POST", srv.URL+"/api/setup", in)
			codes <- code
		}()
	}
	wg.Wait()
	close(codes)
	created := 0
	for code := range codes {
		switch code {
		case 204:
			created++
		case 409:
		default:
			t.Errorf("unexpected status %d", code)
		}
	}
	if created != 1 {
		t.Fatalf("created %d owners, want 1", created)
	}
}

func TestAccountSettings(t *testing.T) {
	srv := newTestServer(t)
	c := newClient(t)
	call(t, c, "POST", srv.URL+"/api/setup", owner)

	code, body := call(t, c, "GET", srv.URL+"/api/account", nil)
	if code != 200 || body["currency"] != "BDT" || body["timezone"] != "Asia/Dhaka" || body["send_hour"] != 9.0 || body["name"] != "My ledger" {
		t.Fatalf("defaults: %d %v", code, body)
	}

	bad := map[string]any{"name": "Shop", "currency": "BDT", "timezone": "Nowhere/City", "send_hour": 9, "locale": "bn"}
	if code, body := call(t, c, "PUT", srv.URL+"/api/account", bad); code != 422 {
		t.Fatalf("bad timezone: %d %v", code, body)
	}
	if code, _ := call(t, c, "PUT", srv.URL+"/api/account", map[string]any{"name": "Shop", "extra": 1}); code != 400 {
		t.Fatalf("unknown field: %d", code)
	}

	good := map[string]any{"name": "Rahim Store", "currency": "usd", "timezone": "UTC", "send_hour": 18, "locale": "en"}
	if code, _ := call(t, c, "PUT", srv.URL+"/api/account", good); code != 204 {
		t.Fatalf("update: %d", code)
	}
	_, body = call(t, c, "GET", srv.URL+"/api/account", nil)
	if body["name"] != "Rahim Store" || body["currency"] != "USD" || body["send_hour"] != 18.0 {
		t.Fatalf("after update: %v", body)
	}

	if code, _ := call(t, newClient(t), "PUT", srv.URL+"/api/account", good); code != 401 {
		t.Fatalf("anonymous update: %d", code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	srv := newTestServer(t)
	c := newClient(t)
	call(t, c, "POST", srv.URL+"/api/setup", owner)
	creds := map[string]any{"email": "owner@example.com", "password": "wrong-pass"}
	for i := range 10 {
		if code, _ := call(t, c, "POST", srv.URL+"/api/login", creds); code != 401 {
			t.Fatalf("attempt %d: %d", i+1, code)
		}
	}
	// Even the right password is refused while blocked.
	creds["password"] = "s3cret-pass"
	if code, _ := call(t, c, "POST", srv.URL+"/api/login", creds); code != 429 {
		t.Fatalf("11th attempt: %d", code)
	}
}

func TestCrossOriginPostRejected(t *testing.T) {
	srv := newTestServer(t)
	req, _ := http.NewRequest("POST", srv.URL+"/api/login", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("cross-origin POST: %d", res.StatusCode)
	}
}

func TestSPAFallbackAndAPINotFound(t *testing.T) {
	srv := newTestServer(t)
	res, err := http.Get(srv.URL + "/settings")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("spa fallback: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if code, body := call(t, newClient(t), "GET", srv.URL+"/api/nope", nil); code != 404 || body["error"] != "not_found" {
		t.Fatalf("api 404: %d %v", code, body)
	}
}
