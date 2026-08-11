// TNBrowser -- chat, polled over the one request queue
//
// The shell's CHAT tab is roughly a hundred IRCClient::* functions in the
// shipped scripts/ChatGui.cs, and the whole of its transport is two of them:
// IRCClient::send writes to a TCPObject, and IRCTCP::onLine reads from one.
// This file replaces those two. Everything above them -- processLine, dispatch,
// the channel and person model, the tribe-tag rendering, every screen -- is the
// shipped code, running unmodified.
//
// # Why not the socket
//
// It is plaintext and there is no TLS behind it: the TribesNext patch's mbedTLS
// and libcurl are wired to HTTPObject alone, and the stock Torque TCPObject is
// untouched. Cleartext IRC is not merely readable either -- RFC 1459 has no
// per-message integrity, and IRCClient::onError (ChatGui.cs:1899) obediently
// disconnects on any ERROR line it is handed.
//
// # Why polling, and not a connection held open
//
// This file used to hold its own HTTPS connection open and read lines as they
// arrived. It worked in every test and took the whole mod down in the field.
//
// Measured on a live client against the real backend: two HTTPObject transfers
// started close together **wedge one of them permanently** -- no onLine, no
// onDisconnect, ever, so nothing in script can even notice. Which one loses
// varies. When it was the request queue's transfer, $TNB::Busy stayed 1 forever
// and mail and the browser died along with chat; when it was the stream, chat
// died in silence. Deleting the stream object drained the queue instantly.
//
// The probe that licensed the old design -- four concurrent transfers, all
// completing -- was run over plain HTTP against a local server. It does not hold
// against production TLS.
//
// So: no long-lived requests, and never two at once. Chat goes through
// TNBApiEnqueueOn like every other call in this mod, on the single connection
// object the queue has always serialised.
//
// # One request, both directions
//
//     payload   <cursor> NL <line> NL <line> ...
//     answer    {"seq":"42","lines":"<line>\n<line>","reset":"0"}
//
// A send is also a poll, so typing costs no extra request. The cursor is what
// the backend replays from, so a poll that fails loses nothing: the next one
// asks from the same place.
//
// The cost is honest and small: a message appears within a poll interval rather
// than instantly, and a logged-in player is one short request every two seconds.

//-----------------------------------------------------------------------------
// State
//-----------------------------------------------------------------------------

// $TNB::ChatWant     1 between connect and disconnect
// $TNB::ChatCursor   last sequence number seen; where the next poll resumes
// $TNB::ChatPending  1 while a poll is queued or in flight
// $TNB::ChatOut      lines waiting to go up with the next poll
// $TNB::ChatFails    consecutive failures, drives the backoff
// $TNB::ChatTimer    the pending tick

//-----------------------------------------------------------------------------
// Starting and stopping
//-----------------------------------------------------------------------------

// Called from the IRCClient::connect override.
//
// Deliberately does no I/O and starts no timer. ChatGui.cs runs connect() as it
// loads, which is during boot -- before the player has logged in to TribesNext.
// Chat would then be the first thing in this mod ever to ask who they are that
// early, and asking is not free: TNBSessionGuid caches its answer in
// $LoginCertificate and TNBSessionModulus reads the key out of that same record,
// so an answer taken before login can pin the session to an account whose
// private key the client does not hold. Every request after that is refused, and
// mail and the browser go down with chat.
//
// So chat waits to be told. TNBChatNudge fires from TNBCertEnsure, which is the
// moment the shell opens a pane with an account in hand -- the same moment mail
// and the browser first read the identity.
function TNBChatConnect()
{
   $TNB::ChatWant = 1;

   // What the shipped connect does first, by way of IRCClient::disconnect.
   //
   // Not housekeeping: IRCClient::reset is the only thing that ever assigns
   // $IRCClient::currentChannel, and half of ChatGui.cs dereferences that global
   // without checking it. Skipping it produced a working connection whose every
   // notify raised "Unable to find object: '' attempting to call function
   // 'FindMember'".
   if (!isObject($IRCClient::channels))
      IRCClient::init();
   IRCClient::reset();

   // A room to land in, so the CHAT tab is not an empty status pane. The shipped
   // IRCClient::onChalRespReply joins $IRCClient::room on connect, and
   // IRCClient::init clears it -- which runs immediately before connect at
   // ChatGui.cs:3415, so this is the moment to set it.
   if ($TNB::ChatRoom !$= "" && $IRCClient::room $= "")
      $IRCClient::room = $TNB::ChatRoom;

   // CONNECTING_WAITING, not CONNECTED: the backend's CHALRESP_REPLY is what
   // moves the state on, exactly as the shipped handshake does, and until then
   // DatabaseQueryi's guard (webstuff.cs:183) still reads us as not ready.
   $IRCClient::state = IDIRC_CONNECTING_WAITING;
}

// The player has reached the shell with an account in hand: start polling.
function TNBChatNudge()
{
   if (!$TNB::ChatWant)
      return;

   $TNB::ChatFails = 0;
   TNBChatTickSoon(0);
}

function TNBChatDisconnect()
{
   $TNB::ChatWant = "";
   $TNB::ChatOut = "";
   $TNB::ChatCursor = 0;
   $TNB::ChatPending = "";

   if (isEventPending($TNB::ChatTimer))
      cancel($TNB::ChatTimer);
   $TNB::ChatTimer = "";
}

//-----------------------------------------------------------------------------
// The poll
//-----------------------------------------------------------------------------

// Arm the next tick, replacing any pending one.
//
// Always through a schedule, never inline: TNBChatSend is reached from inside
// processLine, which is inside the queue's own network callback, and starting a
// request from there wedges the transfer (api.cs:118). The queue defers for the
// same reason.
function TNBChatTickSoon(%delay)
{
   if (isEventPending($TNB::ChatTimer))
      cancel($TNB::ChatTimer);

   $TNB::ChatTimer = schedule(%delay, 0, "TNBChatTick");
}

function TNBChatTick()
{
   if (!$TNB::ChatWant)
      return;

   // One poll in flight at a time. The queue would serialise them anyway, but a
   // second poll carrying the same cursor would ask for the same lines twice.
   if ($TNB::ChatPending)
      return;

   // The guard TNBCertEnsure and TNBClanCertFetch both use: with no TribesNext
   // account there is no session to negotiate, and nothing should try. Keep
   // ticking slowly in case one appears.
   if (TNBSessionGuid() $= "")
   {
      TNBChatTickSoon($TNB::ChatPollMs * 4);
      return;
   }

   %payload = $TNB::ChatCursor;
   if ($TNB::ChatOut !$= "")
      %payload = %payload @ "\n" @ $TNB::ChatOut;
   $TNB::ChatOut = "";

   $TNB::ChatPending = 1;
   TNBApiEnqueueOn($TNB::ChatURI, "chat", %payload, "TNBChatDone", "", 1);
}

function TNBChatDone(%ctx, %status, %result)
{
   $TNB::ChatPending = "";

   if (!$TNB::ChatWant)
      return;

   if (%status !$= "ok")
   {
      // Nothing is lost: the cursor did not move, so the next poll asks from the
      // same place. Back off so a backend that is down costs one request a
      // minute rather than one every two seconds.
      $TNB::ChatFails++;
      %delay = $TNB::ChatPollMs;
      for (%i = 1; %i < $TNB::ChatFails && %delay < 60000; %i++)
         %delay = %delay * 2;
      if (%delay > 60000)
         %delay = 60000;

      TNBChatTickSoon(%delay);
      return;
   }

   $TNB::ChatFails = 0;

   // Copied out now: the parsed tree is freed as soon as this returns.
   %seq = TNBJsonStr(%result, "seq");
   %lines = TNBJsonStr(%result, "lines");
   %reset = TNBJsonStr(%result, "reset");

   if (%seq !$= "")
      $TNB::ChatCursor = %seq;

   %count = getRecordCount(%lines);
   for (%i = 0; %i < %count; %i++)
   {
      %line = getRecord(%lines, %i);
      if (%line !$= "")
         IRCClient::processLine(%line);       // the shipped parser, from here on
   }

   // The backend has no memory of this client -- it was away long enough to be
   // swept -- so the rooms it is still showing have to be asked for again. Next
   // tick, because this is a network callback.
   if (%reset $= "1")
      $TNB::ChatResync = schedule(32, 0, "TNBChatResync");

   // Straight back round if there is something to say, otherwise at the poll
   // interval. Sending is what makes chat feel immediate; waiting is what keeps
   // it cheap.
   if ($TNB::ChatOut !$= "")
      TNBChatTickSoon(0);
   else
      TNBChatTickSoon($TNB::ChatPollMs);
}

// Tell the backend about the rooms we are still showing.
//
// Not IRCClient::reconnect(): that deletes every channel and rebuilds it, which
// the player sees as the chat window emptying. Re-joining what is already on
// screen leaves the panes alone -- the JOIN echo finds the channel and the
// member it already has, and only the member list is refreshed.
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
// Accumulated rather than sent. The caller is routinely inside a network
// callback, and the shipped code bursts -- joining a room sends a JOIN and then
// a MODE, and a resync sends one JOIN per room -- so a tick of accumulation
// turns a handful of requests into one.
function TNBChatSend(%line)
{
   if (%line $= "" || !$TNB::ChatWant)
      return;

   if ($TNB::ChatOut $= "")
      $TNB::ChatOut = %line;
   else
      $TNB::ChatOut = $TNB::ChatOut @ "\n" @ %line;

   // Bring the next poll forward, unless one is already in flight -- its
   // completion will pick these up.
   if (!$TNB::ChatPending)
      TNBChatTickSoon(0);
}

echo("TNBrowser: chat.cs loaded");
