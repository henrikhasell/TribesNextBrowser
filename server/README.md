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

## Identity: checked, not borrowed

This server holds no passwords and no private keys, and it contacts nobody to
decide who you are.

A TribesNext account is an RSA keypair with a certificate — `name`, `guid`, `e`,
`n`, and TribesNext's signature over the four — and that certificate is a
statement anyone can check, because the signature is made with a key whose
public half is compiled into every copy of `IFC22.dll`. Checking it is exactly
what a game server does to a connecting player (`t2csri/serverSide.cs:103-128`),
and it is what this server does now.

The deployment this assumes: **one backend, many game servers**. Players'
clients and every participating Tribes 2 server point at the same address, which
is why `tools/build-vl2.sh --host` bakes it into both packages at once.

```
POST /session   guid=…&cert=<the five fields>&nonce=<hex>
     <- CHALLENGE: <(nonce || challenge) encrypted with the account's key>

POST /session   guid=…&response=<the challenge half, decrypted>
     <- UUID: <token>

POST /session   guid=…&uuid=<token>          every ten minutes
     <- REFRESHED, or TIMEOUT if it lapsed
```

The certificate proves TribesNext issued that name and GUID for that key. It
proves nothing about who is *sending* it — a certificate is public, and the
client hands it to every game server it joins — so the challenge establishes
possession of the private half. Both halves are needed and neither is
sufficient.

Consequences, all deliberate:

- **Nothing upstream has to be reachable.** TribesNext could vanish tomorrow and
  logins here would carry on, which is the point of the exercise.
- **There is no revocation.** A certificate is valid forever, so an account
  banned or deleted upstream still authenticates here. A local ban list is the
  answer if that ever matters; asking TribesNext is not.
- **Registration dates come from first sighting.** A certificate does not carry
  one, and the profile pane needs a date, so an account is dated from the first
  time this server saw it.
- **The session token is a bearer credential**, but now only for this server —
  it means nothing to TribesNext. Fine on a LAN; see Transport below before
  exposing it.

### The pinned key

`internal/auth/key.go` carries TribesNext's signing key: exponent 3, a 4096-bit
modulus, fingerprint `4d80b2ee…c8473788` (sha256 of the modulus hex, printed at
startup). It was recovered arithmetically rather than extracted, because raw RSA
over a bare SHA-1 makes that possible: with `sig^3 ≡ sha1(name \t guid \t e \t n)
(mod N)`, `N` divides `sig^3 - m` for every certificate, and the gcd over a few
of them is `N` exactly. `internal/auth/auth_test.go` re-checks it against a real
certificate on every run, so a wrong constant cannot pass unnoticed.

## Clan tags are signed, not looked up

A game server renders a player's clan tag from `getAuthInfo()`, synchronously,
inside `onConnect`. The game-server mod used to fill that record by asking this
server over HTTP while holding the connection open. It now checks a certificate
the player carries:

```
KeyID <TAB> Issued <TAB> Expire <TAB> GUID <TAB> HexBlob <TAB> Sig
```

`HexBlob` is the same record `/cert` serves; `Sig` is raw RSA over a bare SHA-1
of fields 0–4, which is what the engine's `rsa_mod_exp` can check. The GUID is
bound to the one TribesNext's own authentication phase established a moment
earlier, which is why no certificate chain is needed: we assert only the clan,
and somebody else has already vouched for the identity.

Set it up once:

```sh
./tnserver -genkey clan.pem          # writes clan.pem and clan.pem.pub.cs
./tnserver -dsn ... -clan-key clan.pem -clan-key-id 1
```

`-clan-key` takes either a path or the PEM itself, and `TNB_CLAN_KEY` the same,
because a platform hands secrets over as environment variables and gives you no
filesystem to write one to — App Platform runs this from a distroless image with
no volume. The deployed value is in [.do/app.yaml](../.do/app.yaml) in App
Platform's encrypted form, which only that app can decrypt.

The public half goes to game servers, either committed to
`TNBrowserServer/tnbserver/clankeys.cs` or baked at package time with
`tools/build-vl2.sh --clan-key clan.pem.pub.cs`. It verifies signatures and
cannot make them, so it is not a secret.

Certificates last 30 minutes by default (`-clan-cert-ttl`). Expiry is the only
freshness mechanism — there is no revocation — so it is also what stops a player
who left a clan wearing its tag. It can be that short because **nothing here can
keep anybody out of a game**: a bad signature, an expired certificate, a key the
game server has never heard of, or no certificate at all all mean the same
thing, which is a player with no tag. That is what makes key rotation something
this server can do alone — stamp a new `-clan-key-id`, and game servers that
have not been updated show no tags until they are.

Game servers must run **without** `-nologin`. TribesNext's authentication phase
is what establishes the GUID a certificate is bound to, and it only runs when
`$PlayingOnline` is set, which omitting that flag does.

## Chat

`internal/chat` is an IRC server for exactly one client: the one in
`scripts/ChatGui.cs`, which is a hundred TorqueScript functions over a plaintext
`TCPObject`. The mod replaces two of those functions — `IRCClient::send` and
`IRCTCP::onLine` — so the lines this server produces reach a completely
unmodified chat client over TLS.

Three properties of that client shape everything here, and each has a test:

- **`IRCClient::dispatch` is a closed switch.** A verb it does not know is
  printed to the player as `(cmd:) …`, so there is no welcome burst, no `366`
  after a `353`, and no `PING`. `chat_test.go` asserts every emitted line
  against the transcribed dispatch table.
- **Several client actions raise a wait state** that only specific replies
  clear. A `LIST` with no `323` after it, or a `MODE +b` with no `368`, leaves
  the chat panes titled `CONNECTING` for the rest of the session. `WHO` and
  `WHOIS` are answered even for a nick this server has never heard of, because
  the alternative strands them.
- **`NICK` corrupts the client's person table**, so identity is fixed for the
  life of a connection and a tag change applies on the next one.

Rooms are public ones from `-chat-rooms`, ad-hoc ones anyone can open, private
messages, and `#<Tribe>_Public` / `#<Tribe>_Private` per clan — the names the
shipped `JoinPublicTribeChannel` builds, which is why they are addressed by the
tribe's name with spaces escaped as `_-_01` rather than by its id. Membership is
checked against `clan_members`; rank 2 and above gets channel operator, the same
rung the shipped screens treat as administrative.

**Nothing is stored.** Messages exist in a 256-line ring per connection, which
is there so a stream that drops can resume from its cursor rather than lose what
arrived, and nowhere else. A connection outlives its stream by 30 seconds, so a
reconnect is invisible to everyone else in the room.

State is in memory, which is what `auth.Sessions` already is and what
`instance_count: 1` in `.do/app.yaml` already assumes. Chat is the first thing a
scale-out would break — two instances would each hold half the room — and the
fix then is `LISTEN`/`NOTIFY` or sticky routing, not a table.

Switch it off with `-chat=false`; both routes then answer 404 and the CHAT tab
is an empty room, with nothing else affected.

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
| `-session-ttl` | | how long a session survives unused, default 30m |
| `-dev-trust-guid` | `TNB_DEV_TRUST_GUID` | **insecure**: accept any guid without proof, for the test suites |
| `-migrate` | `TNB_MIGRATE` | apply pending migrations and exit, instead of serving |
| `-chat` | `TNB_CHAT` | serve the chat hub, default on; `TNB_CHAT=0` switches it off |
| `-chat-rooms` | `TNB_CHAT_ROOMS` | comma-separated rooms that always exist, default `Tribes2,Pickup,Newbies` |

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

One address is all the client needs now: sessions and data come from the same
place. Every control the client offers is offered unconditionally — this backend
serves them all, and the client reports a refusal if any backend ever cannot.

## Logs

One line per request on stderr, `slog` text format:

```
level=INFO msg=listening addr=:8080 authkey=4d80b2eec618e086e9cf5b3d24bda0e835c7bed592365307fb005565c8473788
level=INFO msg="session established" guid=4510186 name=orange01
level=INFO msg=request verb=POST path=/db status=200 bytes=135 dur=6ms remote=127.0.0.1:35650 q="array 1" guid=4510186
level=WARN msg=request verb=POST path=/db status=401 bytes=56 dur=0s remote=127.0.0.1:35656
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
- `/clancert` — the signed clan record a player carries into a game. Session
  authenticated, because it says who the holder is and may therefore only be
  handed to them. Answers 404 when the server was started without `-clan-key`,
  which is a supported deployment: everything else serves and players simply
  carry no tag. See [Clan tags](#clan-tags-are-signed-not-looked-up).
- `/chat/stream` — a held-open response carrying IRC lines to one player, one
  per line as `<seq>\t<line>`. Session authenticated. See [Chat](#chat).
- `/chat/send` — the lines going the other way, as a `lines` form field with one
  per line. Answers 204 with no body at all, so anything in the body is an
  error; 409 means the player has no stream open, which is not an
  authentication failure and should not be treated as one.
- `/tn/server/authinfo` — not a client endpoint, and no longer used by the mod;
  the clan-tag lookup the game-server mod made before certificates replaced it.
  Kept because the deployment probe above uses it to prove a request reached Go
  and Postgres rather than an edge cache. Unauthenticated: everything it returns
  — a name and a clan tag — is on the scoreboard of every server that player
  joins.

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
