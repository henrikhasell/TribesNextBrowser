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
		addr     = flag.String("addr", envOr("TNB_ADDR", ":8080"), "listen address")
		dsn      = flag.String("dsn", envOr("TNB_DSN", ""), "PostgreSQL connection string")
		upstream = flag.String("upstream", envOr("TNB_UPSTREAM", auth.DefaultUpstream), "TribesNext endpoint used to verify sessions")
		ttl      = flag.Duration("verify-ttl", 10*time.Minute, "how long a verified session is cached")
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

	srv := &api.Server{
		Store:    store.New(pool),
		Verifier: auth.NewVerifier(*upstream, *ttl),
		Log:      log,
	}

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("listening", "addr", *addr, "upstream", *upstream)
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
