// TNBrowserServer -- settings
//
// This is a *server-side* mod. It runs on a dedicated server (or a listen
// server) and gives connecting players their clan tag, which the stock
// server.cs then renders into their displayed name.
//
// Point it at a TNBrowser backend. The key must match the server's
// TNB_SERVER_KEY: this endpoint answers questions about *other* players, so it
// is not open.

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
