package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// deleteArgs returns delete args for the range covering 102, 103 and 105.
func deleteArgs(configPath string, extra ...string) []string {
	args := []string{"delete", "--from", "2024-02-01", "--to", "2024-03-01", "--delay", "0", "--config", configPath}
	return append(args, extra...)
}

func readLog(t *testing.T, path string) []logRecord {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()
	var records []logRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r logRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("log line %q: %v", sc.Text(), err)
		}
		records = append(records, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}

func recordIDs(records []logRecord) []int64 {
	var ids []int64
	for _, r := range records {
		ids = append(ids, r.PlurkID)
	}
	return ids
}

// chdirTemp switches to a fresh temp dir for the duration of the test.
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	return dir
}

func TestDeleteDryRun(t *testing.T) {
	f := fakePlurk(t)
	useUTC(t)
	dir := chdirTemp(t)
	cfg := saveTestConfig(t)

	out, err := run(t, "", deleteArgs(cfg)...)
	if err != nil {
		t.Fatalf("delete: %v\n%s", err, out)
	}
	for _, want := range []string{
		"https://www.plurk.com/p/2x",
		"https://www.plurk.com/p/2v",
		"https://www.plurk.com/p/2u",
		"3 plurk(s) by Alice (@alice), posted 2024-02-01 00:00 to 2024-03-01 12:00",
		"Dry run: nothing was deleted",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if calls := f.calls(); len(calls) != 0 {
		t.Errorf("dry run called plurkDelete for %v", calls)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("dry run created files: %v", entries)
	}
}

func TestDeleteYes(t *testing.T) {
	f := fakePlurk(t)
	useUTC(t)
	cfg := saveTestConfig(t)
	logPath := filepath.Join(t.TempDir(), "deleted.jsonl")

	// The log record must be on disk before the delete request arrives.
	f.onDelete = func(id int64) {
		if !slices.Contains(recordIDs(readLog(t, logPath)), id) {
			t.Errorf("plurk %d deleted before it was logged", id)
		}
	}

	out, err := run(t, "", deleteArgs(cfg, "--yes", "--log", logPath)...)
	if err != nil {
		t.Fatalf("delete: %v\n%s", err, out)
	}
	if want := []int64{105, 103, 102}; !slices.Equal(f.calls(), want) {
		t.Errorf("delete calls = %v, want %v", f.calls(), want)
	}
	for _, want := range []string{"[1/3] deleted https://www.plurk.com/p/2x", "[3/3] deleted https://www.plurk.com/p/2u", "Deleted 3 of 3 plurk(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	records := readLog(t, logPath)
	if want := []int64{105, 103, 102}; !slices.Equal(recordIDs(records), want) {
		t.Fatalf("log IDs = %v, want %v", recordIDs(records), want)
	}
	r := records[0]
	if r.URL != "https://www.plurk.com/p/2x" || r.ContentRaw != "second in range\nwith a newline" || r.Posted.IsZero() || r.DeletedAt.IsZero() {
		t.Errorf("log record = %+v", r)
	}

	// Everything in the range is gone now.
	out, err = run(t, "", "list", "--from", "2024-02-01", "--to", "2024-03-01", "--config", cfg)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "0 plurk(s)") {
		t.Errorf("list after delete:\n%s", out)
	}
}

func TestDeleteDefaultLogPath(t *testing.T) {
	fakePlurk(t)
	useUTC(t)
	dir := chdirTemp(t)
	cfg := saveTestConfig(t)

	if out, err := run(t, "", deleteArgs(cfg, "--yes")...); err != nil {
		t.Fatalf("delete: %v\n%s", err, out)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "plurk-deleted-*.jsonl"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("log files = %v, %v; want exactly one", matches, err)
	}
	if got := len(readLog(t, matches[0])); got != 3 {
		t.Errorf("log has %d records, want 3", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(matches[0])
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("log mode = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestDeleteRetriesTransientFailure(t *testing.T) {
	f := fakePlurk(t)
	useUTC(t)
	cfg := saveTestConfig(t)
	f.failDelete[103] = 1

	out, err := run(t, "", deleteArgs(cfg, "--yes", "--log", filepath.Join(t.TempDir(), "log.jsonl"))...)
	if err != nil {
		t.Fatalf("delete: %v\n%s", err, out)
	}
	if want := []int64{105, 103, 103, 102}; !slices.Equal(f.calls(), want) {
		t.Errorf("delete calls = %v, want %v", f.calls(), want)
	}
}

func TestDeletePersistentFailure(t *testing.T) {
	f := fakePlurk(t)
	useUTC(t)
	cfg := saveTestConfig(t)
	f.failDelete[103] = -1

	out, err := run(t, "", deleteArgs(cfg, "--yes", "--log", filepath.Join(t.TempDir(), "log.jsonl"))...)
	if err == nil || !strings.Contains(err.Error(), "1 deletion(s) failed") {
		t.Fatalf("delete error = %v, want 1 deletion(s) failed\n%s", err, out)
	}
	if want := []int64{105, 103, 103, 103, 102}; !slices.Equal(f.calls(), want) {
		t.Errorf("delete calls = %v, want %v (3 attempts for 103, then continue)", f.calls(), want)
	}
	for _, want := range []string{"[2/3] failed  https://www.plurk.com/p/2v", "Deleted 2 of 3 plurk(s)", "Failed to delete 1 plurk(s):\n  https://www.plurk.com/p/2v"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestDeleteNothingInRange(t *testing.T) {
	f := fakePlurk(t)
	useUTC(t)
	cfg := saveTestConfig(t)
	logPath := filepath.Join(t.TempDir(), "log.jsonl")

	out, err := run(t, "", "delete", "--from", "2023-01-01", "--to", "2023-01-31", "--yes", "--log", logPath, "--config", cfg)
	if err != nil {
		t.Fatalf("delete: %v\n%s", err, out)
	}
	if !strings.Contains(out, "No plurks found") {
		t.Errorf("output:\n%s", out)
	}
	if len(f.calls()) != 0 {
		t.Errorf("delete calls = %v, want none", f.calls())
	}
	if _, err := os.Stat(logPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("log file created with nothing to delete (stat err = %v)", err)
	}
}

func TestDeleteLogOpenFailure(t *testing.T) {
	f := fakePlurk(t)
	useUTC(t)
	cfg := saveTestConfig(t)
	logPath := filepath.Join(t.TempDir(), "missing-dir", "log.jsonl")

	_, err := run(t, "", deleteArgs(cfg, "--yes", "--log", logPath)...)
	if err == nil || !strings.Contains(err.Error(), "open log") {
		t.Errorf("delete error = %v, want open log error", err)
	}
	if len(f.calls()) != 0 {
		t.Errorf("deleted %v without a log", f.calls())
	}
}

func TestDeleteInterrupted(t *testing.T) {
	f := fakePlurk(t)
	useUTC(t)
	cfg := saveTestConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.onDelete = func(int64) { cancel() }

	out, err := runContext(t, ctx, "", deleteArgs(cfg, "--yes", "--log", filepath.Join(t.TempDir(), "log.jsonl"))...)
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("delete error = %v, want interrupted\n%s", err, out)
	}
	if calls := f.calls(); len(calls) != 1 {
		t.Errorf("delete calls = %v, want exactly 1 before stopping", calls)
	}
}

func TestDeleteNegativeDelay(t *testing.T) {
	fakePlurk(t)
	cfg := saveTestConfig(t)
	_, err := run(t, "", "delete", "--from", "2024-02-01", "--to", "2024-03-01", "--delay", "-1s", "--config", cfg)
	if err == nil || !strings.Contains(err.Error(), "--delay") {
		t.Errorf("delete error = %v, want --delay error", err)
	}
}
