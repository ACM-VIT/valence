package auth

import (
	"context"
	"errors"
	"testing"
)

func TestRequireRole(t *testing.T) {
	as := func(role string) context.Context {
		return withUser(context.Background(), User{ID: 1, Role: role})
	}
	cases := []struct {
		name string
		ctx  context.Context
		need string
		want error
	}{
		{"nobody logged in", context.Background(), "contestant", ErrUnauthenticated},
		{"contestant as contestant", as("contestant"), "contestant", nil},
		{"contestant needs setter", as("contestant"), "setter", ErrPermissionDenied},
		{"setter as setter", as("setter"), "setter", nil},
		{"admin passes setter check", as("admin"), "setter", nil},
		{"setter needs admin", as("setter"), "admin", ErrPermissionDenied},
		{"typo is denied", as("admin"), "admn", ErrPermissionDenied},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := RequireRole(c.ctx, c.need)
			if !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}
