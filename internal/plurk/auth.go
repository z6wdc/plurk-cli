package plurk

import (
	"context"
	"fmt"
	"net/http"

	"github.com/dghubble/oauth1"
)

// RequestToken is the temporary credential from the first OAuth step.
type RequestToken struct {
	Token  string
	Secret string
}

// AccessToken is the long-lived user credential from the last OAuth step.
type AccessToken struct {
	Token  string
	Secret string
}

// Authenticator runs the OAuth 1.0a PIN (oob) flow.
type Authenticator struct {
	cfg       oauth1.Config
	transport http.RoundTripper
}

// NewAuthenticator returns an Authenticator for the given app credentials.
func NewAuthenticator(appKey, appSecret string, opts ...Option) *Authenticator {
	o := buildOptions(opts)
	return &Authenticator{
		cfg: oauth1.Config{
			ConsumerKey:    appKey,
			ConsumerSecret: appSecret,
			CallbackURL:    "oob",
			Endpoint: oauth1.Endpoint{
				RequestTokenURL: o.baseURL + "/OAuth/request_token",
				AuthorizeURL:    o.baseURL + "/OAuth/authorize",
				AccessTokenURL:  o.baseURL + "/OAuth/access_token",
			},
		},
		transport: o.transport,
	}
}

// RequestToken obtains a temporary request token.
func (a *Authenticator) RequestToken(ctx context.Context) (*RequestToken, error) {
	cfg := a.configFor(ctx)
	token, secret, err := cfg.RequestToken()
	if err != nil {
		return nil, fmt.Errorf("get request token: %w", err)
	}
	return &RequestToken{Token: token, Secret: secret}, nil
}

// AuthorizeURL returns the page where the user approves the app and gets a PIN.
func (a *Authenticator) AuthorizeURL(rt *RequestToken) (string, error) {
	u, err := a.cfg.AuthorizationURL(rt.Token)
	if err != nil {
		return "", fmt.Errorf("build authorize URL: %w", err)
	}
	return u.String(), nil
}

// AccessToken exchanges the request token and the user's PIN for an access token.
func (a *Authenticator) AccessToken(ctx context.Context, rt *RequestToken, pin string) (*AccessToken, error) {
	cfg := a.configFor(ctx)
	token, secret, err := cfg.AccessToken(rt.Token, rt.Secret, pin)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}
	return &AccessToken{Token: token, Secret: secret}, nil
}

// configFor returns a copy of the config whose HTTP client honors ctx, since
// oauth1.Config's token methods do not accept a context.
func (a *Authenticator) configFor(ctx context.Context) oauth1.Config {
	cfg := a.cfg
	cfg.HTTPClient = &http.Client{
		Transport: &contextTransport{ctx: ctx, base: a.transport},
		Timeout:   requestTimeout,
	}
	return cfg
}

type contextTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t *contextTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(req.WithContext(t.ctx))
}
