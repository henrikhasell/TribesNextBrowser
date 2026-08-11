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

// The backend, and it is no longer needed at connect time -- or at all.
//
// This mod used to look every connecting player up over HTTP. It now checks a
// certificate the player carries, so a server can run with the backend
// unreachable, on a private network, or behind a firewall that lets nothing
// out, and still show clan tags. The setting is kept because it is where a
// future key refresh would fetch from, and because an operator who has one
// configured should not have to remove it.
if ($TNBS::Host $= "")
   $TNBS::Host = "http://localhost:8080";

// How long a connecting player is held while a certificate transfer that is
// already in progress finishes, in milliseconds.
//
// Almost never reached. The certificate is requested at the top of TribesNext's
// authentication phase and arrives long before the connect that needs it, so
// this covers only the case where the two genuinely race -- and a player whose
// client sent nothing never waits at all.
//
// There is a hard ceiling above this and it is not ours: TribesNext arms a
// 15-second expiry before its own auth phase (t2csri/serverSide.cs:260) and
// cancels it only after the connect completes, so a hold that outlasts it gets
// the player kicked with "This is a TribesNext server." Keep well clear.
if ($TNBS::WaitMs $= "")
   $TNBS::WaitMs = 1000;

// How many certificates a client may offer after it is already in the game.
//
// A player whose client had nothing to send when it joined is asked once, and
// pushes the certificate when its fetch lands -- so one is the ordinary number
// and the rest is headroom for a fetch that failed and was retried.
//
// A limit at all because nothing else bounds it: the transfer is the client's
// to start, and a broken or hostile one could open a new buffer for as long as
// it stayed connected. Failing this limit costs the tag, never the player.
if ($TNBS::LateTries $= "")
   $TNBS::LateTries = 3;

// What this server calls itself when it asks a client for a certificate. The
// client does not read it today; the shipped protocol's one extension point was
// exactly this, and it costs nothing to have one.
if ($TNBS::Version $= "")
   $TNBS::Version = "TNBrowserServer 1.0";

// Print what the mod is doing -- including every reason a player went untagged,
// which is the only way to tell a wrong key from an expired certificate from a
// client that is not running the mod. Useful while setting a server up; noisy
// after.
if ($TNBS::Debug $= "")
   $TNBS::Debug = 0;

// The public keys clan certificates are checked against. A separate file
// because it is the one an operator replaces, and tools/build-vl2.sh --clan-key
// substitutes it wholesale rather than rewriting a line in here.
exec("tnbserver/clankeys.cs");

echo("TNBrowserServer: settings loaded");
