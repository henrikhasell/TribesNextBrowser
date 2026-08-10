# TNBrowser — Tribes 2's community browser and mail, working again

## What this is

Tribes 2 shipped with in-game community screens: warrior profiles, tribe pages
with rosters and administration, a tribe/warrior search, and mail. They are
still in every install — the BROWSER and EMAIL buttons on the launch shell open
them — but they talked to Sierra's WON backend, which shut down in 2003. So
today those buttons lead to:

> There was an error processing your request, please wait a few moments and try
> again.

This mod makes them work, **without changing a single pixel of them**.

It ships no `.gui` file. Not one. The screens a player sees are the shipped
`EmailGui` and `TribeAndWarriorBrowserGui`, driven by the shipped `webemail.cs`
and `webbrowser.cs`, rendering through the shipped control profiles. What the
mod replaces is the layer underneath: `DatabaseQuery()`, the single call every
community pane makes, re-pointed from WON's dead transport to HTTP against a
backend you run.

That is the whole design, and it is why the screens are identical to vanilla by
construction rather than by careful copying.

## How it works

The shipped scripts reach their backend through exactly two functions:

```
DatabaseQuery(ordinal, args, proxyObject, key)                webstuff.cs:218
DatabaseQueryArray(ordinal, maxRows, args, proxyObject, key)           :223
```

Both are TorqueScript, not native, so replacing them in a package is all it
takes. The original framed each call as `dbqax <id> <ordinal> <maxRows> :<args>`
and pushed it down the WON chat socket; this sends it as one HTTP POST and
delivers the answer back through the client's own path —
`onDatabaseQueryResult` once, then `onDatabaseRow` per row.

The ordinals themselves are unchanged, and that is the point. They were Oracle
stored-procedure selectors — the client tests for `ORA-04061` by name in seven
handlers — and the argument tuples and row schemas belong to the shipped
parsers, not to us. All **61** `(form, ordinal)` pairs reachable from the five
community scripts are implemented, each cited in
`server/internal/dbproxy/ordinals.go` to the call site that issues it and the
handler that reads the answer back.

Deactivating the package restores the shipped behaviour exactly: `DatabaseQuery`
goes back to framing `dbqax`, and `WONGetAuthInfo` goes back to not existing.

### Three findings that made this cheap

Established by reading the extracted `base/scripts.vl2` and probing a running
TribesNext client:

1. **Every WON native is absent on a TribesNext-patched binary.**
   `WONGetAuthInfo`, `WONUpdateCertificate`, `WONLoginIRC` and `WONStartLogin`
   all answer *"Unable to find function"*. So there is nothing to shadow —
   defining them is the whole job. The entire identity surface the community
   scripts touch is two names.
2. **Overriding `DatabaseQuery` steps over the chat gate.** `DatabaseQueryi`
   refuses to send anything unless `$IRCClient::state $= IDIRC_CONNECTED`
   (`webstuff.cs:183`) and answers a *fabricated* `1\tORA-04061` when it is
   not. Replacing the two public entry points means nothing here has to talk to
   a chat server.
3. **The shipped `LaunchBrowser` and `LaunchEmail` already do the right
   thing.** Only the launch bar needs a package, to build the online tab set
   and drop CHAT, which this mod does not serve.

## Three parts

| | what it is | who needs it |
|---|---|---|
| `TNBrowser/` | the client mod: five script files, no GUI | every player |
| `server/` | a Go backend answering the 61 ordinals | whoever hosts the community |
| `TNBrowserServer/` | a server-side mod that puts tribe tags in player names | game-server operators |

## Authentication is TribesNext's, and did not change

No password is typed into the mod. It runs the RSA challenge/response against
`/tn/robot/robot_login.php`, and the backend verifies the resulting session
token by asking TribesNext about it. This server holds no passwords, no keys,
and nothing extracted from `IFC22.dll`.

One thing reads as a contradiction and is not: the client no longer speaks
`json_browser.php`, but `internal/auth` still calls TribesNext's copy of it.
Different things — ours *was* a client protocol, theirs is a third-party
verification oracle (`tn_challenge_isAuthorized($guid, $uuid)` takes both, so a
token cannot be replayed against another account).

What the backend adds is the last hop: `WONGetAuthInfo()` serves that same
identity to the shipped scripts in WON's certificate layout, assembled from the
GUID TribesNext just vouched for.

## Requirements

- A **TribesNext-patched client**, for the `HTTPObject` reimplemented on libcurl
  (stock Torque's speaks plain HTTP only) and the RSA login.
- **This backend.** TribesNext's own server cannot serve the mod any more: it
  speaks methods and JSON objects, not ordinals and rows. That is the price of
  running the shipped screens.

## Installing

```sh
./tools/build-vl2.sh --host "http://your-backend:8080"
cp dist/TNBrowser.vl2 <Tribes2>/GameData/MyMod/
```

then launch with `-mod MyMod`. The engine indexes a mod directory's `.vl2`
archives alongside its loose files, so nothing needs unpacking. Settings can
also go in a loose `autoexec.cs` beside the archive — the game execs that last,
so a plain `$TNB::Host = "...";` overrides a baked value.

Builds are reproducible: every archive entry is stamped with the last commit's
date, so rebuilding unchanged sources produces an identical file.

## What works

Everything the two rendering panes can reach:

- **Warrior profiles** — description, URL, graphic, registration date, online
  state, tribe list, history, buddy list.
- **Tribes** — profile, roster, invitations, create, disband, description, tag,
  graphic, recruiting flag, member ranks and titles, kick, leave, primary tribe.
- **Invitations and join requests**, delivered *by mail* with working Accept and
  Reject links. That is not a design choice: the client has no query that lists
  a player's own invitations, so mail is the only channel there is.
- **Mail** — inbox, deleted folder, compose, reply, delete, permanent removal,
  read markers, and the block list with a count of what each block has actually
  turned away.
- **Search** for warriors and tribes, from the browser and the mail address
  book.

Three of the five panes — **News**, **Forums** and **Web Links** — have no
controls in a retail install. `NewsGui`, `ForumsGui` and `weblinksmenu` are
driven by shipped scripts but defined in no `.vl2` and no loose file, so the
script layer shipped in 2002 with its controls removed. Their ordinals are
implemented and swept; nothing can render them. That is a property of the game.

**Chat is not served.** The CHAT tab is dropped from the launch bar rather than
left dead. Online status comes from `accounts.last_seen`, which game servers
already update.

## What this cost

Running the shipped screens means living within them, and two things went:

- **The Sent folder.** `EmailGui` has Inbox and Deleted, and there is no third
  tab to put one in.
- **Anything else the previous hand-built screens grew** that the shipped UI has
  no widget for.

The mod also became backend-exclusive, as above.

## Verification

```sh
./tools/run-tn-container.sh --mod ./TNBrowser --account 2325   # patched game
./tools/run-tests.sh 2325                                      # against the mock
./tools/run-conformance.sh 2325                                # against the Go server
cd server && TNB_TEST_DSN=postgres://... go test ./...
```

**123 assertions inside the real game, 0 failures**: 66 parser, 7 sweep, 28
browser, 22 mail. Plus the Go suite against a real PostgreSQL, because what is
worth testing there is the SQL.

The suites divide the work deliberately:

- `tests/sweep_test.cs` issues **all 61 ordinals in their shipped call form**
  through the real `DatabaseQuery` and checks each answers through the client's
  own delivery path. It proves the framing and routing — and nothing about a
  single field index, because a row with its fields in the wrong order sweeps
  exactly as cleanly as a correct one.
- `tests/browser_test.cs` and `tests/mail_test.cs` drive the **shipped
  controls** and assert on what ends up in them, which is the only thing that
  catches a wrong index. Every object they name is out of the shipped `.gui`
  files, so an assertion that has to change to accommodate a screen means the
  screen is wrong.

`tools/mockserver.py` is a second, independent implementation of the same row
schemas, run by the same suites — so a disagreement between it and
`server/internal/dbproxy` means one of them is wrong about what the client
reads.

## Layout

```
TNBrowser/
├── scripts/autoexec/tnbrowser.cs   entry point: the package and the tab bar
└── tnbrowser/
    ├── settings.cs    endpoints and refresh interval
    ├── json.cs        JSON parser (the engine has none)
    ├── session.cs     the TribesNext RSA session
    ├── api.cs         the HTTP request queue
    └── dbproxy.cs     DatabaseQuery, and the WON certificate
server/
├── internal/dbproxy/  the ordinal table and its 61 handlers
├── internal/store/    PostgreSQL, and the only place rank rules are enforced
├── internal/auth/     the TribesNext session check
└── migrations/
tests/                 four suites, run inside the game
tools/                 container, deploy, mock backend, test runners, packaging
```

## Notes on this engine

Things that cost real time here and are easy to trip over again:

- **The telnet console is global scope.** `%locals` there do not merely fail —
  a `%c` in a `for` loop takes the engine down with an access violation. Use
  `$globals`, or put the code in a function.
- **`rebuildModPaths()` resets the mod path stack to just `base`**, silently
  unloading the mod. Use `setModPaths("<mod>")`.
- **Never construct a `ScriptObject` at file scope.** Same reason: that is
  global scope, and `new` crashes rather than failing.
- **Never start an HTTP request from inside another one's callback.** It sticks
  in `Connecting` forever — libcurl's multi handle is mid-iteration on the
  connection you are in. Defer by one `schedule()` tick.
- **The libcurl `HTTPObject` never calls `onConnectFailed` or `onDNSFailed`.** A
  failed connection surfaces only as `onDisconnect` with nothing received.
- **`HTTPObject.get(addr, uri, query)` ignores its third argument.**
- **`GuiEmailBrowser` is display-only** — `getRowText` returns `""`. That is why
  the shipped client keeps message text in a separate `EmailMessageVector`, and
  why the mail tests read that instead.
- **`$PlayerGfx` and `$TribeGfx` are read and never assigned** by any shipped
  script, so a profile that carries no graphic renders permanently blank. The
  server sends the default each shipped dialog names for itself.
- **Clear a queue before running its callbacks, not after.** Callbacks routinely
  enqueue new work; resetting afterwards silently discards it.
- **Register at most one "wait for session" callback**, or the session layer
  calls back once per waiter and the second wipes the queue the first refilled.
