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
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/henrik/tnbrowser-server/internal/api"
	"github.com/henrik/tnbrowser-server/internal/auth"
	"github.com/henrik/tnbrowser-server/internal/chat"
	"github.com/henrik/tnbrowser-server/internal/clancert"
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

		// Clan certificates. Optional throughout: without a key the feature is
		// off, /clancert answers 404, and everything else serves as before.
		clanKey = flag.String("clan-key", envOr("TNB_CLAN_KEY", ""),
			"clan signing key: a path to a PEM file, or the PEM itself")
		clanKeyID = flag.Int("clan-key-id", envIntOr("TNB_CLAN_KEY_ID", 1),
			"key number stamped into issued certificates, so a game server can pick the right public key")
		clanTTL = flag.Duration("clan-cert-ttl", DefaultClanTTL(),
			"how long an issued clan certificate stays valid")
		genKey = flag.String("genkey", "",
			"write a new clan signing key to this path, print the mod settings, and exit")

		// Chat. On by default and needs no configuration: unlike the clan key
		// there is no secret to hold, and a community server with the CHAT tab
		// dead is the thing this replaced.
		chatOn = flag.Bool("chat", envOr("TNB_CHAT", "1") != "0",
			"serve the community chat hub on /chat/stream and /chat/send")
		chatRooms = flag.String("chat-rooms", envOr("TNB_CHAT_ROOMS", "Tribes2,Pickup,Newbies"),
			"comma-separated rooms that always exist, listed even when empty")
	)
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Before the database: generating a key is a local, offline errand and an
	// operator doing it should not need a DSN to hand.
	if *genKey != "" {
		if err := writeClanKey(*genKey, *clanKeyID, *clanTTL); err != nil {
			log.Error("genkey", "err", err)
			os.Exit(1)
		}
		return
	}

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

	// A configured key that will not load is fatal: the operator asked for this
	// and a silent fallback to "no tags for anyone" is the kind of failure that
	// gets diagnosed a week later.
	var signer *clancert.Signer
	if *clanKey != "" {
		// A path on a machine somebody administers, the PEM itself when a
		// platform hands it over as an environment variable and gives you no
		// filesystem to put it on.
		if clancert.IsPEM(*clanKey) {
			signer, err = clancert.Parse([]byte(*clanKey), *clanKeyID, *clanTTL)
		} else {
			signer, err = clancert.Load(*clanKey, *clanKeyID, *clanTTL)
		}
		if err != nil {
			log.Error("clan signing key", "err", err)
			os.Exit(1)
		}
		log.Info("clan certificates enabled",
			"keyid", signer.KeyID(), "key", signer.Fingerprint(), "ttl", signer.TTL())
	}

	var hub *chat.Hub
	if *chatOn {
		rooms := splitList(*chatRooms)
		hub = chat.New(log, rooms)
		log.Info("chat enabled", "rooms", rooms)
	}

	srv := &api.Server{
		Store:     store.New(pool),
		Sessions:  auth.NewSessions(*ttl),
		Log:       log,
		TrustGUID: *trustGUID,
		ClanCerts: signer,
		Chat:      hub,
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

// splitList reads a comma-separated setting, dropping empties so that
// "a,,b," and "a,b" mean the same thing and "" means none at all.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// DefaultClanTTL is the flag default, read from the environment first so a
// deployment can set it without editing a command line.
func DefaultClanTTL() time.Duration {
	if v := os.Getenv("TNB_CLAN_CERT_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return clancert.DefaultTTL
}

// writeClanKey is the -genkey errand: make a key, write it where nothing else
// can read it, and print the two lines that go into the game-server mod.
//
// Printing the settings rather than the numbers is the point. The alternative
// is an operator transcribing 512 hex characters by hand and discovering the
// typo as tags that silently never appear.
func writeClanKey(path string, keyID int, ttl time.Duration) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite a signing key", path)
	}

	key, err := clancert.Generate(clancert.KeyBits)
	if err != nil {
		return fmt.Errorf("generating a key: %w", err)
	}
	if err := os.WriteFile(path, clancert.MarshalPEM(key), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	// The public half goes beside it, as the exact file the mod wants. That is
	// what tools/build-vl2.sh --clan-key takes, so the whole path from key to
	// installed package is two commands and no transcription.
	signer := clancert.FromKey(key, keyID, ttl)
	pub := path + ".pub.cs"
	if err := os.WriteFile(pub, []byte(clanKeyHeader+signer.ModSettings()+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", pub, err)
	}

	fmt.Printf("Wrote %s -- the signing key. Keep it secret; nothing but this server needs it.\n", path)
	fmt.Printf("Wrote %s -- the public half, for game servers.\n\n", pub)
	fmt.Printf("Serve with:\n    tnserver -clan-key %s -clan-key-id %d\n\n", path, keyID)
	fmt.Printf("Build the game-server package with:\n    tools/build-vl2.sh --clan-key %s\n\n", pub)
	fmt.Printf("Or paste these into a loose autoexec.cs beside the .vl2:\n\n%s\n", signer.ModSettings())
	return nil
}

const clanKeyHeader = `// TNBrowserServer -- the public keys clan certificates are checked against.
//
// Written by "tnserver -genkey". These are public: they verify a signature and
// cannot make one. Keep an old key's lines when adding a new one, so
// certificates issued under it stay checkable until they expire.

`
