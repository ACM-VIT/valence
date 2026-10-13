package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ACM-VIT/valence/pkg/config"
	"github.com/ACM-VIT/valence/pkg/session"
)

func TestDevLoginOnlyWhenEnabled(t *testing.T) {
	cases := []struct {
		env      string
		devLogin bool
		want     bool
	}{
		{"dev", true, true},
		{"dev", false, false}, // forgot DEV_LOGIN
		{"prod", true, false}, // DEV_LOGIN leaked into prod
		{"prod", false, false},
	}
	for _, c := range cases {
		mux := http.NewServeMux()
		RegisterDev(mux, config.Config{Env: c.env, DevLogin: c.devLogin}, nil, nil)

		_, pattern := mux.Handler(httptest.NewRequest("GET", "/auth/dev?user=alice", nil))
		if got := pattern != ""; got != c.want {
			t.Errorf("env=%s DEV_LOGIN=%v: registered=%v, want %v", c.env, c.devLogin, got, c.want)
		}
	}
}

// get fetches url and returns the status code and body.
func get(t *testing.T, c *http.Client, url string) (int, string) {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestDevLoginSignsIn(t *testing.T) {
	users, pool := testUsers(t)
	cfg := config.Config{Env: "dev", DevLogin: true}
	sessions := NewSessions(session.NewPostgresStore(pool), cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/me", sessions.Me)
	RegisterDev(mux, cfg, users, sessions)
	srv := httptest.NewServer(WithUser(sessions, users, mux))
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	name := "dev-" + strings.ToLower(rand.Text()[:8])
	deleteDevUser(t, pool, name)

	// Signing in redirects to /auth/me, which the client follows with the new cookie.
	code, body := get(t, client, srv.URL+"/auth/dev?user="+name+"&role=setter")
	if code != http.StatusOK || !strings.Contains(body, "@"+name) || !strings.Contains(body, "role setter") {
		t.Fatalf("after dev login: %d %q, want 200 as @%s with role setter", code, body, name)
	}

	// Signing in again with another role changes it.
	if _, body = get(t, client, srv.URL+"/auth/dev?user="+name+"&role=admin"); !strings.Contains(body, "role admin") {
		t.Fatalf("after second dev login: %q, want role admin", body)
	}

	// A disabled account loses its session and cannot start a new one.
	_, err := pool.Exec(context.Background(),
		`UPDATE users SET disabled_at = now()
		 WHERE id IN (SELECT user_id FROM identities WHERE provider = 'dev' AND subject = $1)`, name)
	if err != nil {
		t.Fatal(err)
	}
	if code, _ = get(t, client, srv.URL+"/auth/me"); code != http.StatusUnauthorized {
		t.Errorf("/auth/me for a disabled account: %d, want 401", code)
	}
	if code, _ = get(t, client, srv.URL+"/auth/dev?user="+name); code != http.StatusForbidden {
		t.Errorf("dev login for a disabled account: %d, want 403", code)
	}
}

// brokenStore fails every lookup, like a database that is down.
type brokenStore struct{ session.Store }

func (brokenStore) Get(context.Context, []byte) (session.Session, error) {
	return session.Session{}, errors.New("connection refused")
}

func (brokenStore) Touch(context.Context, []byte, time.Time) error {
	return errors.New("connection refused")
}

func TestWithUserDatabaseErrorIsServerError(t *testing.T) {
	sessions := NewSessions(brokenStore{}, config.Config{Env: "dev"})
	called := false
	h := WithUser(sessions, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	// No cookie: the request goes through as anonymous.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/auth/me", nil))
	if !called || rec.Code != http.StatusOK {
		t.Fatalf("without a cookie: called=%v status=%d, want the handler to run", called, rec.Code)
	}

	// A cookie the store can't look up: 500, and the handler never runs.
	called = false
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "anything"})
	h.ServeHTTP(rec, req)
	if called || rec.Code != http.StatusInternalServerError {
		t.Fatalf("store error: called=%v status=%d, want 500 without calling the handler", called, rec.Code)
	}
}
