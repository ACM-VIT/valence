package session

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	db *pgxpool.Pool
}

func NewPostgresStore(db *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{db: db}
}

var _ Store = (*PostgresStore)(nil)

func (s *PostgresStore) Create(ctx context.Context, sess Session) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO sessions (id_hash, user_id, expires_at, user_agent, ip)
		 VALUES ($1, $2, $3, $4, NULLIF($5, '')::inet)`,
		sess.IDHash, sess.UserID, sess.ExpiresAt, sess.UserAgent, sess.IP)
	return err
}

func (s *PostgresStore) Get(ctx context.Context, idHash []byte) (Session, error) {
	sess := Session{IDHash: idHash}
	err := s.db.QueryRow(ctx,
		`SELECT s.user_id, s.created_at, s.expires_at, s.user_agent, coalesce(host(s.ip), '')
		FROM sessions s
	   	JOIN users u ON u.id = s.user_id
		WHERE s.id_hash = $1
	    AND s.expires_at > now()
	    AND u.disabled_at IS NULL`, idHash).
		Scan(&sess.UserID, &sess.CreatedAt, &sess.ExpiresAt, &sess.UserAgent, &sess.IP)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return sess, err
}

func (s *PostgresStore) Touch(ctx context.Context, idHash []byte, expiresAt time.Time) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE sessions SET expires_at = $2 WHERE id_hash = $1 AND expires_at > now()`,
		idHash, expiresAt)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *PostgresStore) Delete(ctx context.Context, idHash []byte) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE id_hash = $1`, idHash)
	return err
}

func (s *PostgresStore) DeleteForUser(ctx context.Context, userID int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

// DeleteExpired removes sessions past their expiry and reports how many it removed.
func (s *PostgresStore) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	return tag.RowsAffected(), err
}
