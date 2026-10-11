package config

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Env          string `env:"VALENCE_ENV" envDefault:"dev"`
	PublicAPIURL string `env:"PUBLIC_API_URL" envDefault:"http://localhost:8080"`
	WebOrigin    string `env:"WEB_ORIGIN" envDefault:"http://localhost:5173"`

	DatabaseURL string `env:"DATABASE_URL,notEmpty"`

	GitHubClientID     string `env:"GITHUB_CLIENT_ID"`
	GitHubClientSecret string `env:"GITHUB_CLIENT_SECRET"`
}

func Load() (Config, error) {
	var c Config

	if err := env.Parse(&c); err != nil {
		return Config{}, err
	}

	return c, c.validate()
}

func (c Config) IsDev() bool { return c.Env == "dev" }

func (c Config) GitHubEnabled() bool { return c.GitHubClientID != "" }

func (c Config) validate() error {
	var errs []error
	if c.Env != "dev" && c.Env != "prod" {
		errs = append(errs, fmt.Errorf("VALENCE_ENV must be dev or prod, got %q", c.Env))
	}
	for name, v := range map[string]string{"PUBLIC_API_URL": c.PublicAPIURL, "WEB_ORIGIN": c.WebOrigin} {
		if u, err := url.Parse(v); err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, fmt.Errorf("%s must be a full URL like http://localhost:8080, got %q", name, v))
		}
	}
	if (c.GitHubClientID == "") != (c.GitHubClientSecret == "") {
		errs = append(errs, errors.New("set both GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET, or neither"))
	}
	return errors.Join(errs...)
}
