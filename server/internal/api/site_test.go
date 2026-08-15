package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/henrik/tnbrowser-server/internal/model"
	"github.com/henrik/tnbrowser-server/internal/store"
)

// The website's endpoints. Same rules as api_test.go: a real PostgreSQL, and
// skipped without TNB_TEST_DSN.

// get fetches a site endpoint and decodes it into v, failing on any status but
// the one expected.
func get(t *testing.T, ts *httptest.Server, path string, want int, v any) {
	t.Helper()

	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s: status %d, want %d: %s", path, resp.StatusCode, want, body)
	}
	if v == nil {
		return
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("GET %s: decode: %v", path, err)
	}
}

// community seeds four warriors and two tribes, then disbands one of them --
// the disbanded row is what several of these tests are about, since it stays in
// the database for old history and mail to name.
//
// It answers with both ids, as strings, because that is how a URL wants them.
func community(t *testing.T, ts *httptest.Server, st *store.Store) (active, disbanded string) {
	t.Helper()

	for _, guid := range []string{"1000", "1001", "1002", "1003"} {
		account(t, ts, guid)
	}

	// warrior-1000 founds both, so it is the sole leader of each and can
	// disband one on its own vote.
	ctx := t.Context()
	if err := st.CreateClan(ctx, "1000", "Test Clan", "[TC]", false, true,
		"We are a test clan."); err != nil {
		t.Fatalf("create Test Clan: %v", err)
	}
	if err := st.CreateClan(ctx, "1000", "Gone Clan", "[GC]", false, false, ""); err != nil {
		t.Fatalf("create Gone Clan: %v", err)
	}

	active, disbanded = clanID(t, st, "Test Clan"), clanID(t, st, "Gone Clan")

	// Founding a tribe does not make you wear its tag -- that is a separate
	// choice, and the directory shows the tag actually being worn.
	if err := st.SetActiveClan(ctx, "1000", numeric(t, active)); err != nil {
		t.Fatalf("wear the tag: %v", err)
	}
	if _, err := st.Disband(ctx, "1000", numeric(t, disbanded), true); err != nil {
		t.Fatalf("disband: %v", err)
	}
	return active, disbanded
}

func numeric(t *testing.T, id string) int64 {
	t.Helper()

	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		t.Fatalf("clan id %q: %v", id, err)
	}
	return n
}

// clanID looks a tribe up by name. Only usable while it is still active, which
// is why community reads both ids before it disbands either.
func clanID(t *testing.T, st *store.Store, name string) string {
	t.Helper()

	hits, err := st.ClanSearch(t.Context(), name)
	if err != nil || len(hits) != 1 {
		t.Fatalf("looking up %s: %d hits, %v", name, len(hits), err)
	}
	return hits[0].ID
}

func TestWarriorDirectoryPaginatesAndSearches(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)
	community(t, ts, st)

	var all page[model.DirectoryWarrior]
	get(t, ts, "/api/warriors", http.StatusOK, &all)
	if all.Total != 4 || len(all.Items) != 4 {
		t.Fatalf("%d of %d warriors, want 4", len(all.Items), all.Total)
	}
	if all.Items[0].Name != "warrior-1000" {
		t.Errorf("first row %q -- the listing should be ordered by name", all.Items[0].Name)
	}
	if all.Items[0].Tag != "[TC]" {
		t.Errorf("founder's tag %q, want the tribe they wear", all.Items[0].Tag)
	}

	// A disbanded tribe must not be counted against the warrior who founded it.
	if all.Items[0].Tribes != 1 {
		t.Errorf("founder is in %d tribes, want 1 -- the disbanded one should not count",
			all.Items[0].Tribes)
	}

	var first page[model.DirectoryWarrior]
	get(t, ts, "/api/warriors?size=2&page=2", http.StatusOK, &first)
	if first.Total != 4 || first.Pages != 2 || len(first.Items) != 2 {
		t.Errorf("page 2 of 2: %+v", first)
	}
	if first.Items[0].Name != "warrior-1002" {
		t.Errorf("second page starts at %q, want warrior-1002", first.Items[0].Name)
	}

	// Substring, not prefix -- the difference from the in-game UserSearch.
	var found page[model.DirectoryWarrior]
	get(t, ts, "/api/warriors?q=1003", http.StatusOK, &found)
	if found.Total != 1 || found.Items[0].Name != "warrior-1003" {
		t.Errorf("searching for a name fragment found %+v", found.Items)
	}
}

func TestTribeDirectoryHidesDisbandedTribes(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)
	community(t, ts, st)

	var got page[model.DirectoryTribe]
	get(t, ts, "/api/tribes", http.StatusOK, &got)
	if got.Total != 1 {
		t.Fatalf("%d tribes listed, want only the active one: %+v", got.Total, got.Items)
	}
	if got.Items[0].Name != "Test Clan" || got.Items[0].Members != 1 {
		t.Errorf("listed %+v", got.Items[0])
	}

	// The tag is searchable as well as the name, which the game's own search
	// never offered.
	var byTag page[model.DirectoryTribe]
	get(t, ts, "/api/tribes?q=TC", http.StatusOK, &byTag)
	if byTag.Total != 1 {
		t.Errorf("searching by tag found %d", byTag.Total)
	}
}

func TestDisbandedTribeIsNotReachableByGuessingAnId(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)
	active, disbanded := community(t, ts, st)

	var ok tribePage
	get(t, ts, "/api/tribes/"+active, http.StatusOK, &ok)
	if ok.Tribe.Name != "Test Clan" || len(ok.Tribe.Members) != 1 {
		t.Errorf("active tribe came back as %+v", ok.Tribe)
	}

	// Disbanding only sets active = FALSE, and ClanView deliberately still
	// resolves the row so old history and mail can name it. The site must not.
	get(t, ts, "/api/tribes/"+disbanded, http.StatusNotFound, nil)
	get(t, ts, "/api/tribes/999999", http.StatusNotFound, nil)
	get(t, ts, "/api/tribes/not-a-number", http.StatusNotFound, nil)
}

func TestWarriorProfileCarriesMembershipsAndHistory(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)
	community(t, ts, st)

	var got warriorPage
	get(t, ts, "/api/warriors/1000", http.StatusOK, &got)

	if got.Warrior.Name != "warrior-1000" {
		t.Fatalf("profile is for %q", got.Warrior.Name)
	}
	if len(got.Warrior.Memberships) == 0 {
		t.Error("no memberships on a warrior who founded a tribe")
	}
	if len(got.History) == 0 {
		t.Error("no history on a warrior who founded a tribe")
	}

	get(t, ts, "/api/warriors/nobody", http.StatusNotFound, nil)
}

func TestStatsCountTheCommunity(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)
	community(t, ts, st)

	var got model.Counts
	get(t, ts, "/api/stats", http.StatusOK, &got)
	if got.Warriors != 4 || got.Tribes != 1 {
		t.Errorf("counted %+v, want 4 warriors and 1 active tribe", got)
	}
	// Every fixture warrior authenticated a moment ago, so all four are inside
	// the presence window.
	if got.Online != 4 {
		t.Errorf("%d online, want 4", got.Online)
	}
}

// A mistyped endpoint must not be answered with the app shell, and a route the
// browser resolves itself must not be answered with a 404.
func TestUnknownPathsAreSortedByPrefix(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)

	resp, err := ts.Client().Get(ts.URL + "/api/warriorz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("mistyped endpoint answered %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("mistyped endpoint answered %s, want JSON", ct)
	}

	// With no built UI in the test binary the app shell is a 503, which is
	// still the site handler rather than the mux's 404 -- that is what this is
	// checking. A release build serves index.html here.
	shell, err := ts.Client().Get(ts.URL + "/warriors/1000")
	if err != nil {
		t.Fatal(err)
	}
	defer shell.Body.Close()

	if shell.StatusCode == http.StatusNotFound {
		t.Error("a client-side route was answered with a 404; a refresh would break")
	}
}

// An asset that is not there must not be answered with the app shell. The
// browser asked for a stylesheet and would get HTML, which it reports as a
// parse error rather than as the missing file it is.
func TestAMissingAssetIsAMiss(t *testing.T) {
	ts := newServer(t, testStore(t))

	resp, err := ts.Client().Get(ts.URL + "/assets/never-built.css")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("a missing asset answered %d, want 404", resp.StatusCode)
	}
}

// The client's own routes must keep their quirks. Both are load-bearing: the
// blank first line is what the live TribesNext server sends, and the closed
// connection is the only completion signal Torque's HTTPObject has.
func TestTheClientProtocolIsUnchangedByTheWebsite(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)
	account(t, ts, "1000")

	resp, err := ts.Client().PostForm(ts.URL+"/cert", map[string][]string{
		"guid": {"1000"}, "uuid": {"session-1000"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.HasPrefix(string(body), "\n") {
		t.Errorf("/cert lost its leading blank line: %q", string(body))
	}
	if !resp.Close {
		t.Error("/cert kept the connection alive; the client's request queue would stall")
	}
}
