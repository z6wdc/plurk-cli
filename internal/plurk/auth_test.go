package plurk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newTestAuthenticator(t *testing.T, h http.HandlerFunc) *Authenticator {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewAuthenticator("app-key", "app-secret", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
}

func assertHeaderContains(t *testing.T, r *http.Request, wants ...string) {
	t.Helper()
	auth := r.Header.Get("Authorization")
	for _, want := range wants {
		if !strings.Contains(auth, want) {
			t.Errorf("Authorization header %q missing %q", auth, want)
		}
	}
}

func TestOAuthFlow(t *testing.T) {
	a := newTestAuthenticator(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/OAuth/request_token":
			assertHeaderContains(t, r, `oauth_consumer_key="app-key"`, `oauth_callback="oob"`)
			w.Write([]byte("oauth_token=req-token&oauth_token_secret=req-secret&oauth_callback_confirmed=true"))
		case "/OAuth/access_token":
			assertHeaderContains(t, r, `oauth_token="req-token"`, `oauth_verifier="123456"`)
			w.Write([]byte("oauth_token=acc-token&oauth_token_secret=acc-secret"))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()

	rt, err := a.RequestToken(ctx)
	if err != nil {
		t.Fatalf("RequestToken: %v", err)
	}
	if *rt != (RequestToken{Token: "req-token", Secret: "req-secret"}) {
		t.Errorf("RequestToken = %+v", *rt)
	}

	authURL, err := a.AuthorizeURL(rt)
	if err != nil {
		t.Fatalf("AuthorizeURL: %v", err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	if u.Path != "/OAuth/authorize" || u.Query().Get("oauth_token") != "req-token" {
		t.Errorf("AuthorizeURL = %q", authURL)
	}

	at, err := a.AccessToken(ctx, rt, "123456")
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if *at != (AccessToken{Token: "acc-token", Secret: "acc-secret"}) {
		t.Errorf("AccessToken = %+v", *at)
	}
}

func TestRequestTokenError(t *testing.T) {
	a := newTestAuthenticator(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid consumer key", http.StatusUnauthorized)
	})
	if _, err := a.RequestToken(context.Background()); err == nil {
		t.Error("RequestToken: want error, got nil")
	}
}

func TestRequestTokenContextCanceled(t *testing.T) {
	a := newTestAuthenticator(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("request should not reach the server")
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.RequestToken(ctx); err == nil {
		t.Error("RequestToken: want error for canceled context, got nil")
	}
}
