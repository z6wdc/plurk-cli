package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/z6wdc/plurk-cli/internal/config"
	"github.com/z6wdc/plurk-cli/internal/plurk"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authorize the CLI with your Plurk account",
	Long: `Authorize the CLI with your Plurk account using the OAuth PIN flow.

You need the App Key and App Secret of an app registered at
https://www.plurk.com/PlurkApp/. They are read from PLURK_APP_KEY and
PLURK_APP_SECRET if set, and prompted for otherwise.`,
	Args: cobra.NoArgs,
	RunE: runLogin,
}

func init() {
	rootCmd.AddCommand(loginCmd)
}

func runLogin(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	in := bufio.NewReader(cmd.InOrStdin())

	path, err := resolveConfigPath()
	if err != nil {
		return err
	}
	existing, err := config.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		existing = &config.Config{}
	} else if err != nil {
		return err
	}

	appKey, err := envOrPrompt(in, out, "PLURK_APP_KEY", "App Key", existing.AppKey)
	if err != nil {
		return err
	}
	appSecret, err := envOrPrompt(in, out, "PLURK_APP_SECRET", "App Secret", existing.AppSecret)
	if err != nil {
		return err
	}

	auth := plurk.NewAuthenticator(appKey, appSecret, plurkOptions(cmd)...)
	rt, err := auth.RequestToken(ctx)
	if err != nil {
		return err
	}
	authURL, err := auth.AuthorizeURL(rt)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nOpen this URL in your browser and authorize the app:\n\n  %s\n\n", authURL)

	pin, err := prompt(in, out, "PIN", "")
	if err != nil {
		return err
	}
	at, err := auth.AccessToken(ctx, rt, pin)
	if err != nil {
		return err
	}

	cfg := &config.Config{
		AppKey:            appKey,
		AppSecret:         appSecret,
		AccessToken:       at.Token,
		AccessTokenSecret: at.Secret,
	}
	user, err := newClient(cmd, cfg).Me(ctx)
	if err != nil {
		return fmt.Errorf("verify credentials: %w", err)
	}
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nLogged in as %s. Credentials saved to %s\n", userName(user), path)
	return nil
}

// envOrPrompt returns the environment variable if set, otherwise prompts.
func envOrPrompt(in *bufio.Reader, out io.Writer, env, label, current string) (string, error) {
	if v := os.Getenv(env); v != "" {
		fmt.Fprintf(out, "Using %s from $%s\n", label, env)
		return v, nil
	}
	return prompt(in, out, label, current)
}

// prompt reads one line from in. If the user enters nothing, current is used;
// its value is never echoed because it may be a secret.
func prompt(in *bufio.Reader, out io.Writer, label, current string) (string, error) {
	if current != "" {
		fmt.Fprintf(out, "%s (press Enter to keep the saved value): ", label)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	line, err := in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read %s: %w", label, err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		line = current
	}
	if line == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	return line, nil
}
