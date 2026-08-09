// Example configuration for the TNBrowser client mod.
//
// Copy this into the mod directory as autoexec.cs, beside TNBrowser.vl2:
//
//     GameData/MyMod/TNBrowser.vl2
//     GameData/MyMod/autoexec.cs
//
// then launch with -mod MyMod. The game execs each mod's
// scripts/autoexec/*.cs first and this file last, so these assignments win
// over the mod's defaults. Leave it out entirely and the mod talks to
// TribesNext, which is the default.

// Where the browser, clan and mail data comes from.
$TNB::Host = "http://your-backend:8080";

// Where you log in. Leave this alone: your account lives at TribesNext, and the
// session token it issues is what your backend verifies you by.
// $TNB::AuthHost = "https://tribesnext.thyth.com";

// Un-hide the things only a TNBrowser backend can serve: Sent and Deleted mail
// folders, block lists, buddy lists, and mail sending that actually delivers.
// Leave it 0 when pointing at TribesNext -- those controls would only fail.
$TNB::FullFeatures = 1;

// Log protocol chatter to the console while setting things up.
// $TNB::Debug = 1;
