// Package model holds the wire types.
//
// The JSON field names here are not a design choice -- they are the protocol
// the Tribes 2 client already speaks, recovered from TribesNext's published
// json_browser.phps and confirmed against the live server. They must not be
// renamed.
//
// Two quirks of that protocol are deliberate:
//
//   - Numbers that the client treats as text (ids, ranks, timestamps) are
//     strings, because the original PHP returned database columns unconverted
//     and the client's parser reads them with getField/TNBJsonStr.
//   - "online" is the exception: the live server returns it as a bare number,
//     which the client's TNBJsonBool handles. Reproduced so the two backends
//     are indistinguishable to the client.
package model

// Membership is a clan as it appears inside a user profile.
type Membership struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Rank   string `json:"rank"`
	Title  string `json:"title"`
	Tag    string `json:"tag"`
	Append string `json:"append"`
}

// User is the userview payload.
type User struct {
	GUID        string       `json:"guid"`
	Name        string       `json:"name"`
	Tag         string       `json:"tag"`
	Append      string       `json:"append"`
	Creation    string       `json:"creation"`
	Website     string       `json:"website"`
	Info        string       `json:"info"`
	Online      int          `json:"online"`
	Memberships []Membership `json:"memberships"`
}

// UserSearchHit is one entry of a usersearch array.
type UserSearchHit struct {
	GUID   string `json:"guid"`
	Name   string `json:"name"`
	Tag    string `json:"tag"`
	Append string `json:"append"`
}

// Member is a player as they appear in a clan roster.
type Member struct {
	GUID   string `json:"guid"`
	Name   string `json:"name"`
	Tag    string `json:"tag"`
	Append string `json:"append"`
	Rank   string `json:"rank"`
	Title  string `json:"title"`
	Online int    `json:"online"`
}

// Clan is the clanview payload.
type Clan struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Tag        string   `json:"tag"`
	Append     string   `json:"append"`
	Recruiting string   `json:"recruiting"`
	Website    string   `json:"website"`
	Info       string   `json:"info"`
	Creation   string   `json:"creation"`
	Picture    string   `json:"picture"`
	Active     string   `json:"active"`
	Members    []Member `json:"members"`
}

// ClanSearchHit is one entry of a clansearch array.
type ClanSearchHit struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// InviteParty is the sender or clan half of an invitation.
type InviteParty struct {
	GUID   string `json:"guid,omitempty"`
	ID     string `json:"id,omitempty"`
	Name   string `json:"name"`
	Tag    string `json:"tag"`
	Append string `json:"append"`
}

// Invite is one entry of the userinvites array.
type Invite struct {
	Sender InviteParty `json:"sender"`
	Clan   InviteParty `json:"clan"`
}

// HistoryEntry is one line of an audit trail.
type HistoryEntry struct {
	Time  string `json:"time"`
	Event string `json:"event"`
}

// Message is one mail item.
//
// The field names are the ones TNBrowser's TNBMailField tries first. That
// client accepts several spellings because the live server's shape could never
// be observed (its inbox is empty and sending is disabled), so this backend
// pins the canonical set.
type Message struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	FromGUID string `json:"fromguid"`
	To       string `json:"to"`
	ToGUID   string `json:"toguid"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Date     string `json:"date"`
	Unread   string `json:"unread"`
	Folder   string `json:"folder"`
}

// Person is a buddy, a blocked player, or someone a clan has invited.
//
// Since and Hits carry the second column the stock screens showed beside the
// name -- SINCE on the buddy list, INVITED on a clan's invitations, and
// "# Blocked Emails" on the block dialog. They are additions to the protocol
// rather than renames: TribesNext serves the login and nothing else now, so
// nothing else reads these payloads.
type Person struct {
	GUID   string `json:"guid"`
	Name   string `json:"name"`
	Tag    string `json:"tag"`
	Append string `json:"append"`
	Online int    `json:"online"`
	Since  string `json:"since"`
	Hits   string `json:"hits"`
}

//-----------------------------------------------------------------------------
// Website types
//
// Everything above is the client's protocol and its field names are fixed.
// Everything below is the public website's own, read by React and by nothing
// else, so it is free to change and free to use real types: a boolean is a
// boolean here rather than the "1"/"0" string PHP used to return.
//-----------------------------------------------------------------------------

// DirectoryWarrior is one row of the website's warrior directory.
type DirectoryWarrior struct {
	GUID     string `json:"guid"`
	Name     string `json:"name"`
	Tag      string `json:"tag"`
	Append   bool   `json:"append"`
	Online   bool   `json:"online"`
	Tribes   int    `json:"tribes"`
	Created  int64  `json:"created"`
	LastSeen int64  `json:"lastSeen"`
}

// DirectoryTribe is one row of the website's tribe directory.
type DirectoryTribe struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Tag        string `json:"tag"`
	Append     bool   `json:"append"`
	Recruiting bool   `json:"recruiting"`
	Members    int    `json:"members"`
	Online     int    `json:"online"`
	Created    int64  `json:"created"`
}

// Counts is the landing page's summary of the community.
type Counts struct {
	Warriors int `json:"warriors"`
	Tribes   int `json:"tribes"`
	Online   int `json:"online"`
}

// Status is the reply shape for every mutating method.
type Status struct {
	Status string `json:"status"`
	Msg    string `json:"msg,omitempty"`
}

// OK is the success reply.
func OK() Status { return Status{Status: "success"} }

// Bool renders a boolean the way the protocol carries them: "1" or "0".
func Bool(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
