package plurk

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGetPlurks(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/APP/Timeline/getPlurks" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"offset": "2024-01-02T03:04:05", "limit": "30", "filter": "my"}
		for k, v := range want {
			if got := r.PostForm.Get(k); got != v {
				t.Errorf("param %s = %q, want %q", k, got, v)
			}
		}
		w.Write([]byte(`{
			"plurks": [
				{"plurk_id": 2, "owner_id": 42, "posted": "Tue, 02 Jan 2024 03:00:00 GMT", "content_raw": "b"},
				{"plurk_id": 1, "owner_id": 42, "posted": "Mon, 01 Jan 2024 03:00:00 GMT", "content_raw": "a"}
			],
			"plurk_users": {}
		}`))
	})

	// The offset is given in a non-UTC zone to check it is converted.
	tokyo := time.FixedZone("JST", 9*60*60)
	offset := time.Date(2024, 1, 2, 12, 4, 5, 0, tokyo)
	got, err := c.GetPlurks(context.Background(), GetPlurksOptions{Offset: offset, Limit: 30, Filter: "my"})
	if err != nil {
		t.Fatalf("GetPlurks: %v", err)
	}
	if len(got) != 2 || got[0].PlurkID != 2 || got[1].ContentRaw != "a" {
		t.Errorf("GetPlurks = %+v", got)
	}
}

func TestDeletePlurk(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/APP/Timeline/plurkDelete" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("plurk_id"); got != "1234567890" {
			t.Errorf("plurk_id = %q", got)
		}
		w.Write([]byte(`{"success_text": "ok"}`))
	})
	if err := c.DeletePlurk(context.Background(), 1234567890); err != nil {
		t.Errorf("DeletePlurk: %v", err)
	}
}

func TestDeletePlurkNotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error_text": "Plurk not found"}`))
	})
	err := c.DeletePlurk(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "Plurk not found") {
		t.Errorf("DeletePlurk error = %v, want Plurk not found", err)
	}
}

func TestGetPlurksOmitsZeroParams(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"offset", "limit", "filter"} {
			if r.PostForm.Has(k) {
				t.Errorf("param %s should be omitted", k)
			}
		}
		w.Write([]byte(`{"plurks": []}`))
	})
	got, err := c.GetPlurks(context.Background(), GetPlurksOptions{})
	if err != nil {
		t.Fatalf("GetPlurks: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("GetPlurks = %+v, want empty", got)
	}
}
