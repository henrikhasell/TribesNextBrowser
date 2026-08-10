package store

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/henrik/tnbrowser-server/internal/model"
)

// activeTag resolves the tag a player currently wears, from their active clan.
// Returns empty strings when they wear none, which the client renders as a
// plain name.
// rowQuerier is satisfied by both the pool and a transaction, so helpers work
// inside a transaction or outside one.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func activeTag(ctx context.Context, q rowQuerier, guid string) (tag string, append_ bool, err error) {
	err = q.QueryRow(ctx, `
		SELECT c.tag, c.append
		  FROM accounts a
		  JOIN clans c ON c.id = a.active_clan
		 WHERE a.guid = $1`, guid).Scan(&tag, &append_)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return tag, append_, err
}

// UserView is the userview method.
func (s *Store) UserView(ctx context.Context, guid string) (*model.User, error) {
	var (
		u        model.User
		created  int64
		lastSeen int64
	)
	err := s.pool.QueryRow(ctx, `
		SELECT guid, name, website, info, created, last_seen
		  FROM accounts WHERE guid = $1`, guid).
		Scan(&u.GUID, &u.Name, &u.Website, &u.Info, &created, &lastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	u.Creation = strconv.FormatInt(created, 10)
	u.Online = online(lastSeen, s.now())

	tag, app, err := activeTag(ctx, s.pool, guid)
	if err != nil {
		return nil, err
	}
	u.Tag, u.Append = tag, model.Bool(app)

	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, m.rank, m.title, c.tag, c.append
		  FROM clan_members m
		  JOIN clans c ON c.id = m.clan_id
		 WHERE m.guid = $1
		 ORDER BY m.rank DESC, c.name`, guid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	u.Memberships = []model.Membership{}
	for rows.Next() {
		var (
			m    model.Membership
			id   int64
			rank int
			app  bool
		)
		if err := rows.Scan(&id, &m.Name, &rank, &m.Title, &m.Tag, &app); err != nil {
			return nil, err
		}
		m.ID = strconv.FormatInt(id, 10)
		m.Rank = strconv.Itoa(rank)
		m.Append = model.Bool(app)
		u.Memberships = append(u.Memberships, m)
	}
	return &u, rows.Err()
}

// UserSearch matches players whose name starts with the query, as the original
// did ("This will match on player names that start with the query name").
func (s *Store) UserSearch(ctx context.Context, q string) ([]model.UserSearchHit, error) {
	hits := []model.UserSearchHit{}
	if strings.TrimSpace(q) == "" {
		return hits, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT a.guid, a.name, COALESCE(c.tag, ''), COALESCE(c.append, FALSE)
		  FROM accounts a
		  LEFT JOIN clans c ON c.id = a.active_clan
		 WHERE a.name ILIKE $1 || '%'
		 ORDER BY a.name
		 LIMIT 100`, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			h   model.UserSearchHit
			app bool
		)
		if err := rows.Scan(&h.GUID, &h.Name, &h.Tag, &app); err != nil {
			return nil, err
		}
		h.Append = model.Bool(app)
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func (s *Store) UserHistory(ctx context.Context, guid string) ([]model.HistoryEntry, error) {
	return s.history(ctx, "user", guid)
}

func (s *Store) history(ctx context.Context, kind, id string) ([]model.HistoryEntry, error) {
	out := []model.HistoryEntry{}
	rows, err := s.pool.Query(ctx, `
		SELECT at, event FROM history
		 WHERE subject_type = $1 AND subject_id = $2
		 ORDER BY at DESC LIMIT 200`, kind, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			e  model.HistoryEntry
			at int64
		)
		if err := rows.Scan(&at, &e.Event); err != nil {
			return nil, err
		}
		e.Time = strconv.FormatInt(at, 10)
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetInfo and SetWebsite are the self-service profile edits.
func (s *Store) SetInfo(ctx context.Context, guid, info string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE accounts SET info = $2 WHERE guid = $1`, guid, info); err != nil {
			return err
		}
		return s.note(ctx, tx, "user", guid, "Changed profile text")
	})
}

func (s *Store) SetWebsite(ctx context.Context, guid, site string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE accounts SET website = $2 WHERE guid = $1`, guid, site); err != nil {
			return err
		}
		if site == "" {
			return s.note(ctx, tx, "user", guid, "Cleared their web address")
		}
		return s.note(ctx, tx, "user", guid,
			"Changed their web address to "+Ref(RefWeb, site))
	})
}

// SetActiveClan chooses which clan's tag the player wears. -1 clears it.
func (s *Store) SetActiveClan(ctx context.Context, guid string, clanID int64) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if clanID < 0 {
			before := activeClanTx(ctx, tx, guid)
			if _, err := tx.Exec(ctx,
				`UPDATE accounts SET active_clan = NULL WHERE guid = $1`, guid); err != nil {
				return err
			}
			// Named while it is still readable: the UPDATE above has already
			// cleared it, so the previous wearing is only knowable from here.
			was := activeClanNameTx(ctx, tx, guid, before)
			if was == "" {
				return s.note(ctx, tx, "user", guid, "Stopped wearing a tribe tag")
			}
			return s.note(ctx, tx, "user", guid,
				"Stopped wearing the tag of "+Ref(RefClan, was))
		}

		// Wearing a tag requires membership -- otherwise anyone could wear any
		// clan's tag, which is exactly what the tag is meant to prove.
		rank, err := rankIn(ctx, tx, clanID, guid)
		if err != nil {
			return err
		}
		if rank < 0 {
			return refuse("you are not a member of that clan")
		}

		if _, err := tx.Exec(ctx,
			`UPDATE accounts SET active_clan = $2 WHERE guid = $1`, guid, clanID); err != nil {
			return err
		}
		return s.note(ctx, tx, "user", guid,
			"Now wears the tag of "+Ref(RefClan, clanNameTx(ctx, tx, clanID)))
	})
}

// UserInvites lists the clan invitations awaiting this player.
func (s *Store) UserInvites(ctx context.Context, guid string) ([]model.Invite, error) {
	out := []model.Invite{}
	rows, err := s.pool.Query(ctx, `
		SELECT i.from_guid, COALESCE(fa.name, '(unknown)'),
		       COALESCE(fc.tag, ''), COALESCE(fc.append, FALSE),
		       c.id, c.name, c.tag, c.append
		  FROM clan_invites i
		  JOIN clans c ON c.id = i.clan_id
		  LEFT JOIN accounts fa ON fa.guid = i.from_guid
		  LEFT JOIN clans fc ON fc.id = fa.active_clan
		 WHERE i.guid = $1
		 ORDER BY i.created DESC`, guid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			inv          model.Invite
			clanID       int64
			senderApp    bool
			clanAppend   bool
			senderGUID   string
			senderName   string
			senderTagStr string
		)
		if err := rows.Scan(&senderGUID, &senderName, &senderTagStr, &senderApp,
			&clanID, &inv.Clan.Name, &inv.Clan.Tag, &clanAppend); err != nil {
			return nil, err
		}
		inv.Sender = model.InviteParty{
			GUID: senderGUID, Name: senderName,
			Tag: senderTagStr, Append: model.Bool(senderApp),
		}
		inv.Clan.ID = strconv.FormatInt(clanID, 10)
		inv.Clan.Append = model.Bool(clanAppend)
		out = append(out, inv)
	}
	return out, rows.Err()
}

// activeClanTx reads the clan whose tag a player is wearing, and
// activeClanNameTx names it. Zero means none.
func activeClanTx(ctx context.Context, tx pgx.Tx, guid string) int64 {
	var id *int64
	if err := tx.QueryRow(ctx,
		`SELECT active_clan FROM accounts WHERE guid = $1`, guid).Scan(&id); err != nil ||
		id == nil {
		return 0
	}
	return *id
}

func activeClanNameTx(ctx context.Context, tx pgx.Tx, guid string, id int64) string {
	if id == 0 {
		return ""
	}
	return clanNameTx(ctx, tx, id)
}
