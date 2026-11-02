package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// GoogleEndpoints are fixed rather than discovered, so startup never depends on a network call.
type GoogleEndpoints struct {
	AuthURL  string
	TokenURL string
	JWKSURL  string
	Issuers  []string
}

var GoogleProduction = GoogleEndpoints{
	AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL: "https://oauth2.googleapis.com/token",
	JWKSURL:  "https://www.googleapis.com/oauth2/v3/certs",
	// Google issues ID tokens with either form of the issuer.
	Issuers: []string{"https://accounts.google.com", "accounts.google.com"},
}

// Google signs users in with Google's OpenID Connect (authorization code + PKCE).
type Google struct {
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	issuers  []string
}

func NewGoogle(clientID, clientSecret, redirectURL string, ep GoogleEndpoints) *Google {
	keys := oidc.NewRemoteKeySet(context.Background(), ep.JWKSURL)
	return &Google{
		oauth: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     oauth2.Endpoint{AuthURL: ep.AuthURL, TokenURL: ep.TokenURL},
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		// The issuer is checked against both accepted forms in Exchange.
		verifier: oidc.NewVerifier("", keys, &oidc.Config{ClientID: clientID, SkipIssuerCheck: true}),
		issuers:  ep.Issuers,
	}
}

// AuthCodeURL is where the browser is sent to choose a Google account.
func (g *Google) AuthCodeURL(state, nonce, pkceVerifier string) string {
	return g.oauth.AuthCodeURL(state,
		oauth2.S256ChallengeOption(pkceVerifier),
		oidc.Nonce(nonce),
		oauth2.SetAuthURLParam("prompt", "select_account"))
}

type GoogleClaims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

// Exchange trades the authorization code for tokens and returns the verified ID token claims.
func (g *Google) Exchange(ctx context.Context, code, pkceVerifier, nonce string) (GoogleClaims, error) {
	token, err := g.oauth.Exchange(ctx, code, oauth2.VerifierOption(pkceVerifier))
	if err != nil {
		return GoogleClaims{}, fmt.Errorf("google token exchange: %w", err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return GoogleClaims{}, errors.New("google: no id_token in token response")
	}
	idToken, err := g.verifier.Verify(ctx, raw)
	if err != nil {
		return GoogleClaims{}, fmt.Errorf("google: verify id_token: %w", err)
	}
	if !slices.Contains(g.issuers, idToken.Issuer) {
		return GoogleClaims{}, fmt.Errorf("google: unexpected issuer %q", idToken.Issuer)
	}
	if idToken.Nonce != nonce {
		return GoogleClaims{}, errors.New("google: nonce mismatch")
	}
	var c GoogleClaims
	if err := idToken.Claims(&c); err != nil {
		return GoogleClaims{}, err
	}
	if c.Subject == "" {
		return GoogleClaims{}, errors.New("google: id_token has no subject")
	}
	return c, nil
}
