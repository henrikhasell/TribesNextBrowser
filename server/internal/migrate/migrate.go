// Package migrate applies the embedded schema files to a database.
//
// It exists because there is nowhere to run psql any more. The server is
// deployed as a distroless image with no shell and no Postgres client, so the
// binary has to be able to migrate itself -- `tnserver -migrate` is what the
// App Platform pre-deploy job runs, and it is the same command a developer runs
// against a local container.
//
// Deliberately small. Migrations are applied in filename order, each in its own
// transaction, and recorded so they are not applied twice. There is no "down":
// a rollback here would mean deleting player data on a bad deploy, which is
// never what you want at 2am.
package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/henrik/tnbrowser-server/migrations"
)

// lockKey is an arbitrary constant identifying this application's migration
// lock. Advisory locks share one namespace per database, so the number only has
// to be one no other tool picks; it is never stored.
const lockKey = 0x746e6272 // "tnbr"

// Apply runs every migration not yet recorded as applied.
//
// Safe to run against a database migrated by hand before this existed: the
// initial schema is written entirely with IF NOT EXISTS, so re-applying it is a
// no-op that simply records the row it was missing.
func Apply(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	files, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	// Everything runs on one connection because the advisory lock below is
	// session-scoped: taken on a different connection than the migration, it
	// would guard nothing.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()

	// Serialise concurrent migrators. The pre-deploy job is a single instance
	// so this should never contend, but "should never" is doing a lot of work
	// in a sentence about two processes both running CREATE TABLE.
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx),
			"SELECT pg_advisory_unlock($1)", lockKey)
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
		    name       TEXT PRIMARY KEY,
		    checksum   TEXT NOT NULL,
		    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied := 0
	for _, f := range files {
		// fs.ReadDir returns entries sorted by filename, which is the ordering
		// the NNNN_ prefix is for.
		name := f.Name()
		if f.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}

		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		sum := sha256.Sum256(body)
		checksum := hex.EncodeToString(sum[:])

		var seen string
		err = conn.QueryRow(ctx,
			"SELECT checksum FROM schema_migrations WHERE name = $1", name).Scan(&seen)
		switch {
		case err == nil && seen == checksum:
			continue
		case err == nil:
			// An applied migration was edited afterwards. The database no
			// longer matches the file that claims to describe it, and applying
			// it again would not fix that -- so stop rather than guess.
			return fmt.Errorf("%s was modified after it was applied "+
				"(recorded %s, file %s); add a new migration instead",
				name, seen[:12], checksum[:12])
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("check %s: %w", name, err)
		}

		if err := applyOne(ctx, conn, name, string(body), checksum); err != nil {
			return err
		}
		log.Info("migration applied", "name", name)
		applied++
	}

	log.Info("migrations up to date", "applied", applied, "total", len(files))
	return nil
}

// applyOne runs a single migration and records it in the same transaction, so a
// failure part-way leaves neither the schema change nor the record behind.
//
// This assumes no migration needs a statement that cannot run inside a
// transaction (CREATE INDEX CONCURRENTLY being the one that usually forces the
// issue). None does today; the day one does, it needs its own path.
func applyOne(ctx context.Context, conn *pgxpool.Conn, name, body, checksum string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, body); err != nil {
		return fmt.Errorf("apply %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO schema_migrations (name, checksum) VALUES ($1, $2)",
		name, checksum); err != nil {
		return fmt.Errorf("record %s: %w", name, err)
	}
	return tx.Commit(ctx)
}
