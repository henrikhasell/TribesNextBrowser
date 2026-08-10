// Package store is the database layer, and the only place clan permission
// rules are enforced.
//
// The client shows or hides administration controls by rank, but that is a
// convenience: nothing there can be trusted, since a player can call any method
// directly. Every rule below is therefore checked again here, and the rank
// thresholds match what TribesNext's backend enforced (recorded in
// tools/mockserver.py, which was written against the published PHP).
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Ranks. The numbers are part of the wire protocol -- clanrank takes an
// "integer 0 to 4" -- so they are named here but stored and sent as integers.
const (
	RankRecruit = 0
	RankMember  = 1
	RankOfficer = 2
	RankSenior  = 3
	RankLeader  = 4
)

// Rank required for each clan action.
const (
	rankToEditInfo = RankOfficer
	rankToInvite   = RankOfficer
	rankToPromote  = RankSenior
	rankToRename   = RankSenior
	rankToDisband  = RankLeader
)

// UserError is a refusal the player should see, as opposed to a fault. The API
// layer turns it into {"status":"error","msg":...}; anything else becomes a
// 500 and is logged, because it means we are broken rather than they are.
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func refuse(format string, args ...any) error {
	return &UserError{Msg: fmt.Sprintf(format, args...)}
}

// ErrNotFound is returned for a subject that does not exist. Read methods turn
// it into an empty result, matching the original API: viewing a missing clan
// answers `[]` rather than an error.
var ErrNotFound = errors.New("not found")

type Store struct {
	pool *pgxpool.Pool
	// now is swappable so tests can pin timestamps.
	now func() int64
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, now: func() int64 { return time.Now().Unix() }}
}

// SetClock pins the clock, for tests.
func (s *Store) SetClock(f func() int64) { s.now = f }

func (s *Store) Now() int64 { return s.now() }

// tx runs fn in a transaction, rolling back on any error. Every mutating
// method uses it: a clan action that half-applies (member removed, history not
// written) would be worse than one that fails outright.
func (s *Store) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// rankIn reports the caller's rank in a clan, or -1 if they are not a member.
func rankIn(ctx context.Context, q pgx.Tx, clanID int64, guid string) (int, error) {
	var rank int
	err := q.QueryRow(ctx,
		`SELECT rank FROM clan_members WHERE clan_id = $1 AND guid = $2`,
		clanID, guid).Scan(&rank)
	if errors.Is(err, pgx.ErrNoRows) {
		return -1, nil
	}
	if err != nil {
		return 0, err
	}
	return rank, nil
}

// requireRank is the gate every clan administration method passes through.
func requireRank(ctx context.Context, q pgx.Tx, clanID int64, guid string, min int) (int, error) {
	rank, err := rankIn(ctx, q, clanID, guid)
	if err != nil {
		return 0, err
	}
	if rank < 0 {
		return 0, refuse("you are not a member of that clan")
	}
	if rank < min {
		return 0, refuse("insufficient rank")
	}
	return rank, nil
}

// note appends to the audit trail behind userhistory/clanhistory.
func (s *Store) note(ctx context.Context, q pgx.Tx, subjectType, subjectID, event string) error {
	_, err := q.Exec(ctx,
		`INSERT INTO history (subject_type, subject_id, event, at) VALUES ($1, $2, $3, $4)`,
		subjectType, subjectID, event, s.now())
	return err
}

// EnsureAccount records a player the first time we see them and refreshes the
// display name, which is authoritative from TribesNext rather than from us.
//
// Called on every verified request, so a player exists here as soon as they log
// in once -- there is no separate registration step.
//
// Reports whether THIS call created the account, which is what makes the
// welcome mail exactly-once: of two concurrent first requests, one INSERTs and
// the other takes the DO UPDATE branch, so only one sees xmax = 0. The cost of
// claiming it here rather than after a successful delivery is that a failed
// welcome is never retried -- acceptable for a greeting that must never stop a
// new player from using the service.
func (s *Store) EnsureAccount(ctx context.Context, guid, name string) (bool, error) {
	var created bool
	err := s.pool.QueryRow(ctx, `
		INSERT INTO accounts (guid, name, created, last_seen)
		VALUES ($1, $2, $3, $3)
		ON CONFLICT (guid) DO UPDATE
		   SET name = EXCLUDED.name, last_seen = EXCLUDED.last_seen
		RETURNING (xmax = 0)`,
		guid, name, s.now()).Scan(&created)
	return created, err
}

// Online reports whether a player is currently on a game server.
//
// Presence is not modelled yet: nothing reports it to this backend. Rather than
// invent a value, treat "seen in the last five minutes" as online, which is
// true for the player making the request and honest-ish for everyone else. When
// the server-side mod starts reporting connects, this becomes a real lookup.
func online(lastSeen, now int64) int {
	if lastSeen > 0 && now-lastSeen < 300 {
		return 1
	}
	return 0
}
