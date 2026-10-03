package timerange

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/z6wdc/plurk-cli/internal/plurk"
)

var taipei = time.FixedZone("CST", 8*60*60)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		from, to  string
		wantStart time.Time
		wantEnd   time.Time
		wantErr   string
	}{
		{
			name: "year", from: "2015-01-01", to: "2015-12-31",
			wantStart: time.Date(2015, 1, 1, 0, 0, 0, 0, taipei),
			wantEnd:   time.Date(2016, 1, 1, 0, 0, 0, 0, taipei),
		},
		{
			name: "single day", from: "2024-02-29", to: "2024-02-29",
			wantStart: time.Date(2024, 2, 29, 0, 0, 0, 0, taipei),
			wantEnd:   time.Date(2024, 3, 1, 0, 0, 0, 0, taipei),
		},
		{name: "to before from", from: "2024-01-02", to: "2024-01-01", wantErr: "before"},
		{name: "bad from", from: "2024/01/01", to: "2024-01-01", wantErr: "--from"},
		{name: "bad to", from: "2024-01-01", to: "2024-13-01", wantErr: "--to"},
		{name: "empty", from: "", to: "2024-01-01", wantErr: "--from"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.from, tt.to, taipei)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Parse error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !got.Start.Equal(tt.wantStart) || !got.End.Equal(tt.wantEnd) {
				t.Errorf("Parse = [%v, %v), want [%v, %v)", got.Start, got.End, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestContains(t *testing.T) {
	r, err := Parse("2024-01-01", "2024-01-01", taipei)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		t    time.Time
		want bool
	}{
		{time.Date(2024, 1, 1, 0, 0, 0, 0, taipei), true},
		{time.Date(2024, 1, 1, 23, 59, 59, 0, taipei), true},
		{time.Date(2024, 1, 2, 0, 0, 0, 0, taipei), false},
		{time.Date(2023, 12, 31, 23, 59, 59, 0, taipei), false},
		// 2023-12-31 16:00 UTC is 2024-01-01 00:00 in Taipei.
		{time.Date(2023, 12, 31, 16, 0, 0, 0, time.UTC), true},
	}
	for _, tt := range tests {
		if got := r.Contains(tt.t); got != tt.want {
			t.Errorf("Contains(%v) = %v, want %v", tt.t, got, tt.want)
		}
	}
}

// fakeLister emulates getPlurks over a fixed timeline: it returns up to
// limit plurks strictly older than offset (truncated to seconds), newest first.
type fakeLister struct {
	plurks  []plurk.Plurk // newest first
	limit   int
	calls   []time.Time
	err     error
	errAt   int  // return err on this call number (1-based)
	ignored bool // ignore offset, to simulate a broken API
}

func (f *fakeLister) GetPlurks(_ context.Context, opts plurk.GetPlurksOptions) ([]plurk.Plurk, error) {
	f.calls = append(f.calls, opts.Offset)
	if f.err != nil && len(f.calls) == f.errAt {
		return nil, f.err
	}
	if opts.Filter != "my" {
		return nil, errors.New("filter must be my")
	}
	offset := opts.Offset.UTC().Truncate(time.Second)
	limit := f.limit
	if limit == 0 {
		limit = opts.Limit
	}
	var page []plurk.Plurk
	for _, p := range f.plurks {
		if f.ignored || p.Posted.Before(offset) {
			page = append(page, p)
			if len(page) == limit {
				break
			}
		}
	}
	return page, nil
}

func mk(id, owner int64, posted time.Time) plurk.Plurk {
	return plurk.Plurk{PlurkID: id, OwnerID: owner, Posted: plurk.Time{Time: posted.UTC()}}
}

func collect(t *testing.T, seq func(func(plurk.Plurk, error) bool)) ([]int64, error) {
	t.Helper()
	var ids []int64
	for p, err := range seq {
		if err != nil {
			return ids, err
		}
		ids = append(ids, p.PlurkID)
	}
	return ids, nil
}

func day(d, h int) time.Time {
	return time.Date(2024, 1, d, h, 0, 0, 0, taipei)
}

func TestPlurks(t *testing.T) {
	const me = 42
	timeline := []plurk.Plurk{
		mk(10, me, day(5, 12)), // after range
		mk(9, me, day(4, 0)),   // exactly at range end: excluded
		mk(8, me, day(3, 23)),
		mk(7, 99, day(3, 20)), // someone else's
		mk(6, me, day(3, 10)),
		mk(5, me, day(2, 10)),
		mk(4, me, day(2, 9)),
		mk(3, me, day(2, 0)), // exactly at range start: included
		mk(2, me, day(1, 23)),
		mk(1, me, day(1, 1)),
	}
	r := Range{Start: day(2, 0), End: day(4, 0)}

	tests := []struct {
		name  string
		limit int
	}{
		{"one page", 30},
		{"page size 1", 1},
		{"page size 2", 2},
		{"page size 3", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeLister{plurks: timeline, limit: tt.limit}
			got, err := collect(t, Plurks(context.Background(), f, r, me))
			if err != nil {
				t.Fatalf("Plurks: %v", err)
			}
			if want := []int64{8, 6, 5, 4, 3}; !slices.Equal(got, want) {
				t.Errorf("Plurks = %v, want %v", got, want)
			}
			if !f.calls[0].Equal(r.End) {
				t.Errorf("first offset = %v, want range end %v", f.calls[0], r.End)
			}
		})
	}
}

func TestPlurksStopsAtRangeStart(t *testing.T) {
	timeline := []plurk.Plurk{mk(3, 1, day(3, 0)), mk(2, 1, day(1, 0))}
	for i := int64(1); i < 100; i++ {
		timeline = append(timeline, mk(-i, 1, day(1, 0).Add(-time.Duration(i)*time.Hour)))
	}
	f := &fakeLister{plurks: timeline, limit: 2}
	r := Range{Start: day(2, 0), End: day(4, 0)}

	got, err := collect(t, Plurks(context.Background(), f, r, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int64{3}) {
		t.Errorf("Plurks = %v, want [3]", got)
	}
	if len(f.calls) != 1 {
		t.Errorf("made %d calls, want 1 (should stop at first plurk before range)", len(f.calls))
	}
}

func TestPlurksSameSecondAcrossPages(t *testing.T) {
	sec := day(2, 12)
	timeline := []plurk.Plurk{
		mk(5, 1, sec.Add(2*time.Second)),
		mk(4, 1, sec.Add(time.Second)),
		mk(3, 1, sec), // last on page 1
		mk(2, 1, sec), // same second as 3; skipped if the next offset were exactly sec
		mk(1, 1, sec.Add(-time.Hour)),
	}
	f := &fakeLister{plurks: timeline, limit: 3}
	r := Range{Start: day(1, 0), End: day(3, 0)}

	got, err := collect(t, Plurks(context.Background(), f, r, 1))
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{5, 4, 3, 2, 1}; !slices.Equal(got, want) {
		t.Errorf("Plurks = %v, want %v", got, want)
	}
}

func TestPlurksEmpty(t *testing.T) {
	f := &fakeLister{}
	got, err := collect(t, Plurks(context.Background(), f, Range{Start: day(1, 0), End: day(2, 0)}, 1))
	if err != nil || len(got) != 0 {
		t.Errorf("Plurks = %v, %v; want empty, nil", got, err)
	}
}

func TestPlurksError(t *testing.T) {
	boom := errors.New("boom")
	timeline := []plurk.Plurk{mk(3, 1, day(3, 0)), mk(2, 1, day(2, 12)), mk(1, 1, day(2, 6))}
	f := &fakeLister{plurks: timeline, limit: 1, err: boom, errAt: 2}

	got, err := collect(t, Plurks(context.Background(), f, Range{Start: day(1, 0), End: day(4, 0)}, 1))
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want %v", err, boom)
	}
	if !slices.Equal(got, []int64{3}) {
		t.Errorf("yielded %v before error, want [3]", got)
	}
}

func TestPlurksStuckPagination(t *testing.T) {
	f := &fakeLister{plurks: []plurk.Plurk{mk(2, 1, day(3, 0)), mk(1, 1, day(2, 0))}, limit: 1, ignored: true}
	// End is before the plurks, so an API that ignores offset never moves backwards.
	_, err := collect(t, Plurks(context.Background(), f, Range{Start: day(1, 0), End: day(2, 12)}, 1))
	if err == nil || !strings.Contains(err.Error(), "pagination stuck") {
		t.Errorf("error = %v, want pagination stuck", err)
	}
}

func TestPlurksEarlyBreak(t *testing.T) {
	timeline := []plurk.Plurk{mk(3, 1, day(3, 0)), mk(2, 1, day(2, 12)), mk(1, 1, day(2, 6))}
	f := &fakeLister{plurks: timeline, limit: 1}
	for p, err := range Plurks(context.Background(), f, Range{Start: day(1, 0), End: day(4, 0)}, 1) {
		if err != nil {
			t.Fatal(err)
		}
		if p.PlurkID != 3 {
			t.Errorf("first plurk = %d, want 3", p.PlurkID)
		}
		break
	}
	if len(f.calls) != 1 {
		t.Errorf("made %d calls after break, want 1", len(f.calls))
	}
}
