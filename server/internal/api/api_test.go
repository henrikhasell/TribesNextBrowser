package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/henrik/tnbrowser-server/internal/auth"
	"github.com/henrik/tnbrowser-server/internal/chat"
	"github.com/henrik/tnbrowser-server/internal/clancert"
	"github.com/henrik/tnbrowser-server/internal/store"
)

// These run against a real PostgreSQL, because what is worth testing here is
// the SQL: rank gates, cascades, transactions and the high-water filter. A
// fake would only re-assert the shape of the Go code.
//
//	TNB_TEST_DSN=postgres://tnbrowser:tnbrowser@127.0.0.1:5433/tnbrowser go test ./...
//
// Skipped without a DSN so `go test ./...` stays useful on a machine with no
// database.

func testStore(t *testing.T) *store.Store {
	t.Helper()

	dsn := os.Getenv("TNB_TEST_DSN")
	if dsn == "" {
		t.Skip("set TNB_TEST_DSN to run the database tests")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	// Truncate rather than recreate: the schema is applied by hand from
	// migrations/, and a test that silently created its own would stop being a
	// check on the migrations.
	for _, table := range []string{
		"clan_invites", "clan_members", "clan_disband_votes", "mail",
		"buddies", "blocks", "history", "clans", "accounts",
	} {
		if _, err := pool.Exec(context.Background(),
			"TRUNCATE TABLE "+table+" CASCADE"); err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
	return store.New(pool)
}

// newServer wires the front door to a session table with the test accounts
// already in it.
//
// Sessions are granted rather than negotiated because negotiating one needs a
// certificate signed by TribesNext, and only TribesNext can make one of those.
// The exchange itself is covered where it lives, in internal/auth; what these
// tests need is an authenticated caller, and GrantToken is the seam for that.
//
// The names matter: several tests below address mail and invitations by warrior
// name, so each fixture GUID gets the name a real certificate would have
// carried.
func newServer(t *testing.T, st *store.Store) *httptest.Server {
	t.Helper()

	sessions := auth.NewSessions(0)
	for guid := 1000; guid < 1010; guid++ {
		g := strconv.Itoa(guid)
		sessions.GrantToken("session-"+g, auth.Identity{GUID: g, Name: "warrior-" + g})
	}

	srv := &Server{
		Store:    st,
		Sessions: sessions,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return ts
}

// db issues one ordinal and returns the decoded answer.
func db(t *testing.T, ts *httptest.Server, guid, form, ordinal, args string) map[string]any {
	t.Helper()

	payload, _ := json.Marshal(map[string]string{
		"form": form, "ordinal": ordinal, "args": args,
	})
	resp, err := ts.Client().PostForm(ts.URL+"/db", url.Values{
		"guid":    {guid},
		"uuid":    {"session-" + guid},
		"payload": {string(payload)},
	})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s: status %d: %s", form, ordinal, resp.StatusCode, body)
	}

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// account creates a player and returns the high-water mark just after they
// joined.
//
// A first authentication is greeted with one message (store.WelcomeMail), so a
// test that polls array 1 from 0 sees it. Polling from this mark instead is
// both the fix and a truer statement of what these tests mean: the mail their
// own actions produced.
func account(t *testing.T, ts *httptest.Server, guid string) string {
	t.Helper()

	// Any ordinal will do -- the account is created by authenticating, not by
	// what is asked for afterwards.
	db(t, ts, guid, "scalar", "5", "")

	mark := "0"
	for _, row := range rows(t, db(t, ts, guid, "array", "1", "0")) {
		mark = strings.SplitN(row, "\t", 2)[0]
	}
	return mark
}

func statusCode(answer map[string]any) string {
	s, _ := answer["status"].(string)
	return strings.SplitN(s, "\t", 2)[0]
}

func statusField(answer map[string]any, i int) string {
	s, _ := answer["status"].(string)
	f := strings.Split(s, "\t")
	if i >= len(f) {
		return ""
	}
	return f[i]
}

func rows(t *testing.T, answer map[string]any) []string {
	t.Helper()
	raw, _ := answer["rows"].([]any)
	out := make([]string, len(raw))
	for i, r := range raw {
		out[i], _ = r.(string)
	}
	return out
}

// A registration date is written once and never moved.
//
// It used to come from TribesNext, which a certificate does not carry, so an
// account now dates from the first time this server saw it. What has not
// changed is that the date is fixed at that first sighting: a later request
// must not move it, and must not backdate last_seen to it either, which would
// report the player offline forever.
//
// Asserted through scalar 23, the ordinal the shipped warrior profile issues:
// status field 6 is the registered date and field 7 the online flag.
func TestRegistrationDateIsWrittenOnceAndKept(t *testing.T) {
	ts := newServer(t, testStore(t))
	account(t, ts, "1003")

	first := statusField(db(t, ts, "1003", "scalar", "23", "warrior-1003"), 6)
	if first == "" {
		t.Fatal("no registration date on a new account")
	}

	db(t, ts, "1003", "scalar", "5", "")

	answer := db(t, ts, "1003", "scalar", "23", "warrior-1003")
	if got := statusField(answer, 6); got != first {
		t.Errorf("registered moved from %q to %q on a later request", first, got)
	}
	if got := statusField(answer, 7); got != "1" {
		t.Errorf("online = %q, want 1 -- last_seen was overwritten", got)
	}
}

func TestUnauthenticatedIsRefused(t *testing.T) {
	ts := newServer(t, testStore(t))

	resp, err := ts.Client().Post(ts.URL+"/db", "application/x-www-form-urlencoded",
		strings.NewReader("payload={}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", resp.StatusCode)
	}
}

// An ordinal nobody implements must refuse loudly. An empty success would
// render as an empty pane, which is indistinguishable from "there is nothing
// here" and hides the gap.
func TestUnknownOrdinalRefusesRatherThanEmpties(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)

	answer := db(t, ts, "1001", "scalar", "999", "")
	if statusCode(answer) == "0" {
		t.Errorf("unknown ordinal answered success: %v", answer["status"])
	}
}

// The certificate is the identity every community pane reads, and its layout is
// load-bearing: field 3 of record 0 reaches the filesystem as the mail cache
// path, and webbrowser.cs compares it against field 3 of a row quad.
func TestCertificateLayout(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)

	// Two ordinals first, so the account exists and owns a tribe.
	db(t, ts, "1001", "scalar", "16", "Big Sucka Fishes\t[BSF]\t1")

	resp, err := ts.Client().PostForm(ts.URL+"/cert", url.Values{
		"guid": {"1001"}, "uuid": {"session-1001"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out struct {
		Cert string `json:"cert"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}

	records := strings.Split(out.Cert, "\n")
	if len(records) < 3 {
		t.Fatalf("certificate has %d records: %q", len(records), out.Cert)
	}
	if got := strings.Split(records[0], "\t"); len(got) != 4 || got[3] != "1001" {
		t.Errorf("record 0 = %q, want a four-field quad ending in the GUID", records[0])
	}
	if records[1] != "1" {
		t.Errorf("record 1 = %q, want the tribe count", records[1])
	}
	if fields := strings.Split(records[2], "\t"); len(fields) != 6 || fields[4] != "4" {
		t.Errorf("record 2 = %q, want six fields with the admin level in field 4",
			records[2])
	}
}

// A tag's own whitespace is the only separator there is.
//
// Nothing between a tag and a name is ever inserted for you: server.cs:689
// builds the in-game name as tag-then-colour-code-then-name, and
// webstuff.cs:12 and :29 do the same for the browser's two link forms. So a
// clan that wants "orange01 [BSF]" registers " [BSF]", with the leading space,
// and the shipped create dialog previews exactly that while they type
// (webbrowser.cs:677) before sending the field unmodified (:167).
//
// Trimming it server-side made that impossible, and invisibly: the tag came
// back one character shorter than the one the player had just been shown.
func TestTagKeepsItsSpacing(t *testing.T) {
	ts := newServer(t, testStore(t))

	// Leading, because append is set and the tag therefore follows the name.
	db(t, ts, "1001", "scalar", "16", "Big Sucka Fishes\t [BSF]\t1")
	// Founding a clan does not wear its tag; scalar 25 does, and it is the
	// worn one that record 0 carries into the game.
	db(t, ts, "1001", "scalar", "25", "Big Sucka Fishes")

	if got := certField(t, ts, "1001", 1); got != " [BSF]" {
		t.Errorf("worn tag = %q, want %q -- the space was trimmed", got, " [BSF]")
	}

	// And again through the edit path, which is a separate call site.
	db(t, ts, "1001", "scalar", "30", "Big Sucka Fishes\t [bsf]")

	if got := certField(t, ts, "1001", 1); got != " [bsf]" {
		t.Errorf("edited tag = %q, want %q -- the space was trimmed", got, " [bsf]")
	}
}

// certField returns one field of record 0 of the certificate -- the quad the
// shipped screens read the player's own name and tag out of.
func certField(t *testing.T, ts *httptest.Server, guid string, i int) string {
	t.Helper()

	resp, err := ts.Client().PostForm(ts.URL+"/cert", url.Values{
		"guid": {guid}, "uuid": {"session-" + guid},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out struct {
		Cert string `json:"cert"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.Split(out.Cert, "\n")[0], "\t")[i]
}

// The first time a GUID authenticates it is greeted by mail, because mail is
// the only channel the shipped screens give this backend for saying something
// unprompted -- and an empty inbox on a fresh install is indistinguishable from
// a server that is not answering.
func TestFirstAuthenticationDeliversWelcomeMail(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)

	db(t, ts, "1001", "scalar", "5", "") // first contact
	got := rows(t, db(t, ts, "1001", "array", "1", "0"))
	if len(got) != 1 {
		t.Fatalf("a new player has %d messages, want the welcome: %v", len(got), got)
	}

	fields := strings.Split(got[0], "\t")
	if fields[1] != store.SystemName {
		t.Errorf("sender name is %q, want %q", fields[1], store.SystemName)
	}
	if fields[4] != store.SystemGUID {
		t.Errorf("sender GUID is %q, want %q", fields[4], store.SystemGUID)
	}
	if fields[12] != "0" {
		t.Errorf("the welcome arrived read (field 12 = %q); nothing marks it bold",
			fields[12])
	}
	if !strings.Contains(fields[15], "Browser and mail") {
		t.Errorf("subject is %q, want the welcome subject", fields[15])
	}

	// Once, not once per request. The claim is the INSERT itself, so a second
	// authentication cannot take it.
	mark := strings.SplitN(got[0], "\t", 2)[0]
	if again := rows(t, db(t, ts, "1001", "array", "1", mark)); len(again) != 0 {
		t.Errorf("a second authentication delivered %d more: %v", len(again), again)
	}

	// And it is not a warrior: the pane accepts an empty query, which would
	// otherwise list the sender alongside everybody else.
	for _, row := range rows(t, db(t, ts, "1001", "array", "3", "\t0\t50")) {
		if strings.Contains(row, store.SystemName) {
			t.Errorf("the system account showed up in a warrior search: %q", row)
		}
	}
}

// The mail sequence argument is a high-water mark and filtering on it is not
// optional: the pane caches what it is handed and polls again with the highest
// id it has, so an unfiltered server makes the inbox grow without bound.
func TestMailHonoursTheHighWaterMark(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)

	account(t, ts, "1001")         // create the account
	mark := account(t, ts, "1002") // and the recipient
	db(t, ts, "1001", "scalar", "5",
		"warrior-1002\t\tFirst\tHello.")

	first := rows(t, db(t, ts, "1002", "array", "1", mark))
	if len(first) != 1 {
		t.Fatalf("first poll returned %d rows, want 1", len(first))
	}
	id := strings.SplitN(first[0], "\t", 2)[0]

	again := rows(t, db(t, ts, "1002", "array", "1", id))
	if len(again) != 0 {
		t.Errorf("polling with the high-water mark returned %d rows, want 0: %v",
			len(again), again)
	}
}

// Blocking is enforced at send time so a blocked sender's mail never occupies
// the mailbox, and the refusal is deliberately indistinguishable from success:
// telling a sender they are blocked invites them to work around it. The hit
// counter is what fills the stock dialog's "# Blocked Emails" column.
func TestBlockingIsSilentAndCounted(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)

	account(t, ts, "1001")
	mark := account(t, ts, "1002")

	if got := statusCode(db(t, ts, "1002", "scalar", "9", "warrior-1001")); got != "0" {
		t.Fatalf("blocking failed: %v", got)
	}
	if got := statusCode(db(t, ts, "1001", "scalar", "5",
		"warrior-1002\t\tBlocked\tHello.")); got != "0" {
		t.Errorf("a blocked send reported failure to the sender: %v", got)
	}

	if got := rows(t, db(t, ts, "1002", "array", "1", mark)); len(got) != 0 {
		t.Errorf("blocked mail was delivered: %v", got)
	}

	blocks := rows(t, db(t, ts, "1002", "array", "2", ""))
	if len(blocks) != 1 {
		t.Fatalf("block list has %d rows, want 1", len(blocks))
	}
	if hits := strings.Split(blocks[0], "\t")[4]; hits != "1" {
		t.Errorf("field 4 of the block row is %q, want the hit count 1", hits)
	}
}

// The client hides administration controls by rank, but that is a convenience:
// a player can issue any ordinal directly, so every rule is checked again here.
func TestRankGateIsEnforcedServerSide(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)

	db(t, ts, "1001", "scalar", "16", "Big Sucka Fishes\t[BSF]\t1")
	db(t, ts, "1002", "scalar", "5", "") // exists, not a member

	answer := db(t, ts, "1002", "scalar", "15",
		"Big Sucka Fishes\t1\tRewritten by a stranger.")
	if statusCode(answer) == "0" {
		t.Errorf("a non-member rewrote a tribe description: %v", answer["status"])
	}

	// And the refusal has to be a sentence: webbrowser.cs:927 puts status
	// field 1 straight into a MessageBoxOK.
	msg := strings.SplitN(answer["status"].(string), "\t", 2)[1]
	if msg == "" || !strings.ContainsAny(msg, " ") {
		t.Errorf("refusal is not a sentence: %q", msg)
	}
}

// Scalar 21 sends the title third and the admin level fourth.
//
// Read the other way round the two do not merely swap: the title is stored as
// whatever number the level was, and the rank becomes atoi("Warlord") -- zero,
// silently demoting the member to recruit as the side effect of renaming them.
// The call site is webbrowser.cs:643, and it sends
//
//	vTribe TAB vPlayer TAB %title TAB vPerm
//
// with %title from E_Title (:641) and vPerm the admin level TAM_OnAction
// stashed (:663). Asserted through array 6, the roster the ROSTER button
// issues, whose field 4 is the title and field 5 the admin level.
func TestMemberTitleAndRankAreNotSwapped(t *testing.T) {
	ts := newServer(t, testStore(t))

	db(t, ts, "1001", "scalar", "16", "Big Sucka Fishes\t[BSF]\t1")
	account(t, ts, "1002")
	db(t, ts, "1001", "scalar", "27", "Big Sucka Fishes\twarrior-1002")
	db(t, ts, "1002", "scalar", "28", "accept\tBig Sucka Fishes")

	answer := db(t, ts, "1001", "scalar", "21",
		"Big Sucka Fishes\twarrior-1002\tWarlord\t2")
	if got := statusCode(answer); got != "0" {
		t.Fatalf("setting the profile failed: %v", answer["status"])
	}

	var row string
	for _, r := range rows(t, db(t, ts, "1001", "array", "6", "Big Sucka Fishes")) {
		if strings.HasPrefix(r, "warrior-1002\t") {
			row = r
		}
	}
	if row == "" {
		t.Fatal("the member is missing from the roster")
	}

	f := strings.Split(row, "\t")
	if got, want := f[4], "Warlord"; got != want {
		t.Errorf("title = %q, want %q -- the arguments are swapped", got, want)
	}
	if got, want := f[5], "2"; got != want {
		t.Errorf("admin level = %q, want %q -- the arguments are swapped", got, want)
	}

	// The client means to refuse a blank title and cannot (webbrowser.cs:634
	// reads a field where it meant to call a method), so the refusal has to
	// happen here -- and it must not take the title with it on the way.
	blank := db(t, ts, "1001", "scalar", "21",
		"Big Sucka Fishes\twarrior-1002\t   \t2")
	if statusCode(blank) == "0" {
		t.Errorf("a blank title was accepted: %v", blank["status"])
	}
	if msg := statusField(blank, 1); !strings.ContainsAny(msg, " ") {
		t.Errorf("refusal is not a sentence: %q", msg)
	}

	for _, r := range rows(t, db(t, ts, "1001", "array", "6", "Big Sucka Fishes")) {
		if strings.HasPrefix(r, "warrior-1002\t") {
			if got := strings.Split(r, "\t")[4]; got != "Warlord" {
				t.Errorf("title = %q after a refused write, want it untouched", got)
			}
		}
	}
}

// An invitation is delivered by mail, because the client has no query that
// lists a player's own invitations -- so the link in that body is the only way
// one can ever be answered.
func TestInvitationArrivesAsMailWithAWorkingLink(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)

	db(t, ts, "1001", "scalar", "16", "Big Sucka Fishes\t[BSF]\t1")
	mark := account(t, ts, "1002")

	if got := statusCode(db(t, ts, "1001", "scalar", "27",
		"Big Sucka Fishes\twarrior-1002")); got != "0" {
		t.Fatalf("invite failed: %v", got)
	}

	got := rows(t, db(t, ts, "1002", "array", "1", mark))
	if len(got) != 1 {
		t.Fatalf("the invited warrior has %d messages, want 1", len(got))
	}

	// The body starts at field 17 and the client rejoins it TAB-separated, so
	// the newline written here is the TAB GuiMLTextCtrl::onURL splits on.
	body := strings.Split(got[0], "\t")[17:]
	joined := strings.Join(body, "\t")
	if !strings.Contains(joined, "<a:acceptinvite\tBig Sucka Fishes\twarrior-1002>") {
		t.Errorf("no working accept link in the body: %q", joined)
	}
	if !strings.Contains(joined, "<a:rejectinvite\t") {
		t.Errorf("no reject link in the body: %q", joined)
	}
}

//-----------------------------------------------------------------------------
// The session route
//
// The exchange itself is tested in internal/auth, where the keys are. What is
// tested here is the wire format, because the client's parser keys on the first
// word of the line (session.cs:272-315) and nothing else -- a reply this server
// gets slightly wrong reads as a hang rather than an error.
//-----------------------------------------------------------------------------

// sessionServer needs no database: /session never touches the store.
func sessionServer(t *testing.T) (*httptest.Server, *auth.Sessions) {
	t.Helper()

	sessions := auth.NewSessions(0)
	srv := &Server{
		Sessions: sessions,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return ts, sessions
}

func sessionPost(t *testing.T, ts *httptest.Server, form url.Values) string {
	t.Helper()

	resp, err := ts.Client().PostForm(ts.URL+"/session", form)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(body))
}

func TestSessionKeepaliveAnswersRefreshedOrTimeout(t *testing.T) {
	ts, sessions := sessionServer(t)
	sessions.GrantToken("live", auth.Identity{GUID: "4510186", Name: "orange01"})

	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{"live token", url.Values{"guid": {"4510186"}, "uuid": {"live"}}, "REFRESHED"},
		{"unknown token", url.Values{"guid": {"4510186"}, "uuid": {"gone"}}, "TIMEOUT"},
		// A live token presented for somebody else is not a session either, and
		// answering TIMEOUT sends the client to negotiate a fresh one.
		{"token for another guid", url.Values{"guid": {"4120041"}, "uuid": {"live"}}, "TIMEOUT"},
		{"no guid", url.Values{"uuid": {"live"}}, "ERR: No GUID specified."},
		{"nothing to do", url.Values{"guid": {"4510186"}}, "ERR: Nothing to do."},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sessionPost(t, ts, c.form); got != c.want {
				t.Errorf("answer = %q, want %q", got, c.want)
			}
		})
	}
}

// A certificate that TribesNext did not sign gets an ERR line, not a challenge
// -- and not a 500, which the client would retry against forever.
func TestSessionRefusesAnUnsignedCertificate(t *testing.T) {
	ts, _ := sessionServer(t)

	got := sessionPost(t, ts, url.Values{
		"guid":  {"4510186"},
		"cert":  {"orange01\t4510186\t10001\tb7c1\tdeadbeef"},
		"nonce": {"1f"},
	})
	if !strings.HasPrefix(got, "ERR: ") {
		t.Errorf("answer = %q, want an ERR line", got)
	}
}

// /db without a session is 401, and with one is not. The bypass exists for the
// in-game suites and must be off by default.
func TestDatabaseRequiresASession(t *testing.T) {
	st := testStore(t)

	sessions := auth.NewSessions(0)
	sessions.GrantToken("good", auth.Identity{GUID: "1001", Name: "warrior-1001"})
	srv := &Server{
		Store:    st,
		Sessions: sessions,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	post := func(values url.Values) int {
		resp, err := ts.Client().PostForm(ts.URL+"/db", values)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	payload, _ := json.Marshal(map[string]string{"form": "scalar", "ordinal": "5"})

	if code := post(url.Values{"guid": {"1001"}, "uuid": {"good"}, "payload": {string(payload)}}); code != http.StatusOK {
		t.Errorf("a session was refused: %d", code)
	}
	if code := post(url.Values{"guid": {"1001"}, "uuid": {"wrong"}, "payload": {string(payload)}}); code != http.StatusUnauthorized {
		t.Errorf("an unknown token got %d, want 401", code)
	}
	if code := post(url.Values{"guid": {"1001"}, "payload": {string(payload)}}); code != http.StatusUnauthorized {
		t.Errorf("no token at all got %d, want 401", code)
	}

	srv.TrustGUID = true
	if code := post(url.Values{"guid": {"1001"}, "payload": {string(payload)}}); code != http.StatusOK {
		t.Errorf("the dev bypass refused a bare guid: %d", code)
	}
}

//-----------------------------------------------------------------------------
// Clan certificates
//-----------------------------------------------------------------------------

// clanCertServer is newServer with a signing key, and hands back the key so a
// test can check a certificate exactly as the game server will.
func clanCertServer(t *testing.T, st *store.Store) (*httptest.Server, *clancert.Signer) {
	t.Helper()

	key, err := clancert.Generate(1024) // small on purpose: these run per test
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	signer := clancert.FromKey(key, 3, 30*time.Minute)

	sessions := auth.NewSessions(0)
	for guid := 1000; guid < 1010; guid++ {
		g := strconv.Itoa(guid)
		sessions.GrantToken("session-"+g, auth.Identity{GUID: g, Name: "warrior-" + g})
	}

	srv := &Server{
		Store:     st,
		Sessions:  sessions,
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		ClanCerts: signer,
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return ts, signer
}

func clanCert(t *testing.T, ts *httptest.Server, guid string) (string, int) {
	t.Helper()

	resp, err := ts.Client().PostForm(ts.URL+"/clancert", url.Values{
		"guid": {guid}, "uuid": {"session-" + guid},
	})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode
	}
	var out struct {
		Cert string `json:"cert"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out.Cert, resp.StatusCode
}

// The certificate a player carries has to say the same thing /cert says, and
// say it in a form a game server can check without asking anyone.
func TestClanCertificateCarriesTheSignedRecord(t *testing.T) {
	st := testStore(t)
	ts, signer := clanCertServer(t, st)

	db(t, ts, "1001", "scalar", "16", "Big Sucka Fishes\t[BSF]\t1")
	db(t, ts, "1001", "scalar", "25", "Big Sucka Fishes")

	cert, code := clanCert(t, ts, "1001")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}

	e, n := signer.PublicHex()
	if err := clancert.Check(e, n, cert, "1001", time.Now()); err != nil {
		t.Fatalf("the game server would refuse this: %v", err)
	}

	record, err := clancert.Record(cert)
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	// Same producer as /cert, so the tag the browser shows and the tag the game
	// renders cannot drift apart.
	quad := strings.Split(strings.Split(record, "\n")[0], "\t")
	if len(quad) != 4 || quad[3] != "1001" {
		t.Fatalf("record 0 = %q, want a quad ending in the GUID", record)
	}
	if quad[1] != "[BSF]" {
		t.Errorf("worn tag = %q, want [BSF]", quad[1])
	}
	if got := certField(t, ts, "1001", 1); got != quad[1] {
		t.Errorf("/cert says %q and /clancert says %q", got, quad[1])
	}
}

// The certificate says who the holder is, so it is only ever handed to them.
func TestClanCertificateNeedsASession(t *testing.T) {
	ts, _ := clanCertServer(t, testStore(t))

	resp, err := ts.Client().PostForm(ts.URL+"/clancert", url.Values{"guid": {"1001"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
}

// A deployment with no signing key is a supported one: it serves everything
// else and players simply carry no tag.
func TestClanCertificateIsOffWithoutAKey(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st) // no ClanCerts

	if _, code := clanCert(t, ts, "1001"); code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", code)
	}

	// And the rest of the front door is unaffected.
	if _, code := db(t, ts, "1001", "scalar", "5", "")["status"]; !code {
		t.Error("/db stopped answering")
	}
}

//-----------------------------------------------------------------------------
// Chat
//
// The hub's own behaviour is covered in internal/chat. What matters here is the
// framing -- "<seq>\t<line>", the cursor, and the two seq-0 sentinels -- because
// the client parses that in nine lines of TorqueScript and cannot tell a
// mistake from silence.
//-----------------------------------------------------------------------------

func chatServer(t *testing.T, st *store.Store) *httptest.Server {
	t.Helper()

	sessions := auth.NewSessions(0)
	for guid := 1000; guid < 1010; guid++ {
		g := strconv.Itoa(guid)
		sessions.GrantToken("session-"+g, auth.Identity{GUID: g, Name: "warrior-" + g})
	}

	srv := &Server{
		Store:    st,
		Sessions: sessions,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Chat:     chat.New(slog.New(slog.NewTextHandler(io.Discard, nil)), []string{"Tribes2"}),
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return ts
}

// openStream starts a stream and returns a reader over it plus a cancel.
// chat runs one poll and returns the decoded answer.
func chatPoll(t *testing.T, ts *httptest.Server, guid, payload string) (map[string]string, int) {
	t.Helper()

	resp, err := ts.Client().PostForm(ts.URL+"/chat", url.Values{
		"guid":    {guid},
		"uuid":    {"session-" + guid},
		"payload": {payload},
	})
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode
	}
	var out map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out, resp.StatusCode
}

func TestChatIsOffWithoutAHub(t *testing.T) {
	ts, _ := sessionServer(t) // no Chat

	resp, err := ts.Client().PostForm(ts.URL+"/chat", url.Values{"guid": {"1001"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", resp.StatusCode)
	}
}

func TestChatNeedsASession(t *testing.T) {
	ts := chatServer(t, testStore(t))

	resp, err := ts.Client().PostForm(ts.URL+"/chat", url.Values{
		"guid": {"1001"}, "uuid": {"wrong"}, "payload": {"0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", resp.StatusCode)
	}
}

// One round trip carries the player's lines up and the answers back down, and
// the cursor is what makes a lost poll cost nothing.
func TestChatPollCarriesBothDirections(t *testing.T) {
	ts := chatServer(t, testStore(t))

	first, _ := chatPoll(t, ts, "1001", "0")
	if !strings.Contains(first["lines"], "CHALRESP_REPLY warrior-1001") {
		t.Fatalf("first poll = %v", first)
	}
	if first["seq"] != "1" {
		t.Errorf("first sequence = %q, want 1", first["seq"])
	}

	// Both commands in one payload, both answered in one reply.
	second, _ := chatPoll(t, ts, "1001", first["seq"]+"\nJOIN #Tribes2\nLIST")
	if !strings.Contains(second["lines"], "JOIN #Tribes2") {
		t.Errorf("no join echo: %v", second)
	}
	if !strings.Contains(second["lines"], " 323 ") {
		t.Errorf("no end of list: %v", second)
	}

	// Polling from the same cursor replays; polling from the new one does not.
	again, _ := chatPoll(t, ts, "1001", first["seq"])
	if !strings.Contains(again["lines"], "JOIN #Tribes2") {
		t.Errorf("a repeated cursor did not replay: %v", again)
	}
	empty, _ := chatPoll(t, ts, "1001", second["seq"])
	if empty["lines"] != "" {
		t.Errorf("an up-to-date cursor returned lines: %v", empty)
	}
}

// A cursor from a connection the hub has forgotten gets reset rather than
// silence, so the client knows to re-join its rooms instead of sitting in
// channels the server has never heard of.
func TestChatResetsAStaleCursor(t *testing.T) {
	ts := chatServer(t, testStore(t))

	answer, _ := chatPoll(t, ts, "1002", "99")
	if answer["reset"] != "1" {
		t.Fatalf("stale cursor was not reset: %v", answer)
	}
	// And the connection starts from the beginning, so the identity is not lost
	// along with the cursor.
	if !strings.Contains(answer["lines"], "CHALRESP_REPLY") {
		t.Fatalf("reset lost the identity: %v", answer)
	}

	// A second poll on a connection it now knows is not a reset.
	answer, _ = chatPoll(t, ts, "1002", answer["seq"])
	if answer["reset"] != "0" {
		t.Errorf("a known cursor was reset: %v", answer)
	}
}
