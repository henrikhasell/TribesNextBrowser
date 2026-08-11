// TNBrowserServer -- entry point
//
// A server-side companion to the TNBrowser client mod. Load it on a dedicated
// server (or a listen server) and connecting players get the clan tag their
// TNBrowser backend has for them, rendered into their name by the game's own
// server.cs.
//
// Install: copy TNBrowserServer.vl2 into GameData/<MOD>/ and launch the server
// with -mod <MOD>. The package built by CI needs nothing else -- it carries the
// public key it checks certificates against. For another backend, put its key
// in a loose autoexec.cs beside the archive:
//
//    $TNBS::ClanKeyE[1] = "10001";
//    $TNBS::ClanKeyN[1] = "...";
//
// which `tnserver -genkey` prints when it makes the key.
//
// Run the server WITHOUT -nologin. TribesNext's authentication phase is what
// establishes the GUID a certificate is bound to, and it only runs when
// $PlayingOnline is set -- which omitting that flag does. With it, no player
// has a verified identity and nobody is tagged.
//
// This mod makes no HTTP requests. It needs no network access of its own and
// the backend can be entirely unreachable from here.
//
// There are two entry points, deliberately. This one runs once during console
// init (console_end.cs: loadCustomScripts()), which covers a listen server the
// moment the game boots. scripts/TNBrowserServerGame.cs runs from inside
// CreateServer() every time a server starts, which covers the paths this one
// misses -- a boot that never reaches console_end.cs, or a mod path that no
// longer lists this mod by the time the game is hosted. Both are guarded, so
// whichever runs first wins and the other does nothing.
//
// This mod changes nothing else: no gameplay, no datablocks, no client
// requirements. A player without a clan, or connecting to a server that does
// not run this, is unaffected.

exec("tnbserver/settings.cs");
exec("tnbserver/clancert.cs");

echo("TNBrowserServer: loaded");
