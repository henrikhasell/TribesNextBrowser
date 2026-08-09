// TNBrowserServer -- settings
//
// This is a *server-side* mod. It runs on a dedicated server (or a listen
// server) and gives connecting players their clan tag, which the stock
// server.cs then renders into their displayed name.
//
// Point it at a TNBrowser backend. The key must match the server's
// TNB_SERVER_KEY: this endpoint answers questions about *other* players, so it
// is not open.
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
   $TNBS::Host = "http://127.0.0.1:8080";

if ($TNBS::AuthInfoURI $= "")
   $TNBS::AuthInfoURI = "/tn/server/authinfo";

if ($TNBS::ServerKey $= "")
   $TNBS::ServerKey = "";

// How long a cached lookup stays good, in seconds. Clan membership changes
// rarely, and a stale tag for a minute is better than a round trip on the
// connect path.
if ($TNBS::CacheSeconds $= "")
   $TNBS::CacheSeconds = 300;

// Print what the mod is doing. Useful while setting a server up; noisy after.
if ($TNBS::Debug $= "")
   $TNBS::Debug = 0;

echo("TNBrowserServer: settings loaded (" @ $TNBS::Host @ ")");
