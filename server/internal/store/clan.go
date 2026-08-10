package store

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/henrik/tnbrowser-server/internal/model"
)

// ClanView is the clanview method: the clan plus its full roster.
func (s *Store) ClanView(ctx context.Context, id int64) (*model.Clan, error) {
	var (
		c         model.Clan
		created   int64
		app, recr bool
		active    bool
		numericID int64
	)
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, tag, append, recruiting, website, info, picture, active, created
		  FROM clans WHERE id = $1`, id).
		Scan(&numericID, &c.Name, &c.Tag, &app, &recr, &c.Website, &c.Info,
			&c.Picture, &active, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	c.ID = strconv.FormatInt(numericID, 10)
	c.Append = model.Bool(app)
	c.Recruiting = model.Bool(recr)
	c.Active = model.Bool(active)
	c.Creation = strconv.FormatInt(created, 10)

	rows, err := s.pool.Query(ctx, `
		SELECT m.guid, a.name, m.rank, m.title, a.last_seen,
		       COALESCE(t.tag, ''), COALESCE(t.append, FALSE)
		  FROM clan_members m
		  JOIN accounts a ON a.guid = m.guid
		  LEFT JOIN clans t ON t.id = a.active_clan
		 WHERE m.clan_id = $1
		 ORDER BY m.rank DESC, a.name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := s.now()
	c.Members = []model.Member{}
	for rows.Next() {
		var (
			m        model.Member
			rank     int
			lastSeen int64
			app      bool
		)
		if err := rows.Scan(&m.GUID, &m.Name, &rank, &m.Title, &lastSeen, &m.Tag, &app); err != nil {
			return nil, err
		}
		m.Rank = strconv.Itoa(rank)
		m.Append = model.Bool(app)
		m.Online = online(lastSeen, now)
		c.Members = append(c.Members, m)
	}
	return &c, rows.Err()
}

// ClanSearch matches anywhere in the name, unlike player search which anchors
// at the start -- that is the asymmetry the original had.
func (s *Store) ClanSearch(ctx context.Context, q string) ([]model.ClanSearchHit, error) {
	hits := []model.ClanSearchHit{}
	if strings.TrimSpace(q) == "" {
		return hits, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, name FROM clans
		 WHERE name ILIKE '%' || $1 || '%' AND active
		 ORDER BY name LIMIT 100`, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			h  model.ClanSearchHit
			id int64
		)
		if err := rows.Scan(&id, &h.Name); err != nil {
			return nil, err
		}
		h.ID = strconv.FormatInt(id, 10)
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func (s *Store) ClanHistory(ctx context.Context, id int64) ([]model.HistoryEntry, error) {
	return s.history(ctx, "clan", strconv.FormatInt(id, 10))
}

// CreateClan founds a clan with the caller as its leader.
//
// The tag keeps its surrounding whitespace; only the name is trimmed. Every
// place a tag meets a name concatenates the two with nothing between them --
// server.cs:689 renders the in-game name, and webstuff.cs:12 and :29 the
// browser's link and text forms -- so the separator, if a clan wants one, has
// to be part of the tag. The shipped screens are built for that: the create
// dialog and the tribe properties pane both show a live preview of
// tag-plus-name while you type (webbrowser.cs:677, :2457) and send the field
// unmodified (:167, :2413), trimming only to test it for blankness. Trimming
// here silently deleted the one character a player typed to fix the very thing
// they were previewing.
// The description and the recruiting flag are set here rather than left to a
// follow-up call, because the create dialog is the only place the client ever
// offers them together: it collects both (CreateTribeDescription and
// $CreateTribeRecruiting) and sends them in the one ordinal, with no second
// query behind it. Dropping them meant a tribe founded through the UI came out
// with an empty description and closed recruitment whatever the player typed --
// and the pane they land on next is the profile that shows exactly that.
func (s *Store) CreateClan(ctx context.Context, guid, name, tag string,
	append_, recruiting bool, info string) error {

	name = strings.TrimSpace(name)
	if name == "" || strings.TrimSpace(tag) == "" {
		return refuse("a new clan needs both a name and a tag")
	}

	return s.tx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM clans WHERE LOWER(name) = LOWER($1))`, name).
			Scan(&exists); err != nil {
			return err
		}
		if exists {
			return refuse("a clan with that name already exists")
		}

		var id int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO clans (name, tag, append, recruiting, info, created)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			name, tag, append_, recruiting, info, s.now()).Scan(&id); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO clan_members (clan_id, guid, rank, title, joined)
			VALUES ($1, $2, $3, 'Tribe Admin 1', $4)`,
			id, guid, RankLeader, s.now()); err != nil {
			return err
		}

		if err := s.note(ctx, tx, "clan", strconv.FormatInt(id, 10), "Clan created"); err != nil {
			return err
		}
		return s.note(ctx, tx, "user", guid, "Created clan "+name)
	})
}

// setClanField backs the simple single-value clan setters. minRank differs per
// field: description and website are officer-level, identity is leadership.
func (s *Store) setClanField(ctx context.Context, guid string, id int64,
	column, value string, minRank int, event string) error {

	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := requireRank(ctx, tx, id, guid, minRank); err != nil {
			return err
		}
		// column is never user input -- it comes from the call sites below.
		if _, err := tx.Exec(ctx,
			`UPDATE clans SET `+column+` = $2 WHERE id = $1`, id, value); err != nil {
			return err
		}
		return s.note(ctx, tx, "clan", strconv.FormatInt(id, 10), event)
	})
}

func (s *Store) SetClanInfo(ctx context.Context, guid string, id int64, v string) error {
	return s.setClanField(ctx, guid, id, "info", v, rankToEditInfo, "Changed clan description")
}

func (s *Store) SetClanWebsite(ctx context.Context, guid string, id int64, v string) error {
	return s.setClanField(ctx, guid, id, "website", v, rankToEditInfo, "Changed clan website")
}

func (s *Store) SetClanPicture(ctx context.Context, guid string, id int64, v string) error {
	return s.setClanField(ctx, guid, id, "picture", v, rankToEditInfo, "Changed clan picture")
}

func (s *Store) SetClanName(ctx context.Context, guid string, id int64, v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return refuse("a clan needs a name")
	}
	return s.setClanField(ctx, guid, id, "name", v, rankToRename, "Renamed clan to "+v)
}

func (s *Store) SetClanRecruiting(ctx context.Context, guid string, id int64, on bool) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := requireRank(ctx, tx, id, guid, rankToEditInfo); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE clans SET recruiting = $2 WHERE id = $1`, id, on); err != nil {
			return err
		}
		event := "Closed recruitment"
		if on {
			event = "Opened recruitment"
		}
		return s.note(ctx, tx, "clan", strconv.FormatInt(id, 10), event)
	})
}

// SetClanTag changes the tag, and keeps its whitespace for the reason
// CreateClan gives.
func (s *Store) SetClanTag(ctx context.Context, guid string, id int64, tag string, append_ bool) error {
	if strings.TrimSpace(tag) == "" {
		return refuse("a clan needs a tag")
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := requireRank(ctx, tx, id, guid, rankToRename); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE clans SET tag = $2, append = $3 WHERE id = $1`, id, tag, append_); err != nil {
			return err
		}
		return s.note(ctx, tx, "clan", strconv.FormatInt(id, 10), "Changed clan tag to "+tag)
	})
}

// Invite offers membership to a player.
func (s *Store) Invite(ctx context.Context, guid string, id int64, target string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := requireRank(ctx, tx, id, guid, rankToInvite); err != nil {
			return err
		}

		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM accounts WHERE guid = $1)`, target).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return refuse("no such player")
		}

		if rank, err := rankIn(ctx, tx, id, target); err != nil {
			return err
		} else if rank >= 0 {
			return refuse("player is already a member")
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO clan_invites (clan_id, guid, from_guid, created)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (clan_id, guid) DO NOTHING`,
			id, target, guid, s.now()); err != nil {
			return err
		}
		return s.note(ctx, tx, "clan", strconv.FormatInt(id, 10), "Invited a player")
	})
}

// ClanInvites lists a clan's outstanding invitations.
func (s *Store) ClanInvites(ctx context.Context, guid string, id int64) ([]model.Person, error) {
	if _, err := s.requireRankOutsideTx(ctx, id, guid, rankToInvite); err != nil {
		return nil, err
	}

	// last_seen and created are carried so the view can do what stock's did:
	// grey out whoever is offline, and fill the INVITED column beside the name.
	out := []model.Person{}
	rows, err := s.pool.Query(ctx, `
		SELECT i.guid, a.name, COALESCE(c.tag, ''), COALESCE(c.append, FALSE),
		       a.last_seen, i.created
		  FROM clan_invites i
		  JOIN accounts a ON a.guid = i.guid
		  LEFT JOIN clans c ON c.id = a.active_clan
		 WHERE i.clan_id = $1
		 ORDER BY i.created DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := s.now()
	for rows.Next() {
		var (
			p        model.Person
			app      bool
			lastSeen int64
			created  int64
		)
		if err := rows.Scan(&p.GUID, &p.Name, &p.Tag, &app, &lastSeen, &created); err != nil {
			return nil, err
		}
		p.Append = model.Bool(app)
		p.Online = online(lastSeen, now)
		p.Since = strconv.FormatInt(created, 10)
		p.Hits = "0"
		out = append(out, p)
	}
	return out, rows.Err()
}

// requireRankOutsideTx is the read-path equivalent of requireRank.
func (s *Store) requireRankOutsideTx(ctx context.Context, id int64, guid string, min int) (int, error) {
	var rank int
	err := s.pool.QueryRow(ctx,
		`SELECT rank FROM clan_members WHERE clan_id = $1 AND guid = $2`, id, guid).Scan(&rank)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, refuse("you are not a member of that clan")
	}
	if err != nil {
		return 0, err
	}
	if rank < min {
		return 0, refuse("insufficient rank")
	}
	return rank, nil
}

// AcceptInvite joins the clan, at the bottom rank. It answers with the GUID of
// whoever issued the invitation, who is the one party to the exchange with no
// other way to learn how it ended: the client has no query that lists a tribe's
// answered invitations, so an invitation the recipient has dealt with simply
// disappears from the pane that showed it.
func (s *Store) AcceptInvite(ctx context.Context, guid string, id int64) (string, error) {
	var from string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`SELECT from_guid FROM clan_invites WHERE clan_id = $1 AND guid = $2`,
			id, guid).Scan(&from); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		tagCmd, err := tx.Exec(ctx,
			`DELETE FROM clan_invites WHERE clan_id = $1 AND guid = $2`, id, guid)
		if err != nil {
			return err
		}
		if tagCmd.RowsAffected() == 0 {
			return refuse("no such invitation")
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO clan_members (clan_id, guid, rank, title, joined)
			VALUES ($1, $2, $3, 'Recruit', $4)
			ON CONFLICT (clan_id, guid) DO NOTHING`,
			id, guid, RankRecruit, s.now()); err != nil {
			return err
		}

		if err := s.note(ctx, tx, "clan", strconv.FormatInt(id, 10), "A player joined"); err != nil {
			return err
		}
		return s.note(ctx, tx, "user", guid, "Joined a clan")
	})
	return from, err
}

// RejectInvite answers with the inviter, for the reason AcceptInvite gives.
func (s *Store) RejectInvite(ctx context.Context, guid string, id int64) (string, error) {
	var from string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`SELECT from_guid FROM clan_invites WHERE clan_id = $1 AND guid = $2`,
			id, guid).Scan(&from); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		_, err := tx.Exec(ctx,
			`DELETE FROM clan_invites WHERE clan_id = $1 AND guid = $2`, id, guid)
		return err
	})
	return from, err
}

// LeaveClan removes the caller from a clan, clearing their tag if they were
// wearing that clan's. It answers with the clan's administrators, who are the
// ones left to notice: nothing tells a tribe its roster has shrunk, and the
// leaver is under no obligation to say.
//
// Read before the delete, so a leaving administrator is not in their own list.
func (s *Store) LeaveClan(ctx context.Context, guid string, id int64) ([]string, error) {
	var admins []string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var err error
		if admins, err = clanAdmins(ctx, tx, id, guid); err != nil {
			return err
		}

		cmd, err := tx.Exec(ctx,
			`DELETE FROM clan_members WHERE clan_id = $1 AND guid = $2`, id, guid)
		if err != nil {
			return err
		}
		if cmd.RowsAffected() == 0 {
			return refuse("you are not a member of that clan")
		}
		if err := clearTagIf(ctx, tx, guid, id); err != nil {
			return err
		}
		return s.note(ctx, tx, "user", guid, "Left a clan")
	})
	if err != nil {
		return nil, err
	}
	return admins, nil
}

// clanAdmins lists the members who can act for a clan, skipping one GUID --
// always the player whose own action is being reported, who does not need
// telling. rankToInvite is the same threshold RequestInvite mails.
func clanAdmins(ctx context.Context, tx pgx.Tx, id int64, except string) ([]string, error) {
	rows, err := tx.Query(ctx,
		`SELECT guid FROM clan_members WHERE clan_id = $1 AND rank >= $2 AND guid <> $3`,
		id, rankToInvite, except)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// clearTagIf drops the player's worn tag when they lose membership of the clan
// it came from. Without this they would keep displaying a tag they no longer
// have any right to.
func clearTagIf(ctx context.Context, tx pgx.Tx, guid string, clanID int64) error {
	_, err := tx.Exec(ctx,
		`UPDATE accounts SET active_clan = NULL WHERE guid = $1 AND active_clan = $2`,
		guid, clanID)
	return err
}

// SetRank changes a member's rank and title. A leader cannot promote anyone
// above themselves.
//
// The blank-title check is here because the client's is broken, not as belt and
// braces. SetMemberProfile (webbrowser.cs:632) means to refuse an empty box and
// says so -- "Member Title cannot be blank...really." -- but tests
// strLen(trim(E_Title.getValue)), without the parentheses, so it reads a
// dynamic field of that name rather than calling the method. The result is
// always "", the length is always 0, and the condition is inverted on top of
// that, so the branch it always takes is the one that sends the query. Nothing
// stops a blank title reaching here, and a member whose title is the empty
// string renders as a gap in the roster column with no way to tell it from a
// rendering fault.
// It answers with the rank the member held before, so a caller can tell a
// promotion from a demotion -- and tell both from the title-only edit the same
// dialog sends, which is not news worth mailing anyone.
func (s *Store) SetRank(ctx context.Context, guid string, id int64, target string,
	rank int, title string) (int, error) {

	if rank < RankRecruit || rank > RankLeader {
		return 0, refuse("rank must be an integer 0 to 4")
	}
	if strings.TrimSpace(title) == "" {
		return 0, refuse("A member's title cannot be blank.")
	}

	var before int
	err := s.tx(ctx, func(tx pgx.Tx) error {
		mine, err := requireRank(ctx, tx, id, guid, rankToPromote)
		if err != nil {
			return err
		}
		if rank > mine {
			return refuse("cannot promote above your own rank")
		}

		if before, err = rankIn(ctx, tx, id, target); err != nil {
			return err
		}

		cmd, err := tx.Exec(ctx, `
			UPDATE clan_members SET rank = $3, title = $4
			 WHERE clan_id = $1 AND guid = $2`, id, target, rank, title)
		if err != nil {
			return err
		}
		if cmd.RowsAffected() == 0 {
			return refuse("target is not a member")
		}
		return s.note(ctx, tx, "clan", strconv.FormatInt(id, 10), "Changed a member's rank")
	})
	return before, err
}

// Kick removes a member. Equal or higher rank cannot be kicked, so leaders
// cannot remove each other.
func (s *Store) Kick(ctx context.Context, guid string, id int64, target string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		mine, err := requireRank(ctx, tx, id, guid, rankToPromote)
		if err != nil {
			return err
		}

		theirs, err := rankIn(ctx, tx, id, target)
		if err != nil {
			return err
		}
		if theirs < 0 {
			return refuse("target is not a member")
		}
		if theirs >= mine {
			return refuse("cannot kick a player of equal or higher rank")
		}

		if _, err := tx.Exec(ctx,
			`DELETE FROM clan_members WHERE clan_id = $1 AND guid = $2`, id, target); err != nil {
			return err
		}
		if err := clearTagIf(ctx, tx, target, id); err != nil {
			return err
		}
		return s.note(ctx, tx, "clan", strconv.FormatInt(id, 10), "A member was removed")
	})
}

// Disband records or withdraws one leader's authorisation. The clan is only
// deactivated once every leader has voted, which is what made this an
// "authorise" rather than a "delete" in the original.
//
// It answers with the members to tell, and only on the vote that actually
// disbands the clan -- the earlier authorisations change nothing anyone can
// see. The list is empty every other time, which is what makes "did this
// disband the clan?" answerable without a second query.
func (s *Store) Disband(ctx context.Context, guid string, id int64, authorise bool) ([]string, error) {
	var told []string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		told = nil
		if _, err := requireRank(ctx, tx, id, guid, rankToDisband); err != nil {
			return err
		}

		if !authorise {
			if _, err := tx.Exec(ctx,
				`DELETE FROM clan_disband_votes WHERE clan_id = $1 AND guid = $2`, id, guid); err != nil {
				return err
			}
			return s.note(ctx, tx, "clan", strconv.FormatInt(id, 10),
				"A leader withdrew disband authorisation")
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO clan_disband_votes (clan_id, guid, created) VALUES ($1, $2, $3)
			ON CONFLICT (clan_id, guid) DO NOTHING`, id, guid, s.now()); err != nil {
			return err
		}

		var leaders, votes int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM clan_members WHERE clan_id = $1 AND rank = $2`,
			id, RankLeader).Scan(&leaders); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM clan_disband_votes v
			  JOIN clan_members m ON m.clan_id = v.clan_id AND m.guid = v.guid
			 WHERE v.clan_id = $1 AND m.rank = $2`, id, RankLeader).Scan(&votes); err != nil {
			return err
		}

		if votes >= leaders && leaders > 0 {
			// Before the update, while the roster is still whole.
			members, err := clanMembers(ctx, tx, id, guid)
			if err != nil {
				return err
			}
			told = members
			if _, err := tx.Exec(ctx,
				`UPDATE clans SET active = FALSE WHERE id = $1`, id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`UPDATE accounts SET active_clan = NULL WHERE active_clan = $1`, id); err != nil {
				return err
			}
			return s.note(ctx, tx, "clan", strconv.FormatInt(id, 10), "Clan disbanded")
		}
		return s.note(ctx, tx, "clan", strconv.FormatInt(id, 10),
			"A leader authorised disbanding")
	})
	if err != nil {
		return nil, err
	}
	return told, nil
}

// clanMembers lists a clan's whole roster, skipping one GUID for the reason
// clanAdmins does.
func clanMembers(ctx context.Context, tx pgx.Tx, id int64, except string) ([]string, error) {
	rows, err := tx.Query(ctx,
		`SELECT guid FROM clan_members WHERE clan_id = $1 AND guid <> $2`, id, except)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
