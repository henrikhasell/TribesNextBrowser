# TNBrowser backend

A self-hosted community server for Tribes 2: the browser, clan and mail
interfaces the game's own screens speak, plus the clan-tag lookup a game server
needs.

It exists because three things cannot be fixed from the client against
TribesNext's backend — mail send is refused, account rename is disabled, and the
clan tag cannot reach the game because the DCE's signing certificate has
expired. Owning the backend removes all three, and restores the features the
original WON screens had that TribesNext dropped: buddy lists, block lists and
the Sent/Deleted mail folders.

## Identity: borrowed, not owned

This server holds no passwords, no keys and no account records of its own.

This is the *only* thing the client still asks TribesNext for. A player proves
who they are there exactly as before — the RSA challenge/response the client
already performs, with no password typed — and sends the resulting `(guid, uuid)`
pair here. Browser, clan and mail traffic goes nowhere near TribesNext.

The deployment this assumes: **one backend, many game servers**. Players' clients
and every participating Tribes 2 server point at the same address, which is why
`tools/build-vl2.sh --host` bakes it into both packages at once. This server then asks TribesNext
whether that pair is a live session:

```
GET https://tribesnext.thyth.com/tn/json/json_browser.php
      ?guid=<guid>&uuid=<uuid>&method=userview&payload={"id":"<guid>"}
```

`200` means genuine, `401` means not. `tn_challenge_isAuthorized($guid, $uuid)`
checks both together upstream, so a token cannot be replayed against a different
GUID. The same call returns the authoritative display name, so verifying and
identifying are one round trip.

Consequences, both deliberate:

- **TribesNext must be reachable for anyone to log in here.** Verified pairs are
  cached for ten minutes, matching the client's session refresh, so steady-state
  traffic is one upstream call per player per session — but an outage there is
  an outage here.
- **The session token is a bearer credential.** Anyone who captures it can use
  it against TribesNext itself, not just this server. Fine on a LAN; see
  Transport below before exposing it.

## Running it

```sh
docker run -d --name tnb-postgres \
  -e POSTGRES_PASSWORD=tnbrowser -e POSTGRES_USER=tnbrowser -e POSTGRES_DB=tnbrowser \
  -p 5433:5432 postgres:16-alpine

go build -o tnserver ./cmd/tnserver
./tnserver -dsn "postgres://tnbrowser:tnbrowser@127.0.0.1:5433/tnbrowser" -migrate
./tnserver -dsn "postgres://tnbrowser:tnbrowser@127.0.0.1:5433/tnbrowser"
```

| flag | env | meaning |
|---|---|---|
| `-addr` | `TNB_ADDR` | listen address, default `:8080` |
| `-dsn` | `TNB_DSN` | PostgreSQL connection string, required |
| `-upstream` | `TNB_UPSTREAM` | TribesNext endpoint used to verify sessions |
| `-verify-ttl` | | how long a verified session is cached, default 10m |
| `-migrate` | `TNB_MIGRATE` | apply pending migrations and exit, instead of serving |

`migrations/` is compiled into the binary, so `-migrate` needs no psql and no
copy of this repository — which is what lets the deployed image be a distroless
one with no shell in it. Files are applied in filename order, each in its own
transaction, and recorded in `schema_migrations` so a second run does nothing.
Running it against a database migrated by hand before this existed is safe: the
initial schema is written with `IF NOT EXISTS` throughout, so it records the row
it was missing and moves on.

Serving never migrates. With more than one instance that would mean every
instance racing to change the schema underneath the ones already answering
requests, so it is a separate run that either finishes first or fails the
deploy.

Point the client at it from the game console:

```
$TNB::Host = "http://your-host:8080";
```

or bake both into the packages so installing is a single file copy:

```sh
../tools/build-vl2.sh --host "http://your-host:8080"
```

`$TNB::AuthHost` stays on TribesNext — that is where the account lives. Every
control the client offers is offered unconditionally: this backend serves them
all, and the client reports a refusal if any backend ever cannot.

## Logs

One line per request on stderr, `slog` text format:

```
level=INFO msg=request verb=GET path=/tn/json/json_browser.php status=200 bytes=135 dur=6ms remote=127.0.0.1:35650 api=userview guid=4510186
level=WARN msg=request verb=GET path=/tn/json/json_browser.php status=401 bytes=56 dur=0s remote=127.0.0.1:35656
```

`api` is the `method` parameter, so a browser or mail call says which one it was.
4xx logs at WARN and 5xx at ERROR, because a 401 here is routine -- the client
treats it as "session expired" and logs in again -- while a 500 is a bug.

The `uuid` is never logged. It is a bearer credential that works against
TribesNext itself, not only here, so a log holding one would be as sensitive as
a password store. The `guid` is logged: it names a player but grants nothing.

## Transport

Plain HTTP by default, which works immediately on a LAN. For anything
internet-facing put it behind a reverse proxy holding a real certificate: the
patched client verifies TLS against `curl-ca-bundle.crt`, so a self-signed
certificate is rejected outright and there is no client-side way to relax that.

## Deploying

`.do/app.yaml` is a DigitalOcean App Platform specification: one service, one
pre-deploy migration job, an existing managed PostgreSQL cluster, and a custom
domain. It is deployed from CI rather than on push, so an untested commit cannot
reach players -- see `.github/workflows/server.yml`.

```sh
doctl apps create --spec .do/app.yaml            # first time
doctl apps update <app-id> --spec .do/app.yaml   # afterwards
```

That arrangement answers Transport above without a reverse proxy to run: App
Platform terminates TLS and renews the certificate itself.

Do not assume which CA that is. The note above about Let's Encrypt verifying
cleanly was written against the TribesNext host; App Platform issues from
**Google Trust Services** instead -- `GTS WE1`, under `GTS Root R4`, itself
cross-signed by `GlobalSign Root CA`. That only works here because the shipped
`curl-ca-bundle.crt` carries `GTS Root R4` directly (119 roots, including the
ISRG pair and three GTS roots), so the chain validates without leaning on the
cross-signature.

It is checked, not reasoned about, because there is no client-side way to
relax it -- an unverifiable certificate would lock every player out at once:

```
new HTTPObject(Probe);
Probe.get("https://tnb.k8s.henrik.si", "/tn/server/authinfo?guid=987654321", "");
-> (unknown)		1	987654321
```

`authinfo` rather than `/healthz` on purpose: the edge will serve a cached
`/healthz` without ever troubling the origin, so a 200 from it proves the
handshake and nothing else. The line above came back through Go and Postgres.

Three things it depends on that live outside this repository, and none of which
fail loudly if they are wrong:

- The managed cluster is named `tnb-db` and sits in the same region as the app.
  A cluster in another region binds fine and then pays a round trip per query.
- The app is in the cluster's **trusted sources**. The connection string is
  supplied by the binding either way, so a database open to the internet looks
  identical from here.
- The `k8s.henrik.si` zone is not being pruned by an external-dns policy of
  `sync`, which would delete the record App Platform creates.

The image itself is ordinary, so none of this is required: `docker build
-t tnserver server/` and a `TNB_DSN` will run it anywhere.

## API

Paths and shapes are TribesNext's, so the client needs no new code:

- `/tn/json/json_browser.php` — the 26 documented methods, plus `buddylist`,
  `buddyadd`, `buddyremove`, `buddyclear`, `blocklist`, `blockadd`,
  `blockremove`
- `/tn/json/json_mail.php` — `count`, `read`, `delete`, `send`, with an optional
  `folder` on read and delete
- `/tn/server/authinfo` — not a client endpoint; the game-server mod's clan
  lookup. Unauthenticated: a game server holds no player token, and everything
  it returns — a name and a clan tag — is on the scoreboard of every server that
  player joins. It is read-only and reveals nothing a game client could not.

Two behavioural differences from TribesNext, both intentional:

- **`send` works.** Theirs refuses every payload shape.
- **`username` is refused**, and says why: the account name belongs to
  TribesNext and is refreshed here on every verified request, so changing it
  locally would be undone within the minute.

## Permissions

Rank rules are enforced in `internal/store`, never in the client. The client
hides controls by rank for convenience, but a player can call any method
directly, so every rule is checked again server-side:

| action | minimum rank |
|---|---|
| clan description, website, picture, recruiting, invite | 2 (Officer) |
| clan name, tag | 3 (Senior Admin) |
| set member rank, kick | 3, and never above/equal to your own |
| authorise disband | 4 (Leader), and every leader must agree |

Wearing a clan's tag requires membership of it, and losing membership clears it.

## Tests

```sh
go test ./...
```

They run against a real PostgreSQL, because what is worth testing here is the
SQL — rank gates, cascades, transactional mutations. Set `TNB_TEST_DSN` to point
elsewhere; the tests truncate every table, so do not aim them at real data.

The client-side suites in `../tests` are the conformance check: pointing
`$TNB::Host` at this server and running `../tools/run-tests.sh` exercises the
whole protocol from inside the real game.
