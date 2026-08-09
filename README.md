# TNBrowser — TribesNext community browser and mail for Tribes 2

## What this is

Tribes 2 shipped with in-game community screens: player profiles, clan ("tribe")
pages with rosters and administration, a player/clan search, and tmail. They are
still in every install — the BROWSER and EMAIL buttons on the launch shell open
them — but they talk to Sierra's WON backend through the engine call
`DatabaseQuery()`. WON shut down in 2003, so today those buttons lead to:

> There was an error processing your request, please wait a few moments and try
> again.

TribesNext rebuilt that backend. This mod rebuilds the screens against it, so the
community browser works again from inside the game.

It ships as a drop-in `.vl2`. The stock scripts and GUIs are left completely
untouched: everything here is `TNB`-prefixed and the shell's BROWSER and EMAIL
buttons are re-pointed with a package, so `deactivatePackage(TNBrowser)` restores
the original behaviour exactly.

## Three parts

| | what it is | who needs it |
|---|---|---|
| `TNBrowser/` | the client mod: the community screens | every player |
| `server/` | a Go backend serving browser, clan and mail data | whoever hosts the community |
| `TNBrowserServer/` | a server-side mod that puts clan tags in player names | game-server operators |

The client reads and writes its data through the backend, and contacts
TribesNext for exactly one thing: the RSA login that proves who you are. That
split is why the backend exists — the three server-side limitations below are
TribesNext's, and owning the data removes all of them while restoring the WON-era
features TribesNext dropped (buddy lists, block lists, Sent/Deleted mail folders,
working mail send).

`$TNB::Host` defaults to `http://localhost:8080` and is normally baked to your
central backend's address at build time; `$TNB::AuthHost` stays TribesNext and is
used by nothing but the login.

Identity stays with TribesNext either way: players log in there with their RSA
key as they always have, and a self-hosted backend verifies the resulting session
token by asking TribesNext about it. No passwords, no key extraction, no second
account system. See `server/README.md`.

## Requirements

- A **TribesNext-patched client**, which supplies the two things the mod cannot
  work without: an `HTTPObject` reimplemented on libcurl that can speak HTTPS
  (stock Torque's is plain HTTP only), and `t2csri_rsa_decrypt()` /
  `$LoginCertificate` for the passwordless session.
- You must be **logged in to a TribesNext account**. The session is negotiated
  with your account's RSA key; no password is ever typed into the mod.

## Installing

```sh
./tools/build-vl2.sh                                   # -> dist/TNBrowser.vl2
cp dist/TNBrowser.vl2 <Tribes2>/GameData/MyMod/
```

then launch with `-mod MyMod`. The engine indexes a mod directory's `.vl2`
archives alongside its loose files, so nothing needs unpacking — verified by
running a mod directory containing only the archive. For development you can
equally copy the `TNBrowser/` source tree into `GameData/` and use
`-mod TNBrowser`.

### Configuring it

Put a plain `autoexec.cs` in the mod directory, beside the archive:

```
GameData/MyMod/TNBrowser.vl2
GameData/MyMod/autoexec.cs      <- your settings
```

```
$TNB::Host = "http://your-backend:8080";
```

The game execs every mod's `scripts/autoexec/*.cs` first and `autoexec.cs` last
(`base/console_end.cs`), so a plain assignment there overrides the mod's
defaults. Nothing needs unpacking, and being a loose file it survives replacing
the `.vl2` with a newer build.

It must be inside the **mod** directory, not `GameData/` — `exec()` resolves
through the mod path, so a file at the GameData root is never found. Both
placements were tested; only the mod directory works.

Copyable starting points are in `examples/`, for the client and the server mod.

### Drop-in packaging

For something you hand to someone else, bake the settings into the archive at
build time instead, so installing is copying one file:

```sh
./tools/build-vl2.sh --host "http://your-backend:8080"
```

One `--host` covers both packages: the client reads its data there and the game
servers look clans up there, because in a normal deployment that is one central
machine. If your game servers must reach it at a different address than players
do, set `$TNBS::Host` in the game server's `autoexec.cs` — the baked value keeps
its `if empty` guard, so the override still wins.

Then `cp dist/TNBrowserServer.vl2 <Tribes2>/GameData/<the active mod>/` is the
entire install — nothing to create, nothing to edit. Verified with a mod
directory containing only the archive: it booted already configured and fetched
a clan tag from the backend.

The archive has to be in the mod the *server* runs. Installing it into one
Tribes 2 directory and hosting from another is the obvious way to see no tags at
all, because nothing then asks the backend and the game falls back to the dead
WON path.

It loads two ways, so it does not depend on any single boot path: once from
`scripts/autoexec/tnbserver.cs` during console init, and again from
`scripts/TNBrowserServerGame.cs`, which `CreateServer()` execs on every server
start (`base/scripts/server.cs` scans `scripts/*Game.cs` across the mod path
stack, archives included). Whichever fires first loads it; the other is a no-op.

Baking works from a copy, so the source tree is untouched, and the settings keep
their `if empty` guard, so a loose `autoexec.cs` can still override a baked
value later.

---

## What is implemented

### Player profiles
Name and active tag, join date, online state, website, profile text, and clan
memberships with rank and title. Clickable links move between players and clans.
On your own profile: edit profile text and website, change which clan tag you
wear, and view account history — the EDIT sub-tab opens the recreated player
properties dialog.

### Clans
Profile, member roster with rank/title/online state, outstanding invitations,
and clan history. Administration, gated by the rank the server enforces:

- description, name, website, tag (with prepend/append side), recruiting toggle,
  clan picture
- invite a player, set a member's rank (0–4) and title, remove a member
- authorise or withdraw a disband

### Search
Players and clans, also used to pick an invite target or a mail recipient.

### Mail
Inbox with unread markers, reading, delete, and a compose/reply window.

### Coverage
**All 26 documented browser API methods are implemented and reachable from the
UI.** Two of them (`username`, and `userclan` in the sense of the tag actually
showing) cannot presently succeed for server-side reasons explained below.

### Screens recreated
Each is derived from the shipped `.gui`, so geometry, control classes and shell
profiles match the original and the screens read as part of the game:

| Stock file | Recreated as |
|---|---|
| `TribeAndWarriorBrowserGui.gui` | `TNBrowserGui.gui` |
| `TribePropertiesDlg.gui` | `TNBClanPropsDlg.gui` |
| `TribeAdminMemberDlg.gui` | `TNBMemberAdminDlg.gui` |
| `BrowserSearchDlg.gui` | `TNBSearchDlg.gui` |
| `BrowserEditInfoDlg.gui` | `TNBEditInfoDlg.gui` |
| `CreateTribeDlg.gui` | `TNBCreateClanDlg.gui` |
| `EmailGui.gui` | `TNBMailGui.gui` |
| `EmailComposeDlg.gui` | `TNBComposeDlg.gui` |
| `WarriorPropertiesDlg.gui` | `TNBPlayerPropsDlg.gui` |
| *(no stock equivalent)* | `TNBPromptDlg.gui`, a one-line input box |

Every one is derived from the shipped file by renaming objects and re-pointing
`command =` strings, never by re-laying-out. Every layout-bearing line
(`position`, `extent`, `minExtent`, sizing, `profile`) is **identical to the
original — zero differences across all nine.**

The roster's right-click menu is recreated too (`TNBRosterPopup`): View Profile,
Send Mail, and for officers Edit Rank and Title / Kick from Clan. Like the stock
one it is built in script rather than shipped as a `.gui`. Buddy and block list
entries are dropped — the API has neither.

---

## What is not implemented, and why

Most of what follows is a limitation of **TribesNext's** backend, not of this
mod, and running your own backend (see below) removes it:

| | against TribesNext | against your own backend |
|---|---|---|
| send mail | refused by the server | **works** |
| clan tag in your name | impossible (expired signing certificate) | **works**, with `TNBrowserServer` |
| where clan/mail writes go | TribesNext | your backend — TribesNext is only asked to log you in |
| Sent / Deleted folders | no folder concept exists | **works** |
| buddy list, block list | no such methods | **works** |
| account rename | disabled during the beta | deliberately not implemented |
| player picture | no method | no method |

Only the last two are genuine "will not" rather than "cannot". The detail
follows.

### Your clan tag appearing in your in-game name — blocked server-side

Choosing which tag you wear (`userclan`) is only half the job, and this caught
me out: game servers never talk to a central system, so the client has to hand
them a signed **community certificate** issued by the DCE. That certificate is
what annotates your account with the current name, clan and tag, and it is where
`getAuthInfo()` — and therefore the name a server displays — gets them from.

The shipped TribesNext client *sends* `$T2CSRI::CommunityCertificate` to servers
(`t2csri/clientSide.cs`) but **never fetches it**; the only reference in
`t2csri.vl2` is the read. Only the 2017 tournament client ever set it. So on a
stock install that global is empty, `t2csri_sendCommunityCert()` returns early,
and no tag can appear.

This mod does **not** try. It used to: `tnbrowser/cert.cs` fetched the
certificate from `/tn/robot/robot_browser.php`, cached the DCE certificates and
refreshed before expiry. It was removed, because the live DCE answers an
authenticated request with:

    ERR: Signer validity period has expired.

Its signing certificate has lapsed. Confirmed with a valid session — the same
session's `json_browser` calls return 200, and an unauthenticated request still
answers `UNAUTHENTICATED`, so the error comes from the issuance path itself.

**This is not the site's TLS certificate**, and relaxing TLS does not help: the
error is byte-identical with strict verification, with `curl -k`, and over plain
HTTP with no TLS at all. The host's Let's Encrypt certificate is valid and
verifies cleanly. What has expired is the DCE's RSA *signing* certificate — the
delegation cert the community system uses to sign community certificates, checked
inside the application after authentication (the same trust chain
`t2csri_verify_deleg_signature` exists for).

**Nor can the failure be bypassed.** Game servers verify the whole chain
themselves (`t2csri/serverSideClans.cs`): the DCE certificate's signature against
the delegation root public key compiled into `IFC22.dll`, the DCE's own validity
window, and then the community certificate's signature using the DCE key. The
first two failures disconnect the client outright ("DCE is not signed by
authoritative root", "Community cert is not signed by a known/valid DCE"). So
there is nothing to ignore client-side — no certificate is ever issued — and a
forged one would need the delegation private key and would still be rejected.

**`TNBrowserServer` solves it without certificates.** A game server running that
mod asks your backend for the record directly
(`/tn/server/authinfo?guid=<player>`) and writes it into
`%client.t2csri_authInfo` itself, which is where the stock `server.cs` reads the
tag from. No signing, no chain, nothing for an expired certificate to break.
Verified end to end: a real logged-in client hosting a listen server produced
`cached tag for 4510186 [[TC]]` then `applied late tag to orange01`.

That leaves one honest limitation: servers *not* running `TNBrowserServer` show
you untagged, exactly as a stock TribesNext client is. The certificate was the
only mechanism that would have federated, and it has not been issuable since its
signer expired.

### Sending mail — refused by the server

The compose and reply windows are fully built and wired, but **sending does not
work, and cannot be made to work from the client.** About thirty payload and
parameter spellings were tried against both mail endpoints, with a real
authenticated session:

- `json_mail.php` `send` → `500 Invalid Parameters`, for every shape
- `robot_mail.php` `send` → `INVALID_RECIP`, for every shape

The official TribesNext client patches also disable the EMAIL launch tab
outright. The window is kept because receiving works, and because the code is
correct the day the server accepts a send; compose reports whatever the server
actually says rather than pretending the message went out.

**FORWARD** and **REPLY ALL** are hidden for the same reason — both need a
working send. `TNBMailForward` is implemented behind the hidden button.

### Message field names are inferred

The account inbox is empty and no message can be created without send, so the
per-message field names could never be observed. `TNBMailField` accepts several
plausible spellings for each field (`from`/`sender`/`name`, …) and degrades to a
blank column rather than a broken screen. It is the one place to adjust if a real
message ever arrives using different names.

### Account rename (`username`) — deliberately not implemented

The control is hidden and the method refuses, on both backends and by decision
rather than omission.

Your account name belongs to your TribesNext account. Theirs disables renames
during the beta; a self-hosted backend only *caches* the name it learns while
verifying a session, and refreshes it on every request, so a local change would
be silently undone within the minute. A button that could only ever mislead is
worse than no button.

### Mail block lists, sender tracking, Sent and Deleted folders

Hidden. The stock EmailGui offers all of these; the TribesNext mail API exposes
only `count`, `read`, `delete` and `send`. There is no folder concept at all —
`count` ignores its folder parameter and always reports the inbox.

### The clan properties SECURITY pane

Hidden. In the original it configured which rank was allowed to perform which
action. TribesNext ranks carry only a number (0–4) and a free-text title, and the
server decides permissions itself, so the controls would have nothing to write
to.

### The player picture

`WarriorPropertiesDlg` is recreated, but its GFX pane is hidden. The API has
`clanpicture` and no user-picture equivalent, so those controls would have
nothing to write to — the same reason the clan dialog's SECURITY pane is
hidden.

### Clan picture upload

The original GFX pane uploaded a JPEG to WON, with size and dimension limits.
`clanpicture` instead stores a path to an image the game already has, so the pane
lists the shipped clan artwork rather than uploading.

---

## How authentication works

No password is typed into the mod. It runs the RSA challenge/response against
`/tn/robot/robot_login.php`: it sends its GUID and a random nonce, decrypts the
challenge the server returns using the account key, checks that the leading bytes
replay the nonce it sent (proving the server answered *this* request), and
returns the remainder. The server issues a session UUID, which then authorises
every call. The session refreshes every ten minutes and backs off quadratically
on failure.

That the same UUID also authorises the JSON APIs was the design's central
assumption; it is confirmed live, below.

## Verification

Two suites, both run inside the real game with the real client:

- **against the mock** (`./tools/run-tests.sh`) — the TribesNext-shaped backend,
  with the extras hidden as they are for a player using thyth's server.
  **201 assertions, 0 failures**: 63 parser, 36 API/session, 70 GUI, 32 mail.
- **against the Go backend** (`./tools/run-conformance.sh`) — **137 assertions,
  0 failures**: 36 API/session, 70 GUI, 31 mail. The same suites, the same
  fixtures, a different server. A failure here is a real behavioural difference
  between the two backends, which is exactly what it is for.

Plus `go test ./...` in `server/`, which runs against a real PostgreSQL because
what is worth testing there is the SQL: rank gates, cascades, transactions.

Confirmed against the live backend with a real account (`tools/live-check.sh`):

- `LOGIN=SUCCESS`, real 1024-bit certificate
- 32-character session UUID from the robot endpoint after the RSA exchange
- `RESULT=ok` from `json_browser.php` — **a robot-issued UUID does authorise the
  JSON API**, so the passwordless login and the documented methods compose, and
  the tab-delimited `robot_browser.php` fallback is not needed

The live backend differs from its documentation in three ways, each now covered
by a regression test: absent strings come back as JSON `null` rather than `""`,
`online` is a bare number rather than a quoted string, and the response body is
prefixed with a blank line.

The mail API is undocumented and its source is not published; its method set was
established by probing the live server (unknown methods answer `501`, valid ones
`200`):

| method | behaviour |
|---|---|
| `count` | message count, as a JSON string |
| `read` | no payload → the message list; `{"id":N}` → one message |
| `delete` | `{"id":N}` |
| `send` | refused, always |

## Layout

```
TNBrowser/
├── scripts/autoexec/tnbrowser.cs   entry point, package, menu overrides
└── tnbrowser/
    ├── settings.cs    endpoints and refresh interval
    ├── json.cs        JSON parser (the engine has none), URL/JSON encoding
    ├── session.cs     RSA challenge/response session
    ├── api.cs         request queue + all 26 browser API methods
    ├── clanprops.cs   clan properties dialog
    ├── playerprops.cs player properties dialog
    ├── mail.cs        mail API + mail window
    ├── panes.cs       browser GUI logic
    └── gui/*.gui      screens, derived from the stock layouts
tests/                 four suites, run inside the game
tools/                 container, deploy, mock backend, test runner, packaging
```

## Running your own backend

`server/` is a Go service that serves the browser, clan and mail data, and
`TNBrowserServer/` is a server-side mod that puts clan tags in players' names.
Together they remove every limitation listed above except the ones that are
TribesNext's to own.

```sh
cd server && go build -o tnserver ./cmd/tnserver
./tnserver -dsn "postgres://..."
```

then in the game console:

```
$TNB::Host = "http://your-host:8080";   # data from your backend
```

`$TNB::AuthHost` stays on TribesNext: players still log in there with their RSA
key, and the backend verifies the resulting session token by asking TribesNext
about it. No passwords, no key extraction, no second account system. Full detail
in `server/README.md`.

## Developing and testing

```sh
./tools/run-tn-container.sh --mod ./TNBrowser 2325   # patched game in Docker
./tools/run-tests.sh 2325                            # all four suites
./tools/deploy.sh 2325                               # push changes, no restart
./tools/live-check.sh                                # live RSA check; prompts for password
./tools/build-vl2.sh                                 # package
```

`run-tn-container.sh` builds a TribesNext-capable container by injecting the
patch files from a real patched install into the plugin's stock image — that
image ships Sierra's original `IFC22.dll`, so without this it can do neither
HTTPS nor RSA. Useful switches: `--login` (required for anything account-related,
see the engine notes), `--keep` (survive a crash so `docker logs` still has the
evidence), `--game-dir DIR` (bind-mount a real install instead of injecting).

`mockserver.py` serves the documented JSON shapes over plain HTTP — including the
write methods, permission failures, and the mail endpoint's refusal to send — so
everything can be exercised offline. Point the mod at it with
`$TNB::Host = "http://172.17.0.1:8099";`.

To adjust a layout, open the game's own GUI editor from the console with
`GuiEdit(0);`. It edits whatever screen is displayed, and **Save** writes the
`.gui` back. No key is bound to it in this build
(`bind(keyboard, F10, GuiEdit);` if you want one).

## Notes on this engine

Things that cost real time here and are easy to trip over again:

- **`rebuildModPaths()` resets the mod path stack to just `base`**, silently
  unloading the mod. Use `setModPaths("<mod>")` to refresh the resource index —
  it rebuilds the index too and appends `base` itself.
- **`.gui` files must not be exec'd from `scripts/autoexec`.** autoexec runs
  before the shell control profiles exist. A control built against a missing
  profile still constructs — `isObject()` returns 1 and every method works — but
  renders with default styling: no pane frame, no title bar, collapsed buttons.
  Load them on first open, and check each file individually, or a screen added
  later never loads in a session where the first one already opened.
- **`setValue(1)` on a `ShellRadioButton` fires that button's `command`.** A
  handler that writes back to the radios calls itself forever and takes the
  engine down. The stock `TAM_OnAction` is a one-liner for exactly this reason.
- **Never start an HTTP request from inside another one's callback.** It sticks
  in `Connecting` forever — libcurl's multi handle is mid-iteration on the
  connection whose callback you are in. The identical request completes in half a
  second when deferred by one `schedule()` tick.
- **The libcurl `HTTPObject` never calls `onConnectFailed` or `onDNSFailed`.** A
  failed connection surfaces only as `onDisconnect` with nothing received.
- **`HTTPObject.get(addr, uri, query)` ignores its third argument.** The query
  string has to be part of the request-URI.
- **`-nologin` disables the whole TribesNext account subsystem**, not just the
  login screen: the patch never registers its `t2csri_*` console functions, so no
  certificate can be loaded. HTTPS and `sha1sum` still work, which makes it look
  like the patch is fine.
- **A missing function's return value is not empty.** Calling an undefined
  function prints "Unable to find function", but an assignment from it still
  yields a meaningless number — so a script that only inspects the value reports
  a plausible-looking status for a call that never happened.
- **`GuiEmailBrowser` is display-only.** `getRowText` returns `""`; rows cannot be
  read back, which is why the stock client kept message text in a separate
  `EmailMessageVector`. It also wants exactly four values after the row id, and
  they land in the From/Subject/Received columns — the leading Status column is
  the envelope icon, drawn from the fourth value. Pass three and no row is added
  at all; pass a status string first and every column shifts one place left.
- A GUI hosted by `LaunchTabView` must define `setKey`, or the shell aborts with
  `Unknown command setKey` and the window never appears.

Two more that are about the async request layer rather than the engine, but cost
just as much time and were both caught by tests rather than by reading:

- **Clear a queue before running its callbacks, not after.** Callbacks routinely
  enqueue new work (a retry, the next step of a flow); resetting the indices
  afterwards silently discards it, leaving a request that was accepted and never
  sent.
- **Register at most one "wait for session" callback.** A pump that runs on a
  timer *and* on every enqueue will otherwise queue a waiter per pump, and the
  session layer then calls back once per waiter — so the second callback wipes
  the queue the first one just refilled.
