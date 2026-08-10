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
				`{"guid":"`+guid+`","name":"warrior-`+guid+`"}`)
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

func statusCode(answer map[string]any) string {
	s, _ := answer["status"].(string)
	return strings.SplitN(s, "\t", 2)[0]
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

// The mail sequence argument is a high-water mark and filtering on it is not
// optional: the pane caches what it is handed and polls again with the highest
// id it has, so an unfiltered server makes the inbox grow without bound.
func TestMailHonoursTheHighWaterMark(t *testing.T) {
	st := testStore(t)
	ts := newServer(t, st)

	db(t, ts, "1001", "scalar", "5", "") // create the account
	db(t, ts, "1002", "scalar", "5", "") // and the recipient
	db(t, ts, "1001", "scalar", "5",
		"warrior-1002\t\tFirst\tHello.")

	first := rows(t, db(t, ts, "1002", "array", "1", "0"))
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

	db(t, ts, "1001", "scalar", "5", "")
	db(t, ts, "1002", "scalar", "5", "")

	if got := statusCode(db(t, ts, "1002", "scalar", "9", "warrior-1001")); got != "0" {
		t.Fatalf("blocking failed: %v", got)
	}
	if got := statusCode(db(t, ts, "1001", "scalar", "5",
		"warrior-1002\t\tBlocked\tHello.")); got != "0" {
		t.Errorf("a blocked send reported failure to the sender: %v", got)
	}

	if got := rows(t, db(t, ts, "1002", "array", "1", "0")); len(got) != 0 {
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
	db(t, ts, "1002", "scalar", "5", "")

	if got := statusCode(db(t, ts, "1001", "scalar", "27",
		"Big Sucka Fishes\twarrior-1002")); got != "0" {
		t.Fatalf("invite failed: %v", got)
	}

	got := rows(t, db(t, ts, "1002", "array", "1", "0"))
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
