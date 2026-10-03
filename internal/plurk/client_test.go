package plurk

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testCreds = Credentials{
	AppKey:      "app-key",
	AppSecret:   "app-secret",
	Token:       "user-token",
	TokenSecret: "user-token-secret",
}

func newTestClient(t *testing.T, h http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	opts = append([]Option{WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithRetryBackoff(time.Millisecond)}, opts...)
	return NewClient(testCreds, opts...)
}

func TestMe(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/APP/Users/me" {
			t.Errorf("got %s %s, want POST /APP/Users/me", r.Method, r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		for _, want := range []string{`OAuth `, `oauth_consumer_key="app-key"`, `oauth_token="user-token"`, `oauth_signature=`} {
			if !strings.Contains(auth, want) {
				t.Errorf("Authorization header %q missing %q", auth, want)
			}
		}
		w.Write([]byte(`{"id": 42, "nick_name": "alice", "display_name": "Alice", "full_name": "Alice A."}`))
	})

	got, err := c.Me(context.Background())
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	want := User{ID: 42, NickName: "alice", DisplayName: "Alice", FullName: "Alice A."}
	if *got != want {
		t.Errorf("Me = %+v, want %+v", *got, want)
	}
}

func TestCallErrors(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantText string
	}{
		{"error_text", http.StatusBadRequest, `{"error_text": "invalid access token"}`, "invalid access token"},
		{"plain body", http.StatusBadGateway, "bad gateway\n", "bad gateway"},
		{"empty body", http.StatusInternalServerError, "", "Internal Server Error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			})
			_, err := c.Me(context.Background())
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("Me error = %v, want *APIError", err)
			}
			if apiErr.StatusCode != tt.status || apiErr.Text != tt.wantText {
				t.Errorf("APIError = %+v, want status %d text %q", apiErr, tt.status, tt.wantText)
			}
		})
	}
}

func TestCallRetry(t *testing.T) {
	tests := []struct {
		name      string
		statuses  []int // response status per attempt; the last one repeats
		wantCalls int32
		wantErr   bool
	}{
		{"success", []int{200}, 1, false},
		{"429 then success", []int{429, 200}, 2, false},
		{"503 twice then success", []int{503, 503, 200}, 3, false},
		{"always 500", []int{500}, 3, true},
		{"400 is not retried", []int{400}, 1, true},
		{"401 is not retried", []int{401}, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				n := int(calls.Add(1))
				status := tt.statuses[min(n, len(tt.statuses))-1]
				w.WriteHeader(status)
				if status == 200 {
					w.Write([]byte(`{"id": 1}`))
				} else {
					w.Write([]byte(`{"error_text": "nope"}`))
				}
			})
			_, err := c.Me(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Me error = %v, wantErr %v", err, tt.wantErr)
			}
			if got := calls.Load(); got != tt.wantCalls {
				t.Errorf("calls = %d, want %d", got, tt.wantCalls)
			}
		})
	}
}

func TestCallRetryCanceledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		cancel()
		w.WriteHeader(http.StatusServiceUnavailable)
	}, WithRetryBackoff(time.Hour))

	_, err := c.Me(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Me error = %v, want context.Canceled", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestCallInvalidJSON(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	})
	if _, err := c.Me(context.Background()); err == nil {
		t.Error("Me: want error for invalid JSON, got nil")
	}
}

func TestCallContextCanceled(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("request should not reach the server")
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Me(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Me error = %v, want context.Canceled", err)
	}
}

func TestLoggerDoesNotLeakSecrets(t *testing.T) {
	var log bytes.Buffer
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id": 1}`))
	}, WithLogger(&log))

	if _, err := c.Me(context.Background()); err != nil {
		t.Fatalf("Me: %v", err)
	}
	got := log.String()
	if !strings.Contains(got, "POST ") || !strings.Contains(got, "/APP/Users/me -> 200") {
		t.Errorf("log = %q, want a line for POST /APP/Users/me", got)
	}
	for _, secret := range []string{testCreds.AppKey, testCreds.AppSecret, testCreds.Token, testCreds.TokenSecret, "oauth_"} {
		if strings.Contains(got, secret) {
			t.Errorf("log contains %q: %q", secret, got)
		}
	}
}
