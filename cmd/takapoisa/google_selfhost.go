//go:build !hosted

package main

import (
	"net/url"

	"github.com/masiur/taka-poisa/internal/auth"
)

// googleFromEnv: Google sign-in is a hosted-app feature, so self-hosted builds never enable it,
// whatever GOOGLE_* variables are set. Owners sign in with email and password.
func googleFromEnv(*url.URL) (*auth.Google, error) {
	return nil, nil
}
