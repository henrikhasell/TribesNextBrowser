package dbproxy

import "github.com/henrik/tnbrowser-server/internal/store"

// The Email pane: webemail.cs, EmailGui.
//
// The other pane a retail install can render. Its rows drive a native control
// -- GuiEmailBrowser::addRow at 0x0067b8e0 -- by way of EmailMessageVector,
// which is where the client keeps the text, because the control itself is
// display-only and cannot be read back.

func init() {
	on(Array, "1", getMail)
	on(Array, "2", getBlockList)
	on(Array, "14", getDeletedMail)

	on(Scalar, "5", sendMail)
	on(Scalar, "6", deleteMail)
	on(Scalar, "7", markMailRead)
	on(Scalar, "8", removeBlock)
	on(Scalar, "9", addBlock)
	on(Scalar, "35", removeMailPermanently)
}

// mailRow lays out the schema both mail ordinals share:
//
//	0=id 1..4=senderQuad 5..8=recipientQuad 9=created 10=isCC 11=isDeleted
//	12=isRead 13=toList 14=ccList 15=subject 16=bodyLineCount 17..=body
//
// Field 12 is what makes an unread row render bold, through
// GuiEmailBrowser::setRowFlags (0x0067b9a0). Field 16 is read into a variable
// and then never used in either branch (webemail.cs:1130, :1147) -- the client
// takes the whole tail with getFields(%row,17) regardless -- but it has to be
// there or the body starts one field early.
func mailRow(m store.MailRow) string {
	head := tab(
		m.ID,
		m.From.Name, m.From.Tag, m.From.Append, m.From.GUID,
		m.To.Name, m.To.Tag, m.To.Append, m.To.GUID,
		date(m.Created),
		m.IsCC, m.IsDeleted, m.IsRead,
		m.ToList, m.CCList, m.Subject,
	)
	return withBody(head, m.Body)
}

// array 1. The argument is a high-water mark and filtering on it is not
// optional: the pane caches what it is handed and polls again with the highest
// id it has, so a server that ignores it lists every message once per poll and
// the inbox grows without bound.
func getMail(c *Ctx, args string) (Answer, error) {
	rows, err := c.Store.MailSince(c.Ctx, c.GUID, atoi(field(args, 0)))
	if err != nil {
		return Answer{}, err
	}

	out := make([]string, 0, len(rows))
	for _, m := range rows {
		out = append(out, mailRow(m))
	}
	return okRows(out), nil
}

func getDeletedMail(c *Ctx, args string) (Answer, error) {
	rows, err := c.Store.MailDeleted(c.Ctx, c.GUID)
	if err != nil {
		return Answer{}, err
	}

	out := make([]string, 0, len(rows))
	for _, m := range rows {
		out = append(out, mailRow(m))
	}
	// Status field 2 is what the pane shows when the folder is empty
	// (webemail.cs:1029's handler), so it gets a sentence rather than a blank.
	answer := okRows(out)
	if len(out) == 0 {
		answer.Status = okStatus("Your deleted folder is empty.")
	}
	return answer, nil
}

// array 2. getTextName(getFields(%row,0,4)) consumes fields 0..4 but reads only
// four of them (webstuff.cs:19); field 4 is then re-read as the count of mail
// that block has actually turned away.
func getBlockList(c *Ctx, args string) (Answer, error) {
	people, err := c.Store.BlockList(c.Ctx, c.GUID)
	if err != nil {
		return Answer{}, err
	}

	out := make([]string, 0, len(people))
	for _, p := range people {
		out = append(out, tab(p.Name, p.Tag, p.Append, p.GUID, p.Hits))
	}
	return okRows(out), nil
}

// scalar 5. The client has already truncated the whole request to 4000
// characters shared across all four fields (webemail.cs:154), which is the
// clearest surviving evidence of how wide the backend column was.
func sendMail(c *Ctx, args string) (Answer, error) {
	to := field(args, 0)
	cc := field(args, 1)
	subject := field(args, 2)
	body := fieldsFrom(args, 3)

	if err := c.Store.MailSend(c.Ctx, c.GUID, to, cc, subject, body); err != nil {
		return userError(err)
	}
	return okResult("Your message has been sent."), nil
}

// scalar 6 moves to the deleted folder; 35 removes permanently. Both arrive
// through DoEmailDelete, which picks the ordinal and nothing else.
func deleteMail(c *Ctx, args string) (Answer, error) {
	if err := c.Store.MailDelete(c.Ctx, c.GUID, atoi(field(args, 0))); err != nil {
		return userError(err)
	}
	return okResult("Message deleted."), nil
}

func removeMailPermanently(c *Ctx, args string) (Answer, error) {
	if err := c.Store.PurgeMail(c.Ctx, c.GUID, atoi(field(args, 0))); err != nil {
		return userError(err)
	}
	return okResult("Message removed."), nil
}

// scalar 7. Fire-and-forget by construction: the only call site in the five
// scripts that passes neither a proxy object nor a key, so this answer is
// reassembled by the client and thrown away. It is still worth answering
// correctly -- a failure here would be invisible either way, which is exactly
// why the write itself has to be right.
func markMailRead(c *Ctx, args string) (Answer, error) {
	if err := c.Store.MarkMailRead(c.Ctx, c.GUID, atoi(field(args, 0))); err != nil {
		return userError(err)
	}
	return okResult("1"), nil
}

// scalar 9 and 8. Status field 1 is shown in a MessageBoxOK on both the success
// and failure paths (webemail.cs:525), so both branches are sentences.
func addBlock(c *Ctx, args string) (Answer, error) {
	target, err := c.Store.FindUser(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no warrior by that name.")
	}
	if err := c.Store.BlockAdd(c.Ctx, c.GUID, target); err != nil {
		return userError(err)
	}
	return Answer{
		Status: okStatus("Mail from that warrior will no longer reach you."),
		Result: "1",
		Rows:   []string{},
	}, nil
}

func removeBlock(c *Ctx, args string) (Answer, error) {
	target, err := c.Store.FindUser(c.Ctx, field(args, 0))
	if err != nil {
		return notFound(err, "There is no warrior by that name.")
	}
	if err := c.Store.BlockRemove(c.Ctx, c.GUID, target); err != nil {
		return userError(err)
	}
	return Answer{
		Status: okStatus("That warrior is no longer blocked."),
		Result: "1",
		Rows:   []string{},
	}, nil
}
