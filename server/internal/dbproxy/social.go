package dbproxy

import "strings"

// The six ordinals both rendering panes issue.
//
// Each has two consumers of different depth, and the shallower one is why the
// row schemas took reading to pin: the mail address book reads field 0 of a
// buddy row and field 1 of a search row and stops there, while the browser
// reads the whole thing. One procedure, two readers -- so a schema that
// satisfies the address book can still be wrong.

func init() {
	on(Array, "3", searchWarriors)
	on(Array, "5", getBuddyList)
	on(Array, "6", getTribeMembers)

	on(Scalar, "10", addBuddy)
	on(Scalar, "11", dropBuddy)
	on(Scalar, "69", getOnlineStatus)
}

// array 3. Field 0 is padding -- the address book reads field 1 and the browser
// reads fields 1.. -- so the id goes there for anything that wants it and the
// name stays where both readers look.
func searchWarriors(c *Ctx, args string) (Answer, error) {
	q, start, count := searchArgs(args)
	hits, err := c.Store.SearchWarriors(c.Ctx, q, start, count)
	if err != nil {
		return Answer{}, err
	}

	rows := make([]string, 0, len(hits))
	for _, h := range hits {
		rows = append(rows, tab(h.GUID, h.Name, h.Tag, h.Append))
	}
	return okRows(rows), nil
}

// array 5. Called with no argument from the mail address book and with a
// warrior name from the browser, so an empty argument means "mine".
//
// Field 5 carries an online flag the client no longer uses: the
// setRowStyleById it fed is commented out at webbrowser.cs:1847. It is sent
// anyway, because the field index after it is not free to move.
func getBuddyList(c *Ctx, args string) (Answer, error) {
	guid := c.GUID
	if name := field(args, 0); name != "" {
		found, err := c.Store.FindUser(c.Ctx, name)
		if err != nil {
			return notFound(err, "There is no warrior by that name.")
		}
		guid = found
	}

	people, err := c.Store.BuddyList(c.Ctx, guid)
	if err != nil {
		return Answer{}, err
	}

	rows := make([]string, 0, len(people))
	for _, p := range people {
		rows = append(rows, tab(p.Name, p.Tag, p.Append, p.GUID,
			date(atoi(p.Since)), p.Online))
	}
	return okRows(rows), nil
}

// array 6, the tribe roster.
//
// The three columns the ROSTER button adds before it queries -- MEMBER, TITLE,
// RNK at webbrowser.cs:1580-1582 -- line up with fields 0, 4 and 5, which is
// what identifies this ordinal as the roster rather than array 10.
//
// Field 8 is canEditKick: whether the CALLER may administer the row, not
// anything about the member. The client draws the roster's right-click menu
// from it, so getting it wrong either hides controls an officer should have or
// offers a recruit controls the server will refuse.
func getTribeMembers(c *Ctx, args string) (Answer, error) {
	id, err := c.Store.FindClan(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	members, err := c.Store.TribeMembers(c.Ctx, id)
	if err != nil {
		return Answer{}, err
	}
	canAdmin, err := c.Store.CanInvite(c.Ctx, c.GUID, id)
	if err != nil {
		return Answer{}, err
	}

	rows := make([]string, 0, len(members))
	for _, m := range members {
		rows = append(rows, tab(
			m.Q.Name, m.Q.Tag, m.Q.Append, m.Q.GUID,
			m.Title, m.Rank, date(m.Joined), "",
			canAdmin && m.Q.GUID != c.GUID,
			m.Online,
		))
	}
	return okRows(rows), nil
}

func addBuddy(c *Ctx, args string) (Answer, error) {
	target, err := c.Store.FindUser(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no warrior by that name.")
	}
	if err := c.Store.BuddyAdd(c.Ctx, c.GUID, target); err != nil {
		return userError(err)
	}
	return Answer{
		Status: okStatus("Added to your buddy list."),
		Result: "1",
		Rows:   []string{},
	}, nil
}

func dropBuddy(c *Ctx, args string) (Answer, error) {
	target, err := c.Store.FindUser(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no warrior by that name.")
	}
	if err := c.Store.BuddyRemove(c.Ctx, c.GUID, target); err != nil {
		return userError(err)
	}
	return Answer{
		Status: okStatus("Removed from your buddy list."),
		Result: "1",
		Rows:   []string{},
	}, nil
}

// scalar 69. The reply is a fixed-width bitmap, not a field list: the handler
// indexes resultString by character position and calls setRowStyle(i, !char)
// for each (webbrowser.cs:879).
//
// The shipped handler never actually runs that loop -- it reads an undefined
// %resultString while its parameter is named %resultStatus, so the variable is
// empty and the for loop's bound is zero. That is a defect in the client, not a
// licence to answer badly: four call sites issue this and a correct answer
// costs nothing, while a wrong-width one would break the moment somebody fixes
// the typo.
func getOnlineStatus(c *Ctx, args string) (Answer, error) {
	ids := fields(args)
	flags, err := c.Store.OnlineFor(c.Ctx, ids)
	if err != nil {
		return Answer{}, err
	}

	var b strings.Builder
	for _, on := range flags {
		b.WriteString(boolField(on))
	}
	return okResult(b.String()), nil
}
