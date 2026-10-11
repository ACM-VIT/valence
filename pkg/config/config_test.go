package config

import (
	"slices"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
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
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("GITHUB_CLIENT_ID", "abc")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error when only the client ID is set")
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error when DATABASE_URL is missing")
	}
}

func TestLoadGoogleDomains(t *testing.T) {
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
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("GOOGLE_CLIENT_ID", "id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "secret")
	t.Setenv("GOOGLE_ALLOWED_DOMAINS", " , ")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error when Google is on but no domains are allowed")
	}
}
