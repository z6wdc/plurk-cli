package plurk

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// Me returns the authorized user.
func (c *Client) Me(ctx context.Context) (*User, error) {
	var u User
	if err := c.call(ctx, "Users/me", nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// GetPlurksOptions are the parameters of Timeline/getPlurks.
type GetPlurksOptions struct {
	// Offset returns only plurks posted before this time. Zero means now.
	Offset time.Time
	// Limit is the maximum number of plurks to return. Zero means the API default.
	Limit int
	// Filter is one of "my", "responded", "private", "favorite", or empty for all.
	Filter string
}

// GetPlurks returns plurks from the user's timeline, newest first.
func (c *Client) GetPlurks(ctx context.Context, opts GetPlurksOptions) ([]Plurk, error) {
	params := url.Values{}
	if !opts.Offset.IsZero() {
		params.Set("offset", opts.Offset.UTC().Format(offsetLayout))
	}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Filter != "" {
		params.Set("filter", opts.Filter)
	}

	var resp struct {
		Plurks []Plurk `json:"plurks"`
	}
	if err := c.call(ctx, "Timeline/getPlurks", params, &resp); err != nil {
		return nil, err
	}
	return resp.Plurks, nil
}

// DeletePlurk permanently deletes one of the user's plurks.
func (c *Client) DeletePlurk(ctx context.Context, plurkID int64) error {
	params := url.Values{"plurk_id": {strconv.FormatInt(plurkID, 10)}}
	return c.call(ctx, "Timeline/plurkDelete", params, nil)
}
