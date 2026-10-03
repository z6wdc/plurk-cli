package cmd

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/z6wdc/plurk-cli/internal/config"
	"github.com/z6wdc/plurk-cli/internal/plurk"
)

// Variables so tests can point the client at an httptest.Server, skip retry
// waits, and use a fixed time zone without touching time.Local.
var (
	apiBaseURL   = plurk.DefaultBaseURL
	retryBackoff = time.Second
	localZone    = time.Local
)

func plurkOptions(cmd *cobra.Command) []plurk.Option {
	opts := []plurk.Option{plurk.WithBaseURL(apiBaseURL), plurk.WithRetryBackoff(retryBackoff)}
	if verbose {
		opts = append(opts, plurk.WithLogger(cmd.ErrOrStderr()))
	}
	return opts
}

func newClient(cmd *cobra.Command, cfg *config.Config) *plurk.Client {
	return plurk.NewClient(credentials(cfg), plurkOptions(cmd)...)
}

func credentials(cfg *config.Config) plurk.Credentials {
	return plurk.Credentials{
		AppKey:      cfg.AppKey,
		AppSecret:   cfg.AppSecret,
		Token:       cfg.AccessToken,
		TokenSecret: cfg.AccessTokenSecret,
	}
}

// userName returns the best human-readable name for u.
func userName(u *plurk.User) string {
	if u.DisplayName != "" && u.DisplayName != u.NickName {
		return u.DisplayName + " (@" + u.NickName + ")"
	}
	return "@" + u.NickName
}
