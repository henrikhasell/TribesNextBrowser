// TNBrowserServer -- clan tags from a signed certificate
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
// This file gets that record from the player, signed, instead of looking it up
// over HTTP while holding the connection open -- which is what the version
// before it did, at the cost of a query per player per server, a cache, and an
// unsigned assertion anyone answering for that host could forge.
//
// Certificate format, one line, six tab-separated fields:
//
//     KeyID <TAB> Issued <TAB> Expire <TAB> GUID <TAB> HexBlob <TAB> Sig
//
// Sig is raw RSA over a bare SHA-1 of fields 0..4 -- no PKCS#1, no ASN.1 -- so
// verifying is rsa_mod_exp against the public key and a string compare. Both
// are natives from IFC22.dll, which this server already requires: without the
// TribesNext patch there is no authentication phase and no GUID to bind to.
//
// # Nothing here ever disconnects anybody
//
// That is the design, not an accident of it. The shipped community-certificate
// path answers every refusal with setDisconnectReason and delete() -- a bad
// signature, an expired certificate, a mismatched GUID, and worst of all an
// unknown issuer, which stalls until t2csri's 15-second timer drops the player
// with "This is a TribesNEXT server. You must install the TribesNEXT client."
//
// Here, every one of those means the player joins with no tag. A key we do not
// recognise, a certificate a minute past its expiry, a truncated transfer, a
// client that does not run the mod at all: all the same, all silent, none of
// them able to keep somebody out of a game.
//
// # Why the request goes out from a certificate chunk
//
// The obvious place to ask the client for its certificate is our own
// onConnect wrapper. The problem is that t2csri packages onConnect too, and
// which of the two wraps the other depends on activation order: if t2csri's is
// outermost it returns without calling Parent:: for the whole authentication
// phase, and ours does not run until that phase is over -- too late to ask
// without holding the player up.
//
// serverCmdt2csri_sendCertChunk is not in a package. It is a plain global
// function, and it is the first thing a connecting client calls: the poke goes
// out, the client answers with its account certificate in 200-byte pieces
// (clientSide.cs:124-127). Overriding it puts our request at the very start of
// the authentication phase in either activation order, roughly fifteen seconds
// before anything is waiting on the answer.

//-----------------------------------------------------------------------------
// Verification
//-----------------------------------------------------------------------------

// Check a certificate and return the auth-info record it carries, or "".
//
// The order is signature, expiry, GUID. Not for speed -- for meaning: until
// the signature checks out, none of the other fields are worth reading.
function TNBSVerifyClanCert(%cert, %guid)
{
   if (getFieldCount(%cert) != 6)
      return TNBSRefuse("malformed", %guid);

   %keyID = getField(%cert, 0);
   %e = $TNBS::ClanKeyE[%keyID];
   %n = $TNBS::ClanKeyN[%keyID];

   // An issuer we hold no key for. Expected during a key rotation, and the
   // reason rotation needs no coordination with server operators: the player
   // joins untagged until this server is updated, and nothing else changes.
   if (%e $= "" || %n $= "")
      return TNBSRefuse("unknown key " @ %keyID, %guid);

   // rsa_mod_exp(sig, e, n) recovers the SHA-1 that was signed. The left-pad
   // is load-bearing: the engine renders a bignum without leading zeros, so
   // one digest in sixteen comes back 39 characters and would not match the
   // 40-character sha1sum. TribesNext's own DCE path forgets this
   // (serverSideClans.cs:73-78); its account path does not (serverSide.cs:114).
   %sigSha = rsa_mod_exp(getField(%cert, 5), %e, %n);
   while (strlen(%sigSha) < 40)
      %sigSha = "0" @ %sigSha;

   if (%sigSha !$= sha1sum(getFieldS(%cert, 0, 4)))
      return TNBSRefuse("bad signature", %guid);

   // Expiry is the only freshness this design has -- there is no revocation --
   // so it is also what stops a player who left a clan wearing its tag.
   //
   // Accurate to about two minutes and no better. The console evaluates
   // numbers as 32-bit floats, and a current epoch second needs 31 bits, so
   // values up here land on multiples of 128. Measured, not assumed: a
   // certificate issued for exactly 1800 seconds reports its own life as 1792.
   // That is why lifetimes here are minutes rather than seconds, and it is the
   // same arithmetic the shipped verifier does (serverSideClans.cs:177).
   if (currentEpochTime() > getField(%cert, 2))
      return TNBSRefuse("expired", %guid);

   // The binding, and the reason no certificate chain is needed. The GUID is
   // whatever t2csri established from the account certificate TribesNext
   // signed, checked a moment ago against a key compiled into IFC22.dll. Our
   // certificate only has to agree with it; without this check a player could
   // wear anybody's clan by presenting their certificate, which is public the
   // moment it is sent to one server.
   if (getField(%cert, 3) !$= %guid)
      return TNBSRefuse("issued for " @ getField(%cert, 3), %guid);

   %blob = getField(%cert, 4);
   %len = strlen(%blob);
   if (%len < 2 || (%len % 2) != 0)
      return TNBSRefuse("odd record", %guid);

   // Hex, because the record carries both the field and the record separator
   // of the string it travels inside. Signature-checked before it is decoded,
   // so what comes out is exactly what was signed.
   %decoded = "";
   for (%i = 0; %i < %len; %i += 2)
      %decoded = %decoded @ collapseEscape("\\x" @ getSubStr(%blob, %i, 2));

   // The trailing newline is what t2csri appends to its own decoded blob
   // (serverSideClans.cs:190) and what its synthesised record ends with
   // (serverSide.cs:186). getRecord tolerates its absence; matching them costs
   // nothing and keeps one format in play instead of two.
   return %decoded @ "\n";
}

function TNBSRefuse(%why, %guid)
{
   if ($TNBS::Debug)
      echo("TNBrowserServer: no tag for " @ %guid @ " (" @ %why @ ")");

   return "";
}

//-----------------------------------------------------------------------------
// Applying it
//-----------------------------------------------------------------------------

// Wear the record.
//
// Before the connect completes this is the whole job: stock server.cs reads
// getAuthInfo() a moment later and builds the name itself. After it, the name
// has already been built and broadcast, so it has to be rebuilt here -- see
// TNBSRetag.
function TNBSApplyRecord(%client, %record)
{
   %client.t2csri_authInfo = %record;
   %client.tnbTagged = 1;

   if ($TNBS::Debug)
      echo("TNBrowserServer: tagged " @ getField(%record, 0) @
           " as " @ getField(%record, 1));
}

// The same, for a certificate that arrived after the player was already in.
//
// A name is not one value, which is what makes this more than an assignment:
// it is the server-side %client.name, the client target every machine renders
// from, and a PlayerRep built once from the MsgClientJoin broadcast
// (message.cs:59). Setting only the first leaves every scoreboard in the game
// showing the old one.
//
// All three are updated here, which is exactly what stock does when two players
// collide on a name (server.cs:679-682) -- the one place it renames somebody
// who is already connected. Following it rather than inventing something is
// also what makes this safe mid-game: that path fires while players are spawned
// and in play.
//
// The message carries an empty format string, the shipped idiom for a silent
// one (server.cs:790). A chat line per late tag would be noise for everybody
// else in the game, and the rename is not news.
function TNBSRetag(%client, %record)
{
   %head = getRecord(%record, 0);
   %tag = getField(%head, 1);
   %append = getField(%head, 2);

   // Nothing to show. A warrior in no tribe is not renamed, and must not be:
   // their name is already the right one.
   if (%tag $= "")
      return;

   // The untagged name stock stored at connect (server.cs:751). Without it
   // there is nothing to rebuild from -- true only for a smurf, who has no
   // verified GUID and never reaches here.
   %base = %client.nameBase;
   if (%base $= "")
      return;

   // Stock's own expression, from the branch that runs when the tag is known in
   // time (server.cs:686-690). Copied rather than shared because it lives
   // inside GameConnection::onConnect with no seam to call.
   if (%append)
      %name = "\cp\c6" @ %base @ "\c7" @ %tag @ "\co";
   else
      %name = "\cp\c7" @ %tag @ "\c6" @ %base @ "\co";

   %was = %client.name;

   %client.name = addTaggedString(%name);
   setTargetName(%client.target, %client.name);
   MessageAll('MsgClientNameChanged', "", %was, %client.name, %client);
}

//-----------------------------------------------------------------------------
// Transport
//-----------------------------------------------------------------------------

// The client's certificate, in 200-byte pieces.
//
// Capped like the shipped handlers cap theirs, and unlike them the cap costs
// the record rather than the player: a client sending more than this is either
// broken or trying something, and either way the answer is a name without a
// tag.
function serverCmdtnb_clanCertChunk(%client, %chunk)
{
   // A second transfer, from a client that had nothing to send when it joined
   // and has since fetched one. tnbCertDone is otherwise a permanent latch, so
   // without this the retry is dropped chunk by chunk in silence.
   //
   // Bounded, because a client can start as many as it likes: three attempts,
   // and none at all once one has succeeded. The buffer cap below still applies
   // to each of them.
   if (%client.tnbCertDone && %client.tnbApplied && !%client.tnbTagged &&
       %client.tnbLate < $TNBS::LateTries)
   {
      %client.tnbLate++;
      %client.tnbCertDone = "";
      %client.tnbCertBuf = "";
   }

   if (%client.tnbCertDone)
      return;

   %client.tnbCertBuf = %client.tnbCertBuf @ %chunk;

   if (strlen(%client.tnbCertBuf) > 20000)
   {
      %client.tnbCertBuf = "";
      %client.tnbCertDone = 1;

      if ($TNBS::Debug)
         echo("TNBrowserServer: clan certificate too long, dropped");
   }
}

// End of transfer. An empty buffer here is a client saying it has none, which
// is a perfectly ordinary thing to say.
//
// Verification does not happen here. It needs the GUID from the account
// certificate, and this arrives during the authentication phase that
// establishes it -- so the record is checked later, in onConnect, where that
// GUID exists.
function serverCmdtnb_clanCertDone(%client)
{
   if (%client.tnbCertDone)
      return;

   %client.tnbCertDone = 1;

   // Late: the player is already in the game. Their client had no certificate
   // when it was asked, fetched one, and is handing it over now.
   //
   // Verified exactly as an early one is -- same signature, expiry and GUID
   // checks, in the same order -- so arriving late buys a certificate nothing.
   // What it costs is a rename, which is why this is the one path that has to
   // put the name back together itself.
   if (%client.tnbApplied)
   {
      if (!%client.tnbTagged && %client.tnbCertBuf !$= "")
      {
         %guid = getField(%client.getAuthInfo(), 3);
         if (%guid !$= "")
         {
            %record = TNBSVerifyClanCert(%client.tnbCertBuf, %guid);
            if (%record !$= "")
            {
               TNBSApplyRecord(%client, %record);
               TNBSRetag(%client, %record);
            }
         }
      }

      %client.tnbCertBuf = "";
      return;
   }

   // Only reachable if the transfer was still running when the connect caught
   // up with it. Normally nothing is waiting: the request goes out at the top
   // of the authentication phase and this lands well before it ends.
   if (%client.tnbWaited)
      TNBSResume(%client);
}

// Ask the client, at the earliest moment there is a client to ask.
//
// Packaged rather than replaced, and Parent:: runs regardless of what we do --
// this is TribesNext's certificate transfer and it is none of our business.
package TNBrowserServer
{
   function serverCmdt2csri_sendCertChunk(%client, %chunk)
   {
      if (!%client.tnbAsked)
      {
         %client.tnbAsked = 1;
         commandToClient(%client, 'tnb_wantClanCert', $TNBS::Version);
      }

      Parent::serverCmdt2csri_sendCertChunk(%client, %chunk);
   }

   // Runs before the stock onConnect reads the auth info, so the record is in
   // place by the time the tag is rendered.
   //
   // Called once or twice depending on which package wraps which: with ours
   // outermost it runs before the authentication phase as well as after. The
   // GUID is the discriminator -- it is empty until t2csri has verified the
   // account certificate -- and the early pass simply steps out of the way.
   function GameConnection::onConnect(%client, %name, %raceGender, %skin, %voice, %voicePitch)
   {
      // getAuthInfo(), not the raw field. For a remote client t2csri fills
      // t2csri_authInfo during its handshake, but for a local connection the
      // field is empty until getAuthInfo() lazily falls back to
      // WONGetAuthInfo() -- which on a listen server is the host's own client
      // mod answering with the full record, tag included. Reading the field
      // directly would find no GUID and silently tag nobody.
      %guid = getField(%client.getAuthInfo(), 3);

      if (%guid $= "" || %client.tnbApplied)
      {
         Parent::onConnect(%client, %name, %raceGender, %skin, %voice, %voicePitch);
         return;
      }

      // A transfer still in flight. Narrow on purpose: it can only be true for
      // a client that has already started sending, so a player whose client
      // does not run the mod never waits for this, and neither does one whose
      // certificate arrived on time.
      if (%client.tnbCertBuf !$= "" && !%client.tnbCertDone && !%client.tnbWaited)
      {
         // Hold the connection rather than rename afterwards, when the answer
         // is this close. Renaming later works -- TNBSRetag does it, for the
         // certificate that arrives after a player is already in -- but it
         // costs three updates and a broadcast to every machine in the game,
         // and none of that is needed for a transfer that is one packet away.
         //
         // The pattern is TribesNext's own (serverSide.cs:239): stash the
         // arguments, arm an expiry, return without Parent::, and re-enter
         // through %client.onConnect once the answer is in.
         %client.tnbWaited = 1;
         %client.tnbName = %name;
         %client.tnbRaceGender = %raceGender;
         %client.tnbSkin = %skin;
         %client.tnbVoice = %voice;
         %client.tnbVoicePitch = %voicePitch;

         %client.tnbExpire = schedule($TNBS::WaitMs, 0, "TNBSResume", %client);
         return;
      }

      %client.tnbApplied = 1;

      if (%client.tnbCertBuf !$= "")
      {
         %record = TNBSVerifyClanCert(%client.tnbCertBuf, %guid);

         // Substitute wholesale: the record carries the tag and the full
         // membership list the game exposes through getAuthInfo(). No rename
         // here -- stock's onConnect below has not built the name yet, and
         // reads this on its way past.
         if (%record !$= "")
            TNBSApplyRecord(%client, %record);
      }

      // Not needed again, and it is the largest thing hanging off the client.
      // A client that has not sent one yet may still do so; that lands in
      // serverCmdtnb_clanCertDone's late branch and refills this.
      %client.tnbCertBuf = "";

      Parent::onConnect(%client, %name, %raceGender, %skin, %voice, %voicePitch);
   }
};

// Resume a held connect.
//
// Two callers race deliberately: the finished transfer and the expiry timer.
// Whichever arrives first wins and the other does nothing, so an answer that
// lands after the timeout cannot connect the player twice.
//
// %client.onConnect and not TNBrowserServer::onConnect -- calling the packaged
// name directly from inside the package would recurse into this override rather
// than continue past it, which is the same reason t2csri resumes its own phase
// with %client.onConnect(%client.tname, ...).
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

// Guarded: this file is exec'd from two entry points (console autoexec and the
// CreateServer scan), and activating twice is a console warning.
if (!isActivePackage(TNBrowserServer))
   activatePackage(TNBrowserServer);

echo("TNBrowserServer: clan tag support active");
