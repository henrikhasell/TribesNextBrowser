package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/henrik/tnbrowser-server/internal/auth"
	"github.com/henrik/tnbrowser-server/internal/clancert"
	"github.com/henrik/tnbrowser-server/internal/dbproxy"
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
// call issues an authenticated request the way the v2 client does: a JSON body
// and identity in the Authorization header.
func call(t *testing.T, ts *httptest.Server, method, path, guid string, body any) *http.Response {
	t.Helper()

	var r io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		r = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(method, ts.URL+path, r)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if guid != "" {
		req.Header.Set("Authorization", "TNB "+guid+":session-"+guid)
	}

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	return resp
}

func db(t *testing.T, ts *httptest.Server, guid, form, ordinal, args string) map[string]any {
	t.Helper()

	var list []string
	if args != "" {
		list = strings.Split(args, "\t")
	}
	resp := call(t, ts, http.MethodPost, "/db", guid, map[string]any{
		"form": form, "ordinal": ordinal, "args": list,
	})
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
	code, _ := answer["code"].(float64)
	return strconv.Itoa(int(code))
}

// statusField reads what used to be a field of the tab-separated status. Index
// 0 is the code and 1 the message; anything beyond comes from the fields array,
// so the call sites keep the numbering the shipped scripts use.
func statusField(answer map[string]any, i int) string {
	switch i {
	case 0:
		code, _ := answer["code"].(float64)
		return strconv.Itoa(int(code))
	case 1:
		msg, _ := answer["message"].(string)
		return msg
	}
	extra, _ := answer["fields"].([]any)
	if i-2 >= len(extra) {
		return ""
	}
	v, _ := extra[i-2].(string)
	return v
}

// rows joins each row's fields the way the client's shim does, so the
// assertions below stay written in terms of what the shipped parsers see.
func rows(t *testing.T, answer map[string]any) []string {
	t.Helper()
	raw, _ := answer["rows"].([]any)
	out := make([]string, len(raw))
	for i, r := range raw {
		fields, _ := r.([]any)
		parts := make([]string, len(fields))
		for j, f := range fields {
			switch v := f.(type) {
			case string:
				parts[j] = v
			case bool:
				// The engine reads a JSON true as 1, which is the spelling the
				// shipped getField callers test for.
				parts[j] = map[bool]string{true: "1", false: "0"}[v]
			case float64:
				parts[j] = strconv.FormatFloat(v, 'f', -1, 64)
			default:
				parts[j] = fmt.Sprint(v)
			}
		}
		out[i] = strings.Join(parts, "\t")
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

	resp := call(t, ts, http.MethodPost, "/db", "", map[string]any{
		"form": "scalar", "ordinal": "5",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", resp.StatusCode)
	}

	var out struct{ Error, Message string }
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("a refusal must be parseable: %v", err)
	}
	if out.Error != "session_expired" {
		t.Errorf("error slug %q, want session_expired -- the client branches on it", out.Error)
	}
	if out.Message == "" {
		t.Error("no sentence to show the player")
	}
}

// A v1 mod posts its query as a urlencoded form. It gets told to update rather
// than a parse error, because an out-of-date install is a thing a player can
// actually fix.
func TestAV1ClientIsToldToUpdate(t *testing.T) {
	ts := newServer(t, testStore(t))

	resp, err := ts.Client().Post(ts.URL+"/db", "application/x-www-form-urlencoded",
		strings.NewReader("payload={}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out struct{ Error, Message string }
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "client_too_old" {
		t.Errorf("error slug %q, want client_too_old", out.Error)
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

	resp := call(t, ts, http.MethodGet, "/cert", "1001", nil)
	defer resp.Body.Close()

	var id dbproxy.Identity
	if err := json.NewDecoder(resp.Body).Decode(&id); err != nil {
		t.Fatal(err)
	}

	if id.GUID != "1001" {
		t.Errorf("guid = %q, want 1001", id.GUID)
	}
	if id.Name == "" {
		t.Error("no warrior name; GameGui.cs:1324 reads it")
	}
	if len(id.Tribes) != 1 {
		t.Fatalf("%d tribes, want 1: %+v", len(id.Tribes), id.Tribes)
	}
	// webbrowser.cs:1909-1926 renders your own tribe list straight out of this,
	// so the id, the rank and the title all have to survive the round trip.
	if tribe := id.Tribes[0]; tribe.Rank != 4 || tribe.ID == 0 || tribe.Title == "" {
		t.Errorf("tribe = %+v, want the founder's rank, id and title", tribe)
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

	resp := call(t, ts, http.MethodGet, "/cert", guid, nil)
	defer resp.Body.Close()

	var id dbproxy.Identity
	if err := json.NewDecoder(resp.Body).Decode(&id); err != nil {
		t.Fatal(err)
	}
	return []string{id.Name, id.Tag, boolText(id.Append), id.GUID}[i]
}

func boolText(b bool) string {
	if b {
		return "1"
	}
	return "0"
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
		t.Errorf("a non-member rewrote a tribe description: %v", answer)
	}

	// And the refusal has to be a sentence: webbrowser.cs:927 puts the message
	// straight into a MessageBoxOK.
	msg, _ := answer["message"].(string)
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

// Joining by asking gives the same title as joining by invitation.
//
// Scalar 28 accept is two operations wearing one ordinal: the warrior answering
// an invitation, and an admin admitting somebody who asked. They insert the
// membership row from different places -- AcceptInvite and AdmitRequester --
// and only the first set a title, so a warrior who joined by asking landed in
// the roster with field 4 empty. The roster shows no rank name of its own
// (field 5 is the bare 0..4), so that column is the only place a member's
// standing appears at all, and empty reads as broken rather than as untitled.
//
// The other direction is covered by TestMemberTitleAndRankAreNotSwapped, which
// joins by invitation and so cannot see this.
func TestAdmittedRequesterGetsATitle(t *testing.T) {
	ts := newServer(t, testStore(t))

	// Field 3 is the recruiting flag, without which scalar 34 is refused --
	// asking to join a tribe that is not recruiting is not a thing the client
	// offers.
	db(t, ts, "1001", "scalar", "16", "Big Sucka Fishes\t[BSF]\t1\t1\t1\t")
	account(t, ts, "1002")

	// 34 asks, and the admin's 28 accept names somebody other than themselves,
	// which is what picks the AdmitRequester branch.
	ask := db(t, ts, "1002", "scalar", "34", "Big Sucka Fishes")
	if got := statusCode(ask); got != "0" {
		t.Fatalf("requesting an invitation failed: %v", ask["status"])
	}
	admit := db(t, ts, "1001", "scalar", "28",
		"accept\tBig Sucka Fishes\twarrior-1002")
	if got := statusCode(admit); got != "0" {
		t.Fatalf("admitting the requester failed: %v", admit["status"])
	}

	var row string
	for _, r := range rows(t, db(t, ts, "1001", "array", "6", "Big Sucka Fishes")) {
		if strings.HasPrefix(r, "warrior-1002\t") {
			row = r
		}
	}
	if row == "" {
		t.Fatal("the admitted warrior is missing from the roster")
	}

	f := strings.Split(row, "\t")
	if got, want := f[4], "Recruit"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if got, want := f[5], "0"; got != want {
		t.Errorf("admin level = %q, want %q", got, want)
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

	// The tribe's own name is a link to its profile, in this body and in every
	// other mail this server sends about a tribe.
	if !strings.Contains(joined, "<a:tribe\tBig Sucka Fishes>Big Sucka Fishes</a>") {
		t.Errorf("the tribe is not a link in the body: %q", joined)
	}
}

// Every mail about a tribe names it as a link, not just the invitation.
//
// Asserted on the TAB rather than on the newline clanLink writes, because the
// tab is what GuiMLTextCtrl::onURL splits the URL on (webbrowser.cs:1063) and
// what the client actually receives -- bodyLines splits the stored body into
// row fields, and getFields(%row,17) rejoins them TAB-separated. A test written
// against the stored form would pass for a body no control could follow.
func TestTribeMailNamesTheTribeAsALink(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)

	db(t, ts, "1001", "scalar", "16", "Big Sucka Fishes\t[BSF]\t1")
	mark := account(t, ts, "1002")
	db(t, ts, "1001", "scalar", "27", "Big Sucka Fishes\twarrior-1002")
	db(t, ts, "1002", "scalar", "28", "accept\tBig Sucka Fishes")

	// A promotion and then a kick: the two ends of what a member hears about,
	// and the two the client shows in a mailbox rather than in a dialog.
	db(t, ts, "1001", "scalar", "21", "Big Sucka Fishes\twarrior-1002\tWarlord\t2")
	db(t, ts, "1001", "scalar", "19", "warrior-1002\tBig Sucka Fishes")

	want := "<a:tribe\tBig Sucka Fishes>Big Sucka Fishes</a>"
	seen := map[string]bool{}
	for _, r := range rows(t, db(t, ts, "1002", "array", "1", mark)) {
		f := strings.Split(r, "\t")
		subject, body := f[15], strings.Join(f[17:], "\t")
		seen[subject] = strings.Contains(body, want)
	}

	for _, subject := range []string{
		"Rank changed in Big Sucka Fishes",
		"Removed from Big Sucka Fishes",
	} {
		linked, ok := seen[subject]
		if !ok {
			t.Errorf("no mail with subject %q", subject)
			continue
		}
		if !linked {
			t.Errorf("%q does not name the tribe as a link", subject)
		}
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

// sessionPost negotiates against /session and returns the decoded answer plus
// the HTTP status, since a refusal is now carried by both.
func sessionPost(t *testing.T, ts *httptest.Server, body map[string]any) (map[string]any, int) {
	t.Helper()

	resp := call(t, ts, http.MethodPost, "/session", "", body)
	defer resp.Body.Close()

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out, resp.StatusCode
}

func TestSessionKeepaliveAnswersRefreshedOrExpired(t *testing.T) {
	ts, sessions := sessionServer(t)
	sessions.GrantToken("live", auth.Identity{GUID: "4510186", Name: "orange01"})

	cases := []struct {
		name  string
		body  map[string]any
		state string
		slug  string
	}{
		{"live token", map[string]any{"guid": "4510186", "uuid": "live"}, "refreshed", ""},
		{"unknown token", map[string]any{"guid": "4510186", "uuid": "gone"}, "expired", ""},
		// A live token presented for somebody else is not a session either, and
		// answering expired sends the client to negotiate a fresh one.
		{"token for another guid", map[string]any{"guid": "4120041", "uuid": "live"}, "expired", ""},
		{"no guid", map[string]any{"uuid": "live"}, "", "bad_request"},
		{"nothing to do", map[string]any{"guid": "4510186"}, "", "bad_request"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, code := sessionPost(t, ts, c.body)

			if c.slug != "" {
				if got["error"] != c.slug {
					t.Errorf("error = %v, want %q", got["error"], c.slug)
				}
				if code == http.StatusOK {
					t.Error("a refusal answered 200")
				}
				return
			}
			if got["state"] != c.state {
				t.Errorf("state = %v, want %q", got["state"], c.state)
			}
			// A keepalive must never leak a token back; only a grant carries one.
			if _, ok := got["uuid"]; ok {
				t.Error("a keepalive answered with a token")
			}
		})
	}
}

// A certificate that TribesNext did not sign is refused, and not with a 500 --
// which the client would retry against forever.
func TestSessionRefusesAnUnsignedCertificate(t *testing.T) {
	ts, _ := sessionServer(t)

	got, code := sessionPost(t, ts, map[string]any{
		"guid":  "4510186",
		"cert":  "orange01\t4510186\t10001\tb7c1\tdeadbeef",
		"nonce": "1f",
	})
	if got["error"] != "bad_certificate" {
		t.Errorf("error = %v, want bad_certificate", got["error"])
	}
	if code != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", code)
	}
	if got["challenge"] != nil {
		t.Error("a refused certificate was answered with a challenge")
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

	query := map[string]any{"form": "scalar", "ordinal": "5"}

	post := func(auth string) int {
		encoded, _ := json.Marshal(query)
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/db", bytes.NewReader(encoded))
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if code := post("TNB 1001:good"); code != http.StatusOK {
		t.Errorf("a session was refused: %d", code)
	}
	if code := post("TNB 1001:wrong"); code != http.StatusUnauthorized {
		t.Errorf("an unknown token got %d, want 401", code)
	}
	if code := post(""); code != http.StatusUnauthorized {
		t.Errorf("no credentials at all got %d, want 401", code)
	}
	// A header in the wrong shape is not a session either.
	if code := post("Bearer 1001"); code != http.StatusUnauthorized {
		t.Errorf("a foreign scheme got %d, want 401", code)
	}

	srv.TrustGUID = true
	if code := post("TNB 1001:"); code != http.StatusOK {
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

	resp := call(t, ts, http.MethodGet, "/clancert", guid, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode
	}
	var out struct {
		Certificate string `json:"certificate"`
		Expires     int64  `json:"expires"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Expires <= 0 {
		t.Error("no expiry; the client refreshes on it")
	}
	return out.Certificate, resp.StatusCode
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

	resp := call(t, ts, http.MethodGet, "/clancert", "", nil)
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
	if _, ok := db(t, ts, "1001", "scalar", "5", "")["code"]; !ok {
		t.Error("/db stopped answering")
	}
}
