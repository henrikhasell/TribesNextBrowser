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

// Your TNBrowser backend, reachable from this game server. Normally baked in at
// build time (tools/build-vl2.sh --host, which sets it for both packages); set
// it here when this server reaches the backend at a different address than
// players do, or to override a build without rebuilding.
$TNBS::Host = "http://your-backend:8080";

// How long a looked-up clan record stays good, in seconds. Clan membership
// changes rarely, and a stale tag for a few minutes is better than a network
// round trip on the connect path.
// $TNBS::CacheSeconds = 300;

// Log each lookup while setting things up.
// $TNBS::Debug = 1;
