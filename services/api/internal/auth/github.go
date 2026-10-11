package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"

	"github.com/AdityaVKochar/valence/pkg/config"
)

const (
	stateCookie    = "valence_oauth_state"
	verifierCookie = "valence_oauth_verifier"
	flowTTL        = 10 * time.Minute
)

type Github struct {
	oauth    *oauth2.Config
	users    *Users
	sessions *Sessions
	secure   bool
}

type githubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

func NewGithub(cfg config.Config, users *Users, sessions *Sessions) *Github {
	return &Github{
		oauth: &oauth2.Config{
			ClientID:     cfg.GitHubClientID,
			ClientSecret: cfg.GitHubClientSecret,
			Endpoint:     github.Endpoint,
			RedirectURL:  cfg.PublicAPIURL + "/auth/github/callback",
			Scopes:       []string{"read:user", "user:email"},
		},
		users:    users,
		sessions: sessions,
		secure:   !cfg.IsDev(),
	}
}

func (g *Github) Login(w http.ResponseWriter, r *http.Request) {
	state := rand.Text()
	verifier := oauth2.GenerateVerifier()

	g.setFlowCookie(w, stateCookie, state, flowTTL)
	g.setFlowCookie(w, verifierCookie, verifier, flowTTL)

	url := g.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, url, http.StatusFound)
}

func (g *Github) setFlowCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/auth/github",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   g.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (g *Github) Callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	state, err1 := r.Cookie(stateCookie)
	verifier, err2 := r.Cookie(verifierCookie)
	if err1 != nil || err2 != nil || subtle.ConstantTimeCompare([]byte(state.Value), []byte(q.Get("state"))) != 1 {
		http.Error(w, "Login expired or was not started here. Please try again", http.StatusBadRequest)
		return
	}
	g.setFlowCookie(w, stateCookie, "", -time.Second)
	g.setFlowCookie(w, verifierCookie, "", -time.Second)

	if q.Get("error") != "" {
		http.Error(w, "GitHub login was cancelled.", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	tok, err := g.oauth.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(verifier.Value))
	if err != nil {
		slog.Warn("GitHub code exchange failed", "err", err)
		http.Error(w, "Could not finish GitHub login. Please try again.", http.StatusBadGateway)
		return
	}

	gu, err := g.fetchUser(ctx, tok)
	if err != nil {
		slog.Warn("GitHub user lookup failed", "err", err)
		http.Error(w, "Could not read your GitHub profile. Please try again.", http.StatusBadGateway)
		return
	}

	userID, err := g.users.Login(ctx, profile{
		Provider:  "github",
		Subject:   strconv.FormatInt(gu.ID, 10),
		Login:     gu.Login,
		Name:      gu.Name,
		AvatarURL: gu.AvatarURL,
	})
	if err != nil {
		slog.Error("saving GitHub user failed", "err", err)
		http.Error(w, "Could not sign you in. Please try again.", http.StatusInternalServerError)
		return
	}

	if err := g.sessions.Start(w, r, userID); err != nil {
		slog.Error("starting session failed", "err", err)
		http.Error(w, "Could not sign you in. Please try again.", http.StatusInternalServerError)
		return
	}

	slog.Info("GitHub login", "user_id", userID, "github_id", gu.ID)
	http.Redirect(w, r, "/auth/me", http.StatusFound)
}

func (g *Github) fetchUser(ctx context.Context, tok *oauth2.Token) (githubUser, error) {
	client := g.oauth.Client(ctx, tok)
	resp, err := client.Get("https://api.github.com/user")
	if err != nil {
		return githubUser{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return githubUser{}, fmt.Errorf("GET /user: %s", resp.Status)
	}

	var gu githubUser
	if err := json.NewDecoder(resp.Body).Decode(&gu); err != nil {
		return githubUser{}, err
	}
	return gu, nil
}
