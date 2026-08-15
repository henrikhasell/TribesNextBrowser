// Package model holds the shapes that cross a boundary: from internal/store to
// internal/dbproxy, which renders them into ordinal rows, and to
// internal/api/site.go, which marshals them for the website.
//
// Nothing here is a wire protocol. The game speaks tab-joined fields inside
// dbproxy.Answer and never sees a struct from this package; the only JSON these
// turn into is read by our own React app. The field names and types are
// therefore ordinary choices, free to change with their callers.
//
// They did not use to be. This package was once the literal shape of a
// method-and-JSON API served to a hand-built set of screens, which is why the
// older types below still carry stringly-typed ids, ranks and timestamps. That
// is inertia, not a requirement -- the newer website types at the bottom use
// real ones, and the rest can follow whenever a caller wants them to.
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

// User is a warrior profile: the account, the tag they wear, and every tribe
// they belong to.
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

// Clan is a tribe profile and its full roster.
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

// HistoryEntry is one line of an audit trail.
type HistoryEntry struct {
	Time  string `json:"time"`
	Event string `json:"event"`
}

// Person is a buddy or a blocked player.
//
// Since and Hits carry the second column the stock screens show beside the
// name -- SINCE on the buddy list, and "# Blocked Emails" on the block dialog.
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
// Written for the React app rather than adapted from anything, which is why
// these use real booleans and integers where the older types above still carry
// strings. See the package comment.
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

// Bool renders a boolean as the "1" or "0" the older types above carry, and
// that dbproxy writes into an ordinal row.
func Bool(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
