# The WON protocol, as Tribes 2 speaks it

Tribes 2's shell has a Community section — **Email**, **News**, **Forums**, the **Tribe &
Warrior Browser**, **Web Links** — and a chat client, and a login screen in front of all of
it. Every one of those talked to WON, the World Opponent Network, which shut down in 2004.

This document specifies the protocol on the wire. It is written from `won-test-harness`, a
stand-in server that drives the shipped 2002 `Tribes2.exe` (SHA-256 `7ad1d40a…7f8f9`) and
records both directions, and it is the reference `server/internal/dbproxy/ordinals.go:29`
points at when it says the ordinal table "was established by driving the shipped binary
against a stand-in".

It is a specification of **four separate protocols** that happen to arrive on four sockets:

| Service | Port | What it speaks |
| --- | --- | --- |
| chat and database proxy | 6667 | RFC 1459, plus four WON verbs, one of which carries the entire community browser |
| login | 15200 | a line protocol — **invented**, because WON's own cannot be served (§2) |
| WON directory | 15104 | WONAPI's binary framing. **Recorded, never answered** (§9) |
| MOTD | 80 | plain HTTP against `www.won.net` |

The first is the protocol proper and occupies most of this document. The second is honest
invention. The third is the one place real WON bytes were ever captured. The fourth is two
headers and a body.

## Everything here is a statement about the client

This is the single most important thing to carry into any implementation, so it goes first.

The real WON servers are gone. Nothing about what they sent is recoverable. What *is*
recoverable is what the shipped client **reads**, because the client is TorqueScript and can
be read line by line. So when this document says field 9 of a mail row is `created`, the
claim being made is that `webemail.cs:1121` assigns `getField(%row,9)` to `%created` — no
more than that. Fields no parser touches are marked `-`; they must exist so the indices line
up, and their contents are arbitrary.

Three labels are used throughout, and they are not decoration:

- **Pinned** — a shipped script line or a `Tribes2.exe` address is cited reading the field.
  Not negotiable; an implementation that disagrees is wrong.
- **Chosen** — the wire permits it, WON's own value is unrecoverable, and a stand-in picked
  something. Another implementation may pick differently.
- **Unmodelled** — named, and left open. The harness answers these with a `T2RE-UNMODELLED`
  status, which the game raises in its own WARNING dialog. Loud beats a silently plausible
  pane.

Script citations are `<file>:<line>` against the files as they come out of
`base/scripts.vl2` — extract them with `unzip -p scripts.vl2 scripts/webemail.cs`. Addresses
are virtual addresses in the retail `Tribes2.exe` named above.

## It is not HTTP, and the tombstone says so

The obvious reading of a 2001 game with a "browser" pane, an `HTTPObject` class at
`0x005bdd50` and hand-rolled `GET %s HTTP/1.1` format strings is wrong for this subsystem.
The HTTP path was cut before release and left in place as a marker:

```
webstuff.cs:36   function HTTPRequest(%script, %update, %destObject, %key)
webstuff.cs:38      error("Call to HTTP request depricated - trace:");
```

with the real body commented out beneath it, referring to a `SecureHTTPObject` posting to
`$WebAddress` / `$WebPath` / `<script>.php`. `HTTPSecureRequest` is the same. Neither is
called from anywhere in the five community scripts.

What replaced it is a query proxy tunnelled over the chat connection. Every pane calls one of
two functions:

```
DatabaseQuery(ordinal, args, proxyObject, key)                  webstuff.cs:218
DatabaseQueryArray(ordinal, maxRows, args, proxyObject, key)    webstuff.cs:223
```

**The backend was Oracle.** That is not inferred from the shape of the thing — the client
tests for a specific Oracle error by name in seven separate handlers:

```
webemail.cs:495  else if (getSubStr(getField(%status,1),0,9) $= "ORA-04061")
```

`ORA-04061` is "existing state of packages has been discarded", the error a client sees when
a PL/SQL package is recompiled under it. So the ordinals are stored-procedure selectors, and
the whole Community section is a thin GUI over a set of numbered stored procedures.

## The login cannot be served on the wire, and one address is the reason

Login is native. `LoginProcess` (`console_start.cs:603`) calls `WONStartLogin`
(`0x006f42e0`) and polls `WONLoginResult` (`0x006f3f80`) once a second until it stops saying
`Waiting`; on success the certificate is inside the process and `WONGetAuthInfo`
(`0x006f2b20`) hands it out. All of that is WONAPI talking its own binary protocol to
`tribes2.m[123].sierra.com:15104`.

The blocker is not the protocol. It is the key:

| What | Where |
| --- | --- |
| the string `_wonkver.pub` | `0x007c6c64`, one xref, `push` at `0x006c9d38` |
| the function that consumes it | `0x006c9bb0`, in the `BigInteger.cpp` region |
| **the key itself** | `0x007c6c78`, `30 82 03 2c 02 82 01 01 00 86 18 0e 3d …` |
| a copy on disk | `GameData/kver.pub`, 816 bytes, **byte-identical** |
| sibling literals | `_wonlogin.ks` at `0x007c6c54`, and `1.1.3` — the WONAPI version |

The shipped `kver.pub` parses as a DER `SEQUENCE` of four `INTEGER`s — 2048-bit, 232-bit,
2048-bit, 2048-bit — which is the `(p, q, g, y)` shape of a discrete-log signature key, not
an RSA modulus and exponent. Everything WONAPI accepts is verified against it.

The verification key is therefore compiled into `.rdata`. A certificate the client accepts
must be signed by the matching private key, which does not exist outside Sierra's build
machines, and replacing the file on disk achieves nothing because the copy that is consulted
is in the image.

**So the login is intercepted one level up, in TorqueScript.** `Namespace::createLocalEntry`
(`0x004285f0`) keeps one entry list per namespace and, on a name match, calls `Entry::clear`
(`0x004280c0`) and returns the existing entry; otherwise it allocates and prepends. All six
`Namespace::addCommand` variants (`0x00428670`–`0x004287b0`) go through it, so a later
registration always wins — which means a script function can replace a native console
command. Confirmed against the running binary rather than inferred:

```
% function WONLoginResult() { return "SHIMTEST"; }
% echo(WONLoginResult());
SHIMTEST
```

Thirteen natives are replaced this way: `WONStartLogin`, `WONLoginResult`, `WONGetAuthInfo`,
`WONUpdateCertificate`, `WONStartCreateAccount`, `WONStartUpdateAccount`,
`WONStartEmailFetch`, `WONStartLoginInfoFetch`, `WONLoginIRC`, `WONInit`, `WONServerLogin`,
`WONDisableFutureCalls`, and TribesNext's `newLoginProcess`.

`WONLoginIRC` (`0x006f5a70`) is the interesting one, and the reason for doing it this way
rather than any other. Because a script answers it, the **real** chat handshake runs —
`CERT` → `CHALLENGE` → `CHALRESP` → `CHALRESP_REPLY` (`ChatGui.cs:1365`, `:2704`, `:2717`) —
instead of the `422` shortcut in §4. That is the only path that gives the local player a name
and a tribe tag in chat.

## The login service is a line protocol, and none of it is WON's

**Everything in this section is invented.** It is what the shipped client's login screen can
be driven with once the natives are replaced; it does not resemble WON's own protocol and is
not evidence about it. It is specified here because a client that wants to log in has to
speak something, and this is what the reference implementation speaks.

Lines are `\r\n`-terminated, fields are TAB-separated, the first field is a verb.
**The server speaks first** (see §4 for why), greeting with `HELLO<TAB>wonproxy`.

| Request | Fields |
| --- | --- |
| `LOGIN` | `<login> <password>` |
| `CREATE` | `<login> <warriorName> <password> <cdkey> <email> <sendinfo>` |
| `UPDATE` | `<token> <password> <email> <sendinfo>` |
| `EMAILFETCH` | `<login>` |
| `REFRESH` | `<token>` |
| `PING` | — |

| Reply | Fields |
| --- | --- |
| `HELLO` | `<serverName>` — sent unprompted on accept |
| `RESULT` | `<status> <code> <codeText> <errorString>` |
| `TOKEN` | `<token>` |
| `AUTHBEGIN` | `<recordCount>` |
| `AUTHREC` | `<fields…>` — `recordCount` of these |
| `AUTHEND` | — |
| `PONG` | — |

`RESULT` is shaped to be returned verbatim by the replacement `WONLoginResult`.
`StartupGui::checkLoginDone` (`console_start.cs:676`) reads field 0 as the status, field 1 as
a progress code, field 2 as the `codeText` it switches on, and field 3 as an error string. On
the success path fields 2 and 3 are the email address and the send-info flag, which
`checkLoginDone` copies into `$CreateAccountEmail` and `$CreateAccountSendInfo` when the
EDIT ACCOUNT button was used (`console_start.cs:768-771`).

**Only four `codeText` values mean anything to the client** — everything else falls through
to its generic "check your internet connection" text. They are enumerated at
`console_start.cs:686-724`, and the ones a login service can usefully produce are:

```
WS_DBProxyServ_InvalidUserName
WS_DBProxyServ_UserDoesNotExist
WS_DBProxyServ_UserExists
WS_DBProxyServ_DBError
```

Those names are WON's, and their prefix is a real clue: the login the player types was
answered by the **DBProxy** service, not by the auth service, which is consistent with the
community backend being the same Oracle instance.

**The login name rule is the client's own**, quoted from the message it shows when the server
rejects one: letters, digits and underscores, 3 to 16 characters (`console_start.cs:687`).
Rejecting a bad name is worth implementing because it is a code path the client demonstrates.

`AUTHBEGIN` / `AUTHREC` / `AUTHEND` exist only because the certificate (§7) is a multi-record
string and this is a line protocol; sending it as lines avoids inventing an escape. The same
three lines are pushed **unprompted** whenever a player's certificate changes — joining a
tribe, being promoted, a tag edit — so that `WonUpdateCertificate()`, which `webbrowser.cs`
calls from eight places, has something current to return instead of having to block.

## The chat connection, and the two ways into it

Tribes 2's IRC client is not in the executable. It is roughly 100 `IRCClient::*` functions of
TorqueScript in `scripts/ChatGui.cs` — the RFC 1459 BNF is quoted verbatim at
`ChatGui.cs:1551-1554` — speaking over a native `TCPObject` (`connect` at `0x005bd1f0`, `send` at
`0x005bd190`).

**The dispatch surface is closed and knowable.** `IRCClient::dispatch` (`ChatGui.cs:1590`) is
a single switch, so the set of things worth sending is finite:

```
verbs     PING PONG PRIVMSG JOIN NICK QUIT ERROR TOPIC PART KICK MODE AWAY
          NOTICE VERSION ACTION INSTANT INVITE
numerics  301 305 306 311 312 315 317 318 319 322 323 324 331 332 341 352 353
          367 368 372 376 401 422 433 444 465 468 471 473 474 475
WON       CERT  CHALLENGE  CHALRESP/CHALRESP_REPLY  DBMSG
```

Anything else the client prints as `(cmd:) …` and drops.

Two rules any server standing in for WON has to obey:

- **The server speaks first.** `IRCTCP::onConnected` is what sends the certificate, and it is
  driven by the socket's connect notification, which Torque routes through `WSAAsyncSelect`
  (IAT slot `0x007dc8b0`) and the window procedure at `0x00559a90` — and that does not arrive
  in a headless container. Dynamix hit the same class of failure and left the workaround in
  place:

  ```
  ChatGui.cs:1541   // HACK:  Windows 2000 bug.  We shouldn't need to do this!
  ChatGui.cs:1542   if ($IRCClient::state $= IDIRC_CONNECTING_SOCKET)
  ChatGui.cs:1543      IRCTCP::onConnected(%this);
  ```

  so *any* inbound line resynchronises the client. A real IRC server opens with a banner,
  which is presumably why this was survivable in 2002. The banner is load-bearing, not
  decoration.
- **The client never sends `NICK` or `USER`.** Identity comes back from the server in
  `CHALRESP_REPLY`, and that is the only path that gives the local player a name.

### The handshake

`DatabaseQueryi` refuses to send anything unless `$IRCClient::state $= IDIRC_CONNECTED`
(`webstuff.cs:183`), and there are exactly two transitions into that state.

**The full handshake**, which is the one that delivers an identity:

```
S: :won NOTICE AUTH :*** Local WON stand-in ready
C: dbqa <id> :<certificate chunk>            (repeated, 400 bytes each)
C: CERT <certificate tail>
S: :won CHALLENGE <challenge>
C: CHALRESP <response>
S: :won CHALRESP_REPLY <nick> <ident> OK
```

`IRCTCP::onConnected` (`ChatGui.cs:1365`) chunks `WONLoginIRC()` through the *same* `dbqa`
framing the database proxy uses and terminates it with `CERT` instead of `dbqax` — so a
request assembler has to accept either command as the closer of a pending accumulation.
`onChalRespReply` (`ChatGui.cs:2717`) parses `<nick> <ident> <reply>` and requires
`WONLoginIRC(%reply) $= "OK"`, which real WON answered by validating a certificate inside
`0x006f5a70` against a live auth server.

**The shortcut**, which does not:

```
S: :won NOTICE AUTH :*** Local WON stand-in ready
C: CERT ERROR
S: :won 422 warrior :MOTD file is missing
C: LIST
```

`IRCClient::onMOTDEnd` (`ChatGui.cs:1916`), dispatched from numerics **376** and **422**,
checks nothing except that the state is `IDIRC_CONNECTING_WAITING`, and sets
`IDIRC_CONNECTED`. `IRCTCP::onConnected` sets `CONNECTING_WAITING` immediately after sending
the certificate, so answering the certificate with a bare `422` reaches the connected state
with no cryptography at all. The connection works and the database proxy works; the player
has no name.

Worth keeping: **with no WON session behind it, the certificate the client sends is the
literal five-byte string `ERROR`** — `WONLoginIRC()` called with no argument returns it, and
the client sends it anyway without checking. That is what a real server would have had to
reject.

### Identity in chat

The nick is a triple, `name^tag^append`, split by the native `IRCGetTriple` (`0x006f5ba0`)
and consumed by `IRCClient::setIdentity` (`ChatGui.cs:1109`). Probed against the shipped
binary:

```
IRCGetTriple("Harabec^[BSF]^1")  ->  Harabec <TAB> [BSF] <TAB> 1
IRCGetTriple("[BSF]^Harabec^0")  ->  [BSF]   <TAB> Harabec <TAB> 0
IRCGetTriple("Harabec")          ->  (empty)
IRCGetTriple("Harabec^^")        ->  (empty)
```

**Pinned:** `append` decides which side the tag goes on, and a warrior in no tribe must be
given a **bare** nick. `Name^^0` yields an empty triple, `%p.nick` stays empty, and the
member list falls back to printing the raw nick, carets and all.

**Pinned, and it constrains the design: the client cannot absorb a mid-session rename.**
`IRCClient::onNick` (`ChatGui.cs:1846`) calls `%person.setName()` and never touches
`displayName`, while `IRCClient::findPerson` (`:1000`) matches on `displayName` alone. After
a `NICK` the client can no longer find that person by their new name — the next message from
them creates a duplicate person object, and their member-list entry keeps the old tag.
Dynamix knew: the one path that renames a warrior calls `IRCClient::quit()` and tells the
player the change "will require you to close and restart the game"
(`webbrowser.cs:1787-1793`). **So do not send `NICK` when a tribe tag changes.** Push a new
certificate and let the tag apply on the next connection.

### Tribe channels

**Half-pinned.** `IRCClient::onList` (`ChatGui.cs:2419-2421`) drops any channel whose name
ends in `_Public` or `_Private` before it reaches the channel list. Those two suffixes are
hardcoded and appear nowhere else, so the client is hiding per-tribe rooms from the public
listing — which is the only surviving record of what WON called them. That they are
`#<Tribe>_Public` and `#<Tribe>_Private`, with spaces replaced by underscores, is **chosen**.

Who holds ops in them is entirely **chosen**: the reference implementation ops members at
`adminLevel >= 2`, the same rung that gates inviting and kicking and the only rung the client
itself knows about (`webbrowser.cs:1921`). `MODE +o` must be sent **after** the `JOIN`,
because `IRCClient::onMode` (`ChatGui.cs:2179`) needs the person to exist first.

One verb here is WON's own rather than RFC 1459's: **`INSTANT`**, the invite-to-chat that
`ChatGui.cs:3091` sends and `onInstantMsg` (`:3070`) receives.

**The ignore list cannot be implemented.** `IRCClient::ignore` (`ChatGui.cs:3244`) only flips
a local flag and never reaches the wire. The server-side equivalent is the email block list
(array 2, scalars 8 and 9).

## The database proxy

This is the heart of the protocol. Every community feature is a `(form, ordinal)` pair, an
argument string, and a reply of status + result + rows.

### Request framing

`DatabaseQueryi` (`webstuff.cs:181`) escapes, chunks and sends:

```
args = args.replace('\r','').replace('\\','\\\\').replace('\n','\\n')
while i + 400 < len(args):
    send('dbqa  <id> :<args[i:i+400]>');  i += 400
send('dbqax <id> <astr> :<args[i:]>')
```

`astr` is **two words**, and it is what distinguishes the two call forms:

| Call | `astr` | On the wire |
| --- | --- | --- |
| `DatabaseQuery(1, …)` | `1 0` | `dbqax 7 1 0 :<args>` |
| `DatabaseQueryArray(1, 0, …)` | `C1 0` | `dbqax 7 C1 0 :<args>` |

**So the key is `(form, ordinal)`, not the ordinal.** Scalar 1 and array 1 are different
stored procedures. Four ordinals collide across the two forms — **0, 1, 10, 11** — and in
every case the two meanings are unrelated: scalar 10 is `addBuddy`, array 10 is a tribe news
feed. Treating the ordinal alone as the key silently merges them.

`DatabaseQueryCancel` (`webstuff.cs:228`) sends `dbqc <id>`.

The second word of `astr` is nominally `maxRows`, but **it is overloaded**: three call sites
put a page number there instead of a limit (`webnews.cs:467`, `webforums.cs:602`, `:920`) and
a fourth puts a real limit of 80 (`webforums.cs:935`). It cannot be read as a row cap in
general, and the reference implementations carry it without interpreting it.

### Response framing

```
:<prefix> DBMSG <id> <lastPacket> :<escaped payload chunk>
```

and the reassembled payload, from `HandleDatabaseProxyResponse` (`webstuff.cs:85`), is

```
<status> \S <resultString> \S <row> \S <row> \S …
```

where `\S` is a literal backslash followed by a capital S. The first two `\S`-terminated
records go to `proxyObject.onDatabaseQueryResult(status, resultString, key)`; every later one
to `proxyObject.onDatabaseRow(row, isLast, key)`.

**Delivery order is part of the contract**, and so is the absence of a delivery:

```
onDatabaseQueryResult(status, resultString, key)   exactly once
onDatabaseRow(row, isLast, key)                    once per row
```

No rows means **no `onDatabaseRow` at all**, and therefore no `isLast` — the row loop at
`webstuff.cs:150` only emits when it finds a trailing separator. Synthesising an empty final
row to carry the flag puts a blank line in every list.

`status` is a TAB-separated field list:

| Field | Meaning |
| --- | --- |
| 0 | the code every handler tests with `getField(%status,0)==0` |
| 1 | a sentence shown verbatim in a `MessageBoxOK` on both success and failure paths, depending on the ordinal (`webemail.cs:551`, `webbrowser.cs:927`, `:1447`) |
| 2, 3 | a total-record count and an ACL for News (`webnews.cs:206`); a per-forum flag for Forums (`webforums.cs:765`); **the entire payload** for the two profile ordinals |

Because field 1 is user-visible on several paths, a failure message has to read like a
sentence, not like a code.

`resultString` is the row count for array forms, and several scalars overload it with a
payload instead — the MOTD text, a tribe or warrior description, an echoed tribe name.

### Four sharp edges

These are properties of the shipped client. All four are defects; all four have to be matched
rather than fixed.

1. **Every element is terminated by `\S`, including the last.** The row loop
   (`webstuff.cs:150`) only emits a row when it finds a trailing separator, so an
   unterminated final row is silently dropped rather than delivered short.
2. **`isLast` is `(buffer empty) and lastPacket`** (`webstuff.cs:156`), so the final `DBMSG`
   of a response must set the flag. Without it the client never signals completion and never
   erases the query from `$DBQueries` — the entry leaks and the pane waits forever.
3. **The client unescapes each packet before concatenating** (`webstuff.cs:112-118`), so an
   escape sequence or a `\S` split across two packets is destroyed. A server must never cut
   inside one. Since a backslash always begins a two-character sequence here, it is enough to
   refuse to cut immediately after an odd-length run of them.
4. **The unescape order is `\n` then `\\`** (`webstuff.cs:112-113`), which is inverted. A
   literal backslash followed by `n` in the data cannot survive, and **`\S` appearing in data
   is a delimiter injection**: escaping makes it `\\S`, the `\\`→`\` pass turns it back into
   a live separator, and the row splits in the middle. Avoid backslashes in data.

A fifth, smaller one: `strpos(params, ':')` finds the first colon in the whole parameter
string, so the id and last-packet words must not contain one.

The correct outbound transform is therefore backslash **first**, then newline — the reverse
of the client's order, and the only ordering that round-trips anything at all — applied to
each element *before* joining, never to the joined string.

### Multi-packet reassembly is real, and the default path does not exercise it

Forcing a 60-byte chunk size split one response into five `DBMSG` packets, cutting mid-field
(`…2002-10-3` / `0 12:00:00`) and mid-row, and the client reassembled all of it with `isLast`
set only on the final row. A single-packet path passing says nothing about this.

## The ordinal table

Sixty-one distinct `(form, ordinal)` pairs are reachable from the five community scripts.
They divide by pane like this:

| Pane | Script | Ordinals |
| --- | --- | --- |
| News | `webnews.cs` | scalar 0–4; array 0, 100 |
| Web Links | `webnews.cs`, `weblinks.cs` | array 15 |
| Email | `webemail.cs` | scalar 5–11, 35, 69; array 1–3, 5, 6, 14 |
| Forums | `webforums.cs` | scalar 12–14, 60–63, 66–68; array 7–9 |
| Tribe & Warrior Browser | `webbrowser.cs` | scalar 15–34, 62, 63; array 4, 10–13 |

**Names are coined from the call site, not recovered.** They were opaque numbers on the wire;
the only way to name one is to read what issues it and what consumes the answer. Five names
moved once the `Link*` helpers in `webbrowser.cs` were read rather than just the
`DatabaseQuery` lines — the helper sets the pane's state and only then issues the query:

| Ordinal | First named | Actually | Evidence |
| --- | --- | --- | --- |
| 19 | inviteToTribe | **kickMember** | `LinkKickMember` sends `<player> TAB <tribe> TAB 0`, state `kickPlayer` (`webbrowser.cs:499`) |
| 27 | tribeMemberOp | **inviteToTribe** | `LinkInvitePlayer` sends `<tribe> TAB <player>` (`:527`) |
| 28 | tribeMemberOp2 | **answerInvitation** | `LinkInvitation`'s own switch enumerates `accept`/`reject`/`cancel` (`:564`) |
| 17 | setProfileText (tribe) | **setWarriorDescription** | `:215` is the `editWarriorDesc` branch; its tribe branch sends 15 |
| 34 | refreshTribe | **requestInvite** | only call site is the `requestlink` branch of `onURL` (`:1138-1140`) |

### Scalar ordinals

`resultString` is unread unless the table says otherwise; where a write's only observable is
`status`, the row column is `—`.

| Ord | Name | Pane | Call site | Arguments |
| --- | --- | --- | --- | --- |
| 0 | `getMOTD` | news | `webnews.cs:77` | none — **`resultString` IS the MOTD** (`:481`) |
| 1 | `postNewsArticle` | news | `webnews.cs:356` | `<category> \t <title> \t <body>` |
| 2 | `editNewsArticle` | news | `webnews.cs:362` | `<editUrl> \t <category> \t <title> \t <body>` |
| 3 | `deleteNewsArticle` | news | `webnews.cs:449` | fields assembled by the caller |
| 4 | `setMOTD` | news | `webnews.cs:524` | `<motd text>` |
| 5 | `sendMail` | email | `webemail.cs:155` | `<to> \t <cc> \t <subject> \t <body>` |
| 6 | `deleteMail` | email | `webemail.cs:141`, `%qnx = 6` | `<messageId>` — to the deleted folder |
| 7 | `markMailRead` | email | `webemail.cs:1328` | `<messageId>` — **fire-and-forget** |
| 8 | `removeBlock` | email | `webemail.cs:445` | `<name>` |
| 9 | `addBlock` | email | `webemail.cs:426`, `webbrowser.cs:375` | `<blockAddress>` |
| 10 | `addBuddy` | email/browser | `webemail.cs:764`, `webbrowser.cs:367` | `<playerName>` |
| 11 | `dropBuddy` | email/browser | `webemail.cs:769`, `webbrowser.cs:359` | `<playerName>` |
| 12 | `postTopicOrReply` | forums | `webforums.cs:527`, `%ord = 12` | `<forumId> \t <topicId> \t <parentPost> \t <subject> \t <body>` |
| 13 | `editPost` | forums | `webforums.cs:527`, `%ord = 13` | `<postId> \t <subject> \t <body>` |
| 14 | `postNewsOrDeletePost` | forums | `:527`, `:552`, `:1474` | **ambiguous** — 3 fields or 1, see below |
| 15 | `setTribeDescription` | browser | `webbrowser.cs:208`, `:2436` | `<tribeName> \t <lineCount> \t <description>` |
| 16 | `createTribe` | browser | `webbrowser.cs:166` | `<name> \t <further fields>` |
| 17 | `setWarriorDescription` | browser | `webbrowser.cs:215`, `:2553` | `<description>`, or the literal `NONE` to clear |
| 18 | `deleteTribe` | browser | `webbrowser.cs:343` | `<tribeName>` |
| 19 | `kickMember` | browser | `webbrowser.cs:499` | `<player> \t <tribe> \t 0` |
| 20 | `toggleTribeFlag` | browser | `webbrowser.cs:520` | `<"Recruiting"\|"Appending"> \t <tribeName> \t <0\|1>` |
| 21 | `setMemberProfile` | browser | `webbrowser.cs:643` | `<tribe> \t <player> \t <title> \t <adminLevel>` |
| 22 | `getTribeProfile` | browser | `webbrowser.cs:1571` | `<tribeName>` — **payload in status** |
| 23 | `getWarriorProfile` | browser | `webbrowser.cs:1890`, `:1947` | `<playerName>` — **payload in status** |
| 24 | `leaveTribe` | browser | `webbrowser.cs:497` | `<tribeName>` |
| 25 | `setPrimaryTribe` | browser | `webbrowser.cs:515` | `<tribeId or tribeName>`; result field 0 = the tribe name |
| 26 | `clearBuddy` | browser | `webbrowser.cs:351` | the literal string `clearBuddy` |
| 27 | `inviteToTribe` | browser | `webbrowser.cs:527` | `<tribe> \t <player>` |
| 28 | `answerInvitation` | browser | `webbrowser.cs:564` | `<"accept"\|"reject"\|"cancel"> \t <tribe> \t <player>` |
| 29 | `setTribeGraphic` | browser | `webbrowser.cs:2494` | `<tribeName> \t <bitmap>` |
| 30 | `setTribeTag` | browser | `webbrowser.cs:2413` | `<tribeId> \t <newTag>` |
| 31 | `setPlayerGraphic` | browser | `webbrowser.cs:2591` | `<bitmap>` |
| 32 | `setPlayerUrl` | browser | `webbrowser.cs:2614` | `<url>` |
| 33 | `setPlayerName` | browser | `webbrowser.cs:2628` | `<newName>` |
| 34 | `requestInvite` | browser | `webbrowser.cs:1140` | `<tribeName>` — **not idempotent** |
| 35 | `removeMailPermanently` | email | `webemail.cs:141`, `%qnx = 35` | `<messageId>` |
| 60 | `requestTopicReview` | forums | `webforums.cs:1185` | `<forumId> \t <topicId> \t <field 11 of the topic row>` |
| 61 | `requestPostReview` | forums | `webforums.cs:1470` | `<forumId> \t <topicId> \t <postId> \t <authorId>` |
| 62 | `removeTopic` | forums/browser | `webforums.cs:1207`, `webbrowser.cs:1148-1178` | `<action 0..7> \t <topicId> \t <field 11>` |
| 63 | `postAdminAction` | browser | `webbrowser.cs:1183-1211` | `<action 0..7> \t <fields 1.. of the parsed URL>` |
| 66 | `lockTopic` | forums | `webforums.cs:1227` | `<topicId> \t <reason>` |
| 67 | `unlockTopic` | forums | `webforums.cs:1194` | `<topicId>` |
| 68 | `moveTopic` | forums | `webforums.cs:1236` | `<topicId> \t <destForumId> \t <destForumName>` |
| 69 | `getOnlineStatus` | email/browser | `webemail.cs:655`, `webbrowser.cs:866`, `:2041`, `:2223` | `<TAB-separated row ids>` — **bitmap reply** |

Four of those need their own paragraph.

**Scalar 7 is fire-and-forget by construction.** It is the only call site in all five scripts
that passes neither a proxy object nor a key — `DatabaseQuery(7, %id)` — so the response is
reassembled and discarded. Observed firing by itself when a message is selected in the pane:
`dbqax 65 7 0 :1001`.

**Scalar 69 answers with a fixed-width bitmap, not fields.** The handler indexes
`resultString` by character position and calls `setRowStyle(i, !char)` — one character per
queried id. It also reads an undefined `%resultString` while its parameter is named
`%resultStatus` (`webbrowser.cs:879`), so the loop never actually executes. A defect in the
shipped script; the reply shape is still pinned by what the loop *would* have read.

**Scalar 5 has a 4000-character budget shared across all four of its fields**
(`webemail.cs:154`) — the client truncates the body to `4000 - len(to + cc + subject)`. That
is evidence the backend column was 4000 wide.

**Scalars 22 and 23 carry their whole payload in the status**, not in rows. `GetProfileHdr`
is handed `getFields(%status,2)` and reads the fields off it:

```
scalar 22   0 code  1 msg  2 tribeId  3 tribeName  4 tribeTag
            5 appending  6 recruiting  7 tribeGfx       + resultString = description
scalar 23   0 code  1 msg  2 playerName  3 playerTag  4 appending  5 playerId
            6 registered  7 online  8 playerURL  9 playerGfx
                                                         + resultString = description
```

Pinned by `GetProfileHdr(0, …)` at `webbrowser.cs:1355` and `GetProfileHdr(1, …)` at `:1667`.
Field 6 of 22 drives the "Recruiting: YES/NO" line and the Request Invite link (`:1358`);
field 4 of 23 chooses `name@tag` versus `tag@name` (`:1666`).

### Array ordinals

| Ord | Name | Pane | Arguments | Row layout |
| --- | --- | --- | --- | --- |
| 0 | `getNewsArticles` | news | `"1" \t <categoryId>` | `0=- 1=topicId 2=articleId 3=postCount+1 4=date 5=updateId 6=authorId 7=- 8..11=authorQuad 12=category 13=headline 14..=body lines` |
| 100 | `getNewsByCategory` | news | `"0" \t "0" \t <categoryTabId>` | same as array 0 — both feed `NewsGui::rebuildText` |
| 15 | `getWebLinks` | weblinks | `"WEBLINK"` | `0=status ("0" to accept the row) 1=name 2=address` |
| 1 | `getMail` | email | `$EmailNextSeq` — a **high-water mark** | `0=id 1..4=senderQuad 5..8=recipientQuad 9=created 10=isCC 11=isDeleted 12=isRead 13=toList 14=ccList 15=subject 16=bodyLineCount 17..=body lines` |
| 14 | `getDeletedMail` | email | `EmailGui.state` | identical to array 1 |
| 2 | `getBlockList` | email | none | `0..3=nameQuad 4=blockedCount` |
| 3 | `searchWarriors` | email/browser | `<text> \t <start> \t <count> \t <flag>` | `0=- 1=name 2..=columns the browser shows and the address book ignores` |
| 4 | `searchTribes` | browser | `<text> \t <start> \t <count> \t 0` | `0=- 1=tribeName 2=tribeTag 3..` |
| 5 | `getBuddyList` | email/browser | none, or `<playerName>` | `0..3=buddyQuad 4=friendsSince 5=online` |
| 6 | `getTribeMembers` | email/browser | `<tribe name>` | `0..3=memberQuad 4=title 5=adminLevel 6=joinDate 7=- 8=canEditKick 9=online` |
| 7 | `getForumList` | forums | none | `0=forumRowId 1=forumName 2=flag 3=forumId` |
| 8 | `getTopicList` | forums | `<forumId>` | `0=- 1=topicId 2=topic 3=postCount 4=- 5=- 6=date 7=- 8=authorName 9=- 10=- 11=- 12=hasDeletes 13=securityLevel 14=maxUpdateId` |
| 9 | `getPostUpdates` | forums | `<topicId> \t <lastPostIdSeen>` | `0=isAuthor 1=- 2=postId 3=parentPostId 4=updateId 5..8=posterQuad 9=- 10=date 11=- 12=isDeleted 13=subject 14..=body lines` |
| 10 | `getTribeNews` | browser | `<tribeName>`, `maxRows` 20 | `0=articleId 1=forumName 2..4=- 5..8=authorQuad 9=lastUpdated 10=title 11..=body lines` — **dead code** |
| 11 | `getTribeInvites` | browser | `<tribeName>` | `0=inviteId 1=inviteDate 2..5=invitorQuad 6..9=invitedQuad 10=isOwned 11=online` |
| 12 | `getWarriorHistory` | browser | `<playerName>` | **not field-structured** — one line of display text per row |
| 13 | `getWarriorTribeList` | browser | `<playerName>` | `0=tribeName 1=- 2=tribeId 3=adminLevel 4=editKick 5=title` |

And the paragraphs those need:

**Array 1 takes a sequence high-water mark, and a server that ignores it is broken.** This
was not read out of the scripts; it fell out of running the pane against a stand-in that
ignored the argument. The first poll sent `0`, the second sent `1002` — the highest id it had
just been handed — and the same two messages arrived twice and were listed twice, four rows
in the inbox for two messages. `CheckEmail` passes `$EmailNextSeq` (`webemail.cs:387`) and
`EmailGui::getCache` sets that to the last cached id (`:1196`). So the argument means
"messages newer than this", and ignoring it makes the inbox grow without bound.

**The ROSTER button uses array 6, not array 10.** Array 6 is the same `getTribeMembers` the
email address book calls — the address book reads only field 0, and `TribePane` pins the
rest. The three MemberList columns it adds beforehand, MEMBER / TITLE / RNK
(`webbrowser.cs:1580-1582`), line up with fields 0, 4 and 5. Field 8 is about the **caller's**
rights, not the listed member's.

**Array 10's schema is read off dead code.** The tribe news feed was cut before release:
`getTribeNews` sets `%this.state = "done"` unconditionally (`webbrowser.cs:1410`) and the
block that would have set `state = "tribeNews"` is commented out immediately below
(`:1412-1420`), so the row branch at `:1506` is unreachable and any rows served are
discarded. What the pane shows instead is a static list of Tribal Forum and Tribal Chat links
(`:1399-1407`). The dead branch is the only surviving description of what this ordinal
returned, and the table carries it on that basis alone.

**Array 13 is only ever issued for other players.** Viewing your own tribe list takes the
fields straight out of `WONGetAuthInfo()` and sends no query at all
(`webbrowser.cs:1909-1927`) — which is also where the field order is corroborated, since that
loop fills the same columns from the certificate.

**Array 7's tribe forums never came from the server.** After the last row the client appends
one row per tribe from `WONGetAuthInfo()`, with a **negated** id (`webforums.cs:817-822`).

**Array 11 field 11 is read inverted.** It drives
`MemberList.setRowStyleById(id, !online)` at `webbrowser.cs:1528`.

**Array 15's failure path is a historical record.** On any non-zero row status the client
falls back to `weblinksmenu::defaultList` — 49 hardcoded fan sites (`weblinks.cs:3-53`), from
`www.planettribes.com` to `www.5assedmonkey.com`. Since the server side is gone, that list is
the only surviving record of what this pane served.

## The certificate is the identity

`WONGetAuthInfo()` returns newline-separated records of TAB-separated fields —
`getRecord(x, n)` splits on newline.

```
record 0    name TAB tag TAB append TAB guid
record 1    <tribe count>
record 2+n  name TAB tag TAB append TAB tribeId TAB adminLevel TAB title
```

**Pinned.** Record 0 field 0 is the warrior name (`GameGui.cs:1324`, `webbrowser.cs:668`) and
field 3 the GUID (`LaunchLanGui.cs:342`, `webemail.cs:1276`); `webbrowser.cs:271` compares
field 3 against field 3 of a row quad, which is what identifies record 0 **as a quad** rather
than four unrelated fields. Record 1 field 0 is the tribe count (`webbrowser.cs:101`,
`webemail.cs:970`). The tribe records are pinned by `webbrowser.cs:1909-1926`: field 3 is the
tribe id, 4 the admin level, 5 the title, 0 the name.

**Chosen.** Fields 1 and 2 of a tribe record. Nothing reads them as a tag or an append flag.
They are there because every other four-field group in this protocol is a quad.
`webforums.cs:822` does read field 2, into the column a forum row uses for its forum id,
which makes no sense either way and looks like a bug in the shipped script.

The GUID reaches the filesystem: `$EmailCachePath` is
`"webcache/" @ getField(getRecord(wonGetAuthInfo(),0),3) @ "/"` (`webemail.cs:15`), so a
wrong field 3 relocates where mail is cached.

### The quad

One identity primitive, reused everywhere: `name TAB tag TAB append TAB uid`, decoded by
`getLinkName` and `getTextName` (`webstuff.cs:2`, `:20`). **When `append` is set the tag
follows the name, otherwise it precedes it** — `Harabec[BSF]` versus `[BSF]Harabec`. Mail
rows, buddy rows, roster rows and invitation rows are all built out of quads.

A deleted account still has to render at full width. A short row shifts every later field, so
substitute a placeholder quad rather than emitting fewer fields.

### Permissions, and tribe 1401

**Pinned: exactly one rung.** `%editkick = %adminLevel >= 2` (`webbrowser.cs:1921`).

Everything else about the ladder is **chosen**. The reference implementation uses 4 founder,
3 administrator, 2 officer, 1 member, and forbids kicking or promoting at or above your own
level.

**Pinned, and worth knowing: `1401` is a tribe id, not a role.** `webbrowser.cs:409` and
`:2248` grant cross-tribe moderator powers to anyone whose certificate contains a tribe with
id 1401 and an admin level above 1. WON kept its staff in one tribe and checked membership of
it — the entire staff model is a row in the tribe table.

## Encoding rules

Collected here because they are easy to get subtly wrong and each one has cost somebody a
debugging session.

- **Lines are UTF-8, `\r\n`-terminated.** Fields within a line or a row are TAB. Records
  within a payload are `\S`. Records within a certificate are `\n`.
- **The only little-endian, UTF-16 construct in the entire protocol** is the WON directory
  frame in §9. Nothing in the community protocol is length-prefixed.
- **Booleans are the strings `"1"` and `"0"`.** The client `getField`s a string and tests
  truthiness.
- **Dates are opaque display strings.** The client parses, sorts and compares none of them.
- **A zero-line body is one empty line, not zero lines.** Emitting nothing shifts every later
  field.
- **A newline in a stored mail body arrives at the control as a TAB**, and that is what makes
  server-sent hyperlinks possible. The round trip:

  ```
  stored body   "line one\nline two"
    body lines  -> row fields 17.. = ["line one", "line two"]     (TAB-joined on the wire)
    client      -> %body = getFields(%row,17)   webemail.cs:1148  (TAB-joined, ONE record)
    EmailGetBody-> prints records 7.. verbatim  webemail.cs:167   (never touches tabs)
    on screen   "line one<TAB>line two"
  ```

  `GuiMLTextCtrl::onURL` splits the URL on TAB — the verb is `getField(%url,0)` and its
  arguments are fields 1.. (`webbrowser.cs:1063-1071`) — so an Accept link has to be
  literally `<a:acceptinvite<TAB>Tribe<TAB>Player>`. Writing a **newline** where the tag
  needs a TAB is therefore correct.

  Two consequences. **A mail body cannot contain a real line break**, and `%bodyCount`
  (field 16) is read into a variable and then never used in either branch (`:1147`, `:1128`)
  — the client takes the whole tail regardless.

**Why this matters beyond formatting:** nothing in the shipped scripts emits the
`acceptinvite`, `rejectinvite` or `requestlink` verbs, so their markup can only ever have
come from the server. And mail is the *only* channel for tribe invitations, because the
client has no player-scoped invite query at all — array 11 is tribe-scoped and admin-gated
(`webbrowser.cs:247`). An invitation that is recorded but not mailed does not exist.

## The WON directory and MOTD, which are recorded and not answered

`WONInit()` at `0x006f2380` hardcodes three directory servers —
`tribes2.m1/m2/m3.sierra.com:15104`, strings at `0x007d18e0`, `0x007d18fc` and `0x007d1918`.
These are **not** the game-server master list, which comes from `$pref::` in `serverQuery.cc`;
they are the WON service directory.

Redirecting those names to a recorder and calling `WONInit(); WONServerLogin();` produces
exactly one 55-byte request and then nothing:

```
0000  37 00 00 00 05 02 00 67 00 37 08 02 06 00 00 12  7......g.7......
0010  00 2f 00 54 00 69 00 74 00 61 00 6e 00 53 00 65  ./.T.i.t.a.n.S.e
0020  00 72 00 76 00 65 00 72 00 73 00 2f 00 41 00 75  .r.v.e.r.s./.A.u
0030  00 74 00 68 00 00 00                             .t.h...
```

Read as: a 4-byte little-endian total length (`0x37` = 55, **inclusive of itself**), eleven
bytes of header (`05 02 00 67 00 37 08 02 06 00 00`, **not interpreted**), a 16-bit
little-endian character count (`0x0012` = 18), then 18 UTF-16LE code units spelling
**`/TitanServers/Auth`** and a wide NUL that is not counted. "Titan" was WON's internal name
for its server platform, and the request matches the log literal
`Waiting\tFetching Authentication server list...` at `0x007d1a…`.

**That is the entire captured record of WONAPI's binary protocol.** One frame, one direction.
The eleven header bytes are not a documented constant and should not be presented as a fixed
header — at least part of that field is computed at runtime. Everything downstream is signed
against the key in §2, so the conversation cannot be continued, and every operation behind it
fails uniformly:

```
WONLoginResult()            -> Error  -1  AuthServerListFail
WONStartEmailFetch(...)     -> Error  -1  AuthServerListFail
WONStartLoginInfoFetch(...) -> Error  -1  AuthServerListFail
GetIRCServerList(0)         -> "" (empty)
```

Two things worth recording. **The account-email operations are a different thing from the
Email pane** — `WONStartEmailFetch` (`0x006f4c20`) and `WONStartLoginInfoFetch`
(`0x006f4d00`) mail you your own password or login name via WON's account service, and are
gated behind the auth server list, not the database proxy. And **the MOTD fetch is
unreachable from the console**: the WON SDK's own HTTP client (`GET ` `0x007ca2b8`, `Host: `
`0x007ca2d4`, `www.won.net` `0x007ca1b0`, paths `/motd/sys/` and `/motd/`) is driven by
`WONAPI::GetMOTDOp` inside the login sequence, no console command reaches it, and the
sequence never gets past the directory. A recorder on port 80 records nothing. That is a gap,
not a pass.

For completeness, the response headers that client parses are `Content-Length`,
`Content-Type`, `Last-Modified` and `Location`.

## What is not known

Collected in one place, each with its citation, because a specification that quietly resolves
its own ambiguities is worse than one that names them.

- **Ordinal 14's argument count.** Three call sites send scalar 14 with two argument shapes
  for what the client labels three operations: 3 fields to post news (`webforums.cs:494`),
  1 field to delete a post (`:552`), 1 field to admin-remove a post (`:1474`). Either the
  procedure was overloaded on argument count or one of these is a copy-paste error.
  Immediately below the third, `webforums.cs:1475` has the ordinal-63 version commented out.
  The client cannot distinguish the cases, so neither can a server; dispatching on field count
  is forced, not deduced.
- **Ordinal 23 field 9.** Read as `getWarriorProfile` it is `playerGfx`; read as
  `getVisitorOptions` (`webbrowser.cs:1745`) it is a **tribe count**, with fields 10.. a
  four-fields-per-tribe list. Nothing on the wire distinguishes them. The call sites compute a
  `%callId` of 1, 2 or 3 (`:1886`, `:1888`, `:1896`) and then never send it. Answering with
  the graphic is the reading that costs the other one nothing — Torque coerces a bitmap path
  to `0`, so `if(%callerTribes > 0)` fails and the options pane lists the caller's tribes out
  of the certificate instead (`:1762`), the branch it takes anyway. A graphic path beginning
  with a digit *would* be read as a count, so guard against that case.
- **Ordinals 62 and 63 carry a 0..7 selector** from eight adjacent call sites
  (`webbrowser.cs:1148-1211`) that nothing in the scripts names. Selector 0 of 62 is the only
  one with a known meaning.
- **Array 3's trailing flag.** The address book sends 1 (`webemail.cs:830`), the browser sends
  0 (`webbrowser.cs:95`). What it selects is not recoverable.
- **Ordinal 3's argument tuple** is assembled by the caller and not pinned field by field.
- **Ordinals 16 and 21** are pinned only to a leading field — `<name> \t <further fields>` and
  `<tribe> \t <further fields>`. Anything past that is a service's own choice, and the two
  reference implementations differ: `won-test-harness` accepts six fields for 16 (name, tag,
  append, recruiting, line count, description), `server/internal/dbproxy` accepts three.
- **Every field marked `-`** is padding whose contents nothing reads.
- **`$PlayerGfx` and `$TribeGfx` are read and never assigned.** `$PlayerGfx` is the fallback
  `PlayerPane::onWake` uses when a profile carries no graphic (`webbrowser.cs:1646`), the same
  fallback in the profile result branch (`:1671`), and what `TWBTabView::onSelect`
  unconditionally reinstates when you switch warrior tabs (`:1025`). No shipped script ever
  writes either. So a warrior with no graphic renders blank, permanently — which means an
  empty field 9 is **not** the neutral choice it looks like, and a server has to supply a
  default. Which default is **chosen**, and the two reference implementations disagree:
  `won-test-harness` sends `texticons/twb/twb_Laserrifle.jpg` (from
  `TribeAndWarriorBrowserGui.gui:444`) and gives tribes no default at all;
  `server/internal/dbproxy` sends `twb_Missilelauncher.jpg` for players
  (`WarriorPropertiesDlg.gui:348`) and `twb_Laserrifle.jpg` for tribes
  (`TribePropertiesDlg.gui:618`). Nothing on the wire favours either.
- **Nothing about the real WON server.** Every response ever recorded was written by a
  stand-in. The request side is the game's; the response side is a stimulus.
- **Nothing about WONAPI past the first directory frame**, and nothing about the auth protocol
  at all.

## Two things that are not protocol, but will bite

**Graphics are never uploaded.** Ordinal 31 carries a *path*, and the dialog that produces it
builds its list by scanning the player's own texture volume for `texticons/twb/twb_*.jpg`
(`WarriorPropertiesDlg::LoadGfxPane`, `webbrowser.cs:2567`; the tribe equivalent at `:2472`).
The "Graphic Requirements" text in `WarriorPropertiesDlg.gui` — 228×150, 28k, JPEG — and its
disabled `FIND NEW GRAPHIC` button (`visible = "0"`) are the residue of a custom-upload path
that did not ship.

**Three of the five panes have no controls in a retail install.** `NewsGui`, `ForumsGui` and
`weblinksmenu` are referenced by `webnews.cs`, `webforums.cs` and `weblinks.cs`, but a scan of
every `.vl2` and every loose `.cs`/`.gui` finds those names only in the script files that
drive them, never in a `new <class>(<name>)`. Their ordinals work and can be exercised at the
protocol level; there is nothing to click. That is a property of the game, not of any server.

## Verifying an implementation against this document

The reference implementation ships three checks that between them cover most of what is
specified above:

```sh
python3 -m wonproxy --self-test       # framing, against a port of the client's own decoder
python3 -m wonproxy --scenario-test   # 116 checks asserting field positions directly
python3 -m wonproxy --list            # the ordinal table, one line per entry
```

The self-test is the one that matters for §6: it runs `pack()` against a line-by-line port of
`HandleDatabaseProxyResponse`, **including its bugs**, across chunk sizes 8 to 39 so that
every escape and separator gets split at every offset. If a packer and that decoder ever
disagree, one of them has drifted from the script.

Nothing above can be checked against a real WON server, and nothing above should be presented
as though it had been.
