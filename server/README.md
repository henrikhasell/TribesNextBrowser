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

A player proves who they are to TribesNext exactly as before — the RSA
challenge/response the client already performs, with no password typed — and
sends the resulting `(guid, uuid)` pair here. This server then asks TribesNext
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

psql "postgres://tnbrowser:tnbrowser@127.0.0.1:5433/tnbrowser" -f migrations/0001_init.sql

go build -o tnserver ./cmd/tnserver
./tnserver -dsn "postgres://tnbrowser:tnbrowser@127.0.0.1:5433/tnbrowser"
```

| flag | env | meaning |
|---|---|---|
| `-addr` | `TNB_ADDR` | listen address, default `:8080` |
| `-dsn` | `TNB_DSN` | PostgreSQL connection string, required |
| `-upstream` | `TNB_UPSTREAM` | TribesNext endpoint used to verify sessions |
| `-verify-ttl` | | how long a verified session is cached, default 10m |

Point the client at it from the game console:

```
$TNB::Host = "http://your-host:8080";
$TNB::FullFeatures = 1;
```

or bake both into the packages so installing is a single file copy:

```sh
../tools/build-vl2.sh --host "http://your-host:8080" --full-features \
                      --server-host "http://your-host:8080"
```

`$TNB::AuthHost` stays on TribesNext — that is where the account lives.
`$TNB::FullFeatures` un-hides the controls only this backend can serve.

## Transport

Plain HTTP by default, which works immediately on a LAN. For anything
internet-facing put it behind a reverse proxy holding a real certificate: the
patched client verifies TLS against `curl-ca-bundle.crt`, so a self-signed
certificate is rejected outright and there is no client-side way to relax that.

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
