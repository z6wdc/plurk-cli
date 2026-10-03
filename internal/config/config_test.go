package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	want := &Config{
		AppKey:            "key",
		AppSecret:         "secret",
		AccessToken:       "token",
		AccessTokenSecret: "token-secret",
	}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if *got != *want {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
}

func TestSavePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions only")
	}
	dir := filepath.Join(t.TempDir(), "plurk-cli")
	path := filepath.Join(dir, "config.json")

	if err := Save(path, &Config{AppKey: "k"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tests := []struct {
		path string
		want os.FileMode
	}{
		{dir, 0o700},
		{path, 0o600},
	}
	for _, tt := range tests {
		info, err := os.Stat(tt.path)
		if err != nil {
			t.Fatalf("Stat %s: %v", tt.path, err)
		}
		if got := info.Mode().Perm(); got != tt.want {
			t.Errorf("%s mode = %o, want %o", tt.path, got, tt.want)
		}
	}
}

func TestSaveOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, &Config{AppKey: "old"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Save(path, &Config{AppKey: "new"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AppKey != "new" {
		t.Errorf("AppKey = %q, want %q", got.AppKey, "new")
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want 1 (temp file left behind?)", len(entries))
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Load error = %v, want os.ErrNotExist", err)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load: want error for invalid JSON, got nil")
	}
}

func TestHasCredentials(t *testing.T) {
	tests := []struct {
		name      string
		cfg       Config
		wantApp   bool
		wantToken bool
	}{
		{"empty", Config{}, false, false},
		{"app only", Config{AppKey: "k", AppSecret: "s"}, true, false},
		{"partial app", Config{AppKey: "k"}, false, false},
		{"full", Config{"k", "s", "t", "ts"}, true, true},
		{"partial token", Config{AppKey: "k", AppSecret: "s", AccessToken: "t"}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.HasAppCredentials(); got != tt.wantApp {
				t.Errorf("HasAppCredentials = %v, want %v", got, tt.wantApp)
			}
			if got := tt.cfg.HasAccessToken(); got != tt.wantToken {
				t.Errorf("HasAccessToken = %v, want %v", got, tt.wantToken)
			}
		})
	}
}
