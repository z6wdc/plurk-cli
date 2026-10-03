package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/z6wdc/plurk-cli/internal/plurk"
	"github.com/z6wdc/plurk-cli/internal/timerange"
)

var deleteFlags struct {
	from, to string
	yes      bool
	logPath  string
	delay    time.Duration
}

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete your own plurks posted within a date range",
	Long: `Delete your own plurks posted within a date range.

By default this is a dry run: it lists the plurks that would be deleted and
deletes nothing. Pass --yes to actually delete them.

Before each plurk is deleted, it is appended to a JSONL log file. Keep this
file: deletion is permanent and the log is your only backup.`,
	Example: `  plurk delete --from 2015-01-01 --to 2015-12-31        # dry run
  plurk delete --from 2015-01-01 --to 2015-12-31 --yes  # really delete`,
	Args: cobra.NoArgs,
	RunE: runDelete,
}

func init() {
	addRangeFlags(deleteCmd, &deleteFlags.from, &deleteFlags.to)
	deleteCmd.Flags().BoolVar(&deleteFlags.yes, "yes", false, "actually delete; without it, only list what would be deleted")
	deleteCmd.Flags().StringVar(&deleteFlags.logPath, "log", "", "JSONL file recording each plurk before deletion (default plurk-deleted-<timestamp>.jsonl)")
	deleteCmd.Flags().DurationVar(&deleteFlags.delay, "delay", 500*time.Millisecond, "pause between delete requests")
	rootCmd.AddCommand(deleteCmd)
}

func runDelete(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	r, err := timerange.Parse(deleteFlags.from, deleteFlags.to, localZone)
	if err != nil {
		return err
	}
	if deleteFlags.delay < 0 {
		return fmt.Errorf("--delay must not be negative")
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

	// Collect everything first: deleting while paginating would shift the
	// pages still to be fetched.
	var plurks []plurk.Plurk
	for p, err := range timerange.Plurks(ctx, client, r, me.ID) {
		if err != nil {
			return err
		}
		plurks = append(plurks, p)
	}
	if len(plurks) == 0 {
		fmt.Fprintf(out, "No plurks found from %s to %s. Nothing to delete.\n", deleteFlags.from, deleteFlags.to)
		return nil
	}

	for i := range plurks {
		fmt.Fprintln(out, formatPlurk(&plurks[i]))
	}
	newest, oldest := plurks[0].Posted.In(localZone), plurks[len(plurks)-1].Posted.In(localZone)
	fmt.Fprintf(out, "\n%d plurk(s) by %s, posted %s to %s\n",
		len(plurks), userName(me), oldest.Format("2006-01-02 15:04"), newest.Format("2006-01-02 15:04"))

	if !deleteFlags.yes {
		fmt.Fprintf(out, "\nDry run: nothing was deleted. Re-run with --yes to delete these %d plurk(s).\n", len(plurks))
		return nil
	}

	logPath := deleteFlags.logPath
	if logPath == "" {
		logPath = "plurk-deleted-" + time.Now().Format("20060102-150405") + ".jsonl"
	}
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer logFile.Close()
	fmt.Fprintf(out, "\nDeleting %d plurk(s). Log: %s\n", len(plurks), logPath)

	res, err := deleteAll(ctx, out, client, logFile, plurks, deleteFlags.delay)
	fmt.Fprintf(out, "\nDeleted %d of %d plurk(s). Log: %s\n", res.deleted, len(plurks), logPath)
	if len(res.failed) > 0 {
		fmt.Fprintf(out, "\nFailed to delete %d plurk(s):\n", len(res.failed))
		for _, p := range res.failed {
			fmt.Fprintf(out, "  %s\n", p.URL())
		}
	}
	if err != nil {
		return err
	}
	if len(res.failed) > 0 {
		return fmt.Errorf("%d deletion(s) failed; re-run the same command to retry", len(res.failed))
	}
	return nil
}

type deleteResult struct {
	deleted int
	failed  []plurk.Plurk
}

// deleteAll logs and deletes each plurk in turn. It stops early, returning
// an error, if the log cannot be written or ctx is canceled. Individual
// delete failures are collected in the result instead.
func deleteAll(ctx context.Context, out io.Writer, client *plurk.Client, logFile *os.File, plurks []plurk.Plurk, delay time.Duration) (deleteResult, error) {
	var res deleteResult
	for i, p := range plurks {
		if i > 0 && delay > 0 {
			if err := sleepContext(ctx, delay); err != nil {
				return res, fmt.Errorf("interrupted: %w", err)
			}
		}
		if err := ctx.Err(); err != nil {
			return res, fmt.Errorf("interrupted: %w", err)
		}

		// The backup must be on disk before the plurk is gone.
		if err := writeLogRecord(logFile, &p); err != nil {
			return res, fmt.Errorf("write log (nothing more will be deleted): %w", err)
		}

		progress := fmt.Sprintf("[%d/%d]", i+1, len(plurks))
		if err := client.DeletePlurk(ctx, p.PlurkID); err != nil {
			if ctx.Err() != nil {
				return res, fmt.Errorf("interrupted: %w", ctx.Err())
			}
			fmt.Fprintf(out, "%s failed  %s: %v\n", progress, p.URL(), err)
			res.failed = append(res.failed, p)
			continue
		}
		fmt.Fprintf(out, "%s deleted %s\n", progress, p.URL())
		res.deleted++
	}
	return res, nil
}

// logRecord is one line of the deletion log.
type logRecord struct {
	PlurkID    int64     `json:"plurk_id"`
	URL        string    `json:"url"`
	Posted     time.Time `json:"posted"`
	Qualifier  string    `json:"qualifier"`
	ContentRaw string    `json:"content_raw"`
	Content    string    `json:"content"`
	DeletedAt  time.Time `json:"deleted_at"`
}

// writeLogRecord appends p as one JSON line and syncs it to disk.
func writeLogRecord(f *os.File, p *plurk.Plurk) error {
	line, err := json.Marshal(logRecord{
		PlurkID:    p.PlurkID,
		URL:        p.URL(),
		Posted:     p.Posted.Time,
		Qualifier:  p.Qualifier,
		ContentRaw: p.ContentRaw,
		Content:    p.Content,
		DeletedAt:  time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// sleepContext waits for d or until ctx is done.
func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
