// TNBrowser -- chat over HTTPS
//
// The shell's CHAT tab is roughly a hundred IRCClient::* functions in the
// shipped scripts/ChatGui.cs, and the whole of its transport is two of them:
// IRCClient::send writes to a TCPObject, and IRCTCP::onLine reads from one.
// This file replaces those two. Everything above them -- processLine, dispatch,
// the channel and person model, the tribe-tag rendering, every screen -- is the
// shipped code, running unmodified.
//
// # Why not just point the socket somewhere
//
// Because it is a plaintext socket and there is no TLS behind it. The
// TribesNext patch's mbedTLS and libcurl are wired to HTTPObject alone; the
// stock Torque TCPObject is untouched. And cleartext IRC is not merely
// readable: RFC 1459 has no per-message integrity, so anyone on the path can
// inject a line -- and IRCClient::onError (ChatGui.cs:1899) obediently
// disconnects on any ERROR it is handed.
//
// # The stream is real, and that was measured
//
// A held-open GET carries lines down as they happen. This build's HTTPObject
// delivers onLine while the response is still open, which was not obvious and
// is the single fact the design rests on: probed in the game against a server
// writing one line a second for thirty seconds, the callbacks arrived at 41ms,
// 1035ms, 2037ms, ... and the transfer ended at 30038ms. A held response also
// survived 120 seconds of complete silence and then delivered the next line
// normally. Four concurrent HTTPObjects all completed together rather than
// serialising, which is what lets chat hold two of its own alongside the
// request queue and the session keepalive.
//
// If any of that had failed this would have had to be a long-poll.
//
// # Framing
//
//     <seq> TAB <irc line>
//
// The sequence number is the cursor: it goes back as ?after= on the next
// connection and the backend replays from its ring, so a stream that drops
// loses nothing. Two lines carry sequence 0 and are not IRC at all -- an empty
// one is the keepalive, and RESET means the backend has no memory of this
// client and the rooms have to be asked for again.
//
// # Everything goes through a schedule
//
// IRCClient::send is called from inside processLine, which is inside onLine.
// Issuing a request from a network callback wedges the libcurl multi handle
// (api.cs:118), so outbound lines are accumulated and posted one tick later --
// which also means the JOIN and the MODE that follows it travel in one request
// rather than two.

//-----------------------------------------------------------------------------
// State
//-----------------------------------------------------------------------------

// $TNB::ChatWant      1 between connect and disconnect: whether to hold a stream
// $TNB::ChatOpen      1 while a stream request is in flight
// $TNB::ChatSeq       last sequence number received, the resume cursor
// $TNB::ChatFails     consecutive failed opens, drives the backoff
// $TNB::ChatOut       outbound lines waiting for the next flush
// $TNB::ChatPosting   1 while a send is in flight
// $TNB::ChatRetry     pending open
// $TNB::ChatFlushEv   pending flush

//-----------------------------------------------------------------------------
// Opening
//-----------------------------------------------------------------------------

// Called from the IRCClient::connect override.
//
// The shipped connect walks a server list from GetIRCServerList(), which has
// answered "" since WON shut down, and its failure path ends in a modal dialog
// telling the player the IRC servers are unreachable. There is one endpoint
// here and no list.
function TNBChatConnect()
{
   $TNB::ChatWant = 1;

   // What the shipped connect does first, by way of IRCClient::disconnect.
   //
   // Not housekeeping: IRCClient::reset is the only thing that ever assigns
   // $IRCClient::currentChannel, and half of ChatGui.cs dereferences that
   // global without checking it. Skipping it produced a working connection
   // whose every notify raised "Unable to find object: '' attempting to call
   // function 'FindMember'".
   //
   // Reset and not disconnect, because disconnect is the override below and
   // would take the stream down with it.
   if (!isObject($IRCClient::channels))
      IRCClient::init();
   IRCClient::reset();

   // A room to land in, so the CHAT tab is not an empty status pane. The
   // shipped IRCClient::onChalRespReply joins $IRCClient::room on connect, and
   // IRCClient::init clears it -- which runs immediately before connect at
   // ChatGui.cs:3415, so this is the moment to set it.
   if ($TNB::ChatRoom !$= "" && $IRCClient::room $= "")
      $IRCClient::room = $TNB::ChatRoom;

   // CONNECTING_WAITING, not CONNECTED: the backend's CHALRESP_REPLY is what
   // moves the state on, exactly as the shipped handshake does, and until then
   // DatabaseQueryi's guard (webstuff.cs:183) still reads us as not ready.
   $IRCClient::state = IDIRC_CONNECTING_WAITING;

   TNBChatOpenSoon();
}

function TNBChatDisconnect()
{
   $TNB::ChatWant = "";
   $TNB::ChatOut = "";

   if (isEventPending($TNB::ChatRetry))
      cancel($TNB::ChatRetry);
   if (isEventPending($TNB::ChatFlushEv))
      cancel($TNB::ChatFlushEv);
   $TNB::ChatRetry = "";
   $TNB::ChatFlushEv = "";

   // ChatOpen is cleared even though a transfer may still be running, because
   // it means "a stream we would use", not "a socket". HTTPObject cannot cancel
   // a transfer; what ends the old one is TNBChatStart deleting the object
   // before it makes a new one, and until then onLine drops what arrives
   // because the want flag is down.
   //
   // Leaving it set was a real failure and not a tidy one: a disconnect
   // followed by a connect -- which is what changing backend does, and what the
   // shipped JOIN CHAT dialog does -- found ChatOpen still 1 from the stream
   // that had just been abandoned, so TNBChatStart returned at its first line
   // and the client sat in CONNECTING_WAITING until the old stream's ten-minute
   // lifetime expired.
   $TNB::ChatOpen = "";
   $TNB::ChatSeq = 0;
}

// The player has reached the shell with an account in hand.
//
// ChatGui.cs runs IRCClient::connect() as it loads, which is during boot and
// therefore before any account is logged in -- so the first few attempts find no
// session and back off, and by the time the player is looking at the launch bar
// the next one can be a minute away. This collapses that wait to nothing.
function TNBChatNudge()
{
   if (!$TNB::ChatWant || $TNB::ChatOpen)
      return;

   $TNB::ChatFails = 0;
   TNBChatOpenSoon();
}

function TNBChatOpenSoon()
{
   if (isEventPending($TNB::ChatRetry))
      cancel($TNB::ChatRetry);
   $TNB::ChatRetry = schedule(32, 0, "TNBChatOpen");
}

function TNBChatOpen()
{
   if (!$TNB::ChatWant || $TNB::ChatOpen)
      return;

   // The guard TNBCertEnsure and TNBClanCertFetch both use: with no TribesNext
   // account there is no session to negotiate. Keep asking anyway, slowly --
   // this runs at shell boot and an account can appear afterwards.
   if (TNBSessionGuid() $= "")
   {
      TNBChatRetryLater();
      return;
   }

   TNBSessionOnReady("TNBChatSessionReady", "");
}

// Session callback, which may run inside the session's own network callback --
// so the transfer starts a tick later rather than here.
function TNBChatSessionReady(%ctx, %status, %reason)
{
   if (%status $= "error")
   {
      TNBChatRetryLater();
      return;
   }
   TNBChatStartSoon();
}

function TNBChatStartSoon()
{
   if (isEventPending($TNB::ChatRetry))
      cancel($TNB::ChatRetry);
   $TNB::ChatRetry = schedule(32, 0, "TNBChatStart");
}

function TNBChatStart()
{
   if (!$TNB::ChatWant || $TNB::ChatOpen || !TNBSessionReady())
      return;

   $TNB::ChatOpen = 1;

   if (isObject(TNBChatStream))
      TNBChatStream.delete();
   new HTTPObject(TNBChatStream);
   TNBChatStream.lines = 0;
   TNBChatStream.setHeader("Accept", "text/plain");

   TNBChatStream.get($TNB::Host,
      $TNB::ChatStreamURI @ "?guid=" @ TNBSessionGuid() @
      "&uuid=" @ $TNB::UUID @ "&after=" @ $TNB::ChatSeq, "");

   if ($TNB::Debug)
      echo("TNBrowser chat: stream opening at " @ $TNB::ChatSeq);
}

// Back off after a failure. Doubling from $TNB::ChatRetryMs to a minute, so a
// backend that is down costs one request a minute rather than one a second, and
// a backend that comes back is noticed within one.
function TNBChatRetryLater()
{
   %delay = $TNB::ChatRetryMs;
   for (%i = 0; %i < $TNB::ChatFails; %i++)
   {
      %delay = %delay * 2;
      if (%delay >= 60000)
      {
         %delay = 60000;
         break;
      }
   }
   $TNB::ChatFails = $TNB::ChatFails + 1;

   if (isEventPending($TNB::ChatRetry))
      cancel($TNB::ChatRetry);
   $TNB::ChatRetry = schedule(%delay, 0, "TNBChatOpen");
}

//-----------------------------------------------------------------------------
// Receiving
//-----------------------------------------------------------------------------

function TNBChatStream::onLine(%this, %line)
{
   %line = trim(%line);
   if (%line $= "" || !$TNB::ChatWant)
      return;

   %tab = strpos(%line, "\t");
   if (%tab == -1)
   {
      // Not our framing, so it is an error page: the session lapsed, or this
      // backend has no chat. Either way the stream is not a stream.
      %this.failed = 1;
      return;
   }

   %this.lines++;
   $TNB::ChatFails = 0;

   %seq = getSubStr(%line, 0, %tab);
   %text = getSubStr(%line, %tab + 1, strlen(%line) - (%tab + 1));

   if (%seq == 0)
   {
      // The keepalive is an empty payload and needs nothing done to it. RESET
      // means the backend has forgotten us -- the grace period lapsed while we
      // were away -- so the rooms this client still shows have to be asked for
      // again. Next tick: this is a network callback.
      if (%text $= "RESET")
      {
         $TNB::ChatSeq = 0;
         schedule(32, 0, "TNBChatResync");
      }
      return;
   }

   $TNB::ChatSeq = %seq;

   // The shipped parser, from here on. Everything this mod does ends at this
   // line.
   IRCClient::processLine(%text);
}

function TNBChatStream::onDisconnect(%this)
{
   $TNB::ChatOpen = "";

   if (!$TNB::ChatWant)
      return;

   if (%this.failed || %this.lines == 0)
   {
      // Nothing readable ever arrived: treat it as a failure and back off.
      TNBChatRetryLater();
      return;
   }

   // A stream that carried traffic and then ended is the ordinary case -- the
   // backend retires one every ten minutes so that reconnecting is a path that
   // runs constantly rather than for the first time during an outage. Straight
   // back in, from the cursor.
   $TNB::ChatFails = 0;
   TNBChatOpenSoon();
}

// The backend has no memory of this client, so tell it about the rooms we are
// still showing.
//
// Not IRCClient::reconnect(): that deletes every channel and rebuilds it, which
// is visible to the player as the chat window emptying. Re-joining what is
// already on screen leaves the panes alone -- the JOIN echo finds the channel
// and the member it already has, and only the member list is refreshed.
function TNBChatResync()
{
   if (!$TNB::ChatWant)
      return;

   for (%i = 1; %i < $IRCClient::channels.getCount(); %i++)
   {
      %c = $IRCClient::channels.getObject(%i);
      if (%c.private)
         continue;                 // a private pane is two people, not a room
      IRCClient::send("JOIN " @ %c.getName() @ " " @ %c.key);
   }
}

//-----------------------------------------------------------------------------
// Sending
//-----------------------------------------------------------------------------

// One line from the IRCClient::send override.
//
// Accumulated rather than sent, for two reasons. The caller is routinely inside
// a network callback and a request started there never completes; and the
// shipped code bursts -- joining a room sends a JOIN and then a MODE, and a
// resync sends one JOIN per room -- so a tick of accumulation turns a handful
// of requests into one.
function TNBChatSend(%line)
{
   if (%line $= "" || !$TNB::ChatWant)
      return;

   if ($TNB::ChatOut $= "")
      $TNB::ChatOut = %line;
   else
      $TNB::ChatOut = $TNB::ChatOut @ "\n" @ %line;

   TNBChatFlushSoon();
}

function TNBChatFlushSoon()
{
   if (isEventPending($TNB::ChatFlushEv))
      return;
   $TNB::ChatFlushEv = schedule(32, 0, "TNBChatFlush");
}

function TNBChatFlush()
{
   if ($TNB::ChatOut $= "" || !$TNB::ChatWant)
      return;

   if ($TNB::ChatPosting)
   {
      // One transfer per object, so wait for the one in flight. Whatever
      // accumulates meanwhile goes in the next request.
      TNBChatFlushSoon();
      return;
   }

   // Nothing to send into. The send endpoint answers 409 when the backend has
   // no stream for this player, so posting now would only lose the line -- and
   // the reopen is already in flight. Wait for it, but not forever: ten seconds
   // of no stream means the backend is down rather than cycling, and a line
   // typed into a dead chat window is not worth replaying into a room that has
   // moved on.
   if (!TNBSessionReady() || !$TNB::ChatOpen)
   {
      $TNB::ChatWait++;
      if ($TNB::ChatWait > 40)
      {
         $TNB::ChatOut = "";
         $TNB::ChatWait = 0;
         return;
      }
      $TNB::ChatFlushEv = schedule(250, 0, "TNBChatFlush");
      return;
   }

   $TNB::ChatWait = 0;

   %body = $TNB::ChatOut;
   $TNB::ChatOut = "";
   $TNB::ChatSent = %body;
   $TNB::ChatPosting = 1;

   if (isObject(TNBChatPoster))
      TNBChatPoster.delete();
   new HTTPObject(TNBChatPoster);
   TNBChatPoster.setHeader("Content-Type", "application/x-www-form-urlencoded");
   TNBChatPoster.post($TNB::Host,
      $TNB::ChatSendURI @ "?guid=" @ TNBSessionGuid() @ "&uuid=" @ $TNB::UUID, "",
      "lines=" @ TNBUrlEncode(%body));

   if ($TNB::Debug)
      echo("TNBrowser chat -> " @ %body);
}

// A successful send is 204 with no body at all, so anything arriving here is an
// error page: 409 because the stream is gone, or 401 because the session is.
// Both are fixed by opening the stream again, which renegotiates on the way.
function TNBChatPoster::onLine(%this, %line)
{
   if (trim(%line) !$= "")
      %this.failed = 1;
}

function TNBChatPoster::onDisconnect(%this)
{
   $TNB::ChatPosting = "";

   if (%this.failed)
   {
      %this.failed = "";
      $TNB::ChatOpen = "";

      // Put the lines back, once. The ordinary cause is a 409 -- the stream
      // ended between the keystroke and the request -- and the same lines
      // succeed as soon as it is open again. Once, because a permanent refusal
      // (a backend with chat switched off answers 404) must not become a loop.
      if (!$TNB::ChatResent && $TNB::ChatSent !$= "")
      {
         $TNB::ChatResent = 1;
         if ($TNB::ChatOut $= "")
            $TNB::ChatOut = $TNB::ChatSent;
         else
            $TNB::ChatOut = $TNB::ChatSent @ "\n" @ $TNB::ChatOut;
      }

      TNBChatOpenSoon();
      return;
   }

   $TNB::ChatResent = "";

   if ($TNB::ChatOut !$= "")
      TNBChatFlushSoon();
}

echo("TNBrowser: chat.cs loaded");
