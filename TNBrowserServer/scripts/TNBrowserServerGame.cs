// TNBrowserServer -- server-side entry point
//
// CreateServer() execs every "scripts/*Game.cs" on the mod path stack, after
// its fixed list of datablock files and before the mission loads:
//
//    %search = "scripts/*Game.cs";
//    for(%file = findFirstFile(%search); %file !$= ""; %file = findNextFile(%search))
//       exec("scripts/" @ fileBase(%file) @ ".cs");     // base/scripts/server.cs
//
// That is the documented hook for server-side mod content, and the reason this
// file exists alongside scripts/autoexec/tnbserver.cs. The autoexec copy runs
// once during console init -- long before any server exists, and only on a boot
// path that reaches console_end.cs. This one runs every time a server starts,
// on a listen server and a dedicated server alike, so the clan-tag hook is in
// place whichever way the game got there.
//
// The scan reaches inside .vl2 archives, since the resource index is built from
// each mod directory's loose files plus its archives -- so dropping the packaged
// mod into GameData/<MOD>/ is still the whole install.
//
// Despite the name this declares no gametype: it defines no <Type>Game class,
// so it can never be selected as a mission type. The name is dictated by the
// scan's pattern, not by what the file does.

// Idempotent by design. CreateServer() runs on every map change, and the
// package survives DestroyServer() -- that only deactivates the packages the
// Game object itself activated (game.deactivatePackages()), never this one.
if (!isActivePackage(TNBrowserServer))
{
   exec("tnbserver/settings.cs");
   exec("tnbserver/clancert.cs");

   echo("TNBrowserServer: loaded at server start");
}
