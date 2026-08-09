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
// immediately, and a cache miss is tagged a moment later by renaming them, using
// the same addTaggedString/setTargetName pair the stock duplicate-name handling
// uses.

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
   %guid = $TNBS::QGuid[$TNBS::QHead];

   if (isObject(TNBSConnection))
      TNBSConnection.delete();
   new HTTPObject(TNBSConnection);

   TNBSConnection.buffer = "";
   TNBSConnection.setHeader("Accept", "text/plain");

   // The query string has to be part of the request-URI: this build's
   // HTTPObject ignores the third argument.
   TNBSConnection.get($TNBS::Host, $TNBS::AuthInfoURI @ "?guid=" @ %guid, "");
}

function TNBSConnection::onLine(%this, %line)
{
   // Reassemble with the line breaks intact: the record is line-structured and
   // getRecord() is what reads it back.
   %this.buffer = %this.buffer @ %line @ "\n";
}

function TNBSConnection::onDisconnect(%this)
{
   %h = $TNBS::QHead;
   if (%h >= $TNBS::QTail)
      return;                       // stray callback after the queue was reset

   $TNBS::QHead = %h + 1;
   $TNBS::Busy = 0;

   %guid = $TNBS::QGuid[%h];
   %client = $TNBS::QClient[%h];
   %info = trim(%this.buffer);

   TNBSHandleRecord(%client, %guid, %info);

   schedule(32, 0, "TNBSPump");
}

function TNBSHandleRecord(%client, %guid, %info)
{

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

   // If the player is still connected and was tagged from a cold cache, fix
   // their name now.
   if (isObject(%client) && %client.tnbPendingTag)
      TNBSApplyLate(%client, %info);
}

// There is deliberately no onConnectFailed handler: this build's libcurl-backed
// HTTPObject never calls it. A failure arrives as onDisconnect with an empty
// buffer, which the empty check above already treats as "no record".

//-----------------------------------------------------------------------------
// Applying the tag
//-----------------------------------------------------------------------------

// Rebuild a player's displayed name from a record, after the fact.
//
// Mirrors what stock server.cs does inline at connect time, including the
// colour codes, so a late tag is indistinguishable from a timely one.
function TNBSApplyLate(%client, %info)
{
   %client.tnbPendingTag = 0;

   %header = getRecord(%info, 0);
   %tag = getField(%header, 1);
   %append = getField(%header, 2);
   if (%tag $= "")
      return;

   %client.t2csri_authInfo = %info;

   %base = getField(%header, 0);
   if (%append)
      %name = "\cp\c6" @ %base @ "\c7" @ %tag @ "\co";
   else
      %name = "\cp\c7" @ %tag @ "\c6" @ %base @ "\co";

   %client.name = addTaggedString(%name);
   if (%client.target > 0)
      setTargetName(%client.target, %client.name);

   if ($TNBS::Debug)
      echo("TNBrowserServer: applied late tag to " @ %base);
}

//-----------------------------------------------------------------------------
// Hook
//-----------------------------------------------------------------------------

package TNBrowserServer
{
   // Runs before the stock onConnect reads the auth info, so a cached record is
   // already in place by the time the tag is rendered.
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
         else
         {
            // Cold cache. Let them in untagged and fix it when the reply lands.
            %client.tnbPendingTag = 1;
            TNBSFetch(%client, %guid);
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
