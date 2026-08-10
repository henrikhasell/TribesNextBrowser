package dbproxy

import (
	"strconv"

	"github.com/henrik/tnbrowser-server/internal/store"
)

// Forums: webforums.cs.
//
// Unrenderable for the same reason as news -- ForumsGui is named only by the
// script that drives it -- so these answers, too, are only ever seen from the
// console.

const forumTopicLimit = 80

func init() {
	on(Array, "7", getForumList)
	on(Array, "8", getTopicList)
	on(Array, "9", getPostUpdates)

	on(Scalar, "12", postTopicOrReply)
	on(Scalar, "13", editPost)
	on(Scalar, "14", postNewsOrDeletePost)
	on(Scalar, "60", requestTopicReview)
	on(Scalar, "61", requestPostReview)
	on(Scalar, "62", removeTopic)
	on(Scalar, "66", lockTopic)
	on(Scalar, "67", unlockTopic)
	on(Scalar, "68", moveTopic)
}

// array 7. After the last row the client appends one entry per tribe out of
// WONGetAuthInfo() with a negated id (webforums.cs:817-822) -- so tribe forums
// were always client-side and a server that invented them would produce
// duplicates.
func getForumList(c *Ctx, args string) (Answer, error) {
	forums, err := c.Store.Forums(c.Ctx)
	if err != nil {
		return Answer{}, err
	}

	rows := make([]string, 0, len(forums))
	for i, f := range forums {
		rows = append(rows, tab(i, f.Name, f.Flag, f.ID))
	}
	return okRows(rows), nil
}

// array 8. maxRows carries a page number at two of the three call sites and a
// real limit at the third, so the slot cannot be read as a cap and the server
// picks its own.
func getTopicList(c *Ctx, args string) (Answer, error) {
	topics, err := c.Store.Topics(c.Ctx, atoi(field(args, 0)), forumTopicLimit)
	if err != nil {
		return Answer{}, err
	}

	rows := make([]string, 0, len(topics))
	for _, t := range topics {
		rows = append(rows, tab(
			"", t.ID, t.Subject, t.Posts, "", "", date(t.Created), "",
			t.Author, "", "", "",
			t.HasDeletes, t.Security, t.MaxPostID,
		))
	}
	return okRows(rows), nil
}

// array 9. Status field 2 is the per-forum flag the client caches as
// ForumsGui.bflag (webforums.cs:765), which is why this one ordinal reads a
// forum id it does not otherwise need.
func getPostUpdates(c *Ctx, args string) (Answer, error) {
	topicID := atoi(field(args, 0))
	since := atoi(field(args, 1))

	posts, err := c.Store.PostsSince(c.Ctx, topicID, since, c.GUID)
	if err != nil {
		return Answer{}, err
	}

	rows := make([]string, 0, len(posts))
	for _, p := range posts {
		head := tab(
			p.IsAuthor, "", p.ID, p.ParentID, p.ID,
			p.Author.Name, p.Author.Tag, p.Author.Append, p.Author.GUID,
			"", date(p.Created), "", p.Deleted, p.Subject,
		)
		rows = append(rows, withBody(head, p.Body))
	}

	return Answer{
		Status: okStatus("0"),
		Result: strconv.Itoa(len(rows)),
		Rows:   rows,
	}, nil
}

// scalar 12: a new topic when the topic id is 0, a reply otherwise. The client
// sends both through this one ordinal and distinguishes them only by whether it
// has a topic id yet (webforums.cs:482, :510).
func postTopicOrReply(c *Ctx, args string) (Answer, error) {
	err := c.Store.PostTopic(c.Ctx, c.GUID,
		atoi(field(args, 0)), atoi(field(args, 1)), atoi(field(args, 2)),
		field(args, 3), fieldsFrom(args, 4))
	if err != nil {
		return userError(err)
	}
	return okResult("Posted."), nil
}

func editPost(c *Ctx, args string) (Answer, error) {
	err := c.Store.EditPost(c.Ctx, c.GUID, atoi(field(args, 0)),
		field(args, 1), fieldsFrom(args, 2))
	if err != nil {
		return userError(err)
	}
	return okResult("Updated."), nil
}

// scalar 14 is genuinely ambiguous, and this is the one place the server has to
// take a position on it.
//
// Three call sites send it with two argument shapes for what the client labels
// three operations: three fields at webforums.cs:494 ("postNews"), one field at
// :552 ("deletePost") and one at :1474 ("adminRemovePost"). Either the
// procedure was overloaded on argument count or one of these is a copy-paste
// error -- immediately below the third, :1475 has the correct-looking call
// commented out in favour of 14.
//
// Branching on the field count is the only reading available: the wire carries
// nothing else that separates them, and the two one-field cases are the same
// operation under different labels anyway.
func postNewsOrDeletePost(c *Ctx, args string) (Answer, error) {
	if len(fields(args)) >= 3 {
		if err := requireStaff(c); err != nil {
			return staffRefusal(err)
		}
		if err := c.Store.PostNews(c.Ctx, c.GUID, 105,
			field(args, 1), fieldsFrom(args, 2)); err != nil {
			return userError(err)
		}
		return okResult("Posted."), nil
	}

	if err := c.Store.DeletePost(c.Ctx, c.GUID, atoi(field(args, 0))); err != nil {
		return userError(err)
	}
	return okResult("Deleted."), nil
}

// scalar 60 and 61 flag something for a moderator's attention. The shipped
// scripts say what is sent and nothing about what came back, so both are
// recorded and acknowledged rather than acted on.
func requestTopicReview(c *Ctx, args string) (Answer, error) {
	if err := c.Store.LogAdminAction(c.Ctx, c.GUID, 60, 0, args); err != nil {
		return Answer{}, err
	}
	return okResult("A moderator has been notified."), nil
}

func requestPostReview(c *Ctx, args string) (Answer, error) {
	if err := c.Store.LogAdminAction(c.Ctx, c.GUID, 61, 0, args); err != nil {
		return Answer{}, err
	}
	return okResult("A moderator has been notified."), nil
}

// scalar 62. The browser issues it with an eight-way selector in field 0 and
// the forums pane with a leading 0; the selector is named nowhere in the
// shipped scripts. The forums reading -- remove this topic -- is the one that
// has a call site whose intent is legible, so it is the one acted on, and the
// browser's selector is recorded alongside.
func removeTopic(c *Ctx, args string) (Answer, error) {
	selector := int(atoi(field(args, 0)))
	if err := c.Store.LogAdminAction(c.Ctx, c.GUID, 62, selector, args); err != nil {
		return Answer{}, err
	}
	if err := c.Store.RemoveTopic(c.Ctx, c.GUID, atoi(field(args, 1))); err != nil {
		return userError(err)
	}
	return okResult("Removed."), nil
}

func lockTopic(c *Ctx, args string) (Answer, error) {
	if err := c.Store.SetTopicLocked(c.Ctx, c.GUID, atoi(field(args, 0)), true); err != nil {
		return userError(err)
	}
	return okResult("Locked."), nil
}

func unlockTopic(c *Ctx, args string) (Answer, error) {
	if err := c.Store.SetTopicLocked(c.Ctx, c.GUID, atoi(field(args, 0)), false); err != nil {
		return userError(err)
	}
	return okResult("Unlocked."), nil
}

func moveTopic(c *Ctx, args string) (Answer, error) {
	if err := c.Store.MoveTopic(c.Ctx, c.GUID,
		atoi(field(args, 0)), atoi(field(args, 1))); err != nil {
		return userError(err)
	}
	return okResult("Moved."), nil
}

//-----------------------------------------------------------------------------

// requireStaff applies WON's own staff model: membership of tribe 1401 above
// admin level 1, which is what webbrowser.cs:409 and :2248 check.
func requireStaff(c *Ctx) error {
	staff, err := c.Store.IsStaff(c.Ctx, c.GUID)
	if err != nil {
		return err
	}
	if !staff {
		return &store.UserError{Msg: "You do not have moderator privileges."}
	}
	return nil
}

func staffRefusal(err error) (Answer, error) {
	return userError(err)
}

func itoa(n int) string { return strconv.Itoa(n) }
