// TNBrowser -- TribesNext profile and clan browser
//
// Endpoint and session settings.
//
// The community backend is reached over HTTPS. That is possible because the
// TribesNext client patch (IFC22.dll) reimplements HTTPObject on top of
// libcurl and ships curl-ca-bundle.crt for certificate verification; the
// stock Torque HTTPObject could only speak plain HTTP.
//
// Verified against the live backend from inside the game:
//   HTTPObject.get("https://tribesnext.thyth.com",
//                  "/tn/robot/robot_login.php?guid=...&nonce=...", "")
// returns a CHALLENGE. Note the query string is part of the request-URI --
// see TNBHttpRequest in api.cs for why the third argument is not used.
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

// Two hosts, because proving who you are and holding the data are separate
// concerns.
//
// $TNB::AuthHost is always TribesNext, and is used for exactly one thing: the
// RSA challenge/response login below. That is where the account lives, and it
// is what makes the resulting session token meaningful to anyone else. Nothing
// else in this mod contacts it -- grep for AuthHost and you should find this
// block and session.cs, nowhere more.
//
// $TNB::Host is the TNBrowser backend holding the browser, clan and mail data:
// one central server that players' clients and game servers both talk to. It
// verifies your session by asking TribesNext about the token, so identity is
// TribesNext's either way, while the data is yours.
//
// The default suits a backend on the same machine, which is the development
// case. A real deployment bakes its own address in at build time
// (tools/build-vl2.sh --host), or sets it in a loose autoexec.cs as above.
if ($TNB::AuthHost $= "")
   $TNB::AuthHost = "https://tribesnext.thyth.com";

if ($TNB::Host $= "")
   $TNB::Host = "http://localhost:8080";

// Robot session endpoint: RSA challenge/response, no password required.
// Always relative to $TNB::AuthHost.
if ($TNB::LoginURI $= "")
   $TNB::LoginURI = "/tn/robot/robot_login.php";

// The database proxy. One endpoint for all 61 stored-procedure ordinals the
// shipped community scripts issue, and the reason this backend cannot be
// TribesNext's: json_browser.php speaks methods and JSON objects, not ordinals
// and rows. Authorises with the same guid/uuid pair the robot login mints.
if ($TNB::DbURI $= "")
   $TNB::DbURI = "/db";

// The identity WONGetAuthInfo() hands to the shipped scripts. Separate from the
// proxy because it is not one of WON's ordinals -- the real client had the
// certificate inside the process, delivered by a login this one does not run.
if ($TNB::CertURI $= "")
   $TNB::CertURI = "/cert";

// How often to ping the session so it stays alive, in seconds. The reference
// tournament client used 10 minutes.
if ($TNB::SessionRefresh $= "")
   $TNB::SessionRefresh = 10 * 60;

// Emit protocol chatter to the console. Never log the full request URI at
// level 1 -- it carries the session UUID.
if ($TNB::Debug $= "")
   $TNB::Debug = 0;

echo("TNBrowser: settings loaded (" @ $TNB::Host @ ")");
