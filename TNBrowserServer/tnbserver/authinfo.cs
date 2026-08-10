// TNBrowserServer -- clan tags for connecting players
//
// How a tag reaches a player's name, and why this mod is small:
//
// The stock server.cs already does the rendering. In GameConnection::onConnect
// it reads the client's auth info and wraps the name itself:
//
//     %tag    = getField( %authInfo, 1 );
//     %append = getField( %authInfo, 2 );
//     if ( %append ) %name = "\cp\c6" @ %name @ "\c7" @ %tag @ "\co";
//     else           %name = "\cp\c7" @ %tag @ "\c6" @ %name @ "\co";
//
// So this mod never touches names. It only has to put the right values in
// %client.t2csri_authInfo before that runs. The record format is documented in
// TribesNext's serverSide.cs:
//
//     Name <TAB> ActiveTag <TAB> Prepend(0)/Append(1) <TAB> guid
//     NumberOfClans
//     ClanName <TAB> Tag <TAB> Append <TAB> clanid <TAB> rank <TAB> title
//
// Normally that record comes from a signed community certificate issued by
// TribesNext's DCE. That path is dead -- the DCE's signing certificate has
// expired, and game servers verify the chain against a root key only its
// operator holds. A server running this mod sources the same record from its
// own backend instead, which is entirely within a server operator's control.
//
// Timing is the only awkward part. onConnect is synchronous and an HTTP lookup
// is not, so this keeps a cache: a player who has connected recently is tagged
// with no round trip at all. A cache miss holds the connection instead --
// onConnect returns without calling Parent::, and the reply re-enters it, this
// time with the record in hand.
//
// Holding it is cheaper than repairing it afterwards, because a name is not one
// value. It is the server-side %client.name, the client target, AND a PlayerRep
// on every connected machine, built once from the MsgClientJoin broadcast
// (message.cs:59) and changed thereafter only by a MsgClientNameChanged
// message. A late rename that sets the first two leaves every scoreboard in the
// game showing the old name, which is what this mod used to do.
//
// The pattern is TribesNext's own: t2csri/serverSide.cs:239 stashes the connect
// arguments, arms a 15-second expiry and returns, then resumes with
// %client.onConnect(%client.tname, ...) once its challenge phase completes. Two
// things follow from sitting inside that mechanism. The wait here must stay well
// under those 15 seconds or t2csri kicks the player mid-hold -- hence
// $TNBS::WaitMs. And a player who quits while held is still "authenticating" as
// far as t2csri's onDrop override is concerned, so no join or leave message is
// broadcast for a connect that never finished.

//-----------------------------------------------------------------------------
// Cache
//-----------------------------------------------------------------------------

function TNBSCacheGet(%guid)
{
   %at = $TNBS::CacheAt[%guid];
   if (%at $= "")
      return "";

   // getSimTime is milliseconds since the server started -- fine for an age,
   // which is all this needs.
   if ((getSimTime() - %at) > ($TNBS::CacheSeconds * 1000))
      return "";

   return $TNBS::CacheInfo[%guid];
}

function TNBSCachePut(%guid, %info)
{
   $TNBS::CacheInfo[%guid] = %info;
   $TNBS::CacheAt[%guid] = getSimTime();
}

//-----------------------------------------------------------------------------
// Lookup
//-----------------------------------------------------------------------------

// One connection, one request at a time, through a queue.
//
// HTTPObject dispatches its callbacks by object name, so per-request objects
// would need per-request handler functions. A single named connection avoids
// that, and serialising is required anyway: two transfers on one connection
// object would interleave their bodies.
function TNBSFetch(%client, %guid)
{
   %t = $TNBS::QTail;
   $TNBS::QGuid[%t] = %guid;
   $TNBS::QClient[%t] = %client;
   $TNBS::QTail = %t + 1;

   // Never start a transfer from inside another one's callback: it sticks in
   // Connecting forever, because libcurl's multi handle is mid-iteration on
   // the connection whose callback we are in. One tick later is fine.
   if (!$TNBS::Busy)
      schedule(32, 0, "TNBSPump");
}

function TNBSPump()
{
   if ($TNBS::Busy)
      return;
   if ($TNBS::QHead >= $TNBS::QTail)
   {
      $TNBS::QHead = 0;
      $TNBS::QTail = 0;
      return;
   }

   $TNBS::Busy = 1;
   $TNBS::Gen++;
   %guid = $TNBS::QGuid[$TNBS::QHead];

   if (isObject(TNBSConnection))
      TNBSConnection.delete();
   new HTTPObject(TNBSConnection);

   TNBSConnection.gen = $TNBS::Gen;
   TNBSConnection.buffer = "";
   TNBSConnection.setHeader("Accept", "text/plain");

   // The query string has to be part of the request-URI: this build's
   // HTTPObject ignores the third argument.
   TNBSConnection.get($TNBS::Host, $TNBS::AuthInfoURI @ "?guid=" @ %guid, "");

   // Give up on a transfer that never finishes. A backend that accepts the
   // connection and then says nothing leaves this build's HTTPObject waiting
   // forever -- there is no onConnectFailed to hear -- and $TNBS::Busy would
   // stay set, so every later lookup queues behind it and no player is ever
   // tagged again until the server restarts. Observed, not theorised.
   $TNBS::XferExpire = schedule($TNBS::WaitMs, 0, "TNBSAbandon", $TNBS::Gen);
}

function TNBSConnection::onLine(%this, %line)
{
   // Reassemble with the line breaks intact: the record is line-structured and
   // getRecord() is what reads it back.
   %this.buffer = %this.buffer @ %line @ "\n";
}

function TNBSConnection::onDisconnect(%this)
{
   // A transfer we already gave up on, finishing late. Its slot has moved on,
   // so acting here would advance the queue a second time.
   if (%this.gen != $TNBS::Gen)
      return;

   if (isEventPending($TNBS::XferExpire))
      cancel($TNBS::XferExpire);

   TNBSAdvance(trim(%this.buffer));
}

function TNBSAbandon(%gen)
{
   if (%gen != $TNBS::Gen || !$TNBS::Busy)
      return;

   error("TNBrowserServer: lookup timed out, abandoning it");
   TNBSAdvance("");
}

// Retire the request at the head of the queue with whatever came back -- a
// record, or nothing at all -- and start the next one.
//
// Bumping the generation is what makes an abandoned transfer harmless: its
// onDisconnect, whenever it arrives, no longer matches.
function TNBSAdvance(%info)
{
   %h = $TNBS::QHead;
   if (%h >= $TNBS::QTail)
      return;                       // stray callback after the queue was reset

   $TNBS::QHead = %h + 1;
   $TNBS::Busy = 0;
   $TNBS::Gen++;

   TNBSHandleRecord($TNBS::QClient[%h], $TNBS::QGuid[%h], %info);

   schedule(32, 0, "TNBSPump");
}

function TNBSHandleRecord(%client, %guid, %info)
{
   // Every path below resumes the connect, including both failures. A player
   // waits out $TNBS::WaitMs only when the backend accepts the connection and
   // then never answers; anything the server actually says, even nothing, lets
   // them in immediately.
   //
   // One tick later, not from here: this runs inside the HTTP connection's own
   // callback, and onConnect sends the mission info and broadcasts to every
   // client in the game. Re-entering the engine that hard from inside a libcurl
   // callback is the mistake this file already warns about for transfers.
   if (isObject(%client))
      schedule(0, 0, "TNBSResume", %client);

   if (%info $= "")
   {
      if ($TNBS::Debug)
         echo("TNBrowserServer: no record for " @ %guid);
      return;
   }

   // The record must be for the player we asked about. A mismatch means the
   // backend is confused or someone is answering for it; either way, drop it
   // rather than hand a player someone else's clan.
   if (getField(getRecord(%info, 0), 3) !$= %guid)
   {
      error("TNBrowserServer: record for the wrong player, ignoring");
      return;
   }

   TNBSCachePut(%guid, %info);

   if ($TNBS::Debug)
      echo("TNBrowserServer: cached tag for " @ %guid @
           " [" @ getField(getRecord(%info, 0), 1) @ "]");
}

// There is deliberately no onConnectFailed handler: this build's libcurl-backed
// HTTPObject never calls it. A failure arrives as onDisconnect with an empty
// buffer, which the empty check above already treats as "no record".

//-----------------------------------------------------------------------------
// Resuming a held connect
//-----------------------------------------------------------------------------

// Let a held connection finish, by re-entering onConnect with the arguments it
// was called with the first time.
//
// Re-entry rather than a direct Parent:: call, because Parent:: resolves only
// inside the packaged function -- the same reason t2csri resumes its own auth
// phase with %client.onConnect(%client.tname, ...) instead.
//
// Two callers race deliberately: the reply and the expiry timer. Whichever
// arrives first wins and the other does nothing, so a slow answer that lands
// after the timeout cannot connect the player twice. Its record is still cached
// by then, which is what makes the next join instant.
function TNBSResume(%client)
{
   if (!isObject(%client) || %client.tnbResumed)
      return;

   %client.tnbResumed = 1;
   if (isEventPending(%client.tnbExpire))
      cancel(%client.tnbExpire);

   %client.onConnect(%client.tnbName, %client.tnbRaceGender, %client.tnbSkin,
                     %client.tnbVoice, %client.tnbVoicePitch);
}

//-----------------------------------------------------------------------------
// Hook
//-----------------------------------------------------------------------------

package TNBrowserServer
{
   // Runs before the stock onConnect reads the auth info, so the record is in
   // place by the time the tag is rendered -- out of the cache if it is warm,
   // and otherwise by holding the connect until it is.
   //
   // Package order against t2csri does not matter. This acts only once
   // getAuthInfo() yields a GUID, which is true on whichever entry follows
   // t2csri's challenge phase, whether this wraps that override or it wraps
   // this one.
   function GameConnection::onConnect(%client, %name, %raceGender, %skin, %voice, %voicePitch)
   {
      // getAuthInfo(), not the raw field. For a remote client t2csri fills
      // t2csri_authInfo during its handshake, but for a local connection the
      // field is empty until getAuthInfo() lazily falls back to
      // WONGetAuthInfo() -- so reading the field directly finds no GUID and
      // silently tags nobody when hosting a listen server.
      %guid = getField(%client.getAuthInfo(), 3);

      if (%guid !$= "")
      {
         %cached = TNBSCacheGet(%guid);
         if (%cached !$= "")
         {
            // Substitute wholesale: the record carries the tag and the full
            // membership list the game exposes through getAuthInfo().
            %client.t2csri_authInfo = %cached;
         }
         else if (!%client.tnbWaited)
         {
            // Cold cache. Hold the connection: nothing downstream has run yet,
            // so nobody has been told a name that would have to be taken back.
            //
            // tnbWaited is set here and never cleared, so the resumed call
            // falls straight through this branch. A lookup that failed leaves
            // the cache cold, and the player joins untagged rather than
            // waiting again -- the old failure mode, reached without hanging.
            %client.tnbWaited = 1;

            %client.tnbName = %name;
            %client.tnbRaceGender = %raceGender;
            %client.tnbSkin = %skin;
            %client.tnbVoice = %voice;
            %client.tnbVoicePitch = %voicePitch;

            %client.tnbExpire = schedule($TNBS::WaitMs, 0, "TNBSResume", %client);
            TNBSFetch(%client, %guid);
            return;
         }
      }

      Parent::onConnect(%client, %name, %raceGender, %skin, %voice, %voicePitch);
   }
};

// Guarded: this file is exec'd from two entry points (console autoexec and the
// CreateServer scan), and activating twice is a console warning.
if (!isActivePackage(TNBrowserServer))
   activatePackage(TNBrowserServer);

echo("TNBrowserServer: clan tag support active");
