package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ACM-VIT/valence/pkg/config"
	"github.com/ACM-VIT/valence/pkg/server"
	"github.com/ACM-VIT/valence/pkg/session"
	"github.com/ACM-VIT/valence/pkg/version"
	"github.com/ACM-VIT/valence/services/api/internal/auth"
)

func main() {
	if err := run(); err != nil {
		slog.Error("API exited", "err", err)
		os.Exit(1)
	}
}

func run() error {

	showVersion := flag.Bool("version", false, "Print the version and exit")
	addr := flag.String("addr", envOr("API_ADDR", ":8080"), "HTTP listen address")
	flag.Parse()
	if *showVersion {
		fmt.Println("API", version.String())
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("database: %w", err)
	}

	// Health checks sit outside WithUser so they never wait on a session lookup.
	root := http.NewServeMux()
	server.Health(root, pool.Ping)
	mux := http.NewServeMux()

	store := session.NewPostgresStore(pool)
	go pruneSessions(ctx, store, time.Hour)
	sessions := auth.NewSessions(store, cfg)
	mux.HandleFunc("GET /auth/me", sessions.Me)
	mux.HandleFunc("POST /auth/logout", sessions.Logout)

	users := auth.NewUsers(pool)
	auth.RegisterDev(mux, cfg, users, sessions)

	if cfg.GitHubEnabled() {
		gh := auth.NewGithub(cfg, users, sessions)
		mux.HandleFunc("GET /auth/github/login", gh.Login)
		mux.HandleFunc("GET /auth/github/callback", gh.Callback)
	}

	if cfg.GoogleEnabled() {
		gg := auth.NewGoogle(cfg, users, sessions)
		mux.HandleFunc("GET /auth/google/login", gg.Login)
		mux.HandleFunc("GET /auth/google/callback", gg.Callback)
	}

	slog.Info("API starting",
		"version", version.String(),
		"env", cfg.Env,
		"github_login", cfg.GitHubEnabled(),
		"google_login", cfg.GoogleEnabled(),
	)
	root.Handle("/", auth.WithUser(sessions, users, mux))
	return server.Run(ctx, *addr, root, 15*time.Second)
}

// pruneSessions deletes expired sessions every interval until ctx is cancelled.
func pruneSessions(ctx context.Context, store *session.PostgresStore, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := store.DeleteExpired(ctx)
			if err != nil {
				slog.Warn("pruning expired sessions failed", "err", err)
				continue
			}
			if n > 0 {
				slog.Info("pruned expired sessions", "count", n)
			}
		}
	}
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
