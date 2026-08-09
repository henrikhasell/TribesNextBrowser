// TNBrowserServer -- entry point
//
// A server-side companion to the TNBrowser client mod. Load it on a dedicated
// server (or a listen server) and connecting players get the clan tag their
// TNBrowser backend has for them, rendered into their name by the game's own
// server.cs.
//
// Install: copy TNBrowserServer.vl2 into GameData/<MOD>/ and launch the server
// with -mod <MOD>. Set the one thing it needs:
//
//    $TNBS::Host = "http://your-backend:8080";
//
// in the server's prefs, or edit tnbserver/settings.cs.
//
// This mod changes nothing else: no gameplay, no datablocks, no client
// requirements. A player without a clan, or connecting to a server that does
// not run this, is unaffected.

exec("tnbserver/settings.cs");
exec("tnbserver/authinfo.cs");

echo("TNBrowserServer: loaded");
