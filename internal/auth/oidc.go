package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// OIDCVerifier uses the provider discovery document and its remote JWKS set.
// The go-oidc key set refreshes signing keys when the provider rotates them.
type OIDCVerifier struct{ verifier *oidc.IDTokenVerifier }

func NewOIDCVerifier(ctx context.Context, issuer, audience string, backchannels ...string) (*OIDCVerifier, error) {
	if issuer == "" || audience == "" {
		return nil, errors.New("OIDC issuer and audience are required")
	}
	if len(backchannels) > 0 && backchannels[0] != "" && backchannels[0] != issuer {
		transport, err := newIssuerTransport(issuer, backchannels[0])
		if err != nil {
			return nil, err
		}
		ctx = oidc.ClientContext(ctx, &http.Client{Transport: transport, Timeout: 10 * time.Second})
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return &OIDCVerifier{verifier: provider.Verifier(&oidc.Config{ClientID: audience})}, nil
}

type issuerTransport struct {
	issuer      *url.URL
	backchannel *url.URL
	base        http.RoundTripper
}

func newIssuerTransport(issuer, backchannel string) (*issuerTransport, error) {
	publicURL, err := url.Parse(issuer)
	if err != nil {
		return nil, err
	}
	internalURL, err := url.Parse(backchannel)
	if err != nil {
		return nil, err
	}
	if publicURL.Scheme == "" || publicURL.Host == "" || internalURL.Scheme == "" || internalURL.Host == "" {
		return nil, errors.New("OIDC issuer and backchannel must be absolute URLs")
	}
	return &issuerTransport{issuer: publicURL, backchannel: internalURL, base: http.DefaultTransport}, nil
}

func (t *issuerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != t.issuer.Scheme || request.URL.Host != t.issuer.Host || !pathWithinBase(request.URL.Path, t.issuer.Path) {
		return nil, errors.New("OIDC backchannel refused request outside configured issuer")
	}
	clone := request.Clone(request.Context())
	clone.URL = cloneURL(request.URL)
	suffix := strings.TrimPrefix(request.URL.Path, strings.TrimSuffix(t.issuer.Path, "/"))
	clone.URL.Scheme = t.backchannel.Scheme
	clone.URL.Host = t.backchannel.Host
	clone.URL.Path = strings.TrimSuffix(t.backchannel.Path, "/") + "/" + strings.TrimPrefix(suffix, "/")
	return t.base.RoundTrip(clone)
}

func pathWithinBase(value, base string) bool {
	base = strings.TrimSuffix(base, "/")
	return base == "" || value == base || strings.HasPrefix(value, base+"/")
}

func cloneURL(value *url.URL) *url.URL {
	copy := *value
	return &copy
}

func (v *OIDCVerifier) Verify(ctx context.Context, raw string) (Principal, error) {
	token, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return Principal{}, err
	}
	var claims struct {
		Subject       string `json:"sub"`
		Issuer        string `json:"iss"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := token.Claims(&claims); err != nil {
		return Principal{}, err
	}
	if claims.Subject == "" {
		return Principal{}, errors.New("subject is required")
	}
	return Principal{UserID: claims.Issuer + "|" + claims.Subject, Issuer: claims.Issuer, Subject: claims.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified}, nil
}
