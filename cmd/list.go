package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/z6wdc/plurk-cli/internal/plurk"
	"github.com/z6wdc/plurk-cli/internal/timerange"
)

const summaryLength = 50

var listFlags struct {
	from, to string
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List your own plurks posted within a date range",
	Example: `  plurk list --from 2015-01-01 --to 2015-12-31
  plurk list --from 2024-03-01 --to 2024-03-01   # a single day`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		out := cmd.OutOrStdout()

		r, err := timerange.Parse(listFlags.from, listFlags.to, localZone)
		if err != nil {
			return err
		}
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		client := newClient(cmd, cfg)
		me, err := client.Me(ctx)
		if err != nil {
			return err
		}

		count := 0
		for p, err := range timerange.Plurks(ctx, client, r, me.ID) {
			if err != nil {
				return err
			}
			fmt.Fprintln(out, formatPlurk(&p))
			count++
		}
		fmt.Fprintf(out, "\n%d plurk(s) from %s to %s\n", count, listFlags.from, listFlags.to)
		return nil
	},
}

func init() {
	addRangeFlags(listCmd, &listFlags.from, &listFlags.to)
	rootCmd.AddCommand(listCmd)
}

// addRangeFlags adds the required --from and --to flags.
func addRangeFlags(cmd *cobra.Command, from, to *string) {
	cmd.Flags().StringVar(from, "from", "", "first day of the range, YYYY-MM-DD in local time (required)")
	cmd.Flags().StringVar(to, "to", "", "last day of the range, inclusive, YYYY-MM-DD in local time (required)")
	cmd.MarkFlagRequired("from")
	cmd.MarkFlagRequired("to")
}

// formatPlurk renders one line: local time, permalink, content summary.
func formatPlurk(p *plurk.Plurk) string {
	posted := p.Posted.In(localZone).Format("2006-01-02 15:04")
	return fmt.Sprintf("%s  %s  %s", posted, p.URL(), summarize(p.ContentRaw, summaryLength))
}

// summarize collapses whitespace and truncates s to n runes.
func summarize(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
