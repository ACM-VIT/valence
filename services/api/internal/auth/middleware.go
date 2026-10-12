package auth

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ACM-VIT/valence/pkg/session"
	"github.com/jackc/pgx/v5"
)

func WithUser(sessions *Sessions, users *Users, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := sessions.Current(w, r)
		if err != nil {
			if !errors.Is(err, session.ErrNotFound) {
				slog.Error("load session", "err", err)
			}
			next.ServeHTTP(w, r)
			return
		}
		u, err := users.ByID(r.Context(), sess.UserID)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Error("load user", "user_id", sess.UserID, "err", err)
			}
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
	})
}
