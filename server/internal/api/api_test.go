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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/henrik/tnbrowser-server/internal/auth"
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

// newServer wires the front door to a stand-in for the TribesNext session
// check -- the one thing this server does not own. Identity stays TribesNext's;
// what is faked here is only the round trip to them.
//
// The stand-in echoes the guid back, because the verifier refuses a 200 whose
// profile is for somebody else: a pairing that upstream did not actually
// enforce would otherwise let any token authorise any account.
//
// It also answers a fixed creation date, which is where accounts.created comes
// from for a player this server has never seen. upstreamCreation is that date.
func newServer(t *testing.T, st *store.Store) *httptest.Server {
	t.Helper()

	upstream := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// The real oracle answers 200 for a live pair and 401 otherwise.
			if r.FormValue("uuid") == "" || r.FormValue("guid") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			guid := r.FormValue("guid")
			_, _ = io.WriteString(w,
				`{"guid":"`+guid+`","name":"warrior-`+guid+
					`","creation":"`+strconv.FormatInt(upstreamCreation, 10)+`"}`)
		}))
	t.Cleanup(upstream.Close)

	srv := &Server{
		Store:    st,
		Verifier: auth.NewVerifier(upstream.URL, 0),
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

// upstreamCreation is the registration date the stand-in oracle reports:
// 2011-03-13, chosen only for being a date no clock in this test could produce
// by accident.
const upstreamCreation = 1300000000

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

// A new account's registration date is TribesNext's, not the moment this
// server first saw them -- otherwise every profile reads as registered today.
//
// Asserted through scalar 23, the ordinal the shipped warrior profile actually
// issues, so this covers the whole path from the upstream field to the pane:
// status field 6 is the registered date (webbrowser.cs reads it as such) and
// date() renders it as YYYY-MM-DD.
func TestRegistrationDateComesFromUpstream(t *testing.T) {
	ts := newServer(t, testStore(t))
	account(t, ts, "1002")

	answer := db(t, ts, "1002", "scalar", "23", "warrior-1002")
	if got, want := statusField(answer, 6), "2011-03-13"; got != want {
		t.Errorf("registered = %q, want %q", got, want)
	}
}

// The date is written once. A player authenticating again must not have their
// registration date moved -- and in particular must not have last_seen
// backdated to it, which would report them offline forever.
func TestRegistrationDateSurvivesLaterRequests(t *testing.T) {
	ts := newServer(t, testStore(t))
	account(t, ts, "1003")

	db(t, ts, "1003", "scalar", "5", "")

	answer := db(t, ts, "1003", "scalar", "23", "warrior-1003")
	if got, want := statusField(answer, 6), "2011-03-13"; got != want {
		t.Errorf("registered = %q, want %q", got, want)
	}
	// Field 7 is the online flag, which online() derives from last_seen. The
	// player just made a request, so anything but 1 means last_seen was
	// clobbered by the registration date.
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
