// TNBrowser -- TribesNext profile and clan browser
//
// Endpoint and session settings.
//
// The community backend is reached over HTTPS. That is possible because the
// TribesNext client patch (IFC22.dll) reimplements HTTPObject on top of
// libcurl and ships curl-ca-bundle.crt for certificate verification; the
// stock Torque HTTPObject could only speak plain HTTP.
//
// Note that the query string is part of the request-URI rather than a separate
// argument -- see TNBHttpRequest in api.cs for why the third argument to
// HTTPObject.get() is not the one you would expect it to be.
//
// Setting these without unpacking the .vl2
// -----------------------------------------------------------------------------
// Put a plain autoexec.cs beside the archive:
//
//     GameData/base/TNBrowser.vl2
//     GameData/base/autoexec.cs       <- your settings
//
// The game execs scripts/autoexec/*.cs from every mod first, then "autoexec.cs"
// last (base/console_end.cs: loadCustomScripts(); exec("autoexec.cs")), so a
// plain assignment there wins over the defaults below.
//
// It has to be inside a mod directory, not GameData/ -- exec() resolves through
// the mod path, so a file at the GameData root is never found. Verified both
// ways.
//
// Keeping it a loose file rather than putting it in the archive means it
// survives replacing the .vl2 with a newer build.

// One host, where there used to be two.
//
// $TNB::Host is the TNBrowser backend: the browser, clan and mail data, and now
// the session as well. It holds no accounts and no passwords -- it verifies the
// account certificate your client already has against TribesNext's signing key,
// then makes you prove you hold the private half. That is the same check every
// game server performs on a connecting player, and it needs TribesNext no more
// than they do.
//
// There used to be a $TNB::AuthHost pointing at tribesnext.thyth.com, because
// the session was negotiated with their robot login and then verified with
// them a second time. Neither round trip exists now, so neither does the
// setting: nothing in this mod contacts TribesNext at all.
//
// The default suits a backend on the same machine, which is the development
// case. A real deployment bakes its own address in at build time
// (tools/build-vl2.sh --host), or sets it in a loose autoexec.cs as above.
if ($TNB::Host $= "")
   $TNB::Host = "http://localhost:8080";

// Session endpoint: RSA challenge/response against the account certificate, no
// password required. Relative to $TNB::Host.
if ($TNB::SessionURI $= "")
   $TNB::SessionURI = "/session";

// The database proxy. One endpoint for all 61 stored-procedure ordinals the
// shipped community scripts issue -- an ordinal and its arguments go up, a
// status and rows come back. Authorises with the guid/uuid pair from
// $TNB::SessionURI above.
if ($TNB::DbURI $= "")
   $TNB::DbURI = "/db";

// The identity WONGetAuthInfo() hands to the shipped scripts. Separate from the
// proxy because it is not one of WON's ordinals -- the real client had the
// certificate inside the process, delivered by a login this one does not run.
if ($TNB::CertURI $= "")
   $TNB::CertURI = "/cert";

// The clan certificate: the signed record a game server renders into your name.
// Relative to $TNB::Host. See tnbrowser/clancert.cs.
if ($TNB::ClanCertURI $= "")
   $TNB::ClanCertURI = "/clancert";

// How long to wait before asking for a clan certificate again after a failed
// attempt, in milliseconds.
//
// Long, because there is nothing to hurry for. A backend with no signing key
// answers 404 and will keep doing so; the only cost of not having a certificate
// is that your clan tag does not show, and the only thing a fast retry would
// achieve is a request every few seconds for the rest of the session.
if ($TNB::ClanCertRetryMs $= "")
   $TNB::ClanCertRetryMs = 5 * 60 * 1000;

// How often to ping the session so it stays alive, in seconds. The reference
// tournament client used 10 minutes.
if ($TNB::SessionRefresh $= "")
   $TNB::SessionRefresh = 10 * 60;

// How long to wait before polling for mail again after a poll failed, in
// milliseconds.
//
// The shipped client has no such interval because it never retries: its error
// branch leaves EmailGui.checkingEmail set and no timer armed, so a single
// failed poll costs the whole session's mail. Shorter than the five minutes a
// successful poll waits, since the usual reason to be here is a race the next
// attempt will win.
if ($TNB::MailRetryMs $= "")
   $TNB::MailRetryMs = 30 * 1000;

// Emit protocol chatter to the console. Never log the full request URI at
// level 1 -- it carries the session UUID.
if ($TNB::Debug $= "")
   $TNB::Debug = 0;

echo("TNBrowser: settings loaded (" @ $TNB::Host @ ")");
