// Package config loads and saves plurk-cli credentials.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds the OAuth credentials for a single Plurk account.
type Config struct {
	AppKey            string `json:"app_key"`
	AppSecret         string `json:"app_secret"`
	AccessToken       string `json:"access_token"`
	AccessTokenSecret string `json:"access_token_secret"`
}

// HasAppCredentials reports whether the app key and secret are set.
func (c *Config) HasAppCredentials() bool {
	return c.AppKey != "" && c.AppSecret != ""
}

// HasAccessToken reports whether the user access token and secret are set.
func (c *Config) HasAccessToken() bool {
	return c.AccessToken != "" && c.AccessTokenSecret != ""
}

// DefaultPath returns <user config dir>/plurk-cli/config.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(dir, "plurk-cli", "config.json"), nil
}

// Load reads the config at path. If the file does not exist, the returned
// error wraps os.ErrNotExist.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &c, nil
}

// Save writes c to path with mode 0600, creating the parent directory with
// mode 0700 if needed. The write is atomic: a temp file is renamed into place.
func Save(path string, c *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	// os.CreateTemp creates the file with mode 0600.
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}
