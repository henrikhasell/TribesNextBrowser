package store

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/henrik/tnbrowser-server/internal/model"
)

// Folders. The original EmailGui had these three tabs; TribesNext's API had no
// folder concept at all, which is why the mod hides two of them today.
const (
	FolderInbox   = "inbox"
	FolderSent    = "sent"
	FolderDeleted = "deleted"
)

func validFolder(f string) string {
	switch f {
	case FolderSent, FolderDeleted:
		return f
	default:
		return FolderInbox
	}
}

// MailCount is the count method: how many messages are in the inbox.
//
// Not "how many are unread", which would be the more useful indicator: the
// reference implementation counts everything in the box, and the live server's
// only observable answer (0, on an empty inbox) does not distinguish the two.
// Matching the reference keeps the two backends interchangeable; unread state
// is carried per message, so a client can count either.
func (s *Store) MailCount(ctx context.Context, guid string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM mail WHERE owner_guid = $1 AND folder = $2`,
		guid, FolderInbox).Scan(&n)
	return n, err
}

// MailList is `read` with no id: the messages in a folder, newest first.
//
// Bodies are included. The original fetched them separately, but a mailbox here
// is small and it lets the client render a selected message with no second
// round trip -- which is what TNBrowser's cached-list path already expects.
func (s *Store) MailList(ctx context.Context, guid, folder string) ([]model.Message, error) {
	folder = validFolder(folder)

	rows, err := s.pool.Query(ctx, `
		SELECT id, from_guid, from_name, to_guid, to_name, subject, body, sent, unread, folder
		  FROM mail WHERE owner_guid = $1 AND folder = $2
		 ORDER BY sent DESC LIMIT 200`, guid, folder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MailRead fetches one message and marks it read.
//
// Scoped to the owner: a player asking for someone else's message id gets
// nothing, rather than someone else's mail.
func (s *Store) MailRead(ctx context.Context, guid string, id int64) ([]model.Message, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, from_guid, from_name, to_guid, to_name, subject, body, sent, unread, folder
		  FROM mail WHERE owner_guid = $1 AND id = $2`, guid, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(out) > 0 {
		if _, err := s.pool.Exec(ctx,
			`UPDATE mail SET unread = FALSE WHERE owner_guid = $1 AND id = $2`, guid, id); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func scanMessage(rows pgx.Rows) (model.Message, error) {
	var (
		m      model.Message
		id     int64
		sent   int64
		unread bool
	)
	if err := rows.Scan(&id, &m.FromGUID, &m.From, &m.ToGUID, &m.To,
		&m.Subject, &m.Body, &sent, &unread, &m.Folder); err != nil {
		return m, err
	}
	m.ID = strconv.FormatInt(id, 10)
	m.Date = strconv.FormatInt(sent, 10)
	m.Unread = model.Bool(unread)
	return m, nil
}

// MailDelete moves a message to the Deleted folder, and purges it if it was
// already there -- the two-stage delete the original's Deleted tab implied.
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

// MailSend delivers a message: one row in the recipient's inbox, one in the
// sender's Sent folder.
//
// This is the method TribesNext refuses outright, so it is the clearest
// behavioural difference between this backend and theirs.
//
// The recipient may be given as a GUID or a name, because the compose window's
// TO: field is free text and its search helper fills in a GUID.
func (s *Store) MailSend(ctx context.Context, guid, to, subject, body string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return refuse("no recipient")
	}

	return s.tx(ctx, func(tx pgx.Tx) error {
		var toGUID, toName string
		err := tx.QueryRow(ctx, `
			SELECT guid, name FROM accounts
			 WHERE guid = $1 OR LOWER(name) = LOWER($1) LIMIT 1`, to).Scan(&toGUID, &toName)
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse("no such player")
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
			// Deliberately indistinguishable from success: telling a sender
			// they have been blocked invites them to work around it.
			return nil
		}

		var fromName string
		if err := tx.QueryRow(ctx,
			`SELECT name FROM accounts WHERE guid = $1`, guid).Scan(&fromName); err != nil {
			return err
		}

		now := s.now()
		if _, err := tx.Exec(ctx, `
			INSERT INTO mail (owner_guid, from_guid, from_name, to_guid, to_name,
			                  subject, body, sent, unread, folder)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, TRUE, $9)`,
			toGUID, guid, fromName, toGUID, toName, subject, body, now, FolderInbox); err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO mail (owner_guid, from_guid, from_name, to_guid, to_name,
			                  subject, body, sent, unread, folder)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, FALSE, $9)`,
			guid, guid, fromName, toGUID, toName, subject, body, now, FolderSent)
		return err
	})
}
