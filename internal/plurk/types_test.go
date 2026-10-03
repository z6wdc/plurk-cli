package plurk

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTimeUnmarshal(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Time
		wantErr bool
	}{
		{`"Fri, 05 Jun 2009 23:07:13 GMT"`, time.Date(2009, 6, 5, 23, 7, 13, 0, time.UTC), false},
		{`"Mon, 01 Jan 2024 00:00:00 GMT"`, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), false},
		{`"2009-06-05T23:07:13Z"`, time.Time{}, true},
		{`""`, time.Time{}, true},
		{`12345`, time.Time{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			var got Time
			err := json.Unmarshal([]byte(tt.in), &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Unmarshal error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && !got.Equal(tt.want) {
				t.Errorf("Unmarshal = %v, want %v", got.Time, tt.want)
			}
			if err == nil && got.Location() != time.UTC {
				t.Errorf("location = %v, want UTC", got.Location())
			}
		})
	}
}

func TestTimeRoundTrip(t *testing.T) {
	in := Time{time.Date(2009, 6, 5, 23, 7, 13, 0, time.UTC)}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"Fri, 05 Jun 2009 23:07:13 GMT"` {
		t.Errorf("Marshal = %s", data)
	}
	var out Time
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !out.Equal(in.Time) {
		t.Errorf("round trip = %v, want %v", out.Time, in.Time)
	}
}

func TestPlurkUnmarshal(t *testing.T) {
	data := `{
		"plurk_id": 1234567890,
		"owner_id": 42,
		"posted": "Fri, 05 Jun 2009 23:07:13 GMT",
		"content_raw": "hello **world**",
		"content": "hello <b>world</b>",
		"qualifier": "says",
		"unknown_field": true
	}`
	var p Plurk
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if p.PlurkID != 1234567890 || p.OwnerID != 42 || p.ContentRaw != "hello **world**" || p.Qualifier != "says" {
		t.Errorf("Plurk = %+v", p)
	}
	if !p.Posted.Equal(time.Date(2009, 6, 5, 23, 7, 13, 0, time.UTC)) {
		t.Errorf("Posted = %v", p.Posted.Time)
	}
}

func TestPlurkURL(t *testing.T) {
	p := Plurk{PlurkID: 1234567890}
	if got, want := p.URL(), "https://www.plurk.com/p/kf12oi"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}
