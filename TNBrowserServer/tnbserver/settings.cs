// TNBrowserServer -- settings
//
// This is a *server-side* mod. It runs on a dedicated server (or a listen
// server) and gives connecting players their clan tag, which the stock
// server.cs then renders into their displayed name.
//
// Point it at your TNBrowser backend -- normally one central server that every
// participating game server and every player's client talks to, so the default
// below is a development convenience rather than a useful guess. Set it at
// build time (tools/build-vl2.sh --host) or in the autoexec.cs described below.
//
// The lookup it uses needs no credential: a name and a clan tag are public,
// visible on the scoreboard of every server the player joins.
//
// Setting these without unpacking the .vl2
// -----------------------------------------------------------------------------
// Put a plain autoexec.cs in the mod directory, beside the archive:
//
//     GameData/MyMod/TNBrowser.vl2
//     GameData/MyMod/autoexec.cs      <- your settings
//
// The game execs scripts/autoexec/*.cs from every mod first, then "autoexec.cs"
// last (base/console_end.cs: loadCustomScripts(); exec("autoexec.cs")), so a
// plain assignment there wins over the defaults below.
//
// It has to be inside the mod directory, not GameData/ -- exec() resolves
// through the mod path, so a file at the GameData root is never found. Verified
// both ways.
//
// Keeping it a loose file rather than putting it in the archive means it
// survives replacing the .vl2 with a newer build.

if ($TNBS::Host $= "")
   $TNBS::Host = "http://localhost:8080";

if ($TNBS::AuthInfoURI $= "")
   $TNBS::AuthInfoURI = "/tn/server/authinfo";

// How long a cached lookup stays good, in seconds. Clan membership changes
// rarely, and a stale tag for a minute is better than a round trip on the
// connect path.
if ($TNBS::CacheSeconds $= "")
   $TNBS::CacheSeconds = 300;

// How long a connecting player is held while their tag is looked up, in
// milliseconds. Only a cold cache waits, and only until the backend answers --
// this is the cap for a backend that accepts the connection and then says
// nothing.
//
// It caps the HTTP transfer as well, since there is no reason to wait longer
// for an answer than we are willing to hold a player for it. The two clocks
// start at different moments: a player's begins when they connect, a transfer's
// when it reaches the head of the queue. So somebody queued behind a slow
// lookup joins untagged on time rather than waiting for a stranger's request,
// and the record that arrives afterwards still warms the cache for their next
// join.
//
// There is a hard ceiling above this and it is not ours: TribesNext arms a
// 15-second expiry before its own auth phase (t2csri/serverSide.cs:226) and
// cancels it only after the connect completes, so a hold that outlasts it gets
// the player kicked with "This is a TribesNext server." Keep well clear.
if ($TNBS::WaitMs $= "")
   $TNBS::WaitMs = 2000;

// Print what the mod is doing. Useful while setting a server up; noisy after.
if ($TNBS::Debug $= "")
   $TNBS::Debug = 0;

echo("TNBrowserServer: settings loaded (" @ $TNBS::Host @ ")");
