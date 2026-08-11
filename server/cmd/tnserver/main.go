// Command tnserver is a self-hosted community backend for Tribes 2.
//
// It serves the browser, clan and mail interfaces the game's own screens speak,
// so a client running the TNBrowser mod can point at it instead of TribesNext.
// It owns no identities: a player proves who they are with a live TribesNext
// session, verified upstream.
//
// Plain HTTP by default. The client verifies TLS against curl-ca-bundle.crt, so
// a self-signed certificate is rejected outright -- for a public deployment put
// this behind a reverse proxy holding a real certificate.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/henrik/tnbrowser-server/internal/api"
	"github.com/henrik/tnbrowser-server/internal/auth"
	"github.com/henrik/tnbrowser-server/internal/migrate"
	"github.com/henrik/tnbrowser-server/internal/store"
)

func main() {
	var (
		addr = flag.String("addr", envOr("TNB_ADDR", ":8080"), "listen address")
		dsn  = flag.String("dsn", envOr("TNB_DSN", ""), "PostgreSQL connection string")
		ttl  = flag.Duration("session-ttl", 30*time.Minute,
			"how long a session survives without being used")
		// The in-game suites run in containers that hold no account key
		// material, so a client in one cannot answer a challenge. This lets
		// them through on a bare GUID. It is announced loudly at startup
		// because a deployment that has it on is one where anybody can be
		// anybody.
		trustGUID = flag.Bool("dev-trust-guid", envOr("TNB_DEV_TRUST_GUID", "") != "",
			"INSECURE: accept any guid without proof, for the test suites")
		// Also readable from TNB_MIGRATE so a deployment can select this mode
		// with an environment variable alone. The App Platform pre-deploy job
		// runs the image's default command, and overriding that command would
		// mean relying on how the platform quotes it against an image with no
		// shell in it.
		migrateOnly = flag.Bool("migrate", envOr("TNB_MIGRATE", "") != "",
			"apply pending schema migrations and exit")
	)
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if *dsn == "" {
		log.Error("no database configured; set -dsn or TNB_DSN")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		log.Error("connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Error("database unreachable", "err", err)
		os.Exit(1)
	}

	// Migrating is a separate run, not something serving does on startup. With
	// more than one instance, startup migration means every instance racing to
	// change the schema underneath the ones already serving requests; as its
	// own step it either succeeds before any new instance takes traffic, or
	// fails the deploy and leaves the running one alone.
	if *migrateOnly {
		if err := migrate.Apply(ctx, pool, log); err != nil {
			log.Error("migrate", "err", err)
			os.Exit(1)
		}
		return
	}

	if *trustGUID {
		log.Warn("-dev-trust-guid is set: any guid is accepted without proof")
	}

	srv := &api.Server{
		Store:     store.New(pool),
		Sessions:  auth.NewSessions(*ttl),
		Log:       log,
		TrustGUID: *trustGUID,
	}

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("listening", "addr", *addr, "authkey", auth.Fingerprint())
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("serve", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
