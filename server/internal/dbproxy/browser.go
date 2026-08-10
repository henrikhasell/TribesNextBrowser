package dbproxy

import (
	"errors"
	"strconv"
	"strings"

	"github.com/henrik/tnbrowser-server/internal/store"
)

// The Tribe & Warrior Browser: webbrowser.cs, TribeAndWarriorBrowserGui.
//
// One of the two panes a retail install can actually render, and the larger of
// them: 26 ordinals across tribe profiles, rosters, invitations, warrior
// profiles, search and every administrative write.

// What a warrior or a tribe with no picture is given.
//
// The obvious answer -- send nothing -- is wrong. $PlayerGfx, the fallback
// PlayerPane::onWake uses when a profile carries no graphic
// (webbrowser.cs:1646), is read three times and assigned by no shipped script,
// so an empty field renders a blank picture permanently: TWBTabView::onSelect
// reinstates the same empty global whenever you switch warrior tabs (:1025),
// blanking the control again even after a good profile has filled it.
// $TribeGfx (:1300, :1366) is unassigned in exactly the same way.
//
// These two paths are not invented: they are what the shipped dialogs set their
// own preview controls to, at WarriorPropertiesDlg.gui:348 and
// TribePropertiesDlg.gui:618. So they are the client's own idea of a default,
// which is a better answer than anything this server could choose.
//
// The path must be non-empty and must start with a letter. Field 9 of ordinal
// 23 is ambiguous -- read as getVisitorOptions it is a tribe COUNT -- and
// Torque coerces a bitmap path to 0, which is what makes the graphic reading
// safe. A name beginning with a digit would coerce to that digit and the
// options pane would try to read that many tribes out of the fields after it.
const (
	defaultPlayerGfx = "texticons/twb/twb_Missilelauncher.jpg"
	defaultTribeGfx  = "texticons/twb/twb_Laserrifle.jpg"
)

// graphicOr keeps a graphic field safe for both readings of ordinal 23.
func graphicOr(path, fallback string) string {
	if path == "" || (path[0] >= '0' && path[0] <= '9') {
		return fallback
	}
	return path
}

func init() {
	on(Array, "4", searchTribes)
	on(Array, "10", getTribeNews)
	on(Array, "11", getTribeInvites)
	on(Array, "12", getWarriorHistory)
	on(Array, "13", getWarriorTribeList)

	on(Scalar, "15", setTribeDescription)
	on(Scalar, "16", createTribe)
	on(Scalar, "17", setWarriorDescription)
	on(Scalar, "18", deleteTribe)
	on(Scalar, "19", kickMember)
	on(Scalar, "20", toggleTribeFlag)
	on(Scalar, "21", setMemberProfile)
	on(Scalar, "22", getTribeProfile)
	on(Scalar, "23", getWarriorProfile)
	on(Scalar, "24", leaveTribe)
	on(Scalar, "25", setPrimaryTribe)
	on(Scalar, "26", clearBuddy)
	on(Scalar, "27", inviteToTribe)
	on(Scalar, "28", answerInvitation)
	on(Scalar, "29", setTribeGraphic)
	on(Scalar, "30", setTribeTag)
	on(Scalar, "31", setPlayerGraphic)
	on(Scalar, "32", setPlayerUrl)
	on(Scalar, "33", setPlayerName)
	on(Scalar, "34", requestInvite)
	on(Scalar, "63", postAdminAction)
}

//-----------------------------------------------------------------------------
// Profiles
//-----------------------------------------------------------------------------

// scalar 22. The payload is the STATUS, not a row: GetProfileHdr is handed
// getFields(%status,2) and reads six fields off it (webbrowser.cs:1355). The
// description goes in the resultString.
func getTribeProfile(c *Ctx, args string) (Answer, error) {
	id, err := c.Store.FindClan(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	p, err := c.Store.TribeProfile(c.Ctx, id)
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}

	status := okStatus(
		"The profile for "+p.Name+" follows.",
		strconv.FormatInt(p.ID, 10),
		p.Name,
		p.Tag,
		boolField(p.Append),
		boolField(p.Recruiting),
		graphicOr(p.Graphic, defaultTribeGfx),
	)
	return okWith(status, p.Info), nil
}

// scalar 23, for both PROFILE and OPTIONS. See the ordinal table for why field
// 9 carries the graphic rather than the tribe count the other reading wants.
func getWarriorProfile(c *Ctx, args string) (Answer, error) {
	guid, err := c.Store.FindUser(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no warrior by that name.")
	}
	p, err := c.Store.WarriorProfile(c.Ctx, guid)
	if err != nil {
		return notFound(err, "There is no warrior by that name.")
	}

	status := okStatus(
		"The profile for "+p.Q.Name+" follows.",
		p.Q.Name,
		p.Q.Tag,
		boolField(p.Q.Append),
		p.Q.GUID,
		date(p.Registered),
		boolField(p.Online),
		p.URL,
		graphicOr(p.Graphic, defaultPlayerGfx),
	)
	return okWith(status, p.Info), nil
}

//-----------------------------------------------------------------------------
// Lists
//-----------------------------------------------------------------------------

// array 4. getTribeName (webbrowser.cs:316) renders fields 1 and 2 as
// "<name> - <tag>" and keeps field 1 as the tab name, so field 1 must be the
// bare name even though the row is a search hit.
func searchTribes(c *Ctx, args string) (Answer, error) {
	q, start, count := searchArgs(args)
	hits, err := c.Store.SearchTribes(c.Ctx, q, start, count)
	if err != nil {
		return Answer{}, err
	}

	rows := make([]string, 0, len(hits))
	for _, h := range hits {
		rows = append(rows, tab(h.ID, h.Name, h.Tag))
	}
	return okRows(rows), nil
}

// array 11. Field 10 is "isOwned" -- whether the row belongs to the caller's
// side of the transaction -- and field 11 an online flag the client reads
// inverted, MemberList.setRowStyleById(id, !online) at webbrowser.cs:1528.
func getTribeInvites(c *Ctx, args string) (Answer, error) {
	id, err := c.Store.FindClan(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	invites, err := c.Store.TribeInvites(c.Ctx, id)
	if err != nil {
		return Answer{}, err
	}

	rows := make([]string, 0, len(invites))
	for _, v := range invites {
		rows = append(rows, tab(
			v.ID, date(v.Created),
			v.From.Name, v.From.Tag, v.From.Append, v.From.GUID,
			v.To.Name, v.To.Tag, v.To.Append, v.To.GUID,
			v.From.GUID == c.GUID,
			v.Online,
		))
	}
	return okRows(rows), nil
}

// array 12. The only ordinal whose rows are not field-structured: each row is
// appended verbatim to a GuiMLTextCtrl (webbrowser.cs:1821), so it is a line of
// display text and may carry Torque markup.
func getWarriorHistory(c *Ctx, args string) (Answer, error) {
	guid, err := c.Store.FindUser(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no warrior by that name.")
	}
	entries, err := c.Store.UserHistory(c.Ctx, guid)
	if err != nil {
		return Answer{}, err
	}

	rows := make([]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, date(atoi(e.Time))+"  "+linkRefs(e.Event))
	}
	return okRows(rows), nil
}

// linkRefs turns the store's {kind:name} markers into links the client follows.
//
// Which is worth doing here and nowhere else: this is the one ordinal whose
// rows are display text rather than fields, so it is the one place a link can
// be written at all. GuiMLTextCtrl::onURL (webbrowser.cs:1063) reads the verb
// from field 0 of the URL and the argument from field 1, and it splits on TAB
// -- a real tab, not the newline mlLink writes, because a history row is
// appended verbatim instead of being rejoined out of fields.
//
// A marker whose kind is not one of these is left exactly as it stands, braces
// and all. Silently dropping it would hide the fact that something wrote a
// marker nothing here knows about.
func linkRefs(event string) string {
	var b strings.Builder
	for {
		open := strings.IndexByte(event, '{')
		if open < 0 {
			break
		}
		close := strings.IndexByte(event[open:], '}')
		colon := strings.IndexByte(event[open:], ':')
		if close < 0 || colon < 0 || colon > close {
			break
		}
		kind := event[open+1 : open+colon]
		name := event[open+colon+1 : open+close]

		verb := ""
		switch kind {
		case store.RefClan:
			verb = "tribe"
		case store.RefWarrior:
			verb = "player"
		case store.RefWeb:
			verb = "wwwlink"
		}

		b.WriteString(event[:open])
		if verb == "" {
			b.WriteString(event[open : open+close+1])
		} else {
			b.WriteString("<a:" + verb + "\t" + name + ">" + name + "</a>")
		}
		event = event[open+close+1:]
	}
	b.WriteString(event)
	return b.String()
}

// array 13. Only ever issued for OTHER warriors -- viewing your own tribe list
// renders straight out of the certificate and sends no query at all
// (webbrowser.cs:1909-1927).
func getWarriorTribeList(c *Ctx, args string) (Answer, error) {
	guid, err := c.Store.FindUser(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no warrior by that name.")
	}
	tribes, err := c.Store.WarriorTribes(c.Ctx, guid)
	if err != nil {
		return Answer{}, err
	}

	rows := make([]string, 0, len(tribes))
	for _, t := range tribes {
		rows = append(rows, tab(t.Name, "", t.ID, t.Rank,
			t.Rank >= store.RankOfficer, t.Title))
	}
	return okRows(rows), nil
}

// array 10. Served so the framing is exercised; the pane discards it. The
// handler sets state = "done" unconditionally (webbrowser.cs:1410) and the
// branch that would have consumed these rows is commented out below it, so
// tribe news was cut before release and nothing renders this.
func getTribeNews(c *Ctx, args string) (Answer, error) {
	if _, err := c.Store.FindClan(c.Ctx, field(args, 0)); err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	return okRows(nil), nil
}

//-----------------------------------------------------------------------------
// Tribe writes
//-----------------------------------------------------------------------------

// scalar 15. The line count in field 1 is the client's own, and the
// description follows it; taking the tail rather than field 2 alone keeps a
// multi-line description intact.
func setTribeDescription(c *Ctx, args string) (Answer, error) {
	id, err := c.Store.FindClan(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	p, err := c.Store.TribeProfile(c.Ctx, id)
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	if err := c.Store.SetClanInfo(c.Ctx, c.GUID, id, fieldsFrom(args, 2)); err != nil {
		return userError(err)
	}
	return okMessage("The description for " + p.Name + " has been updated."), nil
}

// scalar 16. Six fields, not three: the create dialog sends the recruiting flag
// and the description as well (webbrowser.cs:166-171), the description behind
// its own line count exactly as ordinal 15 sends it. Reading only the first
// three founded every UI-created tribe with an empty description and
// recruitment closed.
func createTribe(c *Ctx, args string) (Answer, error) {
	name := field(args, 0)
	tag := field(args, 1)
	appendTag := truthy(field(args, 2))
	recruiting := truthy(field(args, 3))
	info := fieldsFrom(args, 5)

	if err := c.Store.CreateClan(c.Ctx, c.GUID, name, tag, appendTag,
		recruiting, info); err != nil {
		return userError(err)
	}
	return okResult("The tribe "+name+" has been founded, with you as its leader.", name), nil
}

func deleteTribe(c *Ctx, args string) (Answer, error) {
	id, err := c.Store.FindClan(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	p, err := c.Store.TribeProfile(c.Ctx, id)
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}

	// Non-empty only on the authorisation that actually disbands the tribe.
	// The others change nothing a member could notice, so there is nothing to
	// tell them yet.
	told, err := c.Store.Disband(c.Ctx, c.GUID, id, true)
	if err != nil {
		return userError(err)
	}
	c.notifyAll(told, "Tribe disbanded: "+p.Name,
		c.Name+" has disbanded "+p.Name+". You are no longer a member of it.")

	if len(told) > 0 {
		return okMessage("The tribe " + p.Name + " has been disbanded."), nil
	}
	return okMessage("Your authorisation to disband " + p.Name +
		" has been recorded. It disbands once every leader has authorised it."), nil
}

// scalar 19. Named from LinkKickMember and the state it sets, not from the
// DatabaseQuery line -- an earlier reading of this ordinal had it as the invite.
func kickMember(c *Ctx, args string) (Answer, error) {
	target, err := c.Store.FindUser(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no warrior by that name.")
	}
	id, err := c.Store.FindClan(c.Ctx, field(args, 1))
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	p, err := c.Store.TribeProfile(c.Ctx, id)
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	if err := c.Store.Kick(c.Ctx, c.GUID, id, target); err != nil {
		return userError(err)
	}

	// Nothing else would say so. A kicked warrior's next sight of it is the
	// tribe tab quietly missing from a pane they may not open for days.
	c.notify(target, "Removed from "+p.Name,
		c.Name+" has removed you from "+p.Name+".")

	who, err := c.Store.Quad(c.Ctx, target)
	if err != nil {
		return Answer{}, err
	}
	return okMessage("Player " + who.Name + " has been kicked from " + p.Name + "."), nil
}

// scalar 20. "Recruiting" and "Appending" are the only two words any of the
// four call sites passes.
func toggleTribeFlag(c *Ctx, args string) (Answer, error) {
	flag := field(args, 0)
	id, err := c.Store.FindClan(c.Ctx, field(args, 1))
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	on := truthy(field(args, 2))

	p, err := c.Store.TribeProfile(c.Ctx, id)
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}

	var msg string
	switch strings.ToLower(flag) {
	case "recruiting":
		if err := c.Store.SetClanRecruiting(c.Ctx, c.GUID, id, on); err != nil {
			return userError(err)
		}
		msg = p.Name + " is no longer recruiting."
		if on {
			msg = p.Name + " is now recruiting."
		}
	case "appending":
		if err := c.Store.SetClanTag(c.Ctx, c.GUID, id, p.Tag, on); err != nil {
			return userError(err)
		}
		msg = "The tag " + p.Tag + " now goes before the name of every " +
			p.Name + " member."
		if on {
			msg = "The tag " + p.Tag + " now goes after the name of every " +
				p.Name + " member."
		}
	default:
		return fail("%q is not a tribe flag this server knows.", flag), nil
	}
	return okMessage(msg), nil
}

// scalar 21, the TribeAdminMemberDlg write: a member's title and admin level,
// in that order. webbrowser.cs:643 sends
//
//	vTribe TAB vPlayer TAB %title TAB vPerm
//
// -- %title read from E_Title one line above, vPerm the admin level TAM_OnAction
// stashed (:663). Reading the two the other way round does not just swap them:
// the level is stored as the title, and atoi of a title like "Warlord" is 0, so
// renaming a member demoted them to recruit and the refusal band never fired
// because 0 is a legal rank.
func setMemberProfile(c *Ctx, args string) (Answer, error) {
	id, err := c.Store.FindClan(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	target, err := c.Store.FindUser(c.Ctx, field(args, 1))
	if err != nil {
		return notFound(err, "There is no warrior by that name.")
	}
	title := field(args, 2)
	rank := int(atoi(field(args, 3)))
	p, err := c.Store.TribeProfile(c.Ctx, id)
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}

	before, err := c.Store.SetRank(c.Ctx, c.GUID, id, target, rank, title)
	if err != nil {
		return userError(err)
	}

	// Only when the rank moved. The same dialog sends this ordinal for a title
	// edit alone, and mailing a member every time somebody tidies their title
	// would make the ones that matter easy to miss.
	if rank != before {
		verb := "promoted"
		if rank < before {
			verb = "demoted"
		}
		c.notify(target, "Rank changed in "+p.Name,
			c.Name+" has "+verb+" you to "+title+" in "+p.Name+".")
	}

	who, err := c.Store.Quad(c.Ctx, target)
	if err != nil {
		return Answer{}, err
	}
	if rank == before {
		return okMessage(who.Name + " is now titled " + title + " in " + p.Name + "."), nil
	}
	return okMessage("Player " + who.Name + " is now " + title + " in " + p.Name +
		", at admin level " + itoa(rank) + "."), nil
}

func setTribeGraphic(c *Ctx, args string) (Answer, error) {
	id, err := c.Store.FindClan(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	p, err := c.Store.TribeProfile(c.Ctx, id)
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	if err := c.Store.SetClanPicture(c.Ctx, c.GUID, id, field(args, 1)); err != nil {
		return userError(err)
	}
	return okMessage("The graphic for " + p.Name + " has been updated."), nil
}

func setTribeTag(c *Ctx, args string) (Answer, error) {
	id, err := c.Store.FindClan(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	p, err := c.Store.TribeProfile(c.Ctx, id)
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	tag := field(args, 1)
	if err := c.Store.SetClanTag(c.Ctx, c.GUID, id, tag, p.Append); err != nil {
		return userError(err)
	}
	return okMessage("The tag for " + p.Name + " is now " + tag + "."), nil
}

//-----------------------------------------------------------------------------
// Membership
//-----------------------------------------------------------------------------

func leaveTribe(c *Ctx, args string) (Answer, error) {
	id, err := c.Store.FindClan(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	p, err := c.Store.TribeProfile(c.Ctx, id)
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}

	admins, err := c.Store.LeaveClan(c.Ctx, c.GUID, id)
	if err != nil {
		return userError(err)
	}
	c.notifyAll(admins, "Member left "+p.Name,
		c.Name+" has left "+p.Name+".")

	return okMessage("You have left " + p.Name + "."), nil
}

// scalar 25. The resultString is the tribe name, which the client echoes into
// "<name> has been flagged as your primary tribe." (webbrowser.cs:1719). One
// ordinal serves both set and clear -- which branch runs is decided entirely
// client-side by the state LinkMakePrimary was called with.
func setPrimaryTribe(c *Ctx, args string) (Answer, error) {
	arg := field(args, 0)
	if arg == "" || arg == "0" || arg == "-1" {
		if err := c.Store.SetActiveClan(c.Ctx, c.GUID, -1); err != nil {
			return userError(err)
		}
		return okResult("You are now a free agent, wearing no tribe's tag.", ""), nil
	}

	id, err := c.Store.FindClan(c.Ctx, arg)
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	p, err := c.Store.TribeProfile(c.Ctx, id)
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	if err := c.Store.SetActiveClan(c.Ctx, c.GUID, id); err != nil {
		return userError(err)
	}
	return okResult(p.Name+" is now your primary tribe.", p.Name), nil
}

// scalar 27. Recording the invitation is not enough to make it answerable: no
// client query lists a player's own invitations, so the invited warrior is
// mailed a body carrying the accept and reject links. PlayerPane expects
// exactly that -- it deletes the open message and re-checks mail once the
// answer lands (webbrowser.cs:1726-1733).
func inviteToTribe(c *Ctx, args string) (Answer, error) {
	id, err := c.Store.FindClan(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	target, err := c.Store.FindUser(c.Ctx, field(args, 1))
	if err != nil {
		return notFound(err, "There is no warrior by that name.")
	}
	if err := c.Store.Invite(c.Ctx, c.GUID, id, target); err != nil {
		return userError(err)
	}

	p, err := c.Store.TribeProfile(c.Ctx, id)
	if err != nil {
		return Answer{}, err
	}
	who, err := c.Store.Quad(c.Ctx, target)
	if err != nil {
		return Answer{}, err
	}

	body := strings.Join([]string{
		c.Name + " has invited you to join " + p.Name + ".",
		"",
		mlLink("Accept", "acceptinvite", p.Name, who.Name) + "    " +
			mlLink("Reject", "rejectinvite", p.Name, who.Name),
	}, "\n")

	if err := c.Store.Deliver(c.Ctx, target, c.GUID, c.Name,
		"Tribe invitation: "+p.Name, body); err != nil {
		return userError(err)
	}
	return okResult("Player "+who.Name+" has been invited to join "+p.Name+
		". The invitation is in their mail.", p.Name), nil
}

// scalar 28: accept, reject or cancel.
//
// The wire cannot tell an invitation from a join request -- both are answered
// with the same three verbs through the same ordinal -- so who is asking
// decides. Field 2 naming somebody other than the caller, with the caller able
// to invite on the tribe's behalf, is an admin answering a request; anything
// else is a warrior answering their own invitation.
func answerInvitation(c *Ctx, args string) (Answer, error) {
	verb := strings.ToLower(field(args, 0))
	id, err := c.Store.FindClan(c.Ctx, field(args, 1))
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	p, err := c.Store.TribeProfile(c.Ctx, id)
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}

	subject := c.GUID
	if name := field(args, 2); name != "" {
		if guid, err := c.Store.FindUser(c.Ctx, name); err == nil {
			subject = guid
		}
	}

	if verb == "cancel" {
		if _, err := c.Store.CancelInvite(c.Ctx, c.GUID, id, subject); err != nil {
			return userError(err)
		}
		return okResult("That invitation to "+p.Name+" has been withdrawn.", p.Name), nil
	}

	asAdmin := false
	if subject != c.GUID {
		if asAdmin, err = c.Store.CanInvite(c.Ctx, c.GUID, id); err != nil {
			return Answer{}, err
		}
	}

	// Whichever way round it goes, the party who raised the invitation is told
	// how it ended: the client has no query that lists answered invitations, so
	// to the side that did not answer, an acceptance and a rejection look
	// identical -- the row is simply gone from the pane that showed it.
	var tell, mailSubject, mailBody, said string

	switch {
	case verb == "accept" && asAdmin:
		if err = c.Store.AdmitRequester(c.Ctx, c.GUID, id, subject); err == nil {
			tell = subject
			mailSubject = "Join request accepted: " + p.Name
			mailBody = c.Name + " has accepted your request to join " + p.Name + "."
			said = "That warrior's request to join " + p.Name + " has been accepted."
		}
	case verb == "reject" && asAdmin:
		var removed bool
		// Only when a request was actually withdrawn. The ordinal answers
		// success for a reject that matched nothing, and that is not an
		// outcome to mail anyone about.
		if removed, err = c.Store.CancelInvite(c.Ctx, c.GUID, id, subject); err == nil && removed {
			tell = subject
			mailSubject = "Join request declined: " + p.Name
			mailBody = c.Name + " has declined your request to join " + p.Name + "."
			said = "That warrior's request to join " + p.Name + " has been declined."
		}
	case verb == "accept":
		if tell, err = c.Store.AcceptInvite(c.Ctx, c.GUID, id); err == nil {
			mailSubject = "Invitation accepted: " + p.Name
			mailBody = c.Name + " has accepted your invitation to join " + p.Name + "."
			said = "You have joined " + p.Name + "."
		}
	case verb == "reject":
		if tell, err = c.Store.RejectInvite(c.Ctx, c.GUID, id); err == nil {
			mailSubject = "Invitation declined: " + p.Name
			mailBody = c.Name + " has declined your invitation to join " + p.Name + "."
			said = "You have declined the invitation to join " + p.Name + "."
		}
	default:
		return fail("%q is not something that can be done with an invitation.", verb), nil
	}
	if err != nil {
		return userError(err)
	}
	c.notify(tell, mailSubject, mailBody)

	return okResult(said, p.Name), nil
}

// scalar 34. Status field 1 goes straight into a MessageBoxOK
// (webbrowser.cs:1446), so every branch here is a sentence rather than a code.
// The tribe's admins are mailed, because the request is otherwise unanswerable:
// array 11 is tribe-scoped and its tab is admin-only.
func requestInvite(c *Ctx, args string) (Answer, error) {
	id, err := c.Store.FindClan(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no tribe by that name.")
	}
	p, err := c.Store.TribeProfile(c.Ctx, id)
	if err != nil {
		return Answer{}, err
	}

	admins, err := c.Store.RequestInvite(c.Ctx, c.GUID, id)
	if err != nil {
		return userError(err)
	}

	body := strings.Join([]string{
		c.Name + " has asked to join " + p.Name + ".",
		"",
		mlLink("Accept", "acceptinvite", p.Name, c.Name) + "    " +
			mlLink("Reject", "rejectinvite", p.Name, c.Name),
	}, "\n")

	for _, admin := range admins {
		if err := c.Store.Deliver(c.Ctx, admin, c.GUID, c.Name,
			"Join request: "+p.Name, body); err != nil {
			return Answer{}, err
		}
	}

	return okResult("Your request to join "+p.Name+" has been sent to its "+
		"administrators.", p.Name), nil
}

//-----------------------------------------------------------------------------
// Warrior writes
//-----------------------------------------------------------------------------

// scalar 17. Both call sites are the warrior description; the tribe branch of
// the same dialog sends ordinal 15 instead. The literal NONE clears it
// (webbrowser.cs:2553, doClearDescription).
func setWarriorDescription(c *Ctx, args string) (Answer, error) {
	text := args
	if text == "NONE" {
		text = ""
	}
	if err := c.Store.SetInfo(c.Ctx, c.GUID, text); err != nil {
		return userError(err)
	}
	if text == "" {
		return okMessage("The description on " + c.Name + " has been cleared."), nil
	}
	return okMessage("The description on " + c.Name + " has been updated."), nil
}

func setPlayerGraphic(c *Ctx, args string) (Answer, error) {
	if err := c.Store.SetGraphic(c.Ctx, c.GUID, field(args, 0)); err != nil {
		return userError(err)
	}
	return okMessage("The graphic on " + c.Name + " has been updated."), nil
}

func setPlayerUrl(c *Ctx, args string) (Answer, error) {
	url := field(args, 0)
	if err := c.Store.SetWebsite(c.Ctx, c.GUID, url); err != nil {
		return userError(err)
	}
	if url == "" {
		return okMessage("The web address on " + c.Name + " has been cleared."), nil
	}
	return okMessage("The web address on " + c.Name + " is now " + url + "."), nil
}

// scalar 33. Refused, and the refusal is the honest answer rather than an
// omission: the account name belongs to the TribesNext account this server
// authenticates against. It only caches the name it learns while verifying a
// session and refreshes it on every request, so a rename here would be undone
// within the minute -- succeeding and silently reverting is worse than saying
// no.
func setPlayerName(c *Ctx, args string) (Answer, error) {
	return fail("Your warrior name belongs to your TribesNext account and " +
		"must be changed there."), nil
}

func clearBuddy(c *Ctx, args string) (Answer, error) {
	if err := c.Store.BuddyClear(c.Ctx, c.GUID); err != nil {
		return userError(err)
	}
	return okMessage("Your buddy list has been cleared."), nil
}

// scalar 63. WON kept its staff in one tribe and checked membership of it
// (webbrowser.cs:409, :2248), which is the gate applied here. The eight-way
// selector in field 0 is named nowhere in the shipped scripts, so the action is
// recorded rather than interpreted -- guessing at a meaning would be worse than
// keeping the evidence.
func postAdminAction(c *Ctx, args string) (Answer, error) {
	staff, err := c.Store.IsStaff(c.Ctx, c.GUID)
	if err != nil {
		return Answer{}, err
	}
	if !staff {
		return fail("You do not have moderator privileges."), nil
	}
	if err := c.Store.LogAdminAction(c.Ctx, c.GUID, 63,
		int(atoi(field(args, 0))), args); err != nil {
		return Answer{}, err
	}
	return okMessage("That moderator action has been recorded against " +
		c.Name + " for review."), nil
}

//-----------------------------------------------------------------------------
// Shared helpers
//-----------------------------------------------------------------------------

// searchArgs unpacks "<text> \t <start> \t <count> \t <flag>", which both
// search ordinals send. The trailing flag differs by caller -- the address book
// sends 1, the browser 0 -- and what it selected is not recoverable from the
// client, so it is read and ignored rather than guessed at.
func searchArgs(args string) (q string, start, count int) {
	q = field(args, 0)
	start = int(atoi(field(args, 1)))
	count = int(atoi(field(args, 2)))
	if count <= 0 || count > 200 {
		count = 100
	}
	return q, start, count
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// notFound turns a missing subject into a sentence. Every other error is a
// fault and travels up to become a 500.
func notFound(err error, msg string) (Answer, error) {
	if errors.Is(err, store.ErrNotFound) {
		return fail("%s", msg), nil
	}
	return Answer{}, err
}

// userError lets a refusal the store raised through as a readable status and
// keeps everything else a fault.
func userError(err error) (Answer, error) {
	var ue *store.UserError
	if errors.As(err, &ue) {
		return fail("%s", ue.Msg), nil
	}
	if errors.Is(err, store.ErrNotFound) {
		return fail("That no longer exists."), nil
	}
	return Answer{}, err
}
