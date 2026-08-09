package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/henrik/tnbrowser-server/internal/model"
)

// Buddy and block lists.
//
// Both existed in the WON-era browser and email screens -- "Add To Buddylist",
// "Add To Blocklist", TRACKING LIST, BLOCK LIST -- and neither survived into
// TribesNext's API, which is why TNBrowser hides those controls today. They are
// restored here.

// people runs a list query that yields the standard person shape.
func (s *Store) people(ctx context.Context, sql, guid string) ([]model.Person, error) {
	rows, err := s.pool.Query(ctx, sql, guid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := s.now()
	out := []model.Person{}
	for rows.Next() {
		var (
			p        model.Person
			app      bool
			lastSeen int64
		)
		if err := rows.Scan(&p.GUID, &p.Name, &p.Tag, &app, &lastSeen); err != nil {
			return nil, err
		}
		p.Append = model.Bool(app)
		p.Online = online(lastSeen, now)
		out = append(out, p)
	}
	return out, rows.Err()
}

const buddyListSQL = `
	SELECT b.buddy_guid, a.name, COALESCE(c.tag, ''), COALESCE(c.append, FALSE), a.last_seen
	  FROM buddies b
	  JOIN accounts a ON a.guid = b.buddy_guid
	  LEFT JOIN clans c ON c.id = a.active_clan
	 WHERE b.guid = $1
	 ORDER BY a.name`

const blockListSQL = `
	SELECT b.blocked_guid, a.name, COALESCE(c.tag, ''), COALESCE(c.append, FALSE), a.last_seen
	  FROM blocks b
	  JOIN accounts a ON a.guid = b.blocked_guid
	  LEFT JOIN clans c ON c.id = a.active_clan
	 WHERE b.guid = $1
	 ORDER BY a.name`

func (s *Store) BuddyList(ctx context.Context, guid string) ([]model.Person, error) {
	return s.people(ctx, buddyListSQL, guid)
}

func (s *Store) BlockList(ctx context.Context, guid string) ([]model.Person, error) {
	return s.people(ctx, blockListSQL, guid)
}

func (s *Store) BuddyAdd(ctx context.Context, guid, target string) error {
	if target == guid {
		return refuse("you cannot add yourself")
	}
	return s.addRelation(ctx, `
		INSERT INTO buddies (guid, buddy_guid, created) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, guid, target)
}

func (s *Store) BlockAdd(ctx context.Context, guid, target string) error {
	if target == guid {
		return refuse("you cannot block yourself")
	}
	return s.addRelation(ctx, `
		INSERT INTO blocks (guid, blocked_guid, created) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, guid, target)
}

// addRelation checks the target exists before recording it, so a typo becomes a
// clear refusal rather than a silently useless row.
func (s *Store) addRelation(ctx context.Context, sql, guid, target string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM accounts WHERE guid = $1)`, target).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return refuse("no such player")
		}
		_, err := tx.Exec(ctx, sql, guid, target, s.now())
		return err
	})
}

func (s *Store) BuddyRemove(ctx context.Context, guid, target string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM buddies WHERE guid = $1 AND buddy_guid = $2`, guid, target)
	return err
}

func (s *Store) BlockRemove(ctx context.Context, guid, target string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM blocks WHERE guid = $1 AND blocked_guid = $2`, guid, target)
	return err
}

func (s *Store) BuddyClear(ctx context.Context, guid string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM buddies WHERE guid = $1`, guid)
	return err
}

// ClanTagFor is the lookup the game-server mod uses: everything needed to build
// a player's auth-info record, in one query.
//
// It is deliberately not a browser method -- the game server has no player
// token, and needs none: it only ever asks for public facts.
func (s *Store) ClanTagFor(ctx context.Context, guid string) (name, tag string, append_ bool, memberships []model.Membership, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT a.name, COALESCE(c.tag, ''), COALESCE(c.append, FALSE)
		  FROM accounts a
		  LEFT JOIN clans c ON c.id = a.active_clan
		 WHERE a.guid = $1`, guid).Scan(&name, &tag, &append_)
	if err != nil {
		return "", "", false, nil, err
	}

	u, err := s.UserView(ctx, guid)
	if err != nil {
		return "", "", false, nil, err
	}
	return name, tag, append_, u.Memberships, nil
}
