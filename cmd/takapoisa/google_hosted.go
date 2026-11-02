//go:build hosted

package main

import (
	"errors"
	"net/url"
	"os"

	"github.com/masiur/taka-poisa/internal/auth"
)

// googleFromEnv enables Google sign-in on the hosted app (built with -tags hosted) when
// GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET are set.
func googleFromEnv(baseURL *url.URL) (*auth.Google, error) {
	id, secret := os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET")
	if (id == "") != (secret == "") {
		return nil, errors.New("set both GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET, or neither")
	}
	if id == "" {
		return nil, nil
	}
	return auth.NewGoogle(id, secret, baseURL.String()+"/api/auth/google/callback", auth.GoogleProduction), nil
}
