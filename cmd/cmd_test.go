package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/z6wdc/plurk-cli/internal/config"
)

// fakeServer emulates the Plurk endpoints the CLI uses. Delete state is kept
// so getPlurks stops returning deleted plurks.
type fakeServer struct {
	t *testing.T

	mu          sync.Mutex
	deleted     map[int64]bool
	deleteCalls []int64
	// failDelete maps a plurk ID to how many times plurkDelete returns 503
	// for it before succeeding; a negative value means always.
	failDelete map[int64]int
	// onDelete, if set, runs at the start of every plurkDelete request.
	onDelete func(id int64)
}

// fakePlurk starts a fakeServer and points the CLI at it.
func fakePlurk(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{t: t, deleted: map[int64]bool{}, failDelete: map[int64]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/OAuth/request_token":
			w.Write([]byte("oauth_token=req-token&oauth_token_secret=req-secret&oauth_callback_confirmed=true"))
		case "/OAuth/access_token":
			if !strings.Contains(r.Header.Get("Authorization"), `oauth_verifier="123456"`) {
				http.Error(w, "bad verifier", http.StatusUnauthorized)
				return
			}
			w.Write([]byte("oauth_token=acc-token&oauth_token_secret=acc-secret"))
		case "/APP/Users/me":
			if !strings.Contains(r.Header.Get("Authorization"), `oauth_token="acc-token"`) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error_text": "invalid access token"}`))
				return
			}
			w.Write([]byte(`{"id": 42, "nick_name": "alice", "display_name": "Alice"}`))
		case "/APP/Timeline/getPlurks":
			f.servePlurks(w, r)
		case "/APP/Timeline/plurkDelete":
			f.serveDelete(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	oldURL, oldBackoff := apiBaseURL, retryBackoff
	apiBaseURL, retryBackoff = srv.URL, time.Millisecond
	t.Cleanup(func() { apiBaseURL, retryBackoff = oldURL, oldBackoff })
	return f
}

type fakeTimelinePlurk struct {
	ID      int64
	Owner   int64
	Posted  time.Time
	Content string
}

// fakeTimeline is newest first, like the real API.
var fakeTimeline = []fakeTimelinePlurk{
	{106, 42, time.Date(2024, 3, 2, 1, 0, 0, 0, time.UTC), "after the range"},
	{105, 42, time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC), "second in range\nwith a newline"},
	{104, 7, time.Date(2024, 3, 1, 11, 0, 0, 0, time.UTC), "someone else"},
	{103, 42, time.Date(2024, 2, 20, 8, 30, 0, 0, time.UTC), strings.Repeat("長", 60)},
	{102, 42, time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), "first in range"},
	{101, 42, time.Date(2024, 1, 31, 23, 0, 0, 0, time.UTC), "before the range"},
}

// servePlurks emulates Timeline/getPlurks over fakeTimeline with page size 2.
func (f *fakeServer) servePlurks(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		f.t.Errorf("parse form: %v", err)
	}
	if got := r.PostForm.Get("filter"); got != "my" {
		f.t.Errorf("filter = %q, want my", got)
	}
	offset, err := time.Parse("2006-01-02T15:04:05", r.PostForm.Get("offset"))
	if err != nil {
		f.t.Errorf("parse offset: %v", err)
	}

	type item struct {
		PlurkID    int64  `json:"plurk_id"`
		OwnerID    int64  `json:"owner_id"`
		Posted     string `json:"posted"`
		ContentRaw string `json:"content_raw"`
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	page := []item{}
	for _, p := range fakeTimeline {
		if !f.deleted[p.ID] && p.Posted.Before(offset) && len(page) < 2 {
			page = append(page, item{p.ID, p.Owner, p.Posted.Format("Mon, 02 Jan 2006 15:04:05 GMT"), p.Content})
		}
	}
	json.NewEncoder(w).Encode(map[string]any{"plurks": page, "plurk_users": map[string]any{}})
}

func (f *fakeServer) serveDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		f.t.Errorf("parse form: %v", err)
	}
	id, err := strconv.ParseInt(r.PostForm.Get("plurk_id"), 10, 64)
	if err != nil {
		f.t.Errorf("parse plurk_id: %v", err)
	}
	if f.onDelete != nil {
		f.onDelete(id)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls = append(f.deleteCalls, id)
	if n := f.failDelete[id]; n != 0 {
		if n > 0 {
			f.failDelete[id] = n - 1
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error_text": "try again later"}`))
		return
	}
	f.deleted[id] = true
	w.Write([]byte(`{"success_text": "ok"}`))
}

func (f *fakeServer) calls() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.deleteCalls)
}

// saveTestConfig writes a complete config that fakePlurk accepts.
func saveTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := &config.Config{AppKey: "app-key", AppSecret: "app-secret", AccessToken: "acc-token", AccessTokenSecret: "acc-secret"}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	return path
}

// resetCommands restores every flag to its default and clears each command's
// context. Cobra keeps parsed values (and the "changed" state used by required
// flags) between runs, and only propagates the context to subcommands that
// do not have one yet.
func resetCommands(cmd *cobra.Command) {
	reset := func(f *pflag.Flag) {
		f.Value.Set(f.DefValue)
		f.Changed = false
	}
	cmd.Flags().VisitAll(reset)
	cmd.PersistentFlags().VisitAll(reset)
	cmd.SetContext(nil)
	for _, c := range cmd.Commands() {
		resetCommands(c)
	}
}

// run executes the root command with args and stdin, returning stdout.
func run(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	return runContext(t, context.Background(), stdin, args...)
}

func runContext(t *testing.T, ctx context.Context, stdin string, args ...string) (string, error) {
	t.Helper()
	resetCommands(rootCmd)
	var out bytes.Buffer
	rootCmd.SetArgs(args)
	rootCmd.SetIn(strings.NewReader(stdin))
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	err := rootCmd.ExecuteContext(ctx)
	return out.String(), err
}

func TestLoginAndWhoami(t *testing.T) {
	fakePlurk(t)
	t.Setenv("PLURK_APP_KEY", "")
	t.Setenv("PLURK_APP_SECRET", "")
	path := filepath.Join(t.TempDir(), "config.json")

	out, err := run(t, "app-key\napp-secret\n123456\n", "login", "--config", path)
	if err != nil {
		t.Fatalf("login: %v\n%s", err, out)
	}
	if !strings.Contains(out, "/OAuth/authorize?oauth_token=req-token") {
		t.Errorf("login output missing authorize URL:\n%s", out)
	}
	if !strings.Contains(out, "Logged in as Alice (@alice)") {
		t.Errorf("login output missing confirmation:\n%s", out)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := config.Config{AppKey: "app-key", AppSecret: "app-secret", AccessToken: "acc-token", AccessTokenSecret: "acc-secret"}
	if *cfg != want {
		t.Errorf("saved config = %+v, want %+v", *cfg, want)
	}

	out, err = run(t, "", "whoami", "--config", path)
	if err != nil {
		t.Fatalf("whoami: %v\n%s", err, out)
	}
	for _, want := range []string{"Alice", "alice", "42"} {
		if !strings.Contains(out, want) {
			t.Errorf("whoami output missing %q:\n%s", want, out)
		}
	}
}

func TestLoginReusesSavedAppCredentials(t *testing.T) {
	fakePlurk(t)
	t.Setenv("PLURK_APP_KEY", "")
	t.Setenv("PLURK_APP_SECRET", "")
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, &config.Config{AppKey: "app-key", AppSecret: "app-secret"}); err != nil {
		t.Fatal(err)
	}

	out, err := run(t, "\n\n123456\n", "login", "--config", path)
	if err != nil {
		t.Fatalf("login: %v\n%s", err, out)
	}
	if strings.Contains(out, "app-secret") {
		t.Errorf("login echoed the saved secret:\n%s", out)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppKey != "app-key" || cfg.AccessToken != "acc-token" {
		t.Errorf("saved config = %+v", *cfg)
	}
}

func TestLoginFromEnv(t *testing.T) {
	fakePlurk(t)
	t.Setenv("PLURK_APP_KEY", "app-key")
	t.Setenv("PLURK_APP_SECRET", "app-secret")
	path := filepath.Join(t.TempDir(), "config.json")

	out, err := run(t, "123456\n", "login", "--config", path)
	if err != nil {
		t.Fatalf("login: %v\n%s", err, out)
	}
	if !strings.Contains(out, "from $PLURK_APP_KEY") {
		t.Errorf("login output does not mention env var:\n%s", out)
	}
}

func TestLoginWrongPINDoesNotSave(t *testing.T) {
	fakePlurk(t)
	t.Setenv("PLURK_APP_KEY", "app-key")
	t.Setenv("PLURK_APP_SECRET", "app-secret")
	path := filepath.Join(t.TempDir(), "config.json")

	if _, err := run(t, "000000\n", "login", "--config", path); err == nil {
		t.Fatal("login: want error for wrong PIN, got nil")
	}
	if _, err := config.Load(path); err == nil {
		t.Error("config was saved despite failed login")
	}
}

func TestLoginMissingInput(t *testing.T) {
	fakePlurk(t)
	t.Setenv("PLURK_APP_KEY", "")
	t.Setenv("PLURK_APP_SECRET", "")
	path := filepath.Join(t.TempDir(), "config.json")

	_, err := run(t, "", "login", "--config", path)
	if err == nil || !strings.Contains(err.Error(), "App Key is required") {
		t.Errorf("login error = %v, want \"App Key is required\"", err)
	}
}

func TestWhoamiWithoutConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	_, err := run(t, "", "whoami", "--config", path)
	if err == nil || !strings.Contains(err.Error(), "plurk login") {
		t.Errorf("whoami error = %v, want hint to run plurk login", err)
	}
}
