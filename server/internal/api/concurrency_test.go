package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/henrik/tnbrowser-server/internal/auth"
)

// Ten players using the server at the same time.
//
// Nothing about the design makes this obvious: net/http runs every request in
// its own goroutine, so the question is not whether requests overlap -- they
// always do -- but whether anything shared between them is safe to touch at
// once. Three things are shared, and each is here for a different reason:
//
//	the session table    a map behind a sync.Mutex (internal/auth)
//	the ordinal registry written only from init(), read-only forever after
//	the database pool    pgxpool's, concurrent by contract, capped at 5 in
//	                     production -- so ten players share five connections
//	                     and the pool has to queue rather than fail
//
// Run these with -race, or they prove much less than they look like they do.

// warriors is the cast. Ten, which is more than the pool has connections, so
// the queueing path is exercised rather than merely the happy one.
const warriors = 10

func guidOf(i int) string { return strconv.Itoa(1000 + i) }

// TestTenWarriorsAtOnce has every player mail every other player, all at the
// same time. Each send is a write inside a transaction and each poll a read, so
// this is the pair of paths that would collide if anything were unguarded.
func TestTenWarriorsAtOnce(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)

	for i := 0; i < warriors; i++ {
		account(t, ts, guidOf(i))
	}

	var wg sync.WaitGroup
	errs := make(chan string, warriors*warriors)

	for i := 0; i < warriors; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			from := guidOf(i)
			for j := 0; j < warriors; j++ {
				if j == i {
					continue // nobody needs their own mail
				}
				to := "warrior-" + guidOf(j)
				answer := db(t, ts, from, "scalar", "5",
					strings.Join([]string{to, "", fmt.Sprintf("From %s", from),
						"Body"}, "\t"))
				if code, _ := answer["code"].(float64); code != 0 {
					errs <- fmt.Sprintf("%s -> %s refused: %v", from, to, answer["message"])
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)

	for e := range errs {
		t.Error(e)
	}

	// Every mailbox has to hold one message from each of the other nine. A
	// short count means a write that was accepted and dropped, which is the
	// failure worth catching; a long one means a write applied twice.
	for i := 0; i < warriors; i++ {
		who := guidOf(i)
		got := rows(t, db(t, ts, who, "array", "1", "0"))

		delivered := 0
		for _, r := range got {
			if strings.Contains(r, "From 10") {
				delivered++
			}
		}
		if delivered != warriors-1 {
			t.Errorf("%s received %d messages, want %d", who, delivered, warriors-1)
		}
	}
}

// The session table is the one piece of state every single request touches.
// This negotiates, refreshes and reads sessions for ten players at once, which
// is what a server with ten players on it does continuously.
func TestSessionTableUnderConcurrentUse(t *testing.T) {
	ts, sessions := sessionServer(t)

	var wg sync.WaitGroup
	for p := 0; p < warriors; p++ {
		guid := fmt.Sprintf("45101%02d", p)
		sessions.GrantToken("token-"+guid, auth.Identity{GUID: guid, Name: "warrior-" + guid})

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				got, code := sessionPost(t, ts, map[string]any{
					"guid": guid, "uuid": "token-" + guid,
				})
				if code != http.StatusOK || got["state"] != "refreshed" {
					t.Errorf("%s: keepalive answered %v (%d)", guid, got, code)
					return
				}
			}
		}()

		// And one granting fresh sessions alongside, since Grant and Lookup
		// take the same lock and a keepalive-only test would never contend.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				if _, err := sessions.Grant(auth.Identity{GUID: guid + "x"}); err != nil {
					t.Errorf("grant: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// Ten players in the game while somebody reads the website. They share a
// database pool and nothing else, and this is what happens whenever anyone has
// the site open during a game.
func TestSiteAndGameAtOnce(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)
	community(t, ts, st)

	var wg sync.WaitGroup

	for i := 0; i < warriors; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 8; n++ {
				db(t, ts, "1000", "array", "4", "Test")
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 0; n < 20; n++ {
			resp, err := ts.Client().Get(ts.URL + "/api/warriors")
			if err != nil {
				t.Errorf("site: %v", err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("site answered %d", resp.StatusCode)
				return
			}
		}
	}()

	wg.Wait()
}

// Ten players negotiating a session from cold, at once, through the front door.
//
// The bypass stands in for the challenge exchange the containers cannot
// perform; what is under test is the table and the account-creation path behind
// it, not the cryptography. Each first request also writes an accounts row and
// delivers a welcome mail, so this is ten concurrent first-sightings -- the
// narrowest window the server has.
func TestTenColdStartsAtOnce(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)

	var wg sync.WaitGroup
	for i := 0; i < warriors; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			guid := guidOf(i)
			answer := db(t, ts, guid, "scalar", "5", "")
			if _, ok := answer["code"]; !ok {
				t.Errorf("%s got no answer at all", guid)
			}
		}()
	}
	wg.Wait()

	// Exactly one welcome each. EnsureAccount claims the greeting through
	// "did THIS insert create the row?", so two concurrent first requests from
	// the same player would otherwise both send one.
	for i := 0; i < warriors; i++ {
		guid := guidOf(i)
		got := rows(t, db(t, ts, guid, "array", "1", "0"))

		welcomes := 0
		for _, r := range got {
			if strings.Contains(r, "Browser and mail are back") {
				welcomes++
			}
		}
		if welcomes != 1 {
			t.Errorf("%s got %d welcome messages, want exactly 1", guid, welcomes)
		}
	}
}
