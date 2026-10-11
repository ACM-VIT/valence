package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/ACM-VIT/valence/pkg/config"
)

type Google struct {
	oauth          *oauth2.Config
	users          *Users
	sessions       *Sessions
	allowedDomains []string
	secure         bool
}

func NewGoogle(cfg config.Config, users *Users, sessions *Sessions) *Google {
	return &Google{
		oauth: &oauth2.Config{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			Endpoint:     google.Endpoint,
			RedirectURL:  cfg.PublicAPIURL + "/auth/google/callback",
			Scopes:       []string{"openid", "email", "profile"},
		},
		users:          users,
		sessions:       sessions,
		allowedDomains: cfg.GoogleAllowedDomains,
		secure:         !cfg.IsDev(),
	}
}

func (g *Google) Login(w http.ResponseWriter, r *http.Request) {
	state := rand.Text()
	verifier := oauth2.GenerateVerifier()

	g.setFlowCookie(w, stateCookie, state, flowTTL)
	g.setFlowCookie(w, verifierCookie, verifier, flowTTL)

	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier)}
	if len(g.allowedDomains) == 1 && g.allowedDomains[0] != "*" {
		opts = append(opts, oauth2.SetAuthURLParam("hd", g.allowedDomains[0]))
	}
	http.Redirect(w, r, g.oauth.AuthCodeURL(state, opts...), http.StatusFound)
}

type googleUser struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	HostedDomain  string `json:"hd"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

func (g *Google) Callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	state, err1 := r.Cookie(stateCookie)
	verifier, err2 := r.Cookie(verifierCookie)
	if err1 != nil || err2 != nil ||
		subtle.ConstantTimeCompare([]byte(state.Value), []byte(q.Get("state"))) != 1 {
		http.Error(w, "Login expired or was not started here. Please try again.", http.StatusBadRequest)
		return
	}
	g.setFlowCookie(w, stateCookie, "", -time.Second)
	g.setFlowCookie(w, verifierCookie, "", -time.Second)

	if q.Get("error") != "" {
		http.Error(w, "Google login was cancelled.", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	tok, err := g.oauth.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(verifier.Value))
	if err != nil {
		slog.Warn("Google code exchange failed", "err", err)
		http.Error(w, "Could not finish Google login. Please try again.", http.StatusBadGateway)
		return
	}

	gu, err := g.fetchUser(ctx, tok)
	if err != nil {
		slog.Warn("Google user lookup failed", "err", err)
		http.Error(w, "Could not read your Google profile. Please try again.", http.StatusBadGateway)
		return
	}

	if !g.allowed(gu) {
		slog.Info("Google login refused", "hd", gu.HostedDomain, "google_sub", gu.Sub)
		msg := fmt.Sprintf("Only accounts from %s can sign in to Valence. You used %s.",
			strings.Join(g.allowedDomains, " or "), gu.Email)
		http.Error(w, msg, http.StatusForbidden)
		return
	}

	userID, err := g.users.Login(ctx, profile{
		Provider:  "google",
		Subject:   gu.Sub,
		Login:     strings.Split(gu.Email, "@")[0],
		Name:      gu.Name,
		Email:     gu.Email,
		AvatarURL: gu.Picture,
	})
	if errors.Is(err, ErrDisabled) {
		http.Error(w, "This Valence account has been disabled.", http.StatusForbidden)
		return
	}
	if err != nil {
		slog.Error("saving Google user failed", "err", err)
		http.Error(w, "Could not sign you in. Please try again.", http.StatusInternalServerError)
		return
	}
	if err := g.sessions.Start(w, r, userID); err != nil {
		slog.Error("starting session failed", "err", err)
		http.Error(w, "Could not sign you in. Please try again.", http.StatusInternalServerError)
		return
	}

	slog.Info("Google login", "user_id", userID, "google_sub", gu.Sub)
	http.Redirect(w, r, "/auth/me", http.StatusFound)
}

func (g *Google) allowed(gu googleUser) bool {

	if slices.Contains(g.allowedDomains, "*") {
		return gu.EmailVerified
	}

	if !gu.EmailVerified || gu.HostedDomain == "" {
		return false
	}
	hd := strings.ToLower(gu.HostedDomain)
	if !strings.HasSuffix(strings.ToLower(gu.Email), "@"+hd) {
		return false
	}
	return slices.Contains(g.allowedDomains, hd)
}

func (g *Google) fetchUser(ctx context.Context, tok *oauth2.Token) (googleUser, error) {
	client := g.oauth.Client(ctx, tok)
	resp, err := client.Get("https://openidconnect.googleapis.com/v1/userinfo")
	if err != nil {
		return googleUser{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return googleUser{}, fmt.Errorf("GET userinfo: %s", resp.Status)
	}
	var gu googleUser
	if err := json.NewDecoder(resp.Body).Decode(&gu); err != nil {
		return googleUser{}, err
	}
	return gu, nil
}

func (g *Google) setFlowCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/auth/google",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   g.secure,
		SameSite: http.SameSiteLaxMode,
	})
}
