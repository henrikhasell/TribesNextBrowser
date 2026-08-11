package chat

import (
	"sort"
	"strconv"
	"strings"
)

// Handle is one line from a player.
//
// Everything runs under the hub lock. There is no I/O on this path -- identity
// and tribe membership were resolved when the connection was made -- so the lock is
// held for the length of a map lookup and a few string joins, and holding it
// across the whole command is what makes "check membership, then broadcast"
// atomic.
func (c *Conn) Handle(line string) {
	if !c.allow() {
		return
	}
	m, ok := parseMessage(line)
	if !ok {
		return
	}

	h := c.hub
	h.mu.Lock()
	defer h.mu.Unlock()

	switch m.verb {
	case "JOIN":
		h.join(c, m.param(0))
	case "PART":
		h.part(c, m.param(0))
	case "PRIVMSG":
		h.message(c, "PRIVMSG", m.param(0), m.text(1))
	case "NOTICE":
		h.message(c, "NOTICE", m.param(0), m.text(1))
	case "MODE":
		h.mode(c, m)
	case "TOPIC":
		h.topic(c, m.param(0), m.text(1))
	case "KICK":
		h.kick(c, m.param(0), m.param(1), m.text(2))
	case "INVITE":
		h.invite(c, m.param(0), m.param(1))
	case "INSTANT":
		h.instant(c, m.param(0))
	case "LIST":
		h.list(c)
	case "WHO":
		h.who(c, m.param(0))
	case "WHOIS":
		h.whois(c, m.param(0))
	case "AWAY":
		h.away(c, m.text(0))
	case "QUIT":
		h.partAll(c, "Leaving")

	case "PING":
		// The client only sends this from IRCClient::ping, which measures a
		// round trip and reads the answer out of a NOTICE CTCP reply. A plain
		// PONG is enough for the state machine and costs nothing.
		c.send(":" + ServerName + " PONG " + ServerName + " :" + m.text(0))

	case "PONG", "NICK", "VERSION":
		// Deliberately nothing. NICK in particular: the identity for a
		// connection is fixed when it opens, and answering one would mean
		// sending a NICK back, which corrupts the client's person table.

	default:
		// Unknown verbs are dropped rather than answered. There is no numeric
		// for "I did not understand that" in the client's dispatch table, so
		// the only thing an answer could do is print noise.
		c.hub.log.Debug("chat: ignoring", "verb", m.verb, "guid", c.id.GUID)
	}
}

// allow is the rate limiter: a token bucket, refilled by wall clock.
func (c *Conn) allow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.hub.now()
	if !c.last.IsZero() {
		c.tokens += now.Sub(c.last).Seconds() * rateRefill
		if c.tokens > rateBurst {
			c.tokens = rateBurst
		}
	}
	c.last = now
	if c.tokens < 1 {
		return false
	}
	c.tokens--
	return true
}

//-----------------------------------------------------------------------------
// Emitting
//-----------------------------------------------------------------------------

// numeric writes a server reply. Every numeric in this protocol carries the
// recipient's own nick as its first parameter and every handler in ChatGui.cs
// strips it back off, so it is added here rather than at thirty call sites.
func (c *Conn) numeric(code string, args ...string) {
	c.send(":" + ServerName + " " + code + " " + c.nick + " " + strings.Join(args, " "))
}

// notice is how the hub talks to a player in words. It lands in the status pane
// via IRCClient::onNotice.
//
// No \x01 anywhere in the text: onNotice re-dispatches anything containing one
// as an embedded CTCP command (ChatGui.cs:2576).
func (c *Conn) notice(text string) {
	c.send(":" + ServerName + " NOTICE " + c.nick + " :" + strings.ReplaceAll(text, "\x01", ""))
}

//-----------------------------------------------------------------------------
// Channels
//-----------------------------------------------------------------------------

func (h *Hub) join(c *Conn, name string) {
	name = ChannelName(name)
	if name == "" {
		return
	}

	r := h.findRoom(name)
	if r == nil {
		t, isTribe, mine := tribeFor(c.id, name)
		if isTribe && !mine {
			// The one refusal that matters, and the reason tribe rooms are
			// safe to name after a tribe rather than after a secret.
			c.numeric(errInviteOnly, name, ":Cannot join channel (tribe members only)")
			return
		}
		r = &room{
			name:    name,
			members: map[string]*Conn{},
			ops:     map[string]bool{},
			voice:   map[string]bool{},
		}
		if isTribe {
			r.kind = roomTribe
			r.tribeID = t.ID
			r.topic = t.Name
			if strings.HasSuffix(name, "_Private") {
				r.topic = t.Name + " (members only)"
			}
		}
		h.rooms[fold(name)] = r
	} else if r.kind == roomTribe {
		if _, _, mine := tribeFor(c.id, name); !mine {
			c.numeric(errInviteOnly, name, ":Cannot join channel (tribe members only)")
			return
		}
	}

	if _, already := r.members[c.id.GUID]; already {
		// A re-join, which is the normal shape of a reconnect: the client's
		// IRCClient::reconnect stashed its rooms, deleted them, and asked for
		// them again. Rebuild that player's view and tell nobody else, because
		// as far as the room is concerned they never left.
		h.syncJoin(c, r)
		return
	}

	r.members[c.id.GUID] = c
	switch r.kind {
	case roomTribe:
		if t, _, mine := tribeFor(c.id, name); mine && t.Rank >= 2 {
			r.ops[c.id.GUID] = true
		}
	case roomAdHoc:
		// Whoever opens a room runs it. Public rooms deliberately have no
		// operator at all: there is nobody to be one.
		if len(r.members) == 1 {
			r.ops[c.id.GUID] = true
		}
	}

	r.broadcast(":"+c.who+" JOIN "+r.name, c)
	h.syncJoin(c, r)

	// After the JOIN, never before it: IRCClient::onMode needs the person to
	// exist in the channel before it can flag them (ChatGui.cs:2199).
	if r.ops[c.id.GUID] {
		r.broadcast(":"+ServerName+" MODE "+r.name+" +o "+c.nick, c)
	}
}

// syncJoin gives one player the room: the JOIN that creates the channel object,
// its topic, and the member list.
//
// 353 and no 366. The end-of-names numeric is not in IRCClient::dispatch, so
// sending it would print "(cmd:) ... 366 ..." into the status pane; the client
// rebuilds its member list off 353 alone (ChatGui.cs:1954).
func (h *Hub) syncJoin(c *Conn, r *room) {
	c.send(":" + c.who + " JOIN " + r.name)

	if r.topic == "" {
		c.numeric(rplNoTopic, r.name, ":No topic is set")
	} else {
		c.numeric(rplTopic, r.name, ":"+r.topic)
	}

	var names []string
	for _, m := range r.sorted() {
		switch {
		case r.ops[m.id.GUID]:
			names = append(names, "@"+m.nick)
		case r.voice[m.id.GUID]:
			names = append(names, "+"+m.nick)
		default:
			names = append(names, m.nick)
		}
	}
	c.numeric(rplNameReply, "=", r.name, ":"+strings.Join(names, " "))
}

func (h *Hub) part(c *Conn, name string) {
	name = ChannelName(name)
	r := h.findRoom(name)
	if r == nil {
		return
	}
	if _, in := r.members[c.id.GUID]; !in {
		return
	}

	// No reason on a PART: IRCClient::onPart (ChatGui.cs:2048) passes the whole
	// parameter string to findChannel, so "#room :goodbye" is a channel it has
	// never heard of and the player is never removed from the pane.
	line := ":" + c.who + " PART " + r.name
	r.broadcast(line, nil)

	delete(r.members, c.id.GUID)
	delete(r.ops, c.id.GUID)
	delete(r.voice, c.id.GUID)
	h.dropIfEmpty(fold(r.name), r)
}

func (h *Hub) topic(c *Conn, name, text string) {
	r := h.findRoom(ChannelName(name))
	if r == nil {
		return
	}
	if !r.ops[c.id.GUID] {
		c.notice("Only a channel operator can set the topic in " + r.name + ".")
		return
	}
	r.topic = text
	r.broadcast(":"+c.who+" TOPIC "+r.name+" :"+text, nil)
}

func (h *Hub) kick(c *Conn, name, nick, reason string) {
	r := h.findRoom(ChannelName(name))
	if r == nil {
		return
	}
	if !r.ops[c.id.GUID] {
		c.notice("Only a channel operator can remove someone from " + r.name + ".")
		return
	}
	target := h.nicks[fold(nick)]
	if target == nil {
		c.numeric(errNoSuchNick, nick, ":No such nick/channel")
		return
	}
	if _, in := r.members[target.id.GUID]; !in {
		return
	}
	if reason == "" {
		reason = "Kicked"
	}

	r.broadcast(":"+c.who+" KICK "+r.name+" "+target.nick+" :"+reason, nil)
	delete(r.members, target.id.GUID)
	delete(r.ops, target.id.GUID)
	delete(r.voice, target.id.GUID)
	h.dropIfEmpty(fold(r.name), r)
}

// mode covers three unrelated jobs that share a verb.
func (h *Hub) mode(c *Conn, m message) {
	name := ChannelName(m.param(0))
	r := h.findRoom(name)
	if r == nil {
		return
	}

	flags := m.param(1)

	// The bare query the client sends itself on joining (ChatGui.cs:1751). It
	// raises no wait state, but answering keeps the channel's flags honest.
	if flags == "" {
		modes := "+t"
		if r.kind == roomTribe {
			modes = "+tp"
		}
		c.numeric(rplChannelModes, r.name, modes)
		return
	}

	enable := strings.HasPrefix(flags, "+")
	arg := m.param(2)

	for _, f := range flags[1:] {
		switch f {
		case 'b':
			// The ban list dialog. Not implemented, but it must still be
			// answered: IRCClient::requestBanList raised a wait state and only
			// 368 clears it, so silence leaves the pane titled "CONNECTING".
			c.numeric(rplEndOfBanList, r.name, ":End of ban list")
		case 'o', 'v':
			if !r.ops[c.id.GUID] {
				c.notice("Only a channel operator can do that in " + r.name + ".")
				return
			}
			target := h.nicks[fold(arg)]
			if target == nil {
				c.numeric(errNoSuchNick, arg, ":No such nick/channel")
				return
			}
			if _, in := r.members[target.id.GUID]; !in {
				return
			}
			set := r.ops
			if f == 'v' {
				set = r.voice
			}
			if enable {
				set[target.id.GUID] = true
			} else {
				delete(set, target.id.GUID)
			}
			r.broadcast(":"+c.who+" MODE "+r.name+" "+flags[:1]+string(f)+" "+target.nick, nil)
		default:
			// i, l, m, n, p, s, t, k -- the channel options dialog. The client
			// applies them locally the moment it sees them echoed, so refusing
			// silently would leave its idea of the room wrong; say so instead.
			c.notice("Channel options are not supported here.")
			return
		}
	}
}

func (h *Hub) list(c *Conn) {
	names := make([]string, 0, len(h.rooms))
	for key := range h.rooms {
		names = append(names, key)
	}
	sort.Strings(names)

	for _, key := range names {
		r := h.rooms[key]
		c.numeric(rplList, r.name, strconv.Itoa(len(r.members)), ":"+r.topic)
	}

	// Always, even with no rooms at all. IRCClient::requestChannelList raised a
	// wait state and IRCClient::onListEnd is the only thing that clears it.
	c.numeric(rplListEnd, ":End of list")
}

//-----------------------------------------------------------------------------
// People
//-----------------------------------------------------------------------------

// message routes a PRIVMSG or a NOTICE, to a room or to a person.
func (h *Hub) message(c *Conn, verb, target, text string) {
	if target == "" || text == "" {
		return
	}

	if isChannel(target) {
		r := h.findRoom(ChannelName(target))
		if r == nil {
			c.numeric(errNoSuchNick, target, ":No such nick/channel")
			return
		}
		if _, in := r.members[c.id.GUID]; !in {
			c.numeric(errNoSuchNick, target, ":No such nick/channel")
			return
		}
		// Everyone but the sender: their own client already put it on screen.
		r.broadcast(":"+c.who+" "+verb+" "+r.name+" :"+text, c)
		return
	}

	to := h.nicks[fold(target)]
	if to == nil {
		c.numeric(errNoSuchNick, target, ":No such nick/channel")
		return
	}

	// Addressed to the recipient's own nick, which is what makes the client
	// open a private pane titled with the *sender's* name: IRCClient::onPrivMsg
	// (ChatGui.cs:1780) swaps the two when the target is itself.
	to.send(":" + c.who + " " + verb + " " + to.nick + " :" + text)

	if verb == "PRIVMSG" && to.away != "" {
		c.numeric(rplAway, to.nick, ":"+to.away)
	}
}

func (h *Hub) invite(c *Conn, nick, channel string) {
	to := h.nicks[fold(nick)]
	if to == nil {
		c.numeric(errNoSuchNick, nick, ":No such nick/channel")
		return
	}
	name := ChannelName(channel)

	c.numeric(rplInviting, to.nick, name)
	to.send(":" + c.who + " INVITE " + to.nick + " :" + name)
}

// instant is WON's own verb, the "invite to chat" the member-list menu sends.
// The client shows a dialog naming the sender and ignores everything else in
// the line (ChatGui.cs:3070).
func (h *Hub) instant(c *Conn, nick string) {
	to := h.nicks[fold(nick)]
	if to == nil {
		c.numeric(errNoSuchNick, nick, ":No such nick/channel")
		return
	}
	to.send(":" + c.who + " INSTANT " + to.nick + " :0")
}

// who and whois answer even for a nick this hub has never heard of.
//
// That is not sloppiness. Both raise a wait state, and the only replies that
// clear it -- 352 for WHO, 311 for WHOIS -- are dropped by the client unless it
// already knows the nick (ChatGui.cs:2007, :2333). Refusing with 401 would
// leave the chat panes titled "CONNECTING" with no way back, so an answer about
// a stranger is strictly better than no answer at all.
func (h *Hub) who(c *Conn, nick string) {
	if nick == "" {
		c.numeric(rplEndOfWho, ":End of WHO list")
		return
	}
	guid, real := h.describe(nick)
	c.numeric(rplWhoReply, "*", guid, Host, ServerName, nick, "H", ":0", real)
	c.numeric(rplEndOfWho, nick, ":End of WHO list")
}

func (h *Hub) whois(c *Conn, nick string) {
	if nick == "" {
		return
	}
	guid, real := h.describe(nick)
	c.numeric(rplWhoisUser, nick, guid, Host, ":"+real)
	c.numeric(rplWhoisIdle, nick, "0", "0", ":seconds idle")
	c.numeric(rplEndOfWhois, nick, ":End of WHOIS list")
}

// describe is the identity half of a WHO or WHOIS: the GUID, which is the one
// public and stable identifier here, and the warrior name behind the nick.
func (h *Hub) describe(nick string) (guid, real string) {
	if p := h.nicks[fold(nick)]; p != nil {
		return p.id.GUID, p.id.Name
	}
	return "0", Unescape(strings.SplitN(nick, "^", 2)[0])
}

func (h *Hub) away(c *Conn, text string) {
	c.away = text
	if text == "" {
		c.numeric(rplUnaway, ":You are no longer marked as being away")
	} else {
		c.numeric(rplNowAway, ":You have been marked as being away")
	}

	line := ":" + ServerName + " AWAY " + c.nick + " :" + text
	seen := map[string]bool{}
	for _, r := range h.rooms {
		if _, in := r.members[c.id.GUID]; !in {
			continue
		}
		for guid, m := range r.members {
			if m != c && !seen[guid] {
				seen[guid] = true
				m.send(line)
			}
		}
	}
}
