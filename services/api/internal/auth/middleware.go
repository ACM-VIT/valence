package auth

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/ACM-VIT/valence/pkg/session"
)

// WithUser attaches the signed-in user to each request. A missing or expired
// session means "not logged in"; any other lookup failure is a server error,
// so a database outage doesn't look like being signed out.
func WithUser(sessions *Sessions, users *Users, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := sessions.Current(w, r)
		if errors.Is(err, session.ErrNotFound) {
			next.ServeHTTP(w, r)
			return
		}
		if err != nil {
			slog.Error("load session", "err", err)
			http.Error(w, "Something went wrong.", http.StatusInternalServerError)
			return
		}
		u, err := users.ByID(r.Context(), sess.UserID)
		if errors.Is(err, pgx.ErrNoRows) {
			next.ServeHTTP(w, r)
			return
		}
		if err != nil {
			slog.Error("load user", "user_id", sess.UserID, "err", err)
			http.Error(w, "Something went wrong.", http.StatusInternalServerError)
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
	})
}
