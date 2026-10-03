package cmd

import (
	"strings"
	"testing"
	"time"
)

func useUTC(t *testing.T) {
	t.Helper()
	old := localZone
	localZone = time.UTC
	t.Cleanup(func() { localZone = old })
}

func TestList(t *testing.T) {
	fakePlurk(t)
	useUTC(t)
	path := saveTestConfig(t)

	out, err := run(t, "", "list", "--from", "2024-02-01", "--to", "2024-03-01", "--config", path)
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}

	want := []string{
		"2024-03-01 12:00  https://www.plurk.com/p/2x  second in range with a newline",
		"2024-02-20 08:30  https://www.plurk.com/p/2v  " + strings.Repeat("長", 50) + "…",
		"2024-02-01 00:00  https://www.plurk.com/p/2u  first in range",
		"",
		"3 plurk(s) from 2024-02-01 to 2024-03-01",
	}
	if got := strings.Split(strings.TrimSpace(out), "\n"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("list output:\n%s\nwant:\n%s", out, strings.Join(want, "\n"))
	}
}

func TestListErrors(t *testing.T) {
	fakePlurk(t)
	path := saveTestConfig(t)

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"missing flags", []string{"list"}, `required flag(s) "from", "to" not set`},
		{"missing to", []string{"list", "--from", "2024-01-01"}, `required flag(s) "to" not set`},
		{"bad date", []string{"list", "--from", "2024-1-1", "--to", "2024-01-31"}, "invalid --from"},
		{"reversed", []string{"list", "--from", "2024-02-01", "--to", "2024-01-01"}, "before"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := run(t, "", append(tt.args, "--config", path)...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestSummarize(t *testing.T) {
	tests := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"  a\n\tb  c ", 10, "a b c"},
		{"abcdef", 3, "abc…"},
		{"噗浪噗浪", 2, "噗浪…"},
		{"exact", 5, "exact"},
		{"", 5, ""},
	}
	for _, tt := range tests {
		if got := summarize(tt.in, tt.n); got != tt.want {
			t.Errorf("summarize(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
		}
	}
}
