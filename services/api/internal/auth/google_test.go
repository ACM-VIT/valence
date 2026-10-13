package auth

import "testing"

func TestGoogleAllowed(t *testing.T) {
	vit := []string{"vitstudent.ac.in"}
	wildcard := []string{"*"}
	tests := []struct {
		name    string
		domains []string
		user    googleUser
		want    bool
	}{
		{"student account", vit, googleUser{Email: "a.b2024@vitstudent.ac.in", EmailVerified: true, HostedDomain: "vitstudent.ac.in"}, true},
		{"capital letters", vit, googleUser{Email: "A.B2024@VITstudent.ac.in", EmailVerified: true, HostedDomain: "VITstudent.ac.in"}, true},
		{"gmail", vit, googleUser{Email: "someone@gmail.com", EmailVerified: true}, false},
		{"other school", vit, googleUser{Email: "x@other.edu", EmailVerified: true, HostedDomain: "other.edu"}, false},
		{"unverified email", vit, googleUser{Email: "a@vitstudent.ac.in", EmailVerified: false, HostedDomain: "vitstudent.ac.in"}, false},
		{"lookalike domain", vit, googleUser{Email: "x@evilvitstudent.ac.in", EmailVerified: true, HostedDomain: "evilvitstudent.ac.in"}, false},
		{"email outside hd", vit, googleUser{Email: "x@gmail.com", EmailVerified: true, HostedDomain: "vitstudent.ac.in"}, false},
		{"wildcard allows gmail", wildcard, googleUser{Email: "someone@gmail.com", EmailVerified: true}, true},
		{"wildcard still needs verified email", wildcard, googleUser{Email: "someone@gmail.com"}, false},
	}

	for _, tt := range tests {
		g := &Google{allowedDomains: tt.domains}
		if got := g.allowed(tt.user); got != tt.want {
			t.Errorf("%s: allowed = %v, want %v", tt.name, got, tt.want)
		}
	}
}
