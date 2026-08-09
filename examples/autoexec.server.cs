// Example configuration for the TNBrowserServer mod.
//
// This is the *server* side: it runs on the machine hosting the game and gives
// connecting players their clan tag. Copy it into the mod directory as
// autoexec.cs, beside TNBrowserServer.vl2:
//
//     GameData/MyMod/TNBrowserServer.vl2
//     GameData/MyMod/autoexec.cs
//
// then launch the server with -mod MyMod.

// Your TNBrowser backend, reachable from the game server.
$TNBS::Host = "http://your-backend:8080";

// Must match the backend's TNB_SERVER_KEY. Without it the backend refuses the
// lookup and nobody gets a tag -- the mod says so once in the console rather
// than failing quietly.
//
// This key speaks for every player, so treat it as a secret: it is not a
// player's session token and is not scoped to one account.
$TNBS::ServerKey = "the value you passed to tnserver -server-key";

// How long a looked-up clan record stays good, in seconds. Clan membership
// changes rarely, and a stale tag for a few minutes is better than a network
// round trip on the connect path.
// $TNBS::CacheSeconds = 300;

// Log each lookup while setting things up.
// $TNBS::Debug = 1;
