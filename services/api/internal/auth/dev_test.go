package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ACM-VIT/valence/pkg/config"
)

func TestDevLoginOnlyWhenEnabled(t *testing.T) {
	cases := []struct {
		env      string
		devLogin bool
		want     bool
	}{
		{"dev", true, true},
		{"dev", false, false}, // forgot DEV_LOGIN
		{"prod", true, false}, // DEV_LOGIN leaked into prod
		{"prod", false, false},
	}
	for _, c := range cases {
		mux := http.NewServeMux()
		RegisterDev(mux, config.Config{Env: c.env, DevLogin: c.devLogin}, nil, nil)

		_, pattern := mux.Handler(httptest.NewRequest("GET", "/auth/dev?user=alice", nil))
		if got := pattern != ""; got != c.want {
			t.Errorf("env=%s DEV_LOGIN=%v: registered=%v, want %v", c.env, c.devLogin, got, c.want)
		}
	}
}
