// Package timerange selects plurks posted within a date range.
package timerange

import (
	"context"
	"fmt"
	"iter"
	"time"

	"github.com/z6wdc/plurk-cli/internal/plurk"
)

const (
	dateLayout = "2006-01-02"
	pageSize   = 30
)

// Range is the half-open interval [Start, End).
type Range struct {
	Start time.Time
	End   time.Time
}

// Parse builds a Range from two YYYY-MM-DD dates in loc. Both days are
// inclusive: the range runs from the start of from to the end of to.
func Parse(from, to string, loc *time.Location) (Range, error) {
	start, err := time.ParseInLocation(dateLayout, from, loc)
	if err != nil {
		return Range{}, fmt.Errorf("invalid --from date %q: want YYYY-MM-DD", from)
	}
	last, err := time.ParseInLocation(dateLayout, to, loc)
	if err != nil {
		return Range{}, fmt.Errorf("invalid --to date %q: want YYYY-MM-DD", to)
	}
	if last.Before(start) {
		return Range{}, fmt.Errorf("--to (%s) is before --from (%s)", to, from)
	}
	return Range{Start: start, End: last.AddDate(0, 0, 1)}, nil
}

// Contains reports whether t is within the range.
func (r Range) Contains(t time.Time) bool {
	return !t.Before(r.Start) && t.Before(r.End)
}

// Lister fetches a page of plurks. *plurk.Client implements it.
type Lister interface {
	GetPlurks(ctx context.Context, opts plurk.GetPlurksOptions) ([]plurk.Plurk, error)
}

// Plurks yields the plurks owned by ownerID that were posted within r,
// newest first. It pages backwards from r.End and stops at the first plurk
// older than r.Start. On error it yields the error once and stops.
func Plurks(ctx context.Context, l Lister, r Range, ownerID int64) iter.Seq2[plurk.Plurk, error] {
	return func(yield func(plurk.Plurk, error) bool) {
		offset := r.End
		seen := make(map[int64]bool)
		for {
			page, err := l.GetPlurks(ctx, plurk.GetPlurksOptions{Offset: offset, Limit: pageSize, Filter: "my"})
			if err != nil {
				yield(plurk.Plurk{}, fmt.Errorf("get plurks before %s: %w", offset.Format(time.RFC3339), err))
				return
			}
			if len(page) == 0 {
				return
			}

			for _, p := range page {
				if seen[p.PlurkID] {
					continue
				}
				seen[p.PlurkID] = true

				posted := p.Posted.Time
				if posted.Before(r.Start) {
					return
				}
				if !r.Contains(posted) || p.OwnerID != ownerID {
					continue
				}
				if !yield(p, nil) {
					return
				}
			}

			last := page[len(page)-1].Posted.Time
			if !last.Before(offset) {
				yield(plurk.Plurk{}, fmt.Errorf("pagination stuck: plurk at %s is not older than offset %s",
					last.Format(time.RFC3339), offset.Format(time.RFC3339)))
				return
			}
			// Offsets have one-second precision. Overlap by one second so
			// plurks sharing the last timestamp are not skipped; seen
			// filters out the duplicates.
			next := last.Add(time.Second)
			if !next.Before(offset) {
				next = last
			}
			offset = next
		}
	}
}
