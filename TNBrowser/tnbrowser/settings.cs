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

// Two hosts, because logging in and holding the data are separate concerns.
//
// $TNB::AuthHost is always TribesNext: that is where the account lives and
// where the RSA challenge/response happens, and it is what makes the resulting
// session token meaningful to anyone else.
//
// $TNB::Host is wherever the browser, clan and mail data lives. Point it at a
// self-hosted TNBrowser backend to use your own community; leave it at
// TribesNext to use theirs. A custom backend verifies the token by asking
// TribesNext about it, so identity is the same either way.
if ($TNB::AuthHost $= "")
   $TNB::AuthHost = "https://tribesnext.thyth.com";

if ($TNB::Host $= "")
   $TNB::Host = "https://tribesnext.thyth.com";

// Robot session endpoint: RSA challenge/response, no password required.
// Always relative to $TNB::AuthHost.
if ($TNB::LoginURI $= "")
   $TNB::LoginURI = "/tn/robot/robot_login.php";

// Documented JSON browser API. Authorises with the guid/uuid pair minted by
// the robot login above.
if ($TNB::BrowserURI $= "")
   $TNB::BrowserURI = "/tn/json/json_browser.php";

// How often to ping the session so it stays alive, in seconds. The reference
// tournament client used 10 minutes.
if ($TNB::SessionRefresh $= "")
   $TNB::SessionRefresh = 10 * 60;

// Emit protocol chatter to the console. Never log the full request URI at
// level 1 -- it carries the session UUID.
if ($TNB::Debug $= "")
   $TNB::Debug = 0;

echo("TNBrowser: settings loaded (" @ $TNB::Host @ ")");
