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

	"github.com/AdityaVKochar/valence/pkg/config"
	"github.com/AdityaVKochar/valence/pkg/server"
	"github.com/AdityaVKochar/valence/pkg/session"
	"github.com/AdityaVKochar/valence/pkg/version"
	"github.com/AdityaVKochar/valence/services/api/internal/auth"
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

	mux := http.NewServeMux()
	server.Health(mux, nil)

	sessions := auth.NewSessions(session.NewPostgresStore(pool), cfg)
	mux.HandleFunc("GET /auth/me", sessions.Me)
	mux.HandleFunc("POST /auth/logout", sessions.Logout)

	if cfg.GitHubEnabled() {
		gh := auth.NewGithub(cfg, auth.NewUsers(pool), sessions)
		mux.HandleFunc("GET /auth/github/login", gh.Login)
		mux.HandleFunc("GET /auth/github/callback", gh.Callback)
	}

	slog.Info("API starting",
		"version", version.String(),
		"env", cfg.Env,
		"github_login", cfg.GitHubEnabled(),
	)
	return server.Run(ctx, *addr, mux, 15*time.Second)
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
