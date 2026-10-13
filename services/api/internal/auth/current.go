package auth

import (
	"context"
	"errors"
	"net/http"
)

type User struct {
	ID          int64
	Handle      string
	DisplayName string
	Role        string
}

var (
	ErrUnauthenticated  = errors.New("auth: not logged in")
	ErrPermissionDenied = errors.New("auth: not allowed")
)

type userKey struct{}

func withUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userKey{}).(User)
	return u, ok
}

func RequireUser(ctx context.Context) (User, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return User{}, ErrUnauthenticated
	}
	return u, nil
}

var roleRank = map[string]int{"contestant": 1, "setter": 2, "admin": 3}

func RequireRole(ctx context.Context, role string) (User, error) {
	u, err := RequireUser(ctx)
	if err != nil {
		return User{}, err
	}
	need, ok := roleRank[role]
	if !ok || roleRank[u.Role] < need {
		return User{}, ErrPermissionDenied
	}
	return u, nil
}

func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		http.Error(w, "Not logged in.", http.StatusUnauthorized)
	case errors.Is(err, ErrPermissionDenied):
		http.Error(w, "You dont have access to this.", http.StatusForbidden)
	default:
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}

func NewContext(ctx context.Context, u User) context.Context {
	return withUser(ctx, u)
}
