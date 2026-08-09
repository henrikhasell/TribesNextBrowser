package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/henrik/tnbrowser-server/internal/api"
	"github.com/henrik/tnbrowser-server/internal/auth"
	"github.com/henrik/tnbrowser-server/internal/store"
)

// These tests run against a real PostgreSQL, because the behaviour worth
// testing here is the SQL: rank gates, cascade rules and transactional
// mutations. A fake would test the fake.
//
//	docker run -d --name tnb-postgres -e POSTGRES_PASSWORD=tnbrowser \
//	  -e POSTGRES_USER=tnbrowser -e POSTGRES_DB=tnbrowser -p 5433:5432 postgres:16-alpine
//	go test ./...
const defaultTestDSN = "postgres://tnbrowser:tnbrowser@127.0.0.1:5433/tnbrowser"

// knownAccounts is what the fake TribesNext vouches for. Anything else gets a
// 401, exactly as the real one does for an unknown or expired pair.
var knownAccounts = map[string]string{
	"4510186": "orange01",
	"4120041": "Shifter",
	"4200999": "Ravage",
	"4300777": "orangeade",
}

type harness struct {
	t        *testing.T
	srv      *httptest.Server
	upstream *httptest.Server
	pool     *pgxpool.Pool
	store    *store.Store
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	dsn := os.Getenv("TNB_TEST_DSN")
	if dsn == "" {
		dsn = defaultTestDSN
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("no test database (%v); start postgres to run these", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("test database unreachable (%v); start postgres to run these", err)
	}

	// Fresh state per test: these tests assert on counts and orderings, and
	// leftovers from a previous run would make failures depend on run order.
	for _, tbl := range []string{
		"history", "mail", "buddies", "blocks", "clan_disband_votes",
		"clan_invites", "clan_members", "clans", "accounts",
	} {
		if _, err := pool.Exec(ctx, "TRUNCATE "+tbl+" CASCADE"); err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}

	// A stand-in for TribesNext's verification oracle.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		guid := r.URL.Query().Get("guid")
		uuid := r.URL.Query().Get("uuid")
		name, ok := knownAccounts[guid]
		if !ok || uuid != "session-"+guid {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("<h1>Fatal Error</h1><h2>401 Authentication Required</h2>"))
			return
		}
		// Reproduce the live server's leading blank line and null fields.
		_, _ = fmt.Fprintf(w, "\n{\"guid\":%q,\"name\":%q,\"tag\":null,\"online\":1}", guid, name)
	}))
	t.Cleanup(upstream.Close)

	st := store.New(pool)
	s := &api.Server{
		Store:     st,
		Verifier:  auth.NewVerifier(upstream.URL, time.Minute),
		ServerKey: "test-key",
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	srv := httptest.NewServer(s.Routes())
	t.Cleanup(srv.Close)
	t.Cleanup(pool.Close)

	return &harness{t: t, srv: srv, upstream: upstream, pool: pool, store: st}
}

// call invokes a method as a player and decodes the JSON reply.
func (h *harness) call(path, guid, method string, payload map[string]any) (int, []byte) {
	h.t.Helper()

	q := url.Values{}
	q.Set("guid", guid)
	q.Set("uuid", "session-"+guid)
	q.Set("method", method)
	if payload != nil {
		b, _ := json.Marshal(payload)
		q.Set("payload", string(b))
	}

	resp, err := http.Get(h.srv.URL + path + "?" + q.Encode())
	if err != nil {
		h.t.Fatalf("%s %s: %v", path, method, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func (h *harness) browser(guid, method string, payload map[string]any) (int, []byte) {
	return h.call("/tn/json/json_browser.php", guid, method, payload)
}

func (h *harness) mail(guid, method string, payload map[string]any) (int, []byte) {
	return h.call("/tn/json/json_mail.php", guid, method, payload)
}

// decode parses a reply, tolerating the leading blank line the protocol carries.
func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(body))), &v); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return v
}

// status is the {"status":...,"msg":...} envelope.
type status struct {
	Status string `json:"status"`
	Msg    string `json:"msg"`
}

// seed logs a player in once, which is what creates their account row.
func (h *harness) seed(guids ...string) {
	h.t.Helper()
	for _, g := range guids {
		if code, _ := h.browser(g, "userinvites", nil); code != http.StatusOK {
			h.t.Fatalf("seeding %s: got %d", g, code)
		}
	}
}

// makeClan creates a clan owned by guid and returns its id.
func (h *harness) makeClan(guid, name, tag string) string {
	h.t.Helper()

	code, body := h.browser(guid, "createclan",
		map[string]any{"name": name, "tag": tag, "append": "no"})
	if code != http.StatusOK {
		h.t.Fatalf("createclan: got %d", code)
	}
	if st := decode[status](h.t, body); st.Status != "success" {
		h.t.Fatalf("createclan refused: %s", st.Msg)
	}

	_, body = h.browser(guid, "clansearch", map[string]any{"q": name})
	hits := decode[[]struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}](h.t, body)
	if len(hits) != 1 {
		h.t.Fatalf("expected to find the new clan, got %d hits", len(hits))
	}
	return hits[0].ID
}

// --------------------------------------------------------------------------

func TestSessionMustBeVouchedForUpstream(t *testing.T) {
	h := newHarness(t)

	// A pair TribesNext does not recognise is refused, and with the same HTML
	// body the real backend sends so the client's error path is unchanged.
	q := url.Values{}
	q.Set("guid", "4510186")
	q.Set("uuid", "not-a-real-session")
	q.Set("method", "userinvites")

	resp, err := http.Get(h.srv.URL + "/tn/json/json_browser.php?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "401 Authentication Required") {
		t.Fatalf("expected the PHP error body, got %q", body)
	}

	// A GUID nobody vouches for is refused too, even with a plausible token.
	if code, _ := h.browser("9999999", "userinvites", nil); code != http.StatusUnauthorized {
		t.Fatalf("unknown guid: expected 401, got %d", code)
	}
}

func TestProfileRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.seed("4510186")

	if code, body := h.browser("4510186", "userinfo",
		map[string]any{"info": "Hello \"world\" & 100%"}); code != http.StatusOK {
		t.Fatalf("userinfo: %d %s", code, body)
	}
	if code, _ := h.browser("4510186", "usersite",
		map[string]any{"site": "www.example.com"}); code != http.StatusOK {
		t.Fatalf("usersite: %d", code)
	}

	_, body := h.browser("4510186", "userview", map[string]any{"id": "4510186"})
	user := decode[struct {
		GUID    string `json:"guid"`
		Name    string `json:"name"`
		Info    string `json:"info"`
		Website string `json:"website"`
		Online  int    `json:"online"`
	}](t, body)

	if user.GUID != "4510186" {
		t.Fatalf("guid: %q", user.GUID)
	}
	// The name is TribesNext's, not ours -- it arrived with the verification.
	if user.Name != "orange01" {
		t.Fatalf("name should come from upstream, got %q", user.Name)
	}
	if user.Info != "Hello \"world\" & 100%" {
		t.Fatalf("info round trip: %q", user.Info)
	}
	if user.Website != "www.example.com" {
		t.Fatalf("website round trip: %q", user.Website)
	}
	if user.Online != 1 {
		t.Fatalf("the requesting player should read as online, got %d", user.Online)
	}
}

func TestViewingSomethingMissingIsEmptyNotAnError(t *testing.T) {
	h := newHarness(t)
	h.seed("4510186")

	// The original answered [] for a missing subject; the client relies on it.
	_, body := h.browser("4510186", "clanview", map[string]any{"id": "999"})
	if got := strings.TrimSpace(string(body)); got != "[]" {
		t.Fatalf("missing clan should be [], got %s", got)
	}
	_, body = h.browser("4510186", "userview", map[string]any{"id": "123456"})
	if got := strings.TrimSpace(string(body)); got != "[]" {
		t.Fatalf("missing user should be [], got %s", got)
	}
}

func TestClanLifecycleAndRankRules(t *testing.T) {
	h := newHarness(t)
	h.seed("4510186", "4120041", "4200999")

	clan := h.makeClan("4510186", "Test Clan", "[TC]")

	// Founder is the leader.
	_, body := h.browser("4510186", "clanview", map[string]any{"id": clan})
	view := decode[struct {
		Name    string `json:"name"`
		Members []struct {
			GUID string `json:"guid"`
			Rank string `json:"rank"`
		} `json:"members"`
	}](t, body)
	if len(view.Members) != 1 || view.Members[0].Rank != "4" {
		t.Fatalf("founder should be sole leader, got %+v", view.Members)
	}

	// A non-member cannot administer it.
	_, body = h.browser("4120041", "claninfo", map[string]any{"id": clan, "v": "hi"})
	if st := decode[status](t, body); st.Status != "error" {
		t.Fatal("a non-member must not be able to edit clan info")
	}

	// Invite and accept.
	if _, body = h.browser("4510186", "claninvite",
		map[string]any{"id": clan, "to": "4120041"}); decode[status](t, body).Status != "success" {
		t.Fatalf("invite refused: %s", body)
	}
	_, body = h.browser("4120041", "userinvites", nil)
	invites := decode[[]struct {
		Clan struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"clan"`
	}](t, body)
	if len(invites) != 1 || invites[0].Clan.ID != clan {
		t.Fatalf("expected one invitation for the clan, got %+v", invites)
	}
	if _, body = h.browser("4120041", "useraccept",
		map[string]any{"id": clan}); decode[status](t, body).Status != "success" {
		t.Fatalf("accept refused: %s", body)
	}

	// A recruit cannot promote anyone.
	_, body = h.browser("4120041", "clanrank",
		map[string]any{"id": clan, "to": "4120041", "rank": "4", "title": "Usurper"})
	if st := decode[status](t, body); st.Status != "error" {
		t.Fatal("a recruit must not be able to promote themselves")
	}

	// The leader can, but not above their own rank.
	if _, body = h.browser("4510186", "clanrank",
		map[string]any{"id": clan, "to": "4120041", "rank": "2", "title": "Officer"}); decode[status](t, body).Status != "success" {
		t.Fatalf("leader should be able to promote: %s", body)
	}
	_, body = h.browser("4510186", "clanrank",
		map[string]any{"id": clan, "to": "4120041", "rank": "9", "title": "Nope"})
	if st := decode[status](t, body); st.Status != "error" || st.Msg != "rank must be an integer 0 to 4" {
		t.Fatalf("out-of-range rank should be refused with the documented message, got %+v", st)
	}

	// An officer still cannot kick: that needs senior rank.
	_, body = h.browser("4120041", "clankick", map[string]any{"id": clan, "to": "4510186"})
	if st := decode[status](t, body); st.Status != "error" {
		t.Fatal("an officer must not be able to kick")
	}

	// Nor can a leader be kicked by an equal.
	if _, body = h.browser("4510186", "clanrank",
		map[string]any{"id": clan, "to": "4120041", "rank": "4", "title": "Leader"}); decode[status](t, body).Status != "success" {
		t.Fatalf("promote to leader: %s", body)
	}
	_, body = h.browser("4120041", "clankick", map[string]any{"id": clan, "to": "4510186"})
	if st := decode[status](t, body); st.Status != "error" {
		t.Fatal("a leader must not be able to kick another leader")
	}
}

func TestWearingATagRequiresMembership(t *testing.T) {
	h := newHarness(t)
	h.seed("4510186", "4200999")

	clan := h.makeClan("4510186", "Test Clan", "[TC]")

	// An outsider cannot wear the tag -- the whole point of a tag is that it
	// cannot be worn by someone with no claim to it.
	_, body := h.browser("4200999", "userclan", map[string]any{"id": clan})
	if st := decode[status](t, body); st.Status != "error" {
		t.Fatal("a non-member must not be able to wear the clan tag")
	}

	// The founder can, and it shows up on their profile.
	if _, body = h.browser("4510186", "userclan",
		map[string]any{"id": clan}); decode[status](t, body).Status != "success" {
		t.Fatalf("member should be able to wear the tag: %s", body)
	}
	_, body = h.browser("4510186", "userview", map[string]any{"id": "4510186"})
	if tag := decode[struct {
		Tag string `json:"tag"`
	}](t, body).Tag; tag != "[TC]" {
		t.Fatalf("expected the worn tag on the profile, got %q", tag)
	}

	// Leaving drops it, rather than leaving them wearing a tag they lost.
	if _, body = h.browser("4510186", "userleave",
		map[string]any{"id": clan}); decode[status](t, body).Status != "success" {
		t.Fatalf("leave refused: %s", body)
	}
	_, body = h.browser("4510186", "userview", map[string]any{"id": "4510186"})
	if tag := decode[struct {
		Tag string `json:"tag"`
	}](t, body).Tag; tag != "" {
		t.Fatalf("tag should be cleared on leaving, got %q", tag)
	}
}

func TestMailSendReadFoldersAndDelete(t *testing.T) {
	h := newHarness(t)
	h.seed("4510186", "4120041")

	// Sending works here, which is the headline difference from TribesNext.
	if _, body := h.mail("4120041", "send", map[string]any{
		"to": "4510186", "subject": "Scrim?", "body": "Tuesday.",
	}); decode[status](t, body).Status != "success" {
		t.Fatalf("send refused: %s", body)
	}

	_, body := h.mail("4510186", "count", nil)
	if got := strings.TrimSpace(string(body)); got != `"1"` {
		t.Fatalf("count should be a JSON string, got %s", got)
	}

	_, body = h.mail("4510186", "read", nil)
	inbox := decode[[]struct {
		ID       string `json:"id"`
		From     string `json:"from"`
		FromGUID string `json:"fromguid"`
		Subject  string `json:"subject"`
		Body     string `json:"body"`
		Unread   string `json:"unread"`
	}](t, body)
	if len(inbox) != 1 {
		t.Fatalf("expected one message, got %d", len(inbox))
	}
	if inbox[0].From != "Shifter" || inbox[0].FromGUID != "4120041" {
		t.Fatalf("sender should be resolved, got %+v", inbox[0])
	}
	if inbox[0].Unread != "1" {
		t.Fatal("a new message should be unread")
	}

	// The sender keeps a copy in Sent.
	_, body = h.mail("4120041", "read", map[string]any{"folder": "sent"})
	if sent := decode[[]struct {
		Subject string `json:"subject"`
	}](t, body); len(sent) != 1 || sent[0].Subject != "Scrim?" {
		t.Fatalf("sender should have a Sent copy, got %+v", sent)
	}

	// Reading marks the message read but does not remove it, and count reports
	// what is in the inbox rather than what is unread.
	if _, body = h.mail("4510186", "read", map[string]any{"id": inbox[0].ID}); len(body) == 0 {
		t.Fatal("read by id returned nothing")
	}
	_, body = h.mail("4510186", "read", nil)
	if again := decode[[]struct {
		Unread string `json:"unread"`
	}](t, body); len(again) != 1 || again[0].Unread != "0" {
		t.Fatalf("reading should clear the unread flag, got %+v", again)
	}
	_, body = h.mail("4510186", "count", nil)
	if got := strings.TrimSpace(string(body)); got != `"1"` {
		t.Fatalf("count should still see the message in the inbox: %s", got)
	}

	// Delete moves to Deleted first, and only purges on the second delete.
	if _, body = h.mail("4510186", "delete",
		map[string]any{"id": inbox[0].ID}); decode[status](t, body).Status != "success" {
		t.Fatalf("delete refused: %s", body)
	}
	_, body = h.mail("4510186", "count", nil)
	if got := strings.TrimSpace(string(body)); got != `"0"` {
		t.Fatalf("deleting should empty the inbox: %s", got)
	}
	_, body = h.mail("4510186", "read", map[string]any{"folder": "deleted"})
	if del := decode[[]struct {
		ID string `json:"id"`
	}](t, body); len(del) != 1 {
		t.Fatalf("expected the message in Deleted, got %d", len(del))
	}
	if _, body = h.mail("4510186", "delete",
		map[string]any{"id": inbox[0].ID}); decode[status](t, body).Status != "success" {
		t.Fatalf("purge refused: %s", body)
	}
	_, body = h.mail("4510186", "read", map[string]any{"folder": "deleted"})
	if del := decode[[]struct {
		ID string `json:"id"`
	}](t, body); len(del) != 0 {
		t.Fatalf("second delete should purge, got %d", len(del))
	}
}

func TestMailCannotBeReadByAnyoneElse(t *testing.T) {
	h := newHarness(t)
	h.seed("4510186", "4120041")

	if _, body := h.mail("4120041", "send", map[string]any{
		"to": "4510186", "subject": "private", "body": "secret",
	}); decode[status](t, body).Status != "success" {
		t.Fatalf("send refused: %s", body)
	}

	_, body := h.mail("4510186", "read", nil)
	id := decode[[]struct {
		ID string `json:"id"`
	}](t, body)[0].ID

	// A third party guessing the id must get nothing, not someone else's mail.
	h.seed("4200999")
	_, body = h.mail("4200999", "read", map[string]any{"id": id})
	if got := decode[[]struct {
		Body string `json:"body"`
	}](t, body); len(got) != 0 {
		t.Fatalf("mail leaked to another player: %+v", got)
	}
}

func TestBlockingSilentlyDropsMail(t *testing.T) {
	h := newHarness(t)
	h.seed("4510186", "4120041")

	if _, body := h.browser("4510186", "blockadd",
		map[string]any{"to": "4120041"}); decode[status](t, body).Status != "success" {
		t.Fatalf("blockadd refused: %s", body)
	}

	// The sender is told nothing, deliberately: revealing the block invites
	// working around it.
	if _, body := h.mail("4120041", "send", map[string]any{
		"to": "4510186", "subject": "hi", "body": "hi",
	}); decode[status](t, body).Status != "success" {
		t.Fatalf("blocked send should look like success, got %s", body)
	}

	_, body := h.mail("4510186", "count", nil)
	if got := strings.TrimSpace(string(body)); got != `"0"` {
		t.Fatalf("blocked mail should not arrive, count = %s", got)
	}
}

func TestBuddyList(t *testing.T) {
	h := newHarness(t)
	h.seed("4510186", "4120041")

	if _, body := h.browser("4510186", "buddyadd",
		map[string]any{"to": "4120041"}); decode[status](t, body).Status != "success" {
		t.Fatalf("buddyadd refused: %s", body)
	}
	_, body := h.browser("4510186", "buddylist", nil)
	if list := decode[[]struct {
		GUID string `json:"guid"`
		Name string `json:"name"`
	}](t, body); len(list) != 1 || list[0].Name != "Shifter" {
		t.Fatalf("buddy list: %+v", list)
	}

	// Adding someone who does not exist is a refusal, not a silent no-op.
	_, body = h.browser("4510186", "buddyadd", map[string]any{"to": "0000000"})
	if st := decode[status](t, body); st.Status != "error" {
		t.Fatal("adding an unknown player should be refused")
	}

	if _, body = h.browser("4510186", "buddyremove",
		map[string]any{"to": "4120041"}); decode[status](t, body).Status != "success" {
		t.Fatalf("buddyremove refused: %s", body)
	}
	_, body = h.browser("4510186", "buddylist", nil)
	if list := decode[[]struct{}](t, body); len(list) != 0 {
		t.Fatalf("expected empty buddy list, got %d", len(list))
	}
}

func TestAuthInfoEndpointForTheGameServer(t *testing.T) {
	h := newHarness(t)
	h.seed("4510186")

	clan := h.makeClan("4510186", "Test Clan", "[TC]")
	if _, body := h.browser("4510186", "userclan",
		map[string]any{"id": clan}); decode[status](t, body).Status != "success" {
		t.Fatalf("wear tag: %s", body)
	}

	// Wrong key is refused: this endpoint speaks for every player, so it must
	// not be open.
	resp, err := http.Get(h.srv.URL + "/tn/server/authinfo?key=wrong&guid=4510186")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad server key should be 401, got %d", resp.StatusCode)
	}

	resp, err = http.Get(h.srv.URL + "/tn/server/authinfo?key=test-key&guid=4510186")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// The exact record the game's auth-info format wants, so the mod can use it
	// verbatim: Name TAB Tag TAB Append TAB guid / count / one line per clan.
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header, count and one clan line, got %q", body)
	}
	header := strings.Split(lines[0], "\t")
	if len(header) != 4 || header[0] != "orange01" || header[1] != "[TC]" ||
		header[2] != "0" || header[3] != "4510186" {
		t.Fatalf("header record: %q", lines[0])
	}
	if lines[1] != "1" {
		t.Fatalf("clan count: %q", lines[1])
	}
	if fields := strings.Split(lines[2], "\t"); len(fields) != 6 || fields[0] != "Test Clan" {
		t.Fatalf("clan record: %q", lines[2])
	}
}

func TestUnknownMethodIsNotImplemented(t *testing.T) {
	h := newHarness(t)
	h.seed("4510186")

	if code, _ := h.browser("4510186", "nosuchmethod", nil); code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", code)
	}
}
