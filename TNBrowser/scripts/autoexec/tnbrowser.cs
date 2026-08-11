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
exec("tnbrowser/clancert.cs");
exec("tnbrowser/chat.cs");

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

      // The clan certificate carries the same record, so whatever just changed
      // a tribe has invalidated it too. Re-issuing here is what lets a player
      // wear a new tag without relaunching.
      TNBClanCertFetch();
      return true;
   }

   //-- chat ------------------------------------------------------------------
   //
   // Five overrides, and between them they are the entire transport. The chat
   // client itself -- processLine, dispatch, the channel and person model, the
   // panes -- is shipped code that never learns where its bytes come from.

   // The shipped connect dials a TCPObject at an address from
   // GetIRCServerList(), which has answered "" since WON shut down. There is no
   // TLS behind that socket and no way to add one: the patch's libcurl and
   // mbedTLS are wired to HTTPObject alone.
   function IRCClient::connect()
   {
      TNBChatConnect();
   }

   // The one call in the whole of ChatGui.cs that touches the socket.
   function IRCClient::send(%message)
   {
      TNBChatSend(%message);
   }

   function IRCClient::disconnect()
   {
      TNBChatDisconnect();

      // Parent:: for everything else it does -- the away timer, the state, and
      // IRCClient::reset, which clears the channels and the person list.
      Parent::disconnect();
   }

   // The shipped handshake ends here, and so does ours; the difference is one
   // line of it.
   //
   // WONLoginIRC(%reply) is what the shipped version checks, and against a live
   // WON it validated the server's own proof. There is no WON session behind
   // this client, so the native returns "ERROR" and the shipped check refuses
   // every connection. What replaces it is stronger rather than weaker: this
   // stream is carried over TLS and authorised by the session negotiated
   // against the account certificate, so the backend proved itself before the
   // first line of chat rather than in the middle of it.
   //
   // Everything else is the shipped body, because everything else earns its
   // place: the identity assignment is what gives the local player a name and a
   // tribe tag in chat, and the two branches after it are what re-join the
   // rooms a reconnect stashed.
   function IRCClient::onChalRespReply(%prefix, %params)
   {
      %params = nextToken(%params, nick, " ");
      %params = nextToken(%params, ident, " ");
      %params = nextToken(%params, reply, " ");

      %me = $IRCClient::people.getObject(0);
      %me.displayName = %nick;
      IRCClient::setIdentity(%me, %ident);

      if ($IRCClient::state $= IDIRC_CONNECTED)
      {
         // A stream that came back inside the backend's grace period: the rooms
         // never noticed, so only the rendered name needs refreshing.
         IRCClient::correctNick(%me);
         return;
      }

      IRCClient::newMessage("", "Connected to " @ $TNB::Host);
      $IRCClient::state = IDIRC_CONNECTED;
      IRCClient::notify(IDIRC_CONNECTED);

      if ($IRCClient::awaytimeout)
         cancel($IRCClient::awaytimeout);
      $IRCClient::awaytimeout = schedule($AWAY_TIMEOUT, 0, "ChatAway_Timeout");

      if (strlen($IRCClient::room))
         IRCClient::send("JOIN " @ $IRCClient::room);

      for (%i = 0; %i < $IRCClient::previousChannelCount; %i++)
         IRCClient::join($IRCClient::previousChannel[%i] SPC $IRCClient::previousKey[%i]);
      $IRCClient::previousChannelCount = 0;
   }

   // A replacement rather than a wrapper, for two reasons.
   //
   // The first is that the shipped one splits the name^tag^append triple with
   // IRCGetTriple, and that native is not always there: on a client the patch
   // started with -nologin it is simply absent, and calling it prints "Unable
   // to find function IRCGetTriple" and leaves %p.nick empty -- which shows up
   // as every member of the room rendering with raw carets. Splitting three
   // fields in script costs nothing and cannot be missing.
   //
   // The second is escaping. Every parameter in this protocol is
   // space-delimited, so "Sir Lancelot" travels as "Sir_-_01Lancelot" -- the
   // shipped spelling, from IRCClient::doEscapes -- and something has to undo
   // it. IRCClient::displayChannel already does for channel names; nothing ever
   // did for nicks, because WON's own nicks could not contain a space.
   //
   // %p.displayName is deliberately left as it arrived. It is the wire identity
   // that IRCClient::findPerson matches on and that a private message has to be
   // addressed to; only the rendered forms are unescaped.
   function IRCClient::setIdentity(%p, %ident)
   {
      %p.identity = %ident;

      %rest = nextToken(%p.displayName, name, "^");
      %rest = nextToken(%rest, tag, "^");
      nextToken(%rest, append, "^");

      %name = IRCClient::undoEscapes(%name);
      %tag = IRCClient::undoEscapes(%tag);
      %p.untagged = %name;

      // The layout is the shipped one, markup and all: <tribe:1> is the tag and
      // <tribe:0> the name, and the chat pane turns both into clickable links.
      if (%tag $= "")
      {
         %p.nick = %name;
         %p.tagged = "<tribe:0>" @ %name @ "</tribe>";
      }
      else if (%append)
      {
         %p.nick = %name @ %tag;
         %p.tagged = "<tribe:0>" @ %name @ "</tribe><tribe:1>" @ %tag @ "</tribe>";
      }
      else
      {
         %p.nick = %tag @ %name;
         %p.tagged = "<tribe:1>" @ %tag @ "</tribe><tribe:0>" @ %name @ "</tribe>";
      }
   }

   // Swallow the five connection failures that raise a modal dialog.
   //
   // The shipped client had one chance to reach WON and told the player when it
   // failed. This one holds a stream that is retired every ten minutes by
   // design and reopened continuously, so "You have been disconnected from ..."
   // would be a dialog in the player's face during an ordinary reconnect. The
   // failure is still visible where it belongs: the chat pane stops receiving.
   function IRCClient::notify(%event)
   {
      if (%event $= IDIRC_ERR_HOSTNAME || %event $= IDIRC_ERR_TIMEOUT ||
          %event $= IDIRC_ERR_DROPPED || %event $= IDIRC_ERR_BADCHALLENGE ||
          %event $= IDIRC_ERR_BADCHALRESP_REPLY)
         return;

      Parent::notify(%event);
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

   // Fetch on opening the pane.
   //
   // The shipped wake does start a fetch, but only through
   // rbInbox.setValue(1) -> EMailGui.ButtonClick(0) -> GetEMailBtnClick(), and
   // only inside the `!cacheLoaded || EM_Browser.rowCount() == 0` branch
   // (webemail.cs:995). So the pane fetches when it has nothing to show and
   // does not when it has: once the cache holds messages, opening EMAIL renders
   // the cache and then waits out the rest of the five-minute poll. Mail that
   // arrived in between is simply not there, and the player has no way to know
   // the difference between that and an empty mailbox.
   //
   // onWake rather than any of the routes in, because every route ends here --
   // a tab click and viewLastTab (both through LaunchTabView::onSelect above),
   // the initial viewTab inside OpenLaunchTabs, and LaunchEmail() from the
   // toolbar menu (webemail.cs:37).
   function EmailGui::onWake(%this)
   {
      // Parent:: first: it loads the cache and clears checkingEmail (:997),
      // and that flag is set at boot by console_client_patches.cs:1821 -- so a
      // fetch before this returns at CheckEmail's first line on the one wake
      // that most needs it.
      Parent::onWake(%this);

      // Already fetching, either from that shipped branch or from a poll still
      // in flight. Its answer lands in the handlers below either way.
      if (%this.checkingEmail)
         return;

      // The guard TNBCertEnsure uses, for the reason it uses it: with no
      // TribesNext account there is no session to negotiate, and a player on
      // the way to a LAN game should not meet a MessageBoxOK about it. Nothing
      // is waited on beyond this -- the request queue negotiates the session
      // lazily and replays the request (api.cs:147), so a fetch issued before
      // the certificate lands still completes.
      if (TNBSessionGuid() $= "")
         return;

      // false, not true: this is the player asking, the same as GET MAIL. It
      // cancels the pending timer (webemail.cs:377) instead of leaving the
      // fetch to it.
      CheckEmail(false);
   }

   // TribesNext forces EMAIL, CHAT and BROWSER inactive (console_client_patches.cs)
   // because the WON services behind all three shut down in 2003. All three
   // work again: the chat client is the shipped one, over the stream in
   // tnbrowser/chat.cs.
   //
   // Re-enabling happens after Parent:: rather than by passing %makeInactive
   // through, because TribesNext's own override sits between this and the
   // shipped function and sets the flag again on the way down.
   function LaunchTabView::addLaunchTab(%this, %text, %gui, %makeInactive)
   {
      %index = %this.tabCount();
      Parent::addLaunchTab(%this, %text, %gui, %makeInactive);

      // Unconditionally live. An earlier version gated these on the
      // certificate having arrived, which was wrong twice over: it made the
      // tabs permanently dead on a client that never gets one, and it needed
      // the identity fetched at shell startup to un-gate them -- so a player
      // who was not logged in met a session error before touching anything.
      // The identity is now fetched when a pane is actually opened, below.
      if (%text $= "EMAIL" || %text $= "BROWSER" || %text $= "CHAT")
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
   if (TNBSessionGuid() $= "")
      return;

   // The clan certificate is fetched from here too, and before the early
   // return below: the identity being in hand says nothing about whether we
   // hold a signed clan record, and this is the moment a player is known to
   // have a session and to be somewhere other than in a game. It guards itself
   // against refetching one we already have.
   TNBClanCertEnsure();

   // Same reasoning, one layer along: the chat stream is opened at boot, which
   // is before there is an account to open it with, so the backoff can leave
   // the CHAT tab dead for a minute after the player logs in. This is the first
   // moment they are known to have a session.
   TNBChatNudge();

   if (TNBCertReady() || $TNB::CertPending)
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
   if (isObject(%gui) && Canvas.getContent() $= %gui)
      %gui.onWake();
}

// The request queue's indices have to start from a known state.
TNBApiInit();

echo("TNBrowser: loaded (community browser and mail over the DatabaseQuery proxy)");
