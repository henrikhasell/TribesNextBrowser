package chat

import (
	"strings"
)

// ServerName is the prefix on every line this hub originates: ":tnb 353 ...".
//
// The client never parses it -- IRCClient::dispatch throws the prefix away for
// numerics -- but a line with no prefix at all is parsed as though its first
// word were the command, so it has to be *something*.
const ServerName = "tnb"

// Host is the second half of a person's identity, "nick!<guid>@tnb".
//
// IRCClient::findPerson2 (ChatGui.cs:1011) splits the prefix on "!" and hands
// the remainder to setIdentity, which stores it and otherwise ignores it. The
// GUID goes there because WHO and WHOIS render it back to the player, and the
// GUID is the one identifier in this system that is both stable and public.
const Host = "tnb"

// escapeSeq is $ESCAPE_SEQ from ChatGui.cs:9, and "01" is its only defined code
// (IRCClient::undoEscapes, :1085). A space in a name or a channel becomes
// "_-_01" on the wire and comes back out as a space on the way to the screen.
//
// This is not decoration. Every parameter in this protocol is space-delimited,
// so a warrior called "Sir Lancelot" or a tribe called "Blood Eagle" would
// otherwise arrive as two fields and be rendered as one word of each.
const escapeSeq = "_-_01"

// Escape hides spaces the way IRCClient::doEscapes does.
func Escape(s string) string {
	return strings.ReplaceAll(s, " ", escapeSeq)
}

// Unescape is the inverse, for turning a channel name back into a tribe name.
func Unescape(s string) string {
	return strings.ReplaceAll(s, escapeSeq, " ")
}

// Nick builds the triple the client's IRCGetTriple native splits.
//
//	name^tag^append     append decides which side the tag renders on
//	name                a warrior in no tribe
//
// The bare form for an untagged warrior is required, not preferred. "Name^^0"
// makes IRCGetTriple return an empty triple, IRCClient::setIdentity then leaves
// %p.nick empty, and the member list falls back to printing the raw nick --
// carets and all. Probed against the shipped binary; see WON_PROTOCOL.md.
func Nick(name, tag string, appendTag bool) string {
	name = Escape(name)
	if tag == "" {
		return name
	}
	side := "0"
	if appendTag {
		side = "1"
	}
	return name + "^" + Escape(tag) + "^" + side
}

// ChannelName normalises a room name to what goes on the wire: a leading "#"
// and no spaces. IRCClient::channelName (ChatGui.cs:1279) does exactly this on
// the client, and the two have to agree or a room the client asked to join is
// not the room it is put in.
func ChannelName(s string) string {
	s = Escape(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	if s[0] == '#' || s[0] == '&' {
		return s
	}
	return "#" + s
}

func isChannel(s string) bool {
	return s != "" && (s[0] == '#' || s[0] == '&')
}

// fold is the case-insensitive key a room or nick is stored under. IRC has its
// own idea of case folding involving []\ ; this is the plain one, because the
// names here come from a database of warrior and tribe names rather than from
// an arbitrary IRC network.
func fold(s string) string { return strings.ToLower(s) }

//-----------------------------------------------------------------------------
// Inbound
//-----------------------------------------------------------------------------

// message is one parsed line from a client.
//
// Clients never send a prefix -- the shipped IRCClient::send writes the command
// straight out -- but one is tolerated and discarded rather than being mistaken
// for a verb.
type message struct {
	verb     string
	params   []string
	trailing string // the part after " :", already stripped of the colon
	hasTrail bool
}

func parseMessage(line string) (message, bool) {
	line = strings.TrimRight(line, "\r\n")
	line = strings.TrimSpace(line)
	if line == "" {
		return message{}, false
	}

	if line[0] == ':' {
		i := strings.IndexByte(line, ' ')
		if i < 0 {
			return message{}, false
		}
		line = strings.TrimLeft(line[i+1:], " ")
	}

	var m message

	// The trailing parameter is everything after the first " :", and it is the
	// only parameter allowed to contain spaces. A leading ":" with no space
	// before it happens too -- IRCClient::part sends "PART #chan :reason" but
	// IRCClient::topic sends "TOPIC #chan :text" with the colon glued on.
	if i := strings.Index(line, " :"); i >= 0 {
		m.trailing = line[i+2:]
		m.hasTrail = true
		line = line[:i]
	}

	fields := strings.Fields(line)
	if len(fields) == 0 {
		return message{}, false
	}
	m.verb = strings.ToUpper(fields[0])
	m.params = fields[1:]
	return m, true
}

// param returns the nth parameter or "".
func (m message) param(n int) string {
	if n < len(m.params) {
		return m.params[n]
	}
	return ""
}

// text is the message body: the trailing parameter if there was one, otherwise
// whatever parameters are left from n onwards. Both spellings turn up --
// IRCClient::away sends "AWAY :msg" but IRCClient::quit sends a bare "QUIT".
func (m message) text(n int) string {
	if m.hasTrail {
		return m.trailing
	}
	if n < len(m.params) {
		return strings.Join(m.params[n:], " ")
	}
	return ""
}

//-----------------------------------------------------------------------------
// Outbound: what the client can actually hear
//-----------------------------------------------------------------------------

// dispatchable is IRCClient::dispatch (ChatGui.cs:1590-1701), transcribed.
//
// It is a closed switch: anything not in it falls through to `return false` and
// IRCClient::processLine prints the line as "(cmd:) ..." into the status pane.
// So this set is not a description of what we happen to send, it is the
// specification of what we are *allowed* to send, and chat_test.go asserts
// every line the hub emits against it.
//
// Note the absences. There is no 001-005 welcome burst, no 366 (end of NAMES),
// no 372/375 pair -- 372 is here but its opening 375 is not -- and no 302, 351
// or 391. A conventional ircd sends several of those on every connect and every
// one of them would be noise on the player's screen.
var dispatchable = map[string]bool{
	// Verbs.
	"PING": true, "PONG": true, "PRIVMSG": true, "JOIN": true, "NICK": true,
	"QUIT": true, "ERROR": true, "TOPIC": true, "PART": true, "KICK": true,
	"MODE": true, "AWAY": true, "NOTICE": true, "VERSION": true, "ACTION": true,
	"INSTANT": true, "INVITE": true,

	// Numerics.
	"301": true, "305": true, "306": true, "311": true, "312": true,
	"315": true, "317": true, "318": true, "319": true, "322": true,
	"323": true, "324": true, "331": true, "332": true, "341": true,
	"352": true, "353": true, "367": true, "368": true, "372": true,
	"376": true, "401": true, "422": true, "433": true, "444": true,
	"465": true, "468": true, "471": true, "473": true, "474": true,
	"475": true,

	// WON's own three.
	"CHALLENGE": true, "CHALRESP_REPLY": true, "DBMSG": true,
}

// Dispatchable reports whether the client has a handler for this line's verb.
// Exported for the test that walks every emitted line.
func Dispatchable(line string) bool {
	m, ok := parseMessage(line)
	if !ok {
		return false
	}
	return dispatchable[m.verb]
}

// The numerics this hub uses, named so the call sites read as intentions
// rather than as three-digit literals.
const (
	rplAway         = "301" // <me> <nick> :<message>
	rplUnaway       = "305"
	rplNowAway      = "306"
	rplWhoisUser    = "311" // <me> <nick> <user> <host> :<real name>
	rplEndOfWho     = "315"
	rplWhoisIdle    = "317" // <me> <nick> <seconds> <signon> :seconds idle
	rplEndOfWhois   = "318"
	rplList         = "322" // <me> <channel> <count> :<topic>
	rplListEnd      = "323"
	rplChannelModes = "324" // <me> <channel> <modes>
	rplNoTopic      = "331"
	rplTopic        = "332" // <me> <channel> :<topic>
	rplInviting     = "341" // <me> <nick> <channel>
	rplWhoReply     = "352" // <me> <chan> <user> <host> <server> <nick> H :0 <real>
	rplNameReply    = "353" // <me> = <channel> :<nick> <nick> ...
	rplEndOfBanList = "368" // <me> <channel> :End of ban list
	errNoSuchNick   = "401" // <me> <nick> :No such nick/channel
	errInviteOnly   = "473" // <me> <channel> :Cannot join channel
)
