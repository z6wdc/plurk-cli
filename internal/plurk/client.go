// Package plurk is a thin wrapper around the Plurk API 2.0.
package plurk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dghubble/oauth1"
)

// DefaultBaseURL is the Plurk site root. API calls go to <base>/APP/ and
// OAuth calls to <base>/OAuth/.
const DefaultBaseURL = "https://www.plurk.com"

const (
	requestTimeout  = 30 * time.Second
	maxResponseSize = 10 << 20
	maxAttempts     = 3
)

// Credentials are the four OAuth 1.0a values needed to call the API.
type Credentials struct {
	AppKey      string
	AppSecret   string
	Token       string
	TokenSecret string
}

// Option configures a Client or Authenticator.
type Option func(*options)

type options struct {
	baseURL   string
	transport http.RoundTripper
	log       io.Writer
	backoff   time.Duration
}

// WithBaseURL overrides DefaultBaseURL, e.g. to point at an httptest.Server.
func WithBaseURL(u string) Option {
	return func(o *options) { o.baseURL = strings.TrimSuffix(u, "/") }
}

// WithHTTPClient sets the underlying HTTP client. Only its Transport is used;
// OAuth signing is layered on top of it.
func WithHTTPClient(c *http.Client) Option {
	return func(o *options) { o.transport = c.Transport }
}

// WithLogger writes one line per HTTP request (method, URL path, status,
// duration) to w. Headers and bodies are never logged.
func WithLogger(w io.Writer) Option {
	return func(o *options) { o.log = w }
}

// WithRetryBackoff sets the wait before the first retry of a 429 or 5xx
// response. It doubles on each further retry. The default is one second.
func WithRetryBackoff(d time.Duration) Option {
	return func(o *options) { o.backoff = d }
}

func buildOptions(opts []Option) options {
	o := options{baseURL: DefaultBaseURL, backoff: time.Second}
	for _, opt := range opts {
		opt(&o)
	}
	if o.transport == nil {
		o.transport = http.DefaultTransport
	}
	if o.log != nil {
		o.transport = &loggingTransport{base: o.transport, w: o.log}
	}
	return o
}

// Client calls the Plurk API on behalf of an authorized user.
type Client struct {
	baseURL string
	http    *http.Client
	backoff time.Duration
}

// NewClient returns a Client that signs every request with creds.
func NewClient(creds Credentials, opts ...Option) *Client {
	o := buildOptions(opts)
	cfg := oauth1.NewConfig(creds.AppKey, creds.AppSecret)
	ctx := context.WithValue(context.Background(), oauth1.HTTPClient, &http.Client{Transport: o.transport})
	hc := cfg.Client(ctx, oauth1.NewToken(creds.Token, creds.TokenSecret))
	hc.Timeout = requestTimeout
	return &Client{baseURL: o.baseURL, http: hc, backoff: o.backoff}
}

// APIError is returned when the API responds with a non-200 status.
type APIError struct {
	StatusCode int
	Text       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("plurk api: status %d: %s", e.StatusCode, e.Text)
}

// Retryable reports whether the request may succeed if sent again
// (rate limited or server error).
func (e *APIError) Retryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

// call POSTs params to /APP/<endpoint> and decodes the JSON response into out
// (if out is non-nil). Retryable API errors are retried with exponential
// backoff, up to maxAttempts in total.
func (c *Client) call(ctx context.Context, endpoint string, params url.Values, out any) error {
	wait := c.backoff
	for attempt := 1; ; attempt++ {
		err := c.callOnce(ctx, endpoint, params, out)
		var apiErr *APIError
		if err == nil || attempt == maxAttempts || !errors.As(err, &apiErr) || !apiErr.Retryable() {
			return err
		}
		if err := sleep(ctx, wait); err != nil {
			return fmt.Errorf("%s: %w", endpoint, err)
		}
		wait *= 2
	}
}

func (c *Client) callOnce(ctx context.Context, endpoint string, params url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/APP/"+endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return fmt.Errorf("%s: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return fmt.Errorf("%s: read response: %w", endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %w", endpoint, newAPIError(resp.StatusCode, body))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s: decode response: %w", endpoint, err)
	}
	return nil
}

// sleep waits for d or until ctx is done.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func newAPIError(status int, body []byte) *APIError {
	var e struct {
		ErrorText string `json:"error_text"`
	}
	if err := json.Unmarshal(body, &e); err == nil && e.ErrorText != "" {
		return &APIError{StatusCode: status, Text: e.ErrorText}
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		text = text[:200] + "..."
	}
	if text == "" {
		text = http.StatusText(status)
	}
	return &APIError{StatusCode: status, Text: text}
}

// loggingTransport logs requests without headers, bodies or query strings,
// so credentials never reach the log.
type loggingTransport struct {
	base http.RoundTripper
	w    io.Writer
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	target := req.URL.Scheme + "://" + req.URL.Host + req.URL.Path
	if err != nil {
		fmt.Fprintf(t.w, "%s %s: %v\n", req.Method, target, err)
		return nil, err
	}
	fmt.Fprintf(t.w, "%s %s -> %d (%s)\n", req.Method, target, resp.StatusCode, time.Since(start).Round(time.Millisecond))
	return resp, nil
}
