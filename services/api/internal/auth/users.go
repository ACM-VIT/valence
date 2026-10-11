package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Users struct {
	db *pgxpool.Pool
}

func NewUsers(db *pgxpool.Pool) *Users { return &Users{db: db} }

func (u *Users) GithubLogin(ctx context.Context, gu githubUser) (int64, error) {
	subject := strconv.FormatInt(gu.ID, 10)

	tx, err := u.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var userID int64
	err = tx.QueryRow(ctx,
		`UPDATE identities SET last_used_at = now()
		 WHERE provider = 'github' AND subject = $1
		 RETURNING user_id`, subject).Scan(&userID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		userID, err = createUser(ctx, tx, gu)
		if err != nil {
			return 0, err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO identities (provider, subject, user_id) VALUES ('github', $1, $2)`,
			subject, userID)
		if err != nil {
			return 0, err
		}
	case err != nil:
		return 0, err
	}

	_, err = tx.Exec(ctx, `UPDATE users SET last_Login_at = now() WHERE id = $1`, userID)
	if err != nil {
		return 0, err
	}
	return userID, tx.Commit(ctx)
}

func createUser(ctx context.Context, tx pgx.Tx, gu githubUser) (int64, error) {
	base := handleFrom(gu.Login)
	for i := 1; i <= 50; i++ {
		handle := base
		if i > 1 {
			suffix := "-" + strconv.Itoa(i)
			handle = base[:min(len(base), 32-len(suffix))] + suffix
		}
		var id int64
		err := tx.QueryRow(ctx,
			`INSERT INTO users (handle, display_name, avatar_url)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (lower(handle)) DO NOTHING
			 RETURNING id`,
			handle, displayName(gu), gu.AvatarURL).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // handle taken, try the next one
		}
		return id, err
	}

	return 0, fmt.Errorf("no free handle for Github login %q", gu.Login)
}

func handleFrom(login string) string {
	h := login[:min(len(login), 32)]
	for len(h) < 3 {
		h += "_"
	}
	return h
}

func displayName(gu githubUser) string {
	name := []rune(gu.Name)
	if len(name) == 0 {
		name = []rune(gu.Login)
	}
	return string(name[:min(len(name), 64)])
}
