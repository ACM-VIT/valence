package auth

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ACM-VIT/valence/pkg/config"
)

type dev struct {
	users    *Users
	sessions *Sessions
}

func RegisterDev(mux *http.ServeMux, cfg config.Config, users *Users, sessions *Sessions) {
	if !cfg.IsDev() || !cfg.DevLogin {
		return
	}
	slog.Warn("dev login enabled: anyone can sign in as any user")
	d := &dev{users: users, sessions: sessions}
	mux.HandleFunc("GET /auth/dev", d.login)
}

func (d *dev) login(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("user")
	role := r.URL.Query().Get("role")
	if role == "" {
		role = "contestant"
	}
	if name == "" {
		http.Error(w, "Add ?user=NAME (and optionally &role=setter or admin).", http.StatusBadRequest)
		return
	}
	if _, ok := roleRank[role]; !ok {
		http.Error(w, "Unknown role. Use contestant, setter or admin.", http.StatusBadRequest)
		return
	}

	id, err := d.users.Login(r.Context(), profile{
		Provider: "dev", Subject: name, Login: name, Name: name,
	})
	if errors.Is(err, ErrDisabled) {
		http.Error(w, "This Valence account has been disabled.", http.StatusForbidden)
		return
	}
	if err == nil {
		err = d.users.SetRole(r.Context(), id, role)
	}
	if err != nil {
		slog.Error("dev login", "user", name, "err", err)
		http.Error(w, "Could not sign you in.", http.StatusInternalServerError)
		return
	}
	if err := d.sessions.Start(w, r, id); err != nil {
		slog.Error("starting session failed", "err", err)
		http.Error(w, "Could not sign you in. Please try again.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/auth/me", http.StatusSeeOther)
}
