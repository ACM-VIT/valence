package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresStore(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)

	var userID int64
	err = db.QueryRow(ctx,
		`INSERT INTO users (handle) VALUES ('session-test-' || floor(random()*1e6)::text) RETURNING id`).
		Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID) })

	s := NewPostgresStore(db)
	hash := sha256.Sum256([]byte(rand.Text()))

	err = s.Create(ctx, Session{IDHash: hash[:], UserID: userID, ExpiresAt: time.Now().Add(time.Hour), IP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, hash[:])
	if err != nil || got.UserID != userID || got.IP != "127.0.0.1" {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	if err := s.Delete(ctx, hash[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, hash[:]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after Delete, Get err = %v, want ErrNotFound", err)
	}

	expired := sha256.Sum256([]byte(rand.Text()))
	err = s.Create(ctx, Session{IDHash: expired[:], UserID: userID, ExpiresAt: time.Now().Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.DeleteExpired(ctx); err != nil || n < 1 {
		t.Fatalf("DeleteExpired = %d, %v, want at least 1", n, err)
	}
	var left int
	db.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE id_hash = $1`, expired[:]).Scan(&left)
	if left != 0 {
		t.Fatal("expired session still in the table after DeleteExpired")
	}
}
