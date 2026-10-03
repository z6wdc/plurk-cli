package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the authorized Plurk account",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		user, err := newClient(cmd, cfg).Me(cmd.Context())
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Display name: %s\n", user.DisplayName)
		fmt.Fprintf(out, "Nick name:    %s\n", user.NickName)
		fmt.Fprintf(out, "User ID:      %d\n", user.ID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}
