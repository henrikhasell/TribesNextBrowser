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

   // The launch bar's own EMAIL, CHAT and BROWSER tabs are dead: TribesNext
   // forces all three inactive (console_client_patches.cs) because the WON
   // services behind them shut down in 2003. They are added by OpenLaunchTabs
   // only when playing online, which is why they show up greyed out there and
   // not on a LAN game.
   //
   // Adopt the two this mod implements and drop the one it does not, so the bar
   // has no dead entries. Adopting rather than removing also stops the
   // duplicates: viewTab() matches a tab by GUI object, not by label, so
   // opening TNBrowserGui used to add a second "BROWSER" beside the grey one.
   // Now it finds this tab and just selects it.
   function LaunchTabView::addLaunchTab(%this, %text, %gui, %makeInactive)
   {
      if (%text $= "CHAT")
         return;

      if (%text $= "EMAIL" || %text $= "BROWSER")
      {
         // Safe here: OpenLaunchTabs runs once the shell is up, so the control
         // profiles these GUIs are built against already exist.
         if (%text $= "EMAIL")
         {
            TNBMailEnsureGuis();
            %gui = TNBMailGui;
         }
         else
         {
            TNBEnsureGuis();
            %gui = TNBrowserGui;
         }
         %makeInactive = false;
      }

      // Re-enable after the fact rather than passing %makeInactive through:
      // TribesNext's own override sits between this and the stock function and
      // sets the flag again on the way down.
      %index = %this.tabCount();
      Parent::addLaunchTab(%this, %text, %gui, %makeInactive);
      if (!%makeInactive)
         %this.setTabActive(%index, true);
   }

   // Catches the other way in: $pref::Shell::LaunchGui remembers the last tab by
   // name, so a player who quit on Email or Browser has OpenLaunchTabs open the
   // stock GUI directly, which would add a dead tab back beside the live one.
   function LaunchTabView::viewTab(%this, %text, %gui, %key)
   {
      if (%gui $= EmailGui)
      {
         TNBMailEnsureGuis();
         %gui = TNBMailGui;
      }
      else if (%gui $= TribeandWarriorBrowserGui)
      {
         TNBEnsureGuis();
         %gui = TNBrowserGui;
      }

      Parent::viewTab(%this, %text, %gui, %key);
   }
};

activatePackage(TNBrowser);
activatePackage(TNBrowserLinks);

// The API layer's queue indices have to start from a known state.
TNBApiInit();

echo("TNBrowser: loaded (TribesNext profile and clan browser)");
