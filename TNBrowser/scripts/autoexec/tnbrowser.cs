// TNBrowser -- entry point
//
// scripts/autoexec/*.cs is exec'd automatically for every directory on the mod
// path during boot, so this file needs no registration.
//
// The shipped community browser (scripts/webbrowser.cs, driven by the WON
// DatabaseQuery transport) is left in place and untouched. This mod adds its
// own TNB-prefixed GUIs and re-points the shell's BROWSER button at them via a
// package, so nothing is shadowed and deactivating the package restores the
// stock behaviour exactly.

exec("tnbrowser/settings.cs");
exec("tnbrowser/json.cs");
exec("tnbrowser/session.cs");
exec("tnbrowser/api.cs");
exec("tnbrowser/panes.cs");
exec("tnbrowser/cert.cs");
exec("tnbrowser/clanprops.cs");
exec("tnbrowser/playerprops.cs");
exec("tnbrowser/mail.cs");

// The .gui files are deliberately NOT exec'd here. autoexec runs before the
// shell control profiles exist, and a control built against a missing profile
// constructs successfully but renders with default styling instead of the game
// look. TNBEnsureGuis() in panes.cs loads them on first open instead.

package TNBrowser
{
   // The shell's BROWSER button calls LaunchBrowser(); send it to the working
   // implementation instead of the WON one, which cannot connect to anything.
   function LaunchBrowser(%pane, %type)
   {
      TNBrowserOpen();

      if (%pane $= "")
         return;

      if (%type $= "Tribe")
         TNBApiClanView(%pane, "TNBOpenClanTabFromLink", %pane);
      else if (%type $= "Warrior")
         TNBApiUserView(%pane, "TNBOpenPlayerTabFromLink", %pane);
   }

   // Same for the shell's EMAIL button: the stock LaunchEmail opens the
   // WON-backed mail window and schedules a CheckEmail that can never succeed.
   function LaunchEmail()
   {
      TNBMailOpen();
   }
};

activatePackage(TNBrowser);
activatePackage(TNBrowserLinks);

// The API layer's queue indices have to start from a known state.
TNBApiInit();

echo("TNBrowser: loaded (TribesNext profile and clan browser)");
