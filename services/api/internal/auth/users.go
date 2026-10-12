package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type profile struct {
	Provider  string
	Subject   string
	Login     string
	Name      string
	Email     string
	AvatarURL string
}

type Users struct {
	db *pgxpool.Pool
}

var ErrDisabled = errors.New("auth: account disabled")

func NewUsers(db *pgxpool.Pool) *Users { return &Users{db: db} }

var errRaced = errors.New("auth: identity created concurrently")

func (u *Users) Login(ctx context.Context, p profile) (int64, error) {
	id, err := u.login(ctx, p)
	if errors.Is(err, errRaced) {
		id, err = u.login(ctx, p)
	}
	return id, err
}

func (u *Users) login(ctx context.Context, p profile) (int64, error) {
	tx, err := u.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var userID int64
	err = tx.QueryRow(ctx,
		`UPDATE identities SET last_used_at = now()
		 WHERE provider = $1 AND subject = $2
		 RETURNING user_id`, p.Provider, p.Subject).Scan(&userID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		userID, err = createUser(ctx, tx, p)
		if err != nil {
			return 0, err
		}
		tag, err := tx.Exec(ctx,
			`INSERT INTO identities (provider, subject, user_id, email)
			 VALUES ($1, $2, $3, NULLIF($4, ''))
			 ON CONFLICT (provider, subject) DO NOTHING`,
			p.Provider, p.Subject, userID, p.Email)
		if err != nil {
			return 0, err
		}
		if tag.RowsAffected() == 0 {
			return 0, errRaced
		}
	case err != nil:
		return 0, err
	}

	var disabled bool
	err = tx.QueryRow(ctx,
		`UPDATE users SET last_login_at = now() WHERE id = $1
		RETURNING disabled_at IS NOT NULL`, userID).Scan(&disabled)
	if err != nil {
		return 0, err
	}
	if disabled {
		return 0, ErrDisabled
	}

	return userID, tx.Commit(ctx)
}

func createUser(ctx context.Context, tx pgx.Tx, p profile) (int64, error) {
	base := handleFrom(p.Login)
	for i := range 10 {
		handle := base
		if i > 0 {
			// 8 random base32 chars (40 bits), so a taken name can't block signup
			suffix := "-" + strings.ToLower(rand.Text()[:8])
			handle = base[:min(len(base), 32-len(suffix))] + suffix
		}
		var id int64
		err := tx.QueryRow(ctx,
			`INSERT INTO users (handle, display_name, email, avatar_url)
			 VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''))
			 ON CONFLICT (lower(handle)) DO NOTHING
			 RETURNING id`,
			handle, displayName(p), p.Email, p.AvatarURL).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		return id, err
	}
	return 0, fmt.Errorf("no free handle for %s login %q", p.Provider, p.Login)
}

func handleFrom(login string) string {
	h := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, login)
	h = h[:min(len(h), 32)]
	for len(h) < 3 {
		h += "_"
	}
	return h
}

func displayName(p profile) string {
	name := []rune(p.Name)
	if len(name) == 0 {
		name = []rune(p.Login)
	}
	return string(name[:min(len(name), 64)])
}

func (u *Users) ByID(ctx context.Context, id int64) (User, error) {
	var usr User
	err := u.db.QueryRow(ctx,
		`SELECT id, handle, display_name, role::text
		   FROM users
		  WHERE id = $1 AND disabled_at IS NULL`, id).
		Scan(&usr.ID, &usr.Handle, &usr.DisplayName, &usr.Role)
	return usr, err
}

func (u *Users) SetRole(ctx context.Context, id int64, role string) error {
	_, err := u.db.Exec(ctx,
		`UPDATE users SET role = $2::user_role WHERE id = $1`, id, role)
	return err
}