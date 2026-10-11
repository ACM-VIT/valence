package auth

import (
	"context"
	"crypto/rand"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testUsers(t *testing.T) (*Users, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	db, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return NewUsers(db), db
}

func deleteDevUser(t *testing.T, db *pgxpool.Pool, subject string) {
	t.Cleanup(func() {
		db.Exec(context.Background(),
			`DELETE FROM users WHERE id IN (SELECT user_id FROM identities WHERE provider = 'dev' AND subject = $1)`,
			subject)
	})
}

func TestLoginConcurrentFirstLogin(t *testing.T) {
	u, db := testUsers(t)
	ctx := context.Background()
	p := profile{Provider: "dev", Subject: "race-" + rand.Text(), Login: "race-" + rand.Text()[:6]}
	deleteDevUser(t, db, p.Subject)

	const n = 8
	ids := make([]int64, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids[i], errs[i] = u.Login(ctx, p)
		}()
	}
	wg.Wait()

	for i := range n {
		if errs[i] != nil {
			t.Fatalf("login %d: %v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Fatalf("login %d got user %d, login 0 got user %d", i, ids[i], ids[0])
		}
	}
}

func TestLoginTakenHandleGetsRandomSuffix(t *testing.T) {
	u, db := testUsers(t)
	ctx := context.Background()
	login := "taken-" + strings.ToLower(rand.Text()[:6])

	first := profile{Provider: "dev", Subject: rand.Text(), Login: login}
	second := profile{Provider: "dev", Subject: rand.Text(), Login: login}
	deleteDevUser(t, db, first.Subject)
	deleteDevUser(t, db, second.Subject)

	id1, err := u.Login(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := u.Login(ctx, second)
	if err != nil {
		t.Fatal(err)
	}

	var h1, h2 string
	db.QueryRow(ctx, `SELECT handle FROM users WHERE id = $1`, id1).Scan(&h1)
	db.QueryRow(ctx, `SELECT handle FROM users WHERE id = $1`, id2).Scan(&h2)
	if h1 != login {
		t.Fatalf("first handle = %q, want %q", h1, login)
	}
	if !strings.HasPrefix(h2, login+"-") || len(h2) != len(login)+9 {
		t.Fatalf("second handle = %q, want %q plus a random 8-char suffix", h2, login+"-")
	}
}
