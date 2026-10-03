package plurk

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// User is a Plurk user as returned by Users/me.
type User struct {
	ID          int64  `json:"id"`
	NickName    string `json:"nick_name"`
	DisplayName string `json:"display_name"`
	FullName    string `json:"full_name"`
}

// Plurk is a single plurk as returned by the Timeline endpoints.
type Plurk struct {
	PlurkID    int64  `json:"plurk_id"`
	OwnerID    int64  `json:"owner_id"`
	Posted     Time   `json:"posted"`
	ContentRaw string `json:"content_raw"`
	Content    string `json:"content"`
	Qualifier  string `json:"qualifier"`
}

// URL returns the public permalink of the plurk.
func (p *Plurk) URL() string {
	return "https://www.plurk.com/p/" + strconv.FormatInt(p.PlurkID, 36)
}

// Time is a timestamp in Plurk's RFC1123 format, e.g.
// "Fri, 05 Jun 2009 23:07:13 GMT". It is always stored in UTC.
type Time struct {
	time.Time
}

func (t *Time) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("plurk time: %w", err)
	}
	parsed, err := time.Parse(time.RFC1123, s)
	if err != nil {
		return fmt.Errorf("plurk time: %w", err)
	}
	t.Time = parsed.UTC()
	return nil
}

func (t Time) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.UTC().Format(postedLayout))
}

// postedLayout matches what Plurk sends; time.RFC1123 would print "UTC".
const postedLayout = "Mon, 02 Jan 2006 15:04:05 GMT"

// offsetLayout is the format of the getPlurks offset parameter (UTC).
const offsetLayout = "2006-01-02T15:04:05"
