package auth

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// oracle stands in for TribesNext: 200 with a profile for a live pair, 401
// otherwise. body is the profile JSON, so a test can post any shape it likes.
func oracle(t *testing.T, body string) *Verifier {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.FormValue("guid") == "" || r.FormValue("uuid") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// The live server prefixes its body with a blank line; reproduced
			// so trimLeadingSpace stays exercised.
			_, _ = io.WriteString(w, "\n"+body)
		}))
	t.Cleanup(srv.Close)

	v := NewVerifier(srv.URL, time.Minute)
	v.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return v
}

func TestVerifyReadsCreationDate(t *testing.T) {
	v := oracle(t, `{"guid":"4510186","name":"orange01","creation":"1300000000"}`)

	id, err := v.Verify(context.Background(), "4510186", "session")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if id.Name != "orange01" {
		t.Errorf("name = %q, want orange01", id.Name)
	}
	if id.Created != 1300000000 {
		t.Errorf("created = %d, want 1300000000", id.Created)
	}
}

// A date this server cannot read must never cost anybody a login: the identity
// still comes back, just without a registration date, and the store falls back
// to first sighting.
func TestVerifyUnreadableCreationStillAuthorises(t *testing.T) {
	// now is pinned so "future" below is unambiguous, and so a real clock
	// rolling forward cannot turn a rejection into an acceptance.
	const pinned = 1786000000

	cases := []struct {
		name string
		body string
	}{
		{"absent", `{"guid":"4510186","name":"orange01"}`},
		{"null", `{"guid":"4510186","name":"orange01","creation":null}`},
		{"empty", `{"guid":"4510186","name":"orange01","creation":""}`},
		{"spaces", `{"guid":"4510186","name":"orange01","creation":"   "}`},
		// The format most likely to be TribesNext's if the guess is wrong: a
		// SQL datetime rather than unix seconds.
		{"datetime", `{"guid":"4510186","name":"orange01","creation":"2009-03-14 12:00:00"}`},
		// Each of these parses as an integer and would sail through a bare
		// ParseInt, rendering as a date fifty-six thousand years out, before
		// Tribes 2 shipped, or at the epoch.
		{"milliseconds", `{"guid":"4510186","name":"orange01","creation":"1300000000000"}`},
		{"negative", `{"guid":"4510186","name":"orange01","creation":"-1300000000"}`},
		{"zero", `{"guid":"4510186","name":"orange01","creation":"0"}`},
		{"before tribes 2", `{"guid":"4510186","name":"orange01","creation":"100000000"}`},
		{"future", `{"guid":"4510186","name":"orange01","creation":"1900000000"}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := oracle(t, c.body)
			v.now = func() time.Time { return time.Unix(pinned, 0) }

			id, err := v.Verify(context.Background(), "4510186", "session")
			if err != nil {
				t.Fatalf("verify: %v", err)
			}
			if id.GUID != "4510186" || id.Name != "orange01" {
				t.Errorf("identity = %+v, want the profile's guid and name", id)
			}
			if id.Created != 0 {
				t.Errorf("created = %d, want 0 (unknown)", id.Created)
			}
		})
	}
}

// The boundaries of the accepted band, so a later edit cannot widen or narrow
// it by accident.
func TestParseCreationBand(t *testing.T) {
	const pinned = 1786000000

	v := NewVerifier("", time.Minute)
	v.now = func() time.Time { return time.Unix(pinned, 0) }

	cases := []struct {
		raw  string
		want int64
		ok   bool
	}{
		{"946684800", 946684800, true},     // 2000-01-01, the earliest accepted
		{"946684799", 0, false},            // one second before it
		{"1786086400", 1786086400, true},   // pinned + a day, the latest accepted
		{"1786086401", 0, false},           // one second past it
		{" 1300000000 ", 1300000000, true}, // surrounding space is tolerated
	}

	for _, c := range cases {
		got, ok := v.parseCreation(c.raw)
		if ok != c.ok || got != c.want {
			t.Errorf("parseCreation(%q) = %d, %v; want %d, %v", c.raw, got, ok, c.want, c.ok)
		}
	}
}

// The cache returns a whole Identity, so the registration date has to survive a
// hit as well as a miss -- otherwise the first request of a TTL window would
// see a date and the rest would not.
func TestCachedIdentityKeepsCreationDate(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			hits++
			_, _ = io.WriteString(w,
				`{"guid":"4510186","name":"orange01","creation":"1300000000"}`)
		}))
	t.Cleanup(srv.Close)

	v := NewVerifier(srv.URL, time.Minute)
	v.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	for i := 0; i < 2; i++ {
		id, err := v.Verify(context.Background(), "4510186", "session")
		if err != nil {
			t.Fatalf("verify %d: %v", i, err)
		}
		if id.Created != 1300000000 {
			t.Fatalf("verify %d: created = %d, want 1300000000", i, id.Created)
		}
	}
	if hits != 1 {
		t.Errorf("upstream called %d times, want 1 (second read should be cached)", hits)
	}
}

// A profile for somebody else is refused whatever else it carries -- guarding
// the case where upstream does not enforce the guid/uuid pairing after all.
func TestVerifyRefusesMismatchedProfile(t *testing.T) {
	v := oracle(t, `{"guid":"4120041","name":"Shifter","creation":"1300000000"}`)

	if _, err := v.Verify(context.Background(), "4510186", "session"); err != ErrUnauthorised {
		t.Errorf("err = %v, want ErrUnauthorised", err)
	}
}
