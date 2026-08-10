package store

import "context"

// The message a player gets the first time they authenticate here.
//
// This is the only thing the mod can say to a player unprompted. The community
// screens are the shipped ones, so there is nowhere in them to put a banner --
// and a player who opens EMAIL first meets an empty inbox, which looks exactly
// like a backend that is not answering. One unread message fixes that using the
// game's own mechanism: EmailGui plays the got-mail sound and renders the row
// bold, both because the row arrives unread.
//
// The body is deliberately ONE line. The client takes the whole body with
// getFields(%row, 17) (webemail.cs:1130), which rejoins it TAB-separated into a
// single record, and EmailGetBody (webemail.cs:167) prints that verbatim -- so
// a newline written here becomes a tab in one wrapped line, and paragraph
// breaks do not exist. That is the same property that makes the invitation
// links work; GuiMLTextCtrl does the wrapping.
const (
	welcomeSubject = "Browser and mail are back"
	welcomeBody    = "The in-game community screens are working again: warrior profiles, tribe pages, rosters, search and this mailbox all run against a community server now, in place of the WON service that shut down in 2003. Look for BROWSER and EMAIL on the launch bar -- they are the screens Tribes 2 shipped with, unchanged."
)

// SystemGUID is the sender of mail this backend writes on its own behalf.
//
// A TribesNext account GUID is a positive integer, so 0 can never belong to a
// real player and needs no reservation upstream.
const SystemGUID = "0"

// SystemName is what that sender is called in an inbox.
const SystemName = "TNBrowser"

// ensureSystemAccount makes sure the sender has a row to be resolved through.
//
// mailRows does not read mail.from_name -- it resolves the sender quad with
// Quad, which answers "(unknown)" for a GUID with no account. So the row has to
// exist by the time the mail is read. It is created here rather than in a
// migration because both testdata/seed.sql and the Go tests TRUNCATE accounts;
// creating it on demand means it heals itself after either.
//
// last_seen stays 0 so online() never reports the system account as being on a
// game server.
func (s *Store) ensureSystemAccount(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO accounts (guid, name, created, last_seen)
		VALUES ($1, $2, $3, 0)
		ON CONFLICT (guid) DO UPDATE SET name = EXCLUDED.name`,
		SystemGUID, SystemName, s.now())
	return err
}

// WelcomeMail delivers the first-authentication message.
//
// Called only when EnsureAccount reports that it created the account, so it
// runs once per player. Delivery goes through Deliver, the same path tribe
// invitations take: an unread row in the recipient's inbox.
func (s *Store) WelcomeMail(ctx context.Context, guid string) error {
	if err := s.ensureSystemAccount(ctx); err != nil {
		return err
	}
	return s.Deliver(ctx, guid, SystemGUID, SystemName, welcomeSubject, welcomeBody)
}
