package store

import (
	"context"
	"strconv"
	"strings"

	"github.com/henrik/tnbrowser-server/internal/model"
)

// The website's directory queries.
//
// These are listings, which the game never asked for: its screens only ever
// searched, because a Torque list control being fed a whole community was not
// something the shipped UI was built to do. A web page scrolls, so here you can
// simply look at everyone.
//
// Both listings take their total from COUNT(*) OVER (), which is evaluated
// after the LIMIT is decided but over the whole matching set -- so a page and
// the number of pages come back from one round trip rather than two.

// MaxPageSize caps what a caller may ask for. Beyond this the page stops being
// a page.
const MaxPageSize = 100

// Page clamps a requested page size and number into a limit and an offset.
// Exported because the API layer parses the query string and wants the same
// rules applied there, so its reply can say which page it actually served.
func Page(size, number int) (limit, offset int) {
	if size <= 0 {
		size = 25
	}
	if size > MaxPageSize {
		size = MaxPageSize
	}
	if number < 1 {
		number = 1
	}
	return size, (number - 1) * size
}

// Warriors lists registered accounts for the website's directory, with the
// total number matching the query.
//
// The match is a substring, unlike UserSearch -- which anchors at the start of
// the name because that is what the in-game screen did, matching as you type.
// A directory is browsed rather than typed into, and somebody looking for
// "orange" should find "orangeade" whichever end of it they remember.
//
// Ordered by name, case-insensitively, and deliberately not by who is online:
// an ordering that moves while you are paging through it shows some rows twice
// and skips others.
//
// SystemGUID is excluded. It has an accounts row so that the welcome mail has a
// resolvable sender (see ensureSystemAccount), but it is this server writing to
// players, not a warrior anybody can visit.
func (s *Store) Warriors(ctx context.Context, q string, limit, offset int) ([]model.DirectoryWarrior, int, error) {
	out := []model.DirectoryWarrior{}
	q = strings.TrimSpace(q)

	rows, err := s.pool.Query(ctx, `
		SELECT a.guid, a.name,
		       COALESCE(c.tag, ''), COALESCE(c.append, FALSE),
		       a.created, a.last_seen,
		       (SELECT COUNT(*) FROM clan_members m
		          JOIN clans mc ON mc.id = m.clan_id
		         WHERE m.guid = a.guid AND mc.active),
		       COUNT(*) OVER ()
		  FROM accounts a
		  LEFT JOIN clans c ON c.id = a.active_clan
		 WHERE a.guid <> $4
		   AND ($1 = '' OR a.name ILIKE '%' || $1 || '%')
		 ORDER BY LOWER(a.name), a.guid
		 LIMIT $2 OFFSET $3`, q, limit, offset, SystemGUID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	now := s.now()
	total := 0
	for rows.Next() {
		var (
			w        model.DirectoryWarrior
			lastSeen int64
		)
		if err := rows.Scan(&w.GUID, &w.Name, &w.Tag, &w.Append,
			&w.Created, &lastSeen, &w.Tribes, &total); err != nil {
			return nil, 0, err
		}
		w.LastSeen = lastSeen
		w.Online = online(lastSeen, now) == 1
		out = append(out, w)
	}
	return out, total, rows.Err()
}

// Tribes lists active clans with their rosters counted, and the total matching
// the query.
//
// Disbanded clans are excluded, as ClanSearch excludes them: disbanding sets
// active = FALSE rather than deleting the row, so that the history and the
// mail that reference a tribe still resolve its name.
//
// The tag is searched as well as the name. In game you find a tribe by name
// because that is the box the search dialog offers; on a scoreboard you have
// only ever seen the tag, and that is as likely to be what you remember.
func (s *Store) Tribes(ctx context.Context, q string, limit, offset int) ([]model.DirectoryTribe, int, error) {
	out := []model.DirectoryTribe{}
	q = strings.TrimSpace(q)

	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, c.tag, c.append, c.recruiting, c.created,
		       COUNT(m.guid),
		       COUNT(*) FILTER (WHERE a.last_seen > 0 AND $4 - a.last_seen < $5),
		       COUNT(*) OVER ()
		  FROM clans c
		  LEFT JOIN clan_members m ON m.clan_id = c.id
		  LEFT JOIN accounts a ON a.guid = m.guid
		 WHERE c.active
		   AND ($1 = '' OR c.name ILIKE '%' || $1 || '%' OR c.tag ILIKE '%' || $1 || '%')
		 GROUP BY c.id, c.name, c.tag, c.append, c.recruiting, c.created
		 ORDER BY LOWER(c.name), c.id
		 LIMIT $2 OFFSET $3`, q, limit, offset, s.now(), onlineWindow)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	total := 0
	for rows.Next() {
		var (
			t  model.DirectoryTribe
			id int64
		)
		if err := rows.Scan(&id, &t.Name, &t.Tag, &t.Append, &t.Recruiting,
			&t.Created, &t.Members, &t.Online, &total); err != nil {
			return nil, 0, err
		}
		t.ID = strconv.FormatInt(id, 10)
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// Counts is the landing page's three numbers.
func (s *Store) Counts(ctx context.Context) (model.Counts, error) {
	var c model.Counts
	err := s.pool.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM accounts WHERE guid <> $3),
		       (SELECT COUNT(*) FROM clans WHERE active),
		       (SELECT COUNT(*) FROM accounts
		         WHERE guid <> $3 AND last_seen > 0 AND $1 - last_seen < $2)`,
		s.now(), onlineWindow, SystemGUID).Scan(&c.Warriors, &c.Tribes, &c.Online)
	return c, err
}
