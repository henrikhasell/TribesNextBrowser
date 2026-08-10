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

   // NOT an absent native, whatever -nologin suggests: TribesNext defines this
   // itself in t2csri/clientSide.cs:12, and on any client that can go online it
   // is already there. So this is a real override with a real predecessor, and
   // the predecessor did something besides answer -- it assigned
   // $LoginCertificate on the way past, which is the only thing that populates
   // that global on the ordinary login path. TNBSessionGuid no longer depends
   // on it; see session.cs.
   //
   // Falling back matters for the same reason. TribesNext's version drives the
   // yellow active-account highlight on the warrior screen and suppresses a
   // warning when leaving a server, so answering "" until the community
   // certificate lands would break both. The two layouts agree where it counts:
   // field 0 is the name and field 3 the GUID either way.
   function WONGetAuthInfo()
   {
      %cert = TNBCertGet();
      if (%cert !$= "")
         return %cert;

      %account = TNBAccountCertificate();
      if (%account $= "")
         return "";

      return getField(%account, 0) @ "\t\t0\t" @ getField(%account, 1) @ "\n";
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
      // Before Parent::, because Parent:: opens a pane -- and with the launch
      // screen set to Email or Browser (OptionsDlg, $pref::Shell::LaunchGui,
      // read at LaunchLanGui.cs:165) that pane wakes and reads the identity
      // inside this call. Both of these have to be true by then.
      TNBCachePathSync();
      TNBCertEnsure("");

      %was = $PlayingOnline;
      $PlayingOnline = true;

      Parent::OpenLaunchTabs(%gotoWarriorSetup);

      $PlayingOnline = %was;
   }

   // Make a failed mail poll recoverable.
   //
   // CheckEmail (webemail.cs:369) clears checkSchedule and sets checkingEmail
   // before issuing the query, and only the success branches put either back.
   // The error branch (:1096) shows a MessageBoxOK and stops -- so checkingEmail
   // stays set, every later CheckEmail returns at its first line, no timer is
   // armed, and mail is dead for the rest of the session. GET MAIL does not help
   // either; it goes through the same early return.
   //
   // Stock could afford that: a WON client had its chat socket up long before
   // the shell, so the fabricated ORA-04061 that DatabaseQueryi answers with no
   // connection (webstuff.cs:186) almost never fired. Here the first poll can
   // genuinely lose a race with the session, and one lost race should not cost
   // the session's mail.
   function EMailGui::onDatabaseQueryResult(%this, %status, %result, %key)
   {
      Parent::onDatabaseQueryResult(%this, %status, %result, %key);

      if (%this.state !$= "error")
         return;

      %this.checkingEmail = false;
      if (!isEventPending(%this.checkSchedule))
         %this.checkSchedule = schedule($TNB::MailRetryMs, 0, "CheckEmail", true);
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

      // Unconditionally live. An earlier version gated these on the
      // certificate having arrived, which was wrong twice over: it made the
      // tabs permanently dead on a client that never gets one, and it needed
      // the identity fetched at shell startup to un-gate them -- so a player
      // who was not logged in met a session error before touching anything.
      // The identity is now fetched when a pane is actually opened, below.
      if (%text $= "EMAIL" || %text $= "BROWSER")
         %this.setTabActive(%index, true);
   }

   // Every route into a pane goes through here -- a tab click, viewLastTab,
   // and the initial viewTab that OpenLaunchTabs ends with. So it is the one
   // place to make sure there is an identity behind the two panes that read
   // one, and the only place this mod contacts the network without the player
   // having asked for something.
   function LaunchTabView::onSelect(%this, %id, %text)
   {
      Parent::onSelect(%this, %id, %text);

      %gui = %this.gui[%this.getSelectedTab()];
      if (%gui $= EmailGui || %gui $= TribeandWarriorBrowserGui)
         TNBCertEnsure(%gui);
   }
};

activatePackage(TNBrowser);

// Fetch the certificate if we do not have one, and re-wake the pane once it
// lands so it renders against a real identity instead of an empty name.
//
// The pane is shown either way. Refusing to show it would turn "you are not
// logged in" into a tab that does nothing when clicked, which is a worse
// failure than the shipped screens' own -- they put whatever the server says
// in a MessageBoxOK, which is at least a sentence the player can act on.
//
// The account check is the one thing that must not be skipped when this is
// called from the shell rather than from a tab. TNBSessionStart answers a
// missing account by calling error() with "You are not logged in to a
// TribesNext account" (session.cs:166), and a player who never intended to use
// any of this should not meet that on the way to a LAN game. It is a
// synchronous read, so it costs nothing to ask.
function TNBCertEnsure(%gui)
{
   if (TNBCertReady() || $TNB::CertPending)
      return;
   if (TNBSessionGuid() $= "")
      return;

   $TNB::CertPending = 1;
   TNBCertRefresh("TNBCertEnsureDone", %gui);
}

function TNBCertEnsureDone(%gui, %status, %result)
{
   $TNB::CertPending = "";

   if (%status !$= "ok" || !TNBCertReady())
      return;

   // onWake reads the warrior name out of the certificate and opens that
   // warrior's page, so the pane has to be woken again now there is one.
   //
   // Called directly, not through Canvas.setContent: setting the content to
   // what it already is does not re-wake anything, so the setContent this used
   // to do was silently doing nothing at all.
   //
   // The browser only. EmailGui reads no identity on wake beyond the cache
   // path, which TNBCachePathSync has already settled -- and its onWake starts
   // by clearing EM_Browser and the message vector, so waking it here would
   // empty a mailbox that had just arrived.
   if (isObject(%gui) && %gui $= TribeandWarriorBrowserGui &&
       Canvas.getContent() $= %gui)
      %gui.onWake();
}

// The request queue's indices have to start from a known state.
TNBApiInit();

echo("TNBrowser: loaded (community browser and mail over the DatabaseQuery proxy)");
