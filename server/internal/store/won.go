package store

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Reads and writes shaped for the WON stored-procedure ordinals.
//
// The rest of this package answers questions like "what is this clan?"; these
// answer "what goes in this row?". They are separate because the row schemas
// belong to the shipped client's parsers -- a field exists because
// webbrowser.cs:1470 indexes it -- and folding them into the general queries
// would let a parser's quirk leak into everything else.
//
// The other reason they are separate: almost every ordinal is keyed on a NAME.
// getTribeMembers takes a tribe name, getWarriorProfile and
// getWarriorTribeList take a player name. Nothing in the shipped scripts sends
// an id where a name will do, so the resolvers below are load-bearing rather
// than a convenience.

// Quad is the four identity fields that appear all over the row schemas: name,
// tag, append flag, uid. getLinkName and getTextName (webstuff.cs:3, :19)
// decode it -- when append is set the tag follows the name, otherwise it
// precedes -- and webbrowser.cs:271 compares field 3 against the certificate's
// own field 3, which is what makes these four a unit rather than four
// unrelated fields.
type Quad struct {
	Name   string
	Tag    string
	Append bool
	GUID   string
}

// unknownQuad keeps a row well-formed when the account behind it is gone.
//
// Mail from a deleted account still has to render, and a short row would shift
// every later field by one -- which shows up as a plausible-looking pane with
// the wrong data in it, the worst possible failure here.
func unknownQuad(guid string) Quad {
	return Quad{Name: "(unknown)", Append: true, GUID: guid}
}

const quadSQL = `
	SELECT a.name, COALESCE(c.tag, ''), COALESCE(c.append, FALSE)
	  FROM accounts a
	  LEFT JOIN clans c ON c.id = a.active_clan
	 WHERE a.guid = $1`

func (s *Store) Quad(ctx context.Context, guid string) (Quad, error) {
	q := Quad{GUID: guid}
	err := s.pool.QueryRow(ctx, quadSQL, guid).Scan(&q.Name, &q.Tag, &q.Append)
	if errors.Is(err, pgx.ErrNoRows) {
		return unknownQuad(guid), nil
	}
	if err != nil {
		return Quad{}, err
	}
	return q, nil
}

// FindUser resolves the display name the client typed.
//
// The address book and the browser both send display names, and getLinkName
// may have decorated one with a tribe tag on either side before the player
// clicked it, so a name that does not match directly is retried with each
// known tag stripped from whichever end it sits on.
func (s *Store) FindUser(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrNotFound
	}

	var guid string
	err := s.pool.QueryRow(ctx,
		`SELECT guid FROM accounts WHERE LOWER(name) = LOWER($1)`, name).Scan(&guid)
	if err == nil {
		return guid, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	rows, err := s.pool.Query(ctx, `SELECT tag FROM clans WHERE tag <> ''`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return "", err
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	for _, tag := range tags {
		var bare string
		switch {
		case strings.HasPrefix(name, tag):
			bare = strings.TrimPrefix(name, tag)
		case strings.HasSuffix(name, tag):
			bare = strings.TrimSuffix(name, tag)
		default:
			continue
		}
		err := s.pool.QueryRow(ctx,
			`SELECT guid FROM accounts WHERE LOWER(name) = LOWER($1)`, bare).Scan(&guid)
		if err == nil {
			return guid, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
	}
	return "", ErrNotFound
}

// FindClan resolves a tribe name. Several ordinals send an id where the tuple
// says name (ordinal 25 and 30 both do), so a bare number is tried as an id
// first -- a tribe cannot be named as a plain integer, because the create path
// requires a name the client has already validated as text.
func (s *Store) FindClan(ctx context.Context, nameOrID string) (int64, error) {
	nameOrID = strings.TrimSpace(nameOrID)
	if nameOrID == "" {
		return 0, ErrNotFound
	}

	var id int64
	if n := atoi64(nameOrID); n > 0 {
		err := s.pool.QueryRow(ctx,
			`SELECT id FROM clans WHERE id = $1 AND active`, n).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
	}

	err := s.pool.QueryRow(ctx,
		`SELECT id FROM clans WHERE LOWER(name) = LOWER($1) AND active`,
		nameOrID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

func atoi64(s string) int64 {
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int64(r-'0')
	}
	return n
}

//-----------------------------------------------------------------------------
// Profiles
//-----------------------------------------------------------------------------

// WarriorProfile is the payload of scalar 23, which carries it in the status
// rather than in rows.
type WarriorProfile struct {
	Q          Quad
	Registered int64
	Online     bool
	URL        string
	Graphic    string
	Info       string
}

func (s *Store) WarriorProfile(ctx context.Context, guid string) (*WarriorProfile, error) {
	var (
		p        WarriorProfile
		lastSeen int64
	)
	p.Q.GUID = guid
	err := s.pool.QueryRow(ctx, `
		SELECT a.name, COALESCE(c.tag, ''), COALESCE(c.append, FALSE),
		       a.created, a.last_seen, a.website, a.graphic, a.info
		  FROM accounts a
		  LEFT JOIN clans c ON c.id = a.active_clan
		 WHERE a.guid = $1`, guid).
		Scan(&p.Q.Name, &p.Q.Tag, &p.Q.Append, &p.Registered, &lastSeen,
			&p.URL, &p.Graphic, &p.Info)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Online = online(lastSeen, s.now()) == 1
	return &p, nil
}

// TribeProfile is the payload of scalar 22, likewise carried in the status.
type TribeProfile struct {
	ID         int64
	Name       string
	Tag        string
	Append     bool
	Recruiting bool
	Graphic    string
	Info       string
}

func (s *Store) TribeProfile(ctx context.Context, id int64) (*TribeProfile, error) {
	var p TribeProfile
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, tag, append, recruiting, picture, info
		  FROM clans WHERE id = $1`, id).
		Scan(&p.ID, &p.Name, &p.Tag, &p.Append, &p.Recruiting, &p.Graphic, &p.Info)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

//-----------------------------------------------------------------------------
// Lists
//-----------------------------------------------------------------------------

// MemberRow is one row of array 6, the tribe roster the browser and the mail
// address book both read at different depths.
type MemberRow struct {
	Q      Quad
	Title  string
	Rank   int
	Joined int64
	Online bool
}

func (s *Store) TribeMembers(ctx context.Context, clanID int64) ([]MemberRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.guid, a.name, COALESCE(c.tag, ''), COALESCE(c.append, FALSE),
		       m.title, m.rank, m.joined, a.last_seen
		  FROM clan_members m
		  JOIN accounts a ON a.guid = m.guid
		  LEFT JOIN clans c ON c.id = a.active_clan
		 WHERE m.clan_id = $1
		 ORDER BY m.rank DESC, a.name`, clanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := s.now()
	out := []MemberRow{}
	for rows.Next() {
		var (
			m        MemberRow
			lastSeen int64
		)
		if err := rows.Scan(&m.Q.GUID, &m.Q.Name, &m.Q.Tag, &m.Q.Append,
			&m.Title, &m.Rank, &m.Joined, &lastSeen); err != nil {
			return nil, err
		}
		m.Online = online(lastSeen, now) == 1
		out = append(out, m)
	}
	return out, rows.Err()
}

// InviteRow is one row of array 11. Both directions live in the same table:
// Kind tells an invitation the tribe sent from a request a warrior made.
type InviteRow struct {
	ID      int64
	Created int64
	From    Quad
	To      Quad
	Kind    string
	Online  bool
}

func (s *Store) TribeInvites(ctx context.Context, clanID int64) ([]InviteRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT i.id, i.created, i.kind, i.from_guid, i.guid, a.last_seen
		  FROM clan_invites i
		  JOIN accounts a ON a.guid = i.guid
		 WHERE i.clan_id = $1
		 ORDER BY i.created DESC`, clanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := s.now()
	type raw struct {
		r                InviteRow
		fromGUID, toGUID string
	}
	var pending []raw
	for rows.Next() {
		var (
			v        raw
			lastSeen int64
		)
		if err := rows.Scan(&v.r.ID, &v.r.Created, &v.r.Kind,
			&v.fromGUID, &v.toGUID, &lastSeen); err != nil {
			return nil, err
		}
		v.r.Online = online(lastSeen, now) == 1
		pending = append(pending, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := []InviteRow{}
	for _, v := range pending {
		from, err := s.Quad(ctx, v.fromGUID)
		if err != nil {
			return nil, err
		}
		to, err := s.Quad(ctx, v.toGUID)
		if err != nil {
			return nil, err
		}
		v.r.From, v.r.To = from, to
		out = append(out, v.r)
	}
	return out, nil
}

// TribeRow is one row of array 13, a warrior's tribe list.
type TribeRow struct {
	Name  string
	ID    int64
	Rank  int
	Title string
}

func (s *Store) WarriorTribes(ctx context.Context, guid string) ([]TribeRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.name, c.id, m.rank, m.title
		  FROM clan_members m
		  JOIN clans c ON c.id = m.clan_id
		 WHERE m.guid = $1 AND c.active
		 ORDER BY c.name`, guid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TribeRow{}
	for rows.Next() {
		var t TribeRow
		if err := rows.Scan(&t.Name, &t.ID, &t.Rank, &t.Title); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) SearchWarriors(ctx context.Context, q string, start, count int) ([]Quad, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.guid, a.name, COALESCE(c.tag, ''), COALESCE(c.append, FALSE)
		  FROM accounts a
		  LEFT JOIN clans c ON c.id = a.active_clan
		 WHERE a.name ILIKE '%' || $1 || '%'
		 ORDER BY a.name
		 OFFSET $2 LIMIT $3`, q, start, count)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Quad{}
	for rows.Next() {
		var v Quad
		if err := rows.Scan(&v.GUID, &v.Name, &v.Tag, &v.Append); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// TribeHit is one row of array 4. The client renders fields 1 and 2 as
// "<name> - <tag>" and keeps field 1 as the tab name (webbrowser.cs:316).
type TribeHit struct {
	ID   int64
	Name string
	Tag  string
}

func (s *Store) SearchTribes(ctx context.Context, q string, start, count int) ([]TribeHit, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, tag FROM clans
		 WHERE active AND name ILIKE '%' || $1 || '%'
		 ORDER BY name
		 OFFSET $2 LIMIT $3`, q, start, count)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TribeHit{}
	for rows.Next() {
		var t TribeHit
		if err := rows.Scan(&t.ID, &t.Name, &t.Tag); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// OnlineFor answers scalar 69: one flag per guid, in the order asked.
func (s *Store) OnlineFor(ctx context.Context, guids []string) ([]bool, error) {
	out := make([]bool, len(guids))
	if len(guids) == 0 {
		return out, nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT guid, last_seen FROM accounts WHERE guid = ANY($1)`, guids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := s.now()
	seen := map[string]bool{}
	for rows.Next() {
		var (
			guid     string
			lastSeen int64
		)
		if err := rows.Scan(&guid, &lastSeen); err != nil {
			return nil, err
		}
		seen[guid] = online(lastSeen, now) == 1
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i, g := range guids {
		out[i] = seen[g]
	}
	return out, nil
}

//-----------------------------------------------------------------------------
// Mail
//-----------------------------------------------------------------------------

// MailRow is one row of array 1 and array 14, which share a schema.
type MailRow struct {
	ID        int64
	From      Quad
	To        Quad
	Created   int64
	IsCC      bool
	IsDeleted bool
	IsRead    bool
	ToList    string
	CCList    string
	Subject   string
	Body      string
}

const mailSelect = `
	SELECT m.id, m.from_guid, m.to_guid, m.sent, m.is_cc,
	       m.folder = 'deleted', NOT m.unread, m.to_list, m.cc_list,
	       m.subject, m.body
	  FROM mail m
	 WHERE m.owner_guid = $1 AND m.folder = $2`

// MailSince answers array 1, and the sequence argument is not optional.
//
// The pane sends the highest id it has cached ($EmailNextSeq, webemail.cs:387)
// and caches whatever comes back. A server that ignores it hands the same
// messages over on every poll and the inbox grows without bound -- found by
// running the pane against a stand-in that did exactly that, which listed two
// messages four times.
func (s *Store) MailSince(ctx context.Context, guid string, seq int64) ([]MailRow, error) {
	return s.mailRows(ctx, mailSelect+` AND m.id > $3 ORDER BY m.id`,
		guid, "inbox", seq)
}

func (s *Store) MailDeleted(ctx context.Context, guid string) ([]MailRow, error) {
	return s.mailRows(ctx, mailSelect+` ORDER BY m.id`, guid, "deleted")
}

func (s *Store) mailRows(ctx context.Context, sql string, args ...any) ([]MailRow, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type raw struct {
		m                MailRow
		fromGUID, toGUID string
	}
	var pending []raw
	for rows.Next() {
		var v raw
		if err := rows.Scan(&v.m.ID, &v.fromGUID, &v.toGUID, &v.m.Created,
			&v.m.IsCC, &v.m.IsDeleted, &v.m.IsRead, &v.m.ToList, &v.m.CCList,
			&v.m.Subject, &v.m.Body); err != nil {
			return nil, err
		}
		pending = append(pending, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := []MailRow{}
	for _, v := range pending {
		if v.m.From, err = s.Quad(ctx, v.fromGUID); err != nil {
			return nil, err
		}
		if v.m.To, err = s.Quad(ctx, v.toGUID); err != nil {
			return nil, err
		}
		out = append(out, v.m)
	}
	return out, nil
}

// MarkMailRead answers scalar 7. It is fire-and-forget on the client -- the
// only call site in the five scripts that passes neither a proxy object nor a
// key -- so nothing here is ever reported back.
func (s *Store) MarkMailRead(ctx context.Context, guid string, id int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE mail SET unread = FALSE WHERE id = $1 AND owner_guid = $2`, id, guid)
	return err
}

// PurgeMail answers scalar 35: gone, not moved.
func (s *Store) PurgeMail(ctx context.Context, guid string, id int64) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM mail WHERE id = $1 AND owner_guid = $2`, id, guid)
	return err
}

// Deliver writes one message into one mailbox, with no sender-side copy and no
// block check.
//
// This is how the server speaks to a player. Tribe invitations and join
// requests have no other channel -- the client has no query that lists a
// player's own invitations, so the links in these bodies are the only way an
// invitation can be answered at all. Blocking cannot apply: it is a rule about
// other players, and dropping an invitation because the tribe's admin is on a
// block list would break the feature silently.
func (s *Store) Deliver(ctx context.Context, toGUID, fromGUID, fromName, subject, body string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		toName := ""
		if err := tx.QueryRow(ctx,
			`SELECT name FROM accounts WHERE guid = $1`, toGUID).Scan(&toName); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return refuse("no such player")
			}
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO mail (owner_guid, from_guid, from_name, to_guid, to_name,
			                  subject, body, sent, unread, folder, to_list)
			VALUES ($1, $2, $3, $1, $4, $5, $6, $7, TRUE, 'inbox', $4)`,
			toGUID, fromGUID, fromName, toName, subject, body, s.now())
		return err
	})
}

//-----------------------------------------------------------------------------
// Writes the ordinals need and the JSON API never had
//-----------------------------------------------------------------------------

// SetGraphic answers scalar 31. The value is a path into the client's own
// texture volume; nothing is uploaded and nothing is validated here beyond its
// being text, because the list the dialog offers is built from the player's own
// files and a different install legitimately has different ones.
func (s *Store) SetGraphic(ctx context.Context, guid, path string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE accounts SET graphic = $2 WHERE guid = $1`, guid, path)
	return err
}

// RequestInviteWindow is how long a refused repeat lasts. Seven days is the
// harness's figure and the point of it is that a recruiting tribe's admins do
// not get mailed the same request nightly.
const RequestInviteWindow = 7 * 24 * 60 * 60

// RequestInvite answers scalar 34: a warrior asking a recruiting tribe to
// invite them. Returns the admins who should be told.
func (s *Store) RequestInvite(ctx context.Context, guid string, clanID int64) ([]string, error) {
	var admins []string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var recruiting bool
		if err := tx.QueryRow(ctx,
			`SELECT recruiting FROM clans WHERE id = $1 AND active`, clanID).
			Scan(&recruiting); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return refuse("that tribe no longer exists")
			}
			return err
		}
		if !recruiting {
			return refuse("that tribe is not recruiting")
		}

		var member bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM clan_members WHERE clan_id = $1 AND guid = $2)`,
			clanID, guid).Scan(&member); err != nil {
			return err
		}
		if member {
			return refuse("you are already a member of that tribe")
		}

		var since int64
		err := tx.QueryRow(ctx,
			`SELECT created FROM clan_invites WHERE clan_id = $1 AND guid = $2`,
			clanID, guid).Scan(&since)
		switch {
		case err == nil && s.now()-since < RequestInviteWindow:
			return refuse("you have already asked that tribe recently")
		case err == nil:
			if _, err := tx.Exec(ctx,
				`DELETE FROM clan_invites WHERE clan_id = $1 AND guid = $2`,
				clanID, guid); err != nil {
				return err
			}
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO clan_invites (clan_id, guid, from_guid, created, kind)
			VALUES ($1, $2, $2, $3, 'request')`, clanID, guid, s.now()); err != nil {
			return err
		}

		rows, err := tx.Query(ctx,
			`SELECT guid FROM clan_members WHERE clan_id = $1 AND rank >= $2`,
			clanID, rankToInvite)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var g string
			if err := rows.Scan(&g); err != nil {
				return err
			}
			admins = append(admins, g)
		}
		return rows.Err()
	})
	return admins, err
}

// CancelInvite answers the "cancel" verb of scalar 28: the tribe withdrawing an
// invitation, or a warrior withdrawing their own request.
func (s *Store) CancelInvite(ctx context.Context, guid string, clanID int64, target string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if target != guid {
			if _, err := requireRank(ctx, tx, clanID, guid, rankToInvite); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx,
			`DELETE FROM clan_invites WHERE clan_id = $1 AND guid = $2`, clanID, target)
		return err
	})
}

// AdmitRequester is the accept verb of scalar 28 read the other way round: an
// admin letting in the warrior who asked, rather than a warrior accepting an
// invitation. Same ordinal, same three verbs, opposite direction.
func (s *Store) AdmitRequester(ctx context.Context, guid string, clanID int64, target string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := requireRank(ctx, tx, clanID, guid, rankToInvite); err != nil {
			return err
		}

		var kind string
		err := tx.QueryRow(ctx,
			`SELECT kind FROM clan_invites WHERE clan_id = $1 AND guid = $2`,
			clanID, target).Scan(&kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return refuse("there is no outstanding request from that warrior")
		}
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO clan_members (clan_id, guid, rank, title, joined)
			VALUES ($1, $2, $3, '', $4)
			ON CONFLICT (clan_id, guid) DO NOTHING`,
			clanID, target, RankRecruit, s.now()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM clan_invites WHERE clan_id = $1 AND guid = $2`,
			clanID, target); err != nil {
			return err
		}
		return s.note(ctx, tx, "clan", strconv.FormatInt(clanID, 10),
			"admitted "+target)
	})
}

// CanInvite reports whether the caller may answer invitations on the tribe's
// behalf. It is what tells an admin answering a join request apart from a
// warrior answering an invitation, which is the whole of ordinal 28's
// ambiguity -- the wire is identical either way.
func (s *Store) CanInvite(ctx context.Context, guid string, clanID int64) (bool, error) {
	var rank int
	err := s.pool.QueryRow(ctx,
		`SELECT rank FROM clan_members WHERE clan_id = $1 AND guid = $2`,
		clanID, guid).Scan(&rank)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return rank >= rankToInvite, nil
}

// AdminTribeID is the tribe WON kept its staff in. webbrowser.cs:409 and :2248
// grant cross-tribe moderator powers to anyone whose certificate contains a
// tribe with this id and an admin level above 1 -- so membership of one
// particular tribe was the whole of WON's staff model, and it is reproduced
// rather than replaced because the client already draws the controls for it.
const AdminTribeID = 1401

func (s *Store) IsStaff(ctx context.Context, guid string) (bool, error) {
	var rank int
	err := s.pool.QueryRow(ctx,
		`SELECT rank FROM clan_members WHERE clan_id = $1 AND guid = $2`,
		AdminTribeID, guid).Scan(&rank)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return rank > 1, nil
}

// LogAdminAction records scalar 62 and 63. The eight-way selector those two
// carry is named nowhere in the shipped scripts, so it is stored rather than
// interpreted: guessing at a meaning would be worse than keeping the evidence.
func (s *Store) LogAdminAction(ctx context.Context, guid string, ordinal, selector int, args string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO admin_actions (actor_guid, ordinal, selector, args, created)
		VALUES ($1, $2, $3, $4, $5)`, guid, ordinal, selector, args, s.now())
	return err
}
