// Package cmd implements the plurk command-line interface.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/z6wdc/plurk-cli/internal/config"
)

var (
	configPath string
	verbose    bool
)

var rootCmd = &cobra.Command{
	Use:   "plurk",
	Short: "Manage your own Plurk timeline from the command line",
	Long: `plurk is a command-line tool for managing your own Plurk timeline
via the Plurk API 2.0.

Run "plurk login" first to authorize the CLI with your account.`,
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "config file (default <user config dir>/plurk-cli/config.json)")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "print HTTP requests and other debug output")
}

// Execute runs the root command and exits non-zero on error. Ctrl-C cancels
// the command's context.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := rootCmd.ExecuteContext(ctx)
	stop()
	if err != nil {
		os.Exit(1)
	}
}

// resolveConfigPath returns the --config value or the default path.
func resolveConfigPath() (string, error) {
	if configPath != "" {
		return configPath, nil
	}
	return config.DefaultPath()
}

// loadConfig loads the config and requires a complete set of credentials.
func loadConfig() (*config.Config, error) {
	path, err := resolveConfigPath()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no config found at %s; run \"plurk login\" first", path)
	}
	if err != nil {
		return nil, err
	}
	if !cfg.HasAppCredentials() || !cfg.HasAccessToken() {
		return nil, fmt.Errorf("config at %s is incomplete; run \"plurk login\" again", path)
	}
	return cfg, nil
}
