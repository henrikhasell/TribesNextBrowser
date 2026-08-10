// TNBrowser -- entry point
//
// scripts/autoexec/*.cs is exec'd automatically for every directory on the mod
// path during boot, so this file needs no registration.
//
// This mod ships no GUI. Not one: the community screens a player sees are the
// shipped EmailGui and TribeAndWarriorBrowserGui, driven by the shipped
// webemail.cs and webbrowser.cs, rendering through the shipped control
// profiles. What it replaces is the layer underneath -- DatabaseQuery(), the
// one call every community pane makes -- so the screens are identical to
// vanilla by construction rather than by careful copying.
//
// Everything below lives in a package, including the two WON identity
// functions, so deactivatePackage(TNBrowser) puts the shipped behaviour back
// exactly: DatabaseQuery goes back to framing dbqax down the chat socket, and
// WONGetAuthInfo goes back to not existing.

exec("tnbrowser/settings.cs");
exec("tnbrowser/json.cs");
exec("tnbrowser/session.cs");
exec("tnbrowser/api.cs");
exec("tnbrowser/dbproxy.cs");

package TNBrowser
{
   //-- the database proxy ----------------------------------------------------

   function DatabaseQuery(%ordinal, %args, %proxyObject, %key)
   {
      return TNBDbQuery(%ordinal, %args, %proxyObject, %key);
   }

   function DatabaseQueryArray(%ordinal, %maxRows, %args, %proxyObject, %key)
   {
      return TNBDbQueryArray(%ordinal, %maxRows, %args, %proxyObject, %key);
   }

   function DatabaseQueryCancel(%id)
   {
      TNBDbQueryCancel(%id);
   }

   //-- the identity ----------------------------------------------------------

   // Absent natives on a TribesNext client, so these define rather than
   // override. See dbproxy.cs for the record layout and what pins it.
   function WONGetAuthInfo()
   {
      return TNBCertGet();
   }

   // Eight sites call this after an operation that changes a tribe and two use
   // it as an if condition (webbrowser.cs:1788, :2245), so it has to answer
   // now. The refresh it kicks off lands a moment later; both call sites follow
   // the condition with a UI update rather than a re-read of the certificate,
   // which is what makes that safe.
   function WONUpdateCertificate()
   {
      TNBCertRefresh();
      return true;
   }

   //-- the launch bar --------------------------------------------------------

   // The shipped shell picks its online tab set on $PlayingOnline
   // (LaunchLanGui.cs:158), which is false here because nobody logs in to WON
   // any more -- so GAME / EMAIL / CHAT / BROWSER is never built and the two
   // panes this mod serves have no way in. Set it for the duration of the call
   // and put it back immediately: the global also reaches GameGui and the
   // server browser, and leaving it set would advertise internet play that does
   // not exist.
   function OpenLaunchTabs(%gotoWarriorSetup)
   {
      %was = $PlayingOnline;
      $PlayingOnline = true;

      // $pref::Shell::LaunchGui remembers the tab you quit on. Opening straight
      // into Email or Browser before the certificate has arrived renders a
      // warrior with no name and queries the empty string, so send that player
      // to GAME this once; the tab is right there when the identity lands.
      %remembered = $pref::Shell::LaunchGui;
      if (!TNBCertReady() && (%remembered $= "Email" || %remembered $= "Browser"))
         $pref::Shell::LaunchGui = "Game";

      Parent::OpenLaunchTabs(%gotoWarriorSetup);

      $PlayingOnline = %was;
      $pref::Shell::LaunchGui = %remembered;

      TNBCertRefresh("TNBTabsCertLoaded", "");
   }

   // TribesNext forces EMAIL, CHAT and BROWSER inactive (console_client_patches.cs)
   // because the WON services behind all three shut down in 2003. Two of them
   // work again; the third does not, and a dead tab is worse than no tab.
   //
   // Re-enabling happens after Parent:: rather than by passing %makeInactive
   // through, because TribesNext's own override sits between this and the
   // shipped function and sets the flag again on the way down.
   function LaunchTabView::addLaunchTab(%this, %text, %gui, %makeInactive)
   {
      if (%text $= "CHAT")
         return;

      %index = %this.tabCount();
      Parent::addLaunchTab(%this, %text, %gui, %makeInactive);

      if (%text $= "EMAIL" || %text $= "BROWSER")
      {
         $TNB::Tab[%text] = %index;

         // Live only once there is an identity behind them.
         %this.setTabActive(%index, TNBCertReady());
      }
   }
};

activatePackage(TNBrowser);

// The two panes come alive together, because they read the same certificate.
function TNBTabsCertLoaded(%ctx, %status, %result)
{
   if (%status !$= "ok" || !isObject(LaunchTabView))
      return;

   LaunchTabView.setTabActive($TNB::Tab["EMAIL"], true);
   LaunchTabView.setTabActive($TNB::Tab["BROWSER"], true);
}

// The request queue's indices have to start from a known state.
TNBApiInit();

echo("TNBrowser: loaded (community browser and mail over the DatabaseQuery proxy)");
