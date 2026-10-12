package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/ACM-VIT/valence/pkg/config"
	"github.com/ACM-VIT/valence/pkg/session"
)

const (
	sessionCookie = "valence_session"
	sessionTTL    = 30 * 24 * time.Hour
)

type Sessions struct {
	store  session.Store
	secure bool
}

func NewSessions(store session.Store, cfg config.Config) *Sessions {
	return &Sessions{store: store, secure: !cfg.IsDev()}
}

func (s *Sessions) Start(w http.ResponseWriter, r *http.Request, userID int64) error {
	raw := make([]byte, 32)
	rand.Read(raw)
	id := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(id))
	expires := time.Now().Add(sessionTTL)

	err := s.store.Create(r.Context(), session.Session{
		IDHash:    hash[:],
		UserID:    userID,
		ExpiresAt: expires,
		UserAgent: r.UserAgent(),
		IP:        clientIP(r),
	})
	if err != nil {
		return err
	}

	s.setCookie(w, id, int(sessionTTL.Seconds()))
	return nil
}

func (s *Sessions) Current(w http.ResponseWriter, r *http.Request) (session.Session, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return session.Session{}, session.ErrNotFound
	}
	hash := sha256.Sum256([]byte(c.Value))
	sess, err := s.store.Get(r.Context(), hash[:])
	if err != nil {
		return session.Session{}, err
	}

	if time.Until(sess.ExpiresAt) < sessionTTL/2 {
		expires := time.Now().Add(sessionTTL)
		if err := s.store.Touch(r.Context(), sess.IDHash, expires); err != nil {
			slog.Warn("extending session failed", "err", err)
		} else {
			sess.ExpiresAt = expires
			s.setCookie(w, c.Value, int(sessionTTL.Seconds()))
		}
	}
	return sess, nil
}

func (s *Sessions) Me(w http.ResponseWriter, r *http.Request) {
	u, err := RequireUser(r.Context())
	if err != nil {
		writeAuthError(w, err)
		return
	}
	fmt.Fprintf(w, "You are %s (@%s), Valence user #%d, role %s.\n", u.DisplayName, u.Handle, u.ID, u.Role)
}

func (s *Sessions) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		hash := sha256.Sum256([]byte(c.Value))
		if err := s.store.Delete(r.Context(), hash[:]); err != nil {
			slog.Error("deleting session failed", "err", err)
			http.Error(w, "Could not log you out. Please try again.", http.StatusInternalServerError)
			return
		}
	}
	s.setCookie(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Sessions) setCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	return host
}
