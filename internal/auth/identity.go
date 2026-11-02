package auth

import "context"

// ExternalIdentity is a person as verified by an outside sign-in provider.
type ExternalIdentity struct {
	Subject       string // the provider's stable id for the person (not their email)
	Email         string
	EmailVerified bool
	Name          string
}

// IdentityProvider is an outside sign-in (OAuth 2 / OpenID Connect, authorization code + PKCE).
// The open-source server ships none; the hosted app plugs in Google through app.Options.
// The server owns state, nonce and PKCE generation and the redirect URL; a provider only
// builds the authorization URL and exchanges the code for a verified identity, checking
// that the identity is bound to nonce.
type IdentityProvider interface {
	// Name is a short lowercase id, also used in URLs: /api/auth/<name>/start.
	Name() string
	AuthCodeURL(state, nonce, pkceVerifier, redirectURL string) string
	Exchange(ctx context.Context, code, pkceVerifier, nonce, redirectURL string) (ExternalIdentity, error)
}
