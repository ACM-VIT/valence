package config

import (
	"os"
	"slices"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/test")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsDev() || c.GitHubEnabled() {
		t.Fatalf("unexpected defaults: env=%q github=%v", c.Env, c.GitHubEnabled())
	}
}

func TestLoadRejectsHalfGitHubConfig(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("GITHUB_CLIENT_ID", "abc")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error when only the client ID is set")
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error when DATABASE_URL is missing")
	}
}

func TestLoadGoogleDomains(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("GOOGLE_CLIENT_ID", "id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "secret")
	t.Setenv("GOOGLE_ALLOWED_DOMAINS", " VITstudent.ac.in, ,vit.ac.in ")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"vitstudent.ac.in", "vit.ac.in"}
	if !slices.Equal(c.GoogleAllowedDomains, want) {
		t.Fatalf("domains = %q, want %q", c.GoogleAllowedDomains, want)
	}
}

func TestLoadGoogleNeedsDomains(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("GOOGLE_CLIENT_ID", "id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "secret")
	t.Setenv("GOOGLE_ALLOWED_DOMAINS", " , ")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error when Google is on but no domains are allowed")
	}
}

func TestTrailingSlashTrimmed(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("PUBLIC_API_URL", "http://localhost:8080/")
	t.Setenv("WEB_ORIGIN", "http://localhost:3000//")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicAPIURL != "http://localhost:8080" || c.WebOrigin != "http://localhost:3000" {
		t.Fatalf("not trimmed: api=%q web=%q", c.PublicAPIURL, c.WebOrigin)
	}
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"VALENCE_ENV", "DEV_LOGIN", "DATABASE_URL", "PUBLIC_API_URL", "WEB_ORIGIN",
		"GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET",
		"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GOOGLE_ALLOWED_DOMAINS",
	} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}
