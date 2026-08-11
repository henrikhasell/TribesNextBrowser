package chat

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// reader is a test's view of one player's stream.
//
// Every line drained through it is checked against Dispatchable first, so the
// rule that matters most -- emit nothing the client cannot hear -- is enforced
// on every path any test exercises rather than in one test of its own. A verb
// IRCClient::dispatch does not handle is printed to the player as
// "(cmd:) ...", and no amount of correct behaviour behind it makes that
// acceptable.
type reader struct {
	t      *testing.T
	c      *Conn
	cursor int64
}

func (r *reader) drain() []string {
	r.t.Helper()
	lines, _ := r.c.Since(r.cursor)

	var out []string
	for _, l := range lines {
		if !Dispatchable(l.Text) {
			r.t.Fatalf("emitted a line the client cannot dispatch: %q", l.Text)
		}
		out = append(out, l.Text)
		r.cursor = l.Seq
	}
	return out
}

// seen reports whether any drained line contains all of the given substrings.
func seen(lines []string, parts ...string) bool {
	for _, l := range lines {
		ok := true
		for _, p := range parts {
			if !strings.Contains(l, p) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func testHub(t *testing.T) *Hub {
	t.Helper()
	return New(slog.New(slog.NewTextHandler(io.Discard, nil)), []string{"Tribes2"})
}

func join(t *testing.T, h *Hub, name, tag string, tribes ...Tribe) (*Conn, *reader) {
	t.Helper()
	c, fresh := h.Ensure(Identity{
		GUID:   "guid-" + name,
		Name:   name,
		Tag:    tag,
		Tribes: tribes,
	})
	if !fresh {
		t.Fatalf("%s: expected a fresh connection", name)
	}
	return c, &reader{t: t, c: c}
}

//-----------------------------------------------------------------------------

func TestFirstPollAnnouncesIdentity(t *testing.T) {
	h := testHub(t)
	c, r := join(t, h, "Harabec", "BSF")

	lines := r.drain()
	if len(lines) != 1 {
		t.Fatalf("expected one line on the first poll, got %v", lines)
	}
	// The nick is the triple IRCGetTriple splits, and it is the only thing that
	// gives the local player a name in chat.
	if !strings.Contains(lines[0], "CHALRESP_REPLY Harabec^BSF^0 guid-Harabec@tnb OK") {
		t.Fatalf("bad welcome: %q", lines[0])
	}

	// Once per connection, not once per poll. Thirty of these a minute would be
	// thirty trips through IRCClient::onChalRespReply.
	if again, fresh := h.Ensure(c.id); fresh || again != c {
		t.Fatal("a second poll made a new connection")
	}
	if lines := r.drain(); len(lines) != 0 {
		t.Fatalf("a second poll re-announced the identity: %v", lines)
	}
}

func TestUntaggedWarriorGetsABareNick(t *testing.T) {
	// Name^^0 makes IRCGetTriple return an empty triple, %p.nick stays empty,
	// and the member list falls back to printing raw carets.
	if got := Nick("Harabec", "", false); got != "Harabec" {
		t.Fatalf("untagged nick = %q, want %q", got, "Harabec")
	}
	if got := Nick("Sir Lancelot", "Round Table", true); got != "Sir_-_01Lancelot^Round_-_01Table^1" {
		t.Fatalf("escaped nick = %q", got)
	}
}

func TestJoinBroadcastsAndNames(t *testing.T) {
	h := testHub(t)
	a, ra := join(t, h, "Alpha", "")
	b, rb := join(t, h, "Bravo", "")
	ra.drain()
	rb.drain()

	a.Handle("JOIN #Tribes2")
	first := ra.drain()
	if !seen(first, "JOIN #Tribes2") {
		t.Fatalf("no join echo: %v", first)
	}
	if !seen(first, "353", ":Alpha") {
		t.Fatalf("no name reply: %v", first)
	}
	// 366 is not in IRCClient::dispatch; sending one would print into the
	// status pane. The member list is built from 353 alone.
	if seen(first, " 366 ") {
		t.Fatalf("sent an end-of-names: %v", first)
	}

	b.Handle("JOIN #Tribes2")
	if lines := ra.drain(); !seen(lines, ":Bravo!", "JOIN #Tribes2") {
		t.Fatalf("Alpha did not see Bravo join: %v", lines)
	}
	if lines := rb.drain(); !seen(lines, "353", "Alpha", "Bravo") {
		t.Fatalf("Bravo's member list is wrong: %v", lines)
	}
}

func TestMessageIsNotReflectedToItsSender(t *testing.T) {
	// IRCClient::send2 echoes a player's own PRIVMSG into the pane as it sends
	// it, so a reflection shows the line twice.
	h := testHub(t)
	a, ra := join(t, h, "Alpha", "")
	b, rb := join(t, h, "Bravo", "")
	a.Handle("JOIN #Tribes2")
	b.Handle("JOIN #Tribes2")
	ra.drain()
	rb.drain()

	a.Handle("PRIVMSG #Tribes2 :hello there")

	if lines := ra.drain(); seen(lines, "PRIVMSG", "hello there") {
		t.Fatalf("sender saw their own message: %v", lines)
	}
	if lines := rb.drain(); !seen(lines, ":Alpha!", "PRIVMSG #Tribes2 :hello there") {
		t.Fatalf("other member did not receive it: %v", lines)
	}
}

func TestPrivateMessageIsAddressedToTheRecipientsNick(t *testing.T) {
	// IRCClient::onPrivMsg opens a private pane named after the *sender* only
	// when the target is the receiving player's own nick.
	h := testHub(t)
	a, ra := join(t, h, "Alpha", "")
	_, rb := join(t, h, "Bravo", "BSF")
	ra.drain()
	rb.drain()

	a.Handle("PRIVMSG Bravo^BSF^0 :are you there")

	lines := rb.drain()
	if !seen(lines, ":Alpha!", "PRIVMSG Bravo^BSF^0 :are you there") {
		t.Fatalf("private message misrouted: %v", lines)
	}
	if lines := ra.drain(); len(lines) != 0 {
		t.Fatalf("sender got something back: %v", lines)
	}
}

func TestMessageToAnUnknownNick(t *testing.T) {
	h := testHub(t)
	a, ra := join(t, h, "Alpha", "")
	ra.drain()

	a.Handle("PRIVMSG Nobody :hello")
	if lines := ra.drain(); !seen(lines, " 401 ", "Nobody") {
		t.Fatalf("expected a no-such-nick: %v", lines)
	}
}

func TestTribeRoomsAreMembersOnly(t *testing.T) {
	h := testHub(t)
	member, rm := join(t, h, "Alpha", "BE", Tribe{ID: 7, Name: "Blood Eagle", Rank: 2})
	outsider, ro := join(t, h, "Bravo", "")
	rm.drain()
	ro.drain()

	// The channel name is built from the tribe *name*, escaped, exactly as
	// JoinPublicTribeChannel does it.
	member.Handle("JOIN #Blood_-_01Eagle_Private")
	lines := rm.drain()
	if !seen(lines, "JOIN #Blood_-_01Eagle_Private") {
		t.Fatalf("member could not join their own tribe room: %v", lines)
	}
	// Rank 2 is the administrative rung, and ops arrive after the join because
	// IRCClient::onMode needs the person to exist in the channel first.
	if !seen(lines, "353", "@Alpha^BE^0") {
		t.Fatalf("member was not opped: %v", lines)
	}

	outsider.Handle("JOIN #Blood_-_01Eagle_Private")
	if lines := ro.drain(); !seen(lines, " 473 ", "#Blood_-_01Eagle_Private") {
		t.Fatalf("outsider was not refused: %v", lines)
	}
	if lines := rm.drain(); len(lines) != 0 {
		t.Fatalf("the room heard about a refused join: %v", lines)
	}
}

func TestTribeMemberBelowRankGetsNoOps(t *testing.T) {
	h := testHub(t)
	c, r := join(t, h, "Alpha", "BE", Tribe{ID: 7, Name: "Blood Eagle", Rank: 1})
	r.drain()

	c.Handle("JOIN #Blood_-_01Eagle_Public")
	if lines := r.drain(); seen(lines, "353", "@Alpha") {
		t.Fatalf("rank 1 was opped: %v", lines)
	}
}

// Every request that raises IRCClient::connecting() must be answered by
// something that calls IRCClient::connected(), or the chat panes stay titled
// "CONNECTING" with no way back.
func TestEveryWaitStateIsCleared(t *testing.T) {
	h := testHub(t)
	c, r := join(t, h, "Alpha", "")
	c.Handle("JOIN #Tribes2")
	r.drain()

	cases := []struct {
		name, send, want string
	}{
		{"LIST", "LIST", " 323 "},
		{"ban list", "MODE #Tribes2 +b", " 368 "},
		{"WHO", "WHO Alpha", " 352 "},
		{"WHOIS", "WHOIS Alpha", " 311 "},
		// The stranger cases matter most: 352 and 311 are dropped by the client
		// unless it already knows the nick, so answering 401 instead would
		// strand the wait state forever.
		{"WHO a stranger", "WHO Nobody", " 352 "},
		{"WHOIS a stranger", "WHOIS Nobody", " 311 "},
	}

	for _, tc := range cases {
		c.Handle(tc.send)
		if lines := r.drain(); !seen(lines, tc.want) {
			t.Errorf("%s: no %s in %v", tc.name, tc.want, lines)
		}
	}
}

func TestListAlwaysEnds(t *testing.T) {
	h := New(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	c, r := join(t, h, "Alpha", "")
	r.drain()

	c.Handle("LIST")
	lines := r.drain()
	if len(lines) != 1 || !strings.Contains(lines[0], " 323 ") {
		t.Fatalf("an empty list should still end: %v", lines)
	}
}

func TestPartCarriesNoReason(t *testing.T) {
	// IRCClient::onPart passes the whole parameter string to findChannel, so a
	// reason on the end makes it a channel the client has never heard of and
	// the player is never removed from the pane.
	h := testHub(t)
	a, ra := join(t, h, "Alpha", "")
	b, rb := join(t, h, "Bravo", "")
	a.Handle("JOIN #Tribes2")
	b.Handle("JOIN #Tribes2")
	ra.drain()
	rb.drain()

	b.Handle("PART #Tribes2 :goodbye")

	lines := ra.drain()
	if !seen(lines, "PART #Tribes2") {
		t.Fatalf("no part: %v", lines)
	}
	for _, l := range lines {
		if strings.Contains(l, "PART") && strings.Contains(l, "goodbye") {
			t.Fatalf("part carried a reason: %q", l)
		}
	}
}

func TestOpsAreRequiredForTopicAndKick(t *testing.T) {
	h := testHub(t)
	a, ra := join(t, h, "Alpha", "")
	b, rb := join(t, h, "Bravo", "")
	// Alpha opens the room and therefore runs it; Bravo does not.
	a.Handle("JOIN #Pickup")
	b.Handle("JOIN #Pickup")
	ra.drain()
	rb.drain()

	b.Handle("TOPIC #Pickup :mine now")
	if lines := rb.drain(); !seen(lines, "NOTICE", "operator") {
		t.Fatalf("non-op set the topic: %v", lines)
	}

	a.Handle("TOPIC #Pickup :duel night")
	if lines := rb.drain(); !seen(lines, "TOPIC #Pickup :duel night") {
		t.Fatalf("op could not set the topic: %v", lines)
	}

	a.Handle("KICK #Pickup Bravo :out")
	if lines := rb.drain(); !seen(lines, "KICK #Pickup Bravo :out") {
		t.Fatalf("kick not delivered: %v", lines)
	}
	// And they really are out: a message to the room is now refused.
	b.Handle("PRIVMSG #Pickup :still here?")
	if lines := ra.drain(); seen(lines, "still here?") {
		t.Fatalf("a kicked player could still talk: %v", lines)
	}
}

func TestAMissedPollKeepsTheRoom(t *testing.T) {
	h := testHub(t)
	a, ra := join(t, h, "Alpha", "")
	b, rb := join(t, h, "Bravo", "")
	a.Handle("JOIN #Tribes2")
	b.Handle("JOIN #Tribes2")
	ra.drain()
	rb.drain()

	// A poll that never arrived, then one that did, inside the idle window.
	h.Sweep()

	again, fresh := h.Ensure(Identity{GUID: "guid-Bravo", Name: "Bravo"})
	if fresh {
		t.Fatal("a poll inside the idle window made a new connection")
	}
	if again != b {
		t.Fatal("the poll did not find the same connection")
	}
	// Nobody else saw anything: no QUIT, no re-JOIN.
	if lines := ra.drain(); len(lines) != 0 {
		t.Fatalf("the room was told about a missed poll: %v", lines)
	}

	// And a re-join of a room they never left rebuilds their own view only.
	b.Handle("JOIN #Tribes2")
	if lines := rb.drain(); !seen(lines, "JOIN #Tribes2") || !seen(lines, "353") {
		t.Fatalf("re-join did not resync: %v", lines)
	}
	if lines := ra.drain(); len(lines) != 0 {
		t.Fatalf("the room saw a duplicate join: %v", lines)
	}
}

func TestSweepAnnouncesOneQuit(t *testing.T) {
	h := testHub(t)

	// A clock the test owns, so "stopped polling" is a fact rather than a wait.
	base := time.Unix(1785000000, 0)
	now := base
	h.now = func() time.Time { return now }

	a, ra := join(t, h, "Alpha", "")
	b, _ := join(t, h, "Bravo", "")
	a.Handle("JOIN #Tribes2")
	a.Handle("JOIN #Pickup")
	b.Handle("JOIN #Tribes2")
	b.Handle("JOIN #Pickup")
	ra.drain()

	// Alpha keeps polling; Bravo does not.
	now = base.Add(2 * defaultIdle)
	h.Touch("guid-Alpha")
	h.Sweep()

	if h.Conn("guid-Bravo") != nil {
		t.Fatal("a connection that stopped polling was not swept")
	}
	if h.Conn("guid-Alpha") == nil {
		t.Fatal("a connection that kept polling was swept")
	}

	lines := ra.drain()
	var quits int
	for _, l := range lines {
		if strings.Contains(l, " QUIT ") {
			quits++
		}
	}
	// One QUIT, not one per shared room: IRCClient::onQuit removes the person
	// from every channel it can find them in.
	if quits != 1 {
		t.Fatalf("expected exactly one quit, got %d in %v", quits, lines)
	}
}

func TestRingReplaysWhatWasMissed(t *testing.T) {
	h := testHub(t)
	a, _ := join(t, h, "Alpha", "")
	b, rb := join(t, h, "Bravo", "")
	a.Handle("JOIN #Tribes2")
	b.Handle("JOIN #Tribes2")
	rb.drain()

	mark := b.Seq()
	a.Handle("PRIVMSG #Tribes2 :one")
	a.Handle("PRIVMSG #Tribes2 :two")

	lines, gap := b.Since(mark)
	if gap {
		t.Fatal("reported a gap it does not have")
	}
	if len(lines) != 2 || !strings.Contains(lines[1].Text, ":two") {
		t.Fatalf("replay is wrong: %v", lines)
	}
}

func TestRingReportsAnOverrun(t *testing.T) {
	h := testHub(t)
	c, _ := join(t, h, "Alpha", "")
	for i := 0; i < ringSize+10; i++ {
		c.send(":tnb NOTICE Alpha :filler")
	}
	if _, gap := c.Since(1); !gap {
		t.Fatal("an overrun ring did not report a gap")
	}
}

func TestUnknownVerbsAreIgnored(t *testing.T) {
	h := testHub(t)
	c, r := join(t, h, "Alpha", "")
	r.drain()

	for _, line := range []string{"CAP LS", "USER a b c :d", "NICK Somebody", "", "   "} {
		c.Handle(line)
	}
	if lines := r.drain(); len(lines) != 0 {
		t.Fatalf("answered something it should have dropped: %v", lines)
	}
}

func TestRateLimitStopsAFlood(t *testing.T) {
	h := testHub(t)
	c, r := join(t, h, "Alpha", "")
	c.Handle("JOIN #Tribes2")
	r.drain()

	// Well past the burst, with no wall-clock time to refill it.
	for i := 0; i < int(rateBurst)+50; i++ {
		c.Handle("LIST")
	}
	var ends int
	for _, l := range r.drain() {
		if strings.Contains(l, " 323 ") {
			ends++
		}
	}
	if ends > int(rateBurst) {
		t.Fatalf("rate limit let %d requests through", ends)
	}
	if ends == 0 {
		t.Fatal("rate limit let nothing through")
	}
}

func TestParseMessage(t *testing.T) {
	cases := []struct {
		in    string
		verb  string
		p0    string
		text  string
		trail bool
	}{
		{"JOIN #Tribes2", "JOIN", "#Tribes2", "", false},
		{"JOIN #Tribes2 ", "JOIN", "#Tribes2", "", false},
		{"PRIVMSG #a :hello there", "PRIVMSG", "#a", "hello there", true},
		{"KICK #a Bravo :get out", "KICK", "#a", "get out", true},
		{":ignored!x@y PART #a", "PART", "#a", "", false},
		{"AWAY :back soon", "AWAY", "", "back soon", true},
		{"QUIT", "QUIT", "", "", false},
	}
	for _, tc := range cases {
		m, ok := parseMessage(tc.in)
		if !ok {
			t.Errorf("%q did not parse", tc.in)
			continue
		}
		if m.verb != tc.verb || m.param(0) != tc.p0 {
			t.Errorf("%q -> verb %q param %q", tc.in, m.verb, m.param(0))
		}
		if tc.trail && m.text(1) != tc.text {
			t.Errorf("%q -> text %q, want %q", tc.in, m.text(1), tc.text)
		}
	}
}
