package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Folders. The stock EmailGui has these three tabs, and all three work: a sent
// message is kept in the sender's Sent folder, and deleting moves a message to
// Deleted before a second delete purges it.
const (
	FolderInbox   = "inbox"
	FolderSent    = "sent"
	FolderDeleted = "deleted"
)

// MailDelete moves a message to the Deleted folder, and purges it if it was
// already there -- the two-stage delete the stock Deleted tab implies.
func (s *Store) MailDelete(ctx context.Context, guid string, id int64) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		var folder string
		err := tx.QueryRow(ctx,
			`SELECT folder FROM mail WHERE owner_guid = $1 AND id = $2`, guid, id).Scan(&folder)
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse("no such message")
		}
		if err != nil {
			return err
		}

		if folder == FolderDeleted {
			_, err = tx.Exec(ctx,
				`DELETE FROM mail WHERE owner_guid = $1 AND id = $2`, guid, id)
			return err
		}
		_, err = tx.Exec(ctx,
			`UPDATE mail SET folder = $3 WHERE owner_guid = $1 AND id = $2`,
			guid, id, FolderDeleted)
		return err
	})
}

// MailSend delivers a message: one row per recipient's inbox, one in the
// sender's Sent folder.
//
// to and cc are the comma-separated lists the compose window assembled. They
// are stored verbatim alongside every copy as well as being resolved, because
// fields 13 and 14 of a mail row are those lists as display strings -- the
// client shows them back and never resolves them itself.
//
// A recipient may be a GUID or a name: the TO: field is free text and its
// search helper fills in a GUID.
func (s *Store) MailSend(ctx context.Context, guid, to, cc, subject, body string) error {
	toNames := splitList(to)
	ccNames := splitList(cc)
	if len(toNames)+len(ccNames) == 0 {
		return refuse("no recipient")
	}

	return s.tx(ctx, func(tx pgx.Tx) error {
		var fromName string
		if err := tx.QueryRow(ctx,
			`SELECT name FROM accounts WHERE guid = $1`, guid).Scan(&fromName); err != nil {
			return err
		}
		now := s.now()

		deliver := func(who string, isCC bool) error {
			var toGUID, toName string
			err := tx.QueryRow(ctx, `
				SELECT guid, name FROM accounts
				 WHERE guid = $1 OR LOWER(name) = LOWER($1) LIMIT 1`, who).Scan(&toGUID, &toName)
			if errors.Is(err, pgx.ErrNoRows) {
				return refuse("no such player: %s", who)
			}
			if err != nil {
				return err
			}

			// Blocking is enforced here rather than at read time, so a blocked
			// sender's mail never occupies the recipient's mailbox at all.
			var blocked bool
			if err := tx.QueryRow(ctx,
				`SELECT EXISTS (SELECT 1 FROM blocks WHERE guid = $1 AND blocked_guid = $2)`,
				toGUID, guid).Scan(&blocked); err != nil {
				return err
			}
			if blocked {
				// Counted so the block dialog can show what the block has
				// actually turned away, which is the column the stock screen
				// had.
				if _, err := tx.Exec(ctx,
					`UPDATE blocks SET hits = hits + 1 WHERE guid = $1 AND blocked_guid = $2`,
					toGUID, guid); err != nil {
					return err
				}
				// Deliberately indistinguishable from success: telling a sender
				// they have been blocked invites them to work around it.
				return nil
			}

			_, err = tx.Exec(ctx, `
				INSERT INTO mail (owner_guid, from_guid, from_name, to_guid, to_name,
				                  subject, body, sent, unread, folder, is_cc,
				                  to_list, cc_list)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, TRUE, $9, $10, $11, $12)`,
				toGUID, guid, fromName, toGUID, toName, subject, body, now,
				FolderInbox, isCC, to, cc)
			return err
		}

		for _, who := range toNames {
			if err := deliver(who, false); err != nil {
				return err
			}
		}
		for _, who := range ccNames {
			if err := deliver(who, true); err != nil {
				return err
			}
		}

		primary := to
		if primary == "" {
			primary = cc
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO mail (owner_guid, from_guid, from_name, to_guid, to_name,
			                  subject, body, sent, unread, folder, to_list, cc_list)
			VALUES ($1, $2, $3, '', $4, $5, $6, $7, FALSE, $8, $9, $10)`,
			guid, guid, fromName, primary, subject, body, now,
			FolderSent, to, cc)
		return err
	})
}

// splitList unpacks a comma-separated recipient list, dropping the empties a
// trailing comma leaves behind.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
