package dbproxy

import "github.com/henrik/tnbrowser-server/internal/store"

// News, the MOTD and Web Links: webnews.cs, weblinks.cs.
//
// None of this can be rendered. NewsGui and weblinksmenu are driven by those
// scripts but defined in no .vl2 and no loose file anywhere in a retail
// install, so the pane shipped in 2002 with its controls removed. The ordinals
// are still reachable from the console, which is the only place these answers
// will ever be seen.
//
// That is a reason to be careful rather than a reason to be sloppy: an ordinal
// nothing renders is one where a wrong field index cannot be caught by looking
// at it.

// newsFeedLimit is what the pane would have paged through. maxRows carries the
// page number rather than a limit for both news ordinals (webnews.cs:467), so
// the client offers no cap of its own and the server has to pick one.
const newsFeedLimit = 50

func init() {
	on(Scalar, "0", getMOTD)
	on(Scalar, "1", postNewsArticle)
	on(Scalar, "2", editNewsArticle)
	on(Scalar, "3", deleteNewsArticle)
	on(Scalar, "4", setMOTD)

	on(Array, "0", getNewsArticles)
	on(Array, "100", getNewsByCategory)

	on(Array, "15", getWebLinks)
}

// scalar 0. The resultString IS the payload here, not a row count: the handler
// does setText(%RowCount_Result) (webnews.cs:481).
func getMOTD(c *Ctx, args string) (Answer, error) {
	text, err := c.Store.MOTD(c.Ctx)
	if err != nil {
		return Answer{}, err
	}
	return okResult("The message of the day follows.", text), nil
}

func setMOTD(c *Ctx, args string) (Answer, error) {
	if err := requireStaff(c); err != nil {
		return staffRefusal(err)
	}
	if err := c.Store.SetMOTD(c.Ctx, args); err != nil {
		return userError(err)
	}
	return okMessage("The message of the day is now " + quoted(args) + "."), nil
}

// newsRow lays out the schema array 0 and array 100 share:
//
//	0=- 1=topicId 2=articleId 3=postCount+1 4=date 5=updateId 6=authorId 7=-
//	8..11=authorQuad 12=category 13=headline 14..=body lines
//
// Fields 0 and 7 are padding the parser never reads; they exist so 8..11 land
// where NewsGui::rebuildText looks for the quad.
func newsRow(a store.NewsArticle) string {
	head := tab(
		"", a.ID, a.ID, 1, date(a.Created), a.Updated, a.Author.GUID, "",
		a.Author.Name, a.Author.Tag, a.Author.Append, a.Author.GUID,
		a.Category, a.Headline,
	)
	return withBody(head, a.Body)
}

// array 0. Status fields 2 and 3 are a total-record count and an ACL
// (webnews.cs:206); the pane uses the ACL to decide whether to draw its edit
// controls at all.
func getNewsArticles(c *Ctx, args string) (Answer, error) {
	return newsFeed(c, atoi(field(args, 1)))
}

// array 100. The call site comments its tuple as
// "ordinal.page.start.direction.category", so the category is field 2.
func getNewsByCategory(c *Ctx, args string) (Answer, error) {
	return newsFeed(c, atoi(field(args, 2)))
}

func newsFeed(c *Ctx, category int64) (Answer, error) {
	articles, err := c.Store.NewsArticles(c.Ctx, category, newsFeedLimit)
	if err != nil {
		return Answer{}, err
	}

	staff, err := c.Store.IsStaff(c.Ctx, c.GUID)
	if err != nil {
		return Answer{}, err
	}

	rows := make([]string, 0, len(articles))
	for _, a := range articles {
		rows = append(rows, newsRow(a))
	}

	return Answer{
		Status: okStatus("", itoa(len(rows)), boolField(staff)),
		Result: itoa(len(rows)),
		Rows:   rows,
	}, nil
}

func postNewsArticle(c *Ctx, args string) (Answer, error) {
	if err := requireStaff(c); err != nil {
		return staffRefusal(err)
	}
	if err := c.Store.PostNews(c.Ctx, c.GUID, atoi(field(args, 0)),
		field(args, 1), fieldsFrom(args, 2)); err != nil {
		return userError(err)
	}
	return okMessage("The news article " + quoted(field(args, 1)) +
		" has been posted."), nil
}

func editNewsArticle(c *Ctx, args string) (Answer, error) {
	if err := requireStaff(c); err != nil {
		return staffRefusal(err)
	}
	if err := c.Store.EditNews(c.Ctx, atoi(field(args, 0)), atoi(field(args, 1)),
		field(args, 2), fieldsFrom(args, 3)); err != nil {
		return userError(err)
	}
	return okMessage("The news article " + quoted(field(args, 2)) +
		" has been updated."), nil
}

// scalar 3. The caller assembles the fields itself and the shipped script does
// not say what goes in them beyond the first, so the article id is read from
// field 0 and the rest is left alone.
func deleteNewsArticle(c *Ctx, args string) (Answer, error) {
	if err := requireStaff(c); err != nil {
		return staffRefusal(err)
	}
	id := atoi(field(args, 0))
	if err := c.Store.DeleteNews(c.Ctx, id); err != nil {
		return userError(err)
	}
	return okMessage("News article " + itoa64(id) + " has been deleted."), nil
}

// array 15. Field 0 is a per-row status and the client accepts the row only
// when it is "0" -- a row-level veto, which is unusual enough to be worth
// naming. On a non-zero query status it abandons the server's list entirely and
// falls back to its own 50 hardcoded sites (weblinks.cs:1-56).
func getWebLinks(c *Ctx, args string) (Answer, error) {
	links, err := c.Store.WebLinks(c.Ctx)
	if err != nil {
		return Answer{}, err
	}

	rows := make([]string, 0, len(links))
	for _, l := range links {
		rows = append(rows, tab("0", l.Name, l.Address))
	}
	return okRows(rows), nil
}
