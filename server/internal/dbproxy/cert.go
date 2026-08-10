package dbproxy

import (
	"strconv"
	"strings"
)

// The certificate WONGetAuthInfo() hands to the shipped scripts.
//
// Not one of WON's ordinals: the real client had this inside the process,
// produced by a login that never reached the database proxy at all. It is here
// because the identity has to come from somewhere and this server is the only
// thing that knows both who TribesNext says you are and which tribes you are
// in.
//
// The layout is pinned by the scripts that read it, and the reading is worth
// stating because it is what makes record 0 four fields rather than four
// coincidences:
//
//	record 0    name TAB tag TAB append TAB guid
//	record 1    <tribe count>
//	record 2+n  name TAB tag TAB append TAB tribeId TAB adminLevel TAB title
//
// Field 0 of record 0 is the warrior name (GameGui.cs:1324) and field 3 the
// GUID (webemail.cs:1276, and LaunchLanGui.cs:342). webbrowser.cs:271 compares
// that field against field 3 of a row quad -- so record 0 IS a quad, in the
// same layout every row schema uses. Record 1 field 0 is the tribe count
// (webbrowser.cs:101), and webbrowser.cs:1909-1926 pins the tribe records by
// rendering your own tribe list straight out of them: field 3 the tribe id,
// field 4 the admin level, field 5 the title.
//
// Downstream it reaches the filesystem: the shell writes its mail cache to
// webcache/<guid>/email1 from $EmailCachePath (webemail.cs:15), so a wrong
// field 3 does not merely misrender, it moves where mail is cached.
func Certificate(c *Ctx) (string, error) {
	self, err := c.Store.Quad(c.Ctx, c.GUID)
	if err != nil {
		return "", err
	}

	tribes, err := c.Store.WarriorTribes(c.Ctx, c.GUID)
	if err != nil {
		return "", err
	}

	records := []string{
		strings.Join([]string{self.Name, self.Tag, boolField(self.Append), self.GUID}, "\t"),
		strconv.Itoa(len(tribes)),
	}

	for _, t := range tribes {
		p, err := c.Store.TribeProfile(c.Ctx, t.ID)
		if err != nil {
			return "", err
		}
		records = append(records, strings.Join([]string{
			p.Name,
			p.Tag,
			boolField(p.Append),
			strconv.FormatInt(t.ID, 10),
			strconv.Itoa(t.Rank),
			t.Title,
		}, "\t"))
	}

	// Records are newline-separated because that is what getRecord splits on.
	return strings.Join(records, "\n"), nil
}
