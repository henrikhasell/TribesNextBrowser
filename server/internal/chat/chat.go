// Package chat is the community chat server: the parts of RFC 1459 that the
// game's own client understands, and nothing else.
//
// Tribes 2's IRC client is not in the executable. It is roughly a hundred
// IRCClient::* functions of TorqueScript in scripts/ChatGui.cs, and the whole
// of its transport is two leaf functions -- IRCClient::send and
// IRCTCP::onLine. The mod replaces those two with an HTTPS stream, so the lines
// this package produces reach a completely unmodified client: the channel
// model, the member lists, the tribe-tag rendering and every screen are the
// shipped ones.
//
// # Why not a real ircd
//
// Because the client is not a real IRC client. Three things follow from that,
// and they shape every decision here:
//
//   - IRCClient::dispatch is a closed switch. A verb it does not know is
//     printed to the player as "(cmd:) ...", so the welcome burst, the end of
//     NAMES and half the numerics a conventional server sends are not
//     harmless -- they are visible noise. See dispatchable in wire.go.
//   - Several client actions raise a wait state (IRCClient::connecting) that
//     only specific replies clear. A LIST with no 323 after it leaves the
//     chat panes titled "CONNECTING" forever.
//   - A NICK for someone already known corrupts the client's person table
//     (ChatGui.cs:1846 renames but ChatGui.cs:1000 still matches on the old
//     name), so identity is fixed for the life of a connection.
//
// # Why in-process
//
// State lives in memory and dies with the process. That matches what this
// service already is: auth.Sessions is an in-memory map and .do/app.yaml pins
// instance_count to 1. Chat is the first thing that would break under a
// scale-out -- two instances would each hold half the room -- and the fix then
// is Postgres LISTEN/NOTIFY or sticky routing, not a database table. Nothing is
// persisted deliberately: the messages are not ours to keep.
package chat

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// ringSize is how many lines a disconnected client can miss and still
	// receive on reconnect. At chat speeds this is minutes of a busy room.
	ringSize = 256

	// defaultGrace is how long a connection's presence survives its stream
	// dropping. Long enough that a reconnect is invisible to everyone else in
	// the room, short enough that a player who closed the game leaves it.
	defaultGrace = 30 * time.Second

	// Rate limiting, per connection: a steady five lines a second with twenty
	// in hand. The client itself bursts -- joining a room sends a JOIN and
	// then a MODE, and a reconnect re-joins every room at once -- so the
	// burst has to be comfortably above anything the shipped scripts do.
	rateRefill = 5.0
	rateBurst  = 20.0
)

// Identity is who a connection belongs to, resolved once when the stream
// opens. It is the same data dbproxy.Certificate assembles, and from the same
// two store calls, because it answers the same question: what does this
// warrior's name look like, and which tribes are they in.
type Identity struct {
	GUID   string
	Name   string
	Tag    string
	Append bool
	Tribes []Tribe
}

// Tribe is one membership. Rank is the clan_members rank: 2 and above is the
// rung the shipped screens treat as administrative (webbrowser.cs:1921), and
// the rung that gets channel operator here.
type Tribe struct {
	ID   int64
	Name string
	Rank int
}

type entry struct {
	seq  int64
	line string
}

// Hub owns every connection and every room.
type Hub struct {
	mu    sync.Mutex
	conns map[string]*Conn // by guid
	nicks map[string]*Conn // by folded wire nick
	rooms map[string]*room // by folded channel name

	log   *slog.Logger
	grace time.Duration
	now   func() time.Time
}

type roomKind int

const (
	roomAdHoc roomKind = iota
	roomPublic
	roomTribe
)

type room struct {
	name    string // wire name, e.g. "#Blood_-_01Eagle_Public"
	topic   string
	kind    roomKind
	tribeID int64
	members map[string]*Conn // by guid
	ops     map[string]bool  // by guid
	voice   map[string]bool  // by guid
}

// New builds a hub with a set of always-present public rooms. Those rooms
// appear in LIST even when empty and survive their last member leaving; every
// other room is created by the first person to join it and disappears with the
// last.
func New(log *slog.Logger, public []string) *Hub {
	h := &Hub{
		conns: map[string]*Conn{},
		nicks: map[string]*Conn{},
		rooms: map[string]*room{},
		log:   log,
		grace: defaultGrace,
		now:   time.Now,
	}
	for _, name := range public {
		wire := ChannelName(name)
		if wire == "" {
			continue
		}
		h.rooms[fold(wire)] = &room{
			name:    wire,
			kind:    roomPublic,
			members: map[string]*Conn{},
			ops:     map[string]bool{},
			voice:   map[string]bool{},
		}
	}
	return h
}

// Conn is one player's presence: their identity, the rooms they are in, and
// the ring of lines waiting to reach them.
//
// It outlives the HTTP request that carries its stream. That is the whole point
// of Attach/Detach: a stream that drops and comes back within the grace period
// finds the same Conn, still in the same rooms, with the lines it missed still
// in the ring -- and nobody else in the room saw anything happen.
type Conn struct {
	hub  *Hub
	id   Identity
	nick string
	who  string // the "nick!guid@tnb" prefix on everything they say

	mu       sync.Mutex
	seq      int64
	ring     []entry
	notify   chan struct{}
	attached bool
	reap     *time.Timer

	away   string
	tokens float64
	last   time.Time
}

// Attach registers a stream for this identity and returns the connection it
// should read from, and whether that connection is a new one.
//
// A second stream for the same GUID takes over the existing connection rather
// than making another one: one warrior is one presence, and the shipped client
// has no concept of being in a room twice.
//
// The fresh flag is what a reconnecting client needs to know. If its stream
// dropped and came back inside the grace period it finds the same connection,
// still in the same rooms, and should carry on as though nothing happened. If
// it finds a new one, everything the hub knew about it is gone and it has to
// re-join -- and only the hub can tell the two apart.
func (h *Hub) Attach(id Identity) (conn *Conn, fresh bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	c := h.conns[id.GUID]
	if c == nil {
		fresh = true
		c = &Conn{
			hub:    h,
			id:     id,
			nick:   Nick(id.Name, id.Tag, id.Append),
			notify: make(chan struct{}, 1),
			tokens: rateBurst,
			last:   h.now(),
		}
		c.who = c.nick + "!" + id.GUID + "@" + Host
		h.conns[id.GUID] = c
		h.nicks[fold(c.nick)] = c
	} else if c.reap != nil {
		// Reviving one that was on its way out. Identity is deliberately not
		// refreshed: a tag change would mean a new nick, and a new nick means
		// NICK, which this client cannot absorb. It applies on the next
		// connection instead -- the same conclusion webbrowser.cs:1787 reached.
		c.reap.Stop()
		c.reap = nil
	}
	c.attached = true

	// The client reaches IDIRC_CONNECTED through IRCClient::onChalRespReply and
	// nowhere else on this path, so this line is what makes the chat window
	// live. The mod's override of that function drops the WONLoginIRC() check
	// the shipped one does -- there is no WON session to check against, and the
	// session that authorised this stream is a stronger proof than the one it
	// replaces. See TNBrowser/tnbrowser/chat.cs.
	c.send(":" + ServerName + " CHALRESP_REPLY " + c.nick + " " + c.id.GUID + "@" + Host + " OK")
	return c, fresh
}

// Detach gives up the stream. Presence survives for the grace period, so a
// reconnect inside it is invisible to every other player.
func (h *Hub) Detach(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	c.attached = false
	if c.reap != nil {
		c.reap.Stop()
	}
	c.reap = time.AfterFunc(h.grace, func() { h.reapConn(c) })
}

// Conn finds a live connection by GUID, for the send endpoint.
func (h *Hub) Conn(guid string) *Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[guid]
}

// reapConn is the grace period expiring: the player is gone.
func (h *Hub) reapConn(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if c.attached {
		return // came back while the timer was in flight
	}
	if h.conns[c.id.GUID] != c {
		return // already replaced
	}

	h.partAll(c, "Connection closed")
	delete(h.conns, c.id.GUID)
	delete(h.nicks, fold(c.nick))
}

// partAll removes a connection from every room and tells the rooms about it
// once, as a QUIT. Caller holds h.mu.
func (h *Hub) partAll(c *Conn, reason string) {
	line := ":" + c.who + " QUIT :" + reason
	seen := map[string]bool{}
	for key, r := range h.rooms {
		if _, in := r.members[c.id.GUID]; !in {
			continue
		}
		delete(r.members, c.id.GUID)
		delete(r.ops, c.id.GUID)
		delete(r.voice, c.id.GUID)
		for guid, m := range r.members {
			if !seen[guid] {
				seen[guid] = true
				m.send(line)
			}
		}
		h.dropIfEmpty(key, r)
	}
}

// dropIfEmpty forgets an ad-hoc room nobody is in. Configured public rooms and
// tribe rooms stay, so they keep their topic and appear in LIST.
func (h *Hub) dropIfEmpty(key string, r *room) {
	if r.kind == roomAdHoc && len(r.members) == 0 {
		delete(h.rooms, key)
	}
}

//-----------------------------------------------------------------------------
// The ring
//-----------------------------------------------------------------------------

// send queues one line for this player. Everything the hub emits goes through
// here, which is what makes the dispatch-table assertion in the tests possible.
func (c *Conn) send(line string) {
	c.mu.Lock()
	c.seq++
	c.ring = append(c.ring, entry{c.seq, line})
	if len(c.ring) > ringSize {
		c.ring = append([]entry(nil), c.ring[len(c.ring)-ringSize:]...)
	}
	c.mu.Unlock()

	select {
	case c.notify <- struct{}{}:
	default:
	}
}

// Line is one queued line and the cursor value that acknowledges it.
type Line struct {
	Seq  int64
	Text string
}

// Since returns the lines after cursor, and whether the ring had already
// discarded something the caller had not seen.
//
// A gap is not an error worth telling the player about: it means they were
// disconnected long enough for a busy room to overrun the ring, and the lines
// are simply gone. It is logged and no more.
func (c *Conn) Since(cursor int64) (lines []Line, gap bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.ring) == 0 {
		return nil, false
	}
	if cursor < c.ring[0].seq-1 {
		gap = true
	}
	for _, e := range c.ring {
		if e.seq > cursor {
			lines = append(lines, Line{e.seq, e.line})
		}
	}
	return lines, gap
}

// Seq is the sequence number of the last line queued.
func (c *Conn) Seq() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seq
}

// Nick is the wire nick, for logging.
func (c *Conn) Nick() string { return c.nick }

// Wait blocks until there is something new, the deadline passes, or the request
// is cancelled. It reports whether new lines are waiting.
func (c *Conn) Wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-c.notify:
		return true
	case <-t.C:
		return false
	case <-ctx.Done():
		return false
	}
}

//-----------------------------------------------------------------------------
// Rooms
//-----------------------------------------------------------------------------

// findRoom resolves a wire name, creating the room if the caller is allowed to
// create it. Caller holds h.mu.
func (h *Hub) findRoom(name string) *room {
	return h.rooms[fold(name)]
}

// tribeFor matches a channel name against the connection's own tribes.
//
// The suffixes are not ours to choose: JoinPublicTribeChannel (ChatGui.cs:252)
// builds the name as channelName(tribe) @ "_Public", and IRCClient::findChannel
// keys a channel's "tribe" flag off exactly "_Public" and "_Private" -- which is
// also why IRCClient::onList hides both from the room list. A tribe room is
// therefore addressed by the tribe's *name*, escaped, and not by its id.
func tribeFor(id Identity, name string) (Tribe, bool, bool) {
	base := strings.TrimPrefix(name, "#")
	var suffix string
	switch {
	case strings.HasSuffix(base, "_Public"):
		suffix = "_Public"
	case strings.HasSuffix(base, "_Private"):
		suffix = "_Private"
	default:
		return Tribe{}, false, false
	}
	want := fold(strings.TrimSuffix(base, suffix))
	for _, t := range id.Tribes {
		if fold(Escape(t.Name)) == want {
			return t, true, true
		}
	}
	return Tribe{}, true, false // a tribe room, but not one of theirs
}

// members returns the room's occupants sorted by nick, so NAMES and LIST are
// stable between calls and a test can assert on them.
func (r *room) sorted() []*Conn {
	out := make([]*Conn, 0, len(r.members))
	for _, c := range r.members {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].nick < out[j].nick })
	return out
}

// broadcast sends to everyone in the room, optionally skipping one member.
//
// The skip is not an optimisation. IRCClient::send2 (ChatGui.cs:2761) echoes a
// player's own PRIVMSG into the channel pane as it sends it, so reflecting one
// back shows it twice.
func (r *room) broadcast(line string, except *Conn) {
	for _, m := range r.members {
		if m == except {
			continue
		}
		m.send(line)
	}
}
