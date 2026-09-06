package auth

import (
	"context"
	"errors"

	"github.com/coreos/go-oidc/v3/oidc"
)

// OIDCVerifier uses the provider discovery document and its remote JWKS set.
// The go-oidc key set refreshes signing keys when the provider rotates them.
type OIDCVerifier struct{ verifier *oidc.IDTokenVerifier }

func NewOIDCVerifier(ctx context.Context, issuer, audience string) (*OIDCVerifier, error) {
	if issuer == "" || audience == "" {
		return nil, errors.New("OIDC issuer and audience are required")
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return &OIDCVerifier{verifier: provider.Verifier(&oidc.Config{ClientID: audience})}, nil
}

func (v *OIDCVerifier) Verify(ctx context.Context, raw string) (Principal, error) {
	token, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return Principal{}, err
	}
	var claims struct {
		Subject string `json:"sub"`
		Issuer  string `json:"iss"`
		Email   string `json:"email"`
	}
	if err := token.Claims(&claims); err != nil {
		return Principal{}, err
	}
	if claims.Subject == "" {
		return Principal{}, errors.New("subject is required")
	}
	return Principal{UserID: claims.Issuer + "|" + claims.Subject, Issuer: claims.Issuer, Subject: claims.Subject, Email: claims.Email}, nil
}
