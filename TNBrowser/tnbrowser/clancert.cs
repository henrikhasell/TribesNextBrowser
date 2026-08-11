// TNBrowser -- the clan certificate a player carries into a game
//
// A game server builds the displayed name from GameConnection::getAuthInfo(),
// synchronously, inside onConnect. This is how the clan half of that record
// gets there: the backend signs it, the client carries it, and a game server
// running TNBrowserServer checks the signature offline. No lookup on the
// connect path, and nothing for the game server to hold a player waiting for.
//
// Format, one line, six tab-separated fields:
//
//    KeyID <TAB> Issued <TAB> Expire <TAB> GUID <TAB> HexBlob <TAB> Sig
//
// # It is only ever sent to a server that asks
//
// The certificate lives in $TNB::ClanCert and NOT in
// $T2CSRI::CommunityCertificate, which is the shipped global the client offers
// to every server that pokes it. That distinction is the whole safety property
// of this design.
//
// A stock TribesNext server receiving a community certificate it cannot resolve
// does not shrug: serverSideClans.cs:156 asks the client for the issuing DCE's
// certificate, clientSideClans.cs:31 has none to send and returns silently,
// serverSide.cs:57 then reschedules the challenge every 250ms, and 15 seconds
// later t2csri_expireClient drops the player with "This is a TribesNEXT server.
// You must install the TribesNEXT client to play." -- telling them to install a
// patch they are already running.
//
// So the send is gated on a server asking for it by name. A server without the
// mod never asks, this client never sends, and joining one is exactly as it was
// before this file existed.
//
// # Timers are relative, and deliberately
//
// Freshness is decided from the certificate's own issued/expire fields as a
// duration, never by comparing an expiry against a local clock. The client has
// no reason to trust its own clock against the backend's, and a machine an hour
// out would otherwise either discard every certificate on arrival or keep them
// long past their life. Relative timers cannot be wrong that way.

//-----------------------------------------------------------------------------
// Fetch
//-----------------------------------------------------------------------------

// Get one if we do not already have one.
//
// This is the shell-open path, and it must be cheap to call repeatedly: opening
// EMAIL and then BROWSER should not cost two certificates. TNBClanCertFetch is
// the unconditional form, for the paths that know the one we hold is stale.
function TNBClanCertEnsure()
{
   if ($TNB::ClanCert !$= "")
      return;

   TNBClanCertFetch();
}

// Ask the backend for a fresh certificate.
//
// Silent about everything. There is no failure here worth putting in front of a
// player: a certificate that does not arrive costs a clan tag, and the pane
// they were actually looking at is unaffected.
function TNBClanCertFetch()
{
   if ($TNB::ClanCertPending)
      return;

   // The guard TNBCertEnsure uses, for the same reason: with no TribesNext
   // account there is no session to negotiate, and nothing should try.
   if (TNBSessionGuid() $= "")
      return;

   $TNB::ClanCertPending = 1;
   TNBApiEnqueueOn($TNB::ClanCertURI, "clancert", "", "TNBClanCertLoaded", "", 0);
}

function TNBClanCertLoaded(%ctx, %status, %result)
{
   $TNB::ClanCertPending = "";

   if (%status !$= "ok")
   {
      // A backend with no signing key answers 404, which arrives here as an
      // unreadable response -- indistinguishable from being offline, and it
      // does not matter which it was. Try again later, rarely.
      TNBClanCertRetryLater();
      return;
   }

   // Copied out now: the parsed tree is freed as soon as this returns.
   %cert = TNBJsonStr(%result, "cert");

   if (getFieldCount(%cert) != 6)
   {
      TNBClanCertRetryLater();
      return;
   }

   %issued = getField(%cert, 1);
   %expire = getField(%cert, 2);
   %life = %expire - %issued;

   // A certificate with no life left in it is not worth carrying, and a
   // negative one means the backend is confused. Either way, do not hold it.
   if (%life <= 0)
   {
      TNBClanCertRetryLater();
      return;
   }

   $TNB::ClanCert = %cert;

   TNBClanCertCancelTimers();
   // Refresh at half life, discard at nine tenths. The gap between the two is
   // the margin for a refresh that fails: it gets the rest of the certificate's
   // life to succeed, and if it never does the stale one is dropped before it
   // expires rather than after.
   $TNB::ClanCertRenew = schedule(%life * 500, 0, "TNBClanCertFetch");
   $TNB::ClanCertDrop = schedule(%life * 900, 0, "TNBClanCertDiscard");

   if ($TNB::Debug)
      echo("TNBrowser: clan certificate held, key " @ getField(%cert, 0) @
           ", " @ %life @ "s");

   // A server asked for this while we had nothing, and we are still on it.
   // Handing it over now is what gets a player tagged who joined before their
   // first certificate arrived -- the server rebuilds the name in place.
   //
   // Compared against the connection that asked, so a fetch that outlived the
   // connection is dropped rather than pushed at whatever server we are on now.
   if ($TNB::ClanCertAsked !$= "")
   {
      %asked = $TNB::ClanCertAsked;
      $TNB::ClanCertAsked = "";

      if (isObject(ServerConnection) && %asked == ServerConnection)
         TNBClanCertSend();
   }
}

// Drop what we have.
//
// Sending nothing is the correct degradation and sending something unverifiable
// is not: the game server would refuse it and the player would be untagged
// either way, but only one of those costs a round trip and a log line.
function TNBClanCertDiscard()
{
   $TNB::ClanCert = "";
   TNBClanCertCancelTimers();
   TNBClanCertFetch();
}

// A fetch that failed, to be tried again later.
//
// The renew timer only. The discard timer must survive this, and an earlier
// version of it did not: cancelling both here meant the first failed refresh
// also cancelled the drop that was going to protect the certificate we still
// hold, and nothing ever re-armed it. The client then went on offering an
// expired certificate to every server it joined -- untagged either way, since
// nothing here can refuse a player, but a transfer and a refusal per join for
// no reason, which is exactly what the discard exists to avoid.
function TNBClanCertRetryLater()
{
   if (isEventPending($TNB::ClanCertRenew))
      cancel($TNB::ClanCertRenew);

   $TNB::ClanCertRenew = schedule($TNB::ClanCertRetryMs, 0, "TNBClanCertFetch");
}

function TNBClanCertCancelTimers()
{
   if (isEventPending($TNB::ClanCertRenew))
      cancel($TNB::ClanCertRenew);
   if (isEventPending($TNB::ClanCertDrop))
      cancel($TNB::ClanCertDrop);

   $TNB::ClanCertRenew = "";
   $TNB::ClanCertDrop = "";
}

//-----------------------------------------------------------------------------
// Delivery
//-----------------------------------------------------------------------------

// A game server running TNBrowserServer asking for the certificate.
//
// 200 characters per command, which is the shipped chunk size
// (clientSideClans.cs:35) and set by what a commandToServer argument will
// carry. The server reassembles and is told when the last one has gone.
//
// %version is what the server announced. Unused today and sent anyway, because
// the shipped protocol's one extension point was exactly this and it cost it
// nothing to have.
function clientCmdtnb_wantClanCert(%version)
{
   if ($TNB::ClanCert !$= "")
   {
      $TNB::ClanCertAsked = "";
      TNBClanCertSend();
      return;
   }

   // Nothing to send yet. Remember who asked and fetch one: the answer is
   // seconds away, and TNBClanCertLoaded hands it over when it lands rather
   // than leaving the player untagged for the whole session.
   //
   // The connection object and not a flag. A flag set here and read after a
   // fetch that outlived the connection would push a certificate at whatever
   // server we are on by then -- and if that one does not run this mod,
   // tnb_clanCertChunk does not exist there and every chunk is a console error
   // on somebody else's server.
   $TNB::ClanCertAsked = ServerConnection;
   TNBClanCertFetch();
}

// Hand the certificate to the server we are connected to, 200 characters at a
// time -- the shipped chunk size (clientSideClans.cs:35), set by what a
// commandToServer argument will carry.
function TNBClanCertSend()
{
   if ($TNB::ClanCert $= "" || !isObject(ServerConnection))
      return;

   %cert = $TNB::ClanCert;
   %len = strlen(%cert);
   for (%i = 0; %i < %len; %i += 200)
      commandToServer('tnb_clanCertChunk', getSubStr(%cert, %i, 200));

   commandToServer('tnb_clanCertDone');

   if ($TNB::Debug)
      echo("TNBrowser: sent clan certificate (" @ %len @ " chars)");
}

echo("TNBrowser: clancert.cs loaded");
