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

// Print what the mod is doing. Useful while setting a server up; noisy after.
if ($TNBS::Debug $= "")
   $TNBS::Debug = 0;

echo("TNBrowserServer: settings loaded (" @ $TNBS::Host @ ")");
