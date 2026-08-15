// TNBrowser -- community session via RSA challenge/response
//
// The game never keeps the account password around, so a session cannot be
// negotiated with one. Instead the backend runs a challenge/response against
// the account's RSA key:
//
//   1. Client sends its account certificate and a random nonce.
//   2. Server checks the certificate's TribesNext signature, then returns a
//      challenge encrypted with the account's public key.
//   3. Client decrypts it, checks that the leading bytes replay the nonce it
//      sent (proving the server answered *this* request), and returns the
//      remainder.
//   4. Server issues a session UUID, used to authorise every later call.
//
// This mirrors tournamentNetClient2's tournament/login.cs, with the two
// substitutions the current client requires:
//
//   * raw TCPObject with a hand-assembled request  ->  HTTPObject (HTTPS)
//   * rubyEval("... $accountKey.decrypt ...")      ->  t2csri_rsa_decrypt()
//
// The 2017 tournament client predates both; the Ruby interpreter it relied on
// is gone from this build.
//
// The exchange used to run against TribesNext's robot login, with the backend
// then asking TribesNext whether the resulting token was real -- so logging in
// here needed tribesnext.thyth.com to be up, twice over. It now runs against
// the community backend itself, which verifies the certificate against
// TribesNext's signing key rather than asking TribesNext anything. Step 1 sends
// the certificate for that reason: the old endpoint looked the account's public
// key up by GUID because it held the account database, and ours does not.
//
// The wire format is deliberately unchanged. Every line this file parses below
// is one TribesNext already answered with, which is why none of the retry,
// backoff or waiter logic had to be touched.

//-----------------------------------------------------------------------------
// State
//-----------------------------------------------------------------------------

// $TNB::UUID       current session token, "" when not logged in
// $TNB::Nonce      nonce awaiting a challenge
// $TNB::Challenge  challenge awaiting decryption
// $TNB::Errors     consecutive failures, drives the backoff
// $TNB::Schedule   pending retry/refresh event

// Waiter table state. Initialised here, at load, because "" is not 0 when used
// as an array index: on a client that has never called TNBSessionEnd -- which
// is every real game, since only the tests end a session explicitly -- the
// first waiter was stored at $TNB::WaitFn[""] while the count advanced to 1.
// The flush then called $TNB::WaitFn[0], found nothing, and the waiter was
// silently dropped.
//
// It cost exactly one request, the first of the session, and only ever the
// first: by the second the count is a real number. That is why it presented as
// "the mail window sits on Checking for mail... until I click the folder
// again", and why no suite saw it -- they all call TNBSessionEnd first.
if ($TNB::WaitCount $= "")
   $TNB::WaitCount = 0;

function TNBSessionReady()
{
   return ($TNB::UUID !$= "");
}

// The account GUID. $TNB::GuidOverride exists so the mod can be driven against
// the mock backend on a client that has never logged in.
//
// $LoginCertificate cannot be trusted to hold anything on its own. Nothing in
// TribesNext assigns it on the ordinary login path -- loginScreens.cs:339 does,
// but only in the account-CREATION branch. What normally fills it is a side
// effect of TribesNext's own WONGetAuthInfo (t2csri/clientSide.cs:12), which
// assigns it before returning.
//
// This mod overrides that function, so the side effect stopped happening: a
// player who really was logged in got "You are not logged in to a TribesNext
// account", because the global still held the "-1" that the pre-login call had
// left in it. So ask the account subsystem directly rather than relying on
// somebody else having asked recently.
function TNBSessionGuid()
{
   if ($TNB::GuidOverride !$= "")
      return $TNB::GuidOverride;

   %guid = getField($LoginCertificate, 1);
   if (%guid !$= "")
      return %guid;

   $LoginCertificate = TNBAccountCertificate();
   return getField($LoginCertificate, 1);
}

// The account certificate, or "" if there is no account subsystem.
//
// t2csri_getAccountCertificate exists only when the patch registered its
// console functions, which -nologin skips entirely. A missing function does not
// return empty in this engine -- it prints "Unable to find function" and the
// assignment still yields a meaningless number -- so the answer is checked for
// the shape of a certificate before it is trusted. A real one has five fields;
// anything with fewer than two cannot carry a GUID in field 1.
function TNBAccountCertificate()
{
   %cert = t2csri_getAccountCertificate();
   if (getFieldCount(%cert) < 2)
      return "";
   return %cert;
}

// Hex modulus of the account key; its length sizes the nonce.
function TNBSessionModulus()
{
   return getField($LoginCertificate, 3);
}

//-----------------------------------------------------------------------------
// Readiness callbacks
//
// GUI code wants to say "run this once we have a session" without caring
// whether that is immediately or after a round trip.
//-----------------------------------------------------------------------------

function TNBSessionOnReady(%fn, %ctx)
{
   if (TNBSessionReady())
   {
      call(%fn, %ctx);
      return;
   }

   %n = $TNB::WaitCount;
   if (%n $= "")
      %n = 0;
   $TNB::WaitFn[%n] = %fn;
   $TNB::WaitCtx[%n] = %ctx;
   $TNB::WaitCount = %n + 1;

   TNBSessionStart();
}

function TNBSessionFlushWaiters()
{
   %count = $TNB::WaitCount;
   $TNB::WaitCount = 0;
   for (%i = 0; %i < %count; %i++)
   {
      // Skip empties rather than call(""), which the console reports only as
      // ": Unknown command." -- an error message with nothing in it to trace.
      if ($TNB::WaitFn[%i] $= "")
         continue;
      call($TNB::WaitFn[%i], $TNB::WaitCtx[%i]);
   }
}

function TNBSessionFailWaiters(%reason)
{
   %count = $TNB::WaitCount;
   $TNB::WaitCount = 0;
   for (%i = 0; %i < %count; %i++)
   {
      if ($TNB::WaitFn[%i] $= "")
         continue;
      call($TNB::WaitFn[%i], $TNB::WaitCtx[%i], "error", %reason);
   }
}

//-----------------------------------------------------------------------------
// Negotiation
//-----------------------------------------------------------------------------

function TNBSessionStart()
{
   // One negotiation at a time.
   //
   // TNBSessionSend deletes and recreates TNBSessionInterface, so a second
   // start while the first is in flight destroys it -- and worse, replaces
   // $TNB::Nonce, so the challenge that was already on its way fails the replay
   // check and the exchange begins again. Two negotiations chasing each other
   // never converge, and with no session every pane in the mod is dead at once:
   // mail and the browser wait on the same token.
   //
   // The request queue has its own guard for exactly this
   // ($TNB::AwaitingSession, api.cs:158), and it is not enough: it covers the
   // queue's own path, while the keepalive below is a second entry point that
   // fires on a timer. So the guard lives here, where the shared connection
   // object is.
   //
   // Dropping the duplicate loses nothing: the waiter list is shared, so
   // whoever asked second is called when the negotiation already running
   // finishes.
   //
   // The age check is the backstop. Every completion path clears the flag, but
   // if one were ever missed this would pin the session down permanently, and
   // that failure is indistinguishable from the one it is here to prevent.
   if ($TNB::SessionBusy && (getSimTime() - $TNB::SessionSentAt) < 30000)
      return;

   // Never two transfers at once, which is the harder rule and the one that
   // matters more.
   //
   // This object is the last thing in the mod that does not go through the
   // request queue, and it cannot: the queue waits on the session, so a session
   // that waited on the queue would wait on itself. What it can do is stay out
   // of the way. Two HTTPObject transfers started close together against a real
   // HTTPS endpoint wedge one of them permanently -- no callback of any kind,
   // ever -- and when the wedged one is the queue's, every pane in the mod dies
   // behind it. Measured on a live client against the deployed backend, where
   // it took out mail and the browser together; it does not reproduce over
   // plain HTTP against a local server, which is why it went unnoticed for so
   // long.
   //
   // Only the keepalive can collide. A negotiation cannot: the queue declines to
   // send until the session is ready, so while one is running nothing else is in
   // flight. The keepalive runs *because* the session is up, which is exactly
   // when the queue is free to be busy -- so it defers, and a refresh that is a
   // few seconds late costs nothing against a thirty-minute expiry.
   if ($TNB::UUID !$= "" && $TNB::Busy)
   {
      if (isEventPending($TNB::Schedule))
         cancel($TNB::Schedule);
      $TNB::Schedule = schedule(2000, 0, "TNBSessionStart");
      return;
   }

   if (isEventPending($TNB::Schedule))
      cancel($TNB::Schedule);
   $TNB::Schedule = "";

   %guid = TNBSessionGuid();
   if (%guid $= "")
   {
      TNBSessionFail("You are not logged in to a TribesNext account.");
      return;
   }

   %query = "guid=" @ %guid @ "&";

   if ($TNB::UUID !$= "")
      %query = %query @ "uuid=" @ $TNB::UUID;
   else if ($TNB::Challenge $= "")
   {
      // The certificate travels with the nonce. It is public -- the client
      // hands the same five fields to every game server it joins
      // (t2csri/clientSide.cs:120-124) -- and it is what lets a backend with no
      // account database verify who is asking and encrypt a challenge only
      // they can read.
      //
      // Sent when there is one, and the request goes out without it when there
      // is not, rather than refusing here. A client launched -nologin has no
      // account subsystem at all (t2csri_getAccountCertificate does not exist),
      // which is every test container -- and whether that is allowed to log in
      // is the server's decision to make, not ours. A real backend answers ERR;
      // one started for the test suites hands over a session.
      %query = %query @ "nonce=" @ TNBSessionMakeNonce();

      %cert = TNBAccountCertificate();
      if (%cert !$= "")
      {
         // Remembered so a failed replay check can say which account the
         // challenge was minted for. The backend encrypts with the key in the
         // certificate it was handed, and the reply is decrypted with the key
         // of the account that is logged in -- two different questions, and
         // nothing here proves they have the same answer. This machine has two
         // accounts in public.store, which is the configuration where they can
         // differ.
         $TNB::CertName = getField(%cert, 0);
         $TNB::CertGuid = getField(%cert, 1);
         %query = %query @ "&cert=" @ TNBUrlEncode(%cert);
      }
   }
   else
   {
      %response = TNBSessionAnswerChallenge();
      if (%response $= "")
         return;                 // AnswerChallenge already restarted or failed
      %query = %query @ "response=" @ %response;
   }

   TNBSessionSend(%query);
}

// A random hex nonce half the width of the modulus, so that nonce plus the
// server's challenge still fits in one RSA block. The leading "1" keeps the
// value from being truncated when it round-trips through a bignum that drops
// leading zeros.
function TNBSessionMakeNonce()
{
   %length = strlen(TNBSessionModulus()) / 2;
   if (%length < 8)
      %length = 8;

   %nonce = "1";
   for (%i = 1; %i < %length; %i++)
      %nonce = %nonce @ getSubStr($TNBJson::HexDigits, getRandom(0, 15), 1);

   $TNB::Nonce = %nonce;
   return %nonce;
}

// Decrypt the stored challenge and extract the part to send back. Returns ""
// if the challenge was unusable, having already scheduled a retry.
function TNBSessionAnswerChallenge()
{
   %challenge = strlwr($TNB::Challenge);

   // A challenge is pure hex. Anything else is the server trying to get
   // arbitrary text into an interpreter argument, so drop it and start over --
   // the same guard the stock client applies to server challenges.
   for (%i = 0; %i < strlen(%challenge); %i++)
   {
      if (!isxdigit(getSubStr(%challenge, %i, 1)))
      {
         error("TNBrowser: non-hex challenge from server, discarding");
         $TNB::Challenge = "";
         TNBSessionRetry();
         return "";
      }
   }

   // No nonce means no question was asked, so nothing here can answer one. The
   // comparison below would pass on its own -- getSubStr(x, 0, 0) and "" are
   // equal -- and the whole decrypted blob would go back as the response, which
   // the backend refuses without saying why. Caught separately so the console
   // says what actually happened.
   if ($TNB::Nonce $= "")
   {
      error("TNBrowser: a challenge arrived with no nonce outstanding, discarding");
      $TNB::Challenge = "";
      TNBSessionRetry();
      return "";
   }

   %decrypted = t2csri_rsa_decrypt(%challenge);

   %replay = getSubStr(%decrypted, 0, strlen($TNB::Nonce));
   if (%replay !$= $TNB::Nonce)
   {
      // Say what was seen, because the bare message names a symptom that has
      // several causes and rules none of them out.
      //
      // Verified against a live client (orange01, 1024-bit key) and the
      // deployed backend, so these are not among them: t2csri_rsa_decrypt
      // round-trips this exact plaintext layout, including a blob whose leading
      // hex digit the server's rendering dropped, and the engine reassembles a
      // response line split across TCP segments before onLine sees it.
      //
      // What is left is a mismatch of identity or of timing, and the three
      // lines below tell them apart. A decrypted value the right length but
      // wrong throughout means it was encrypted for a key other than the one
      // that decrypted it -- look at the two accounts on the last line. A short
      // or empty one means the decrypt itself failed. A value that replays some
      // other nonce means a second negotiation replaced ours while this
      // challenge was in flight.
      error("TNBrowser: challenge did not replay our nonce, discarding");
      error("TNBrowser:   nonce " @ strlen($TNB::Nonce) @ " chars, opens " @ getSubStr($TNB::Nonce, 0, 16));
      error("TNBrowser:   decrypted " @ strlen(%decrypted) @ " chars, opens " @ getSubStr(%decrypted, 0, 16));
      %now = TNBAccountCertificate();
      error("TNBrowser:   certificate sent for " @ $TNB::CertName @ "/" @ $TNB::CertGuid @
            ", account now " @ getField(%now, 0) @ "/" @ getField(%now, 1));
      $TNB::Challenge = "";
      TNBSessionRetry();
      return "";
   }

   return getSubStr(%decrypted, strlen($TNB::Nonce), strlen(%decrypted));
}

function TNBSessionSend(%query)
{
   if (isObject(TNBSessionInterface))
      TNBSessionInterface.delete();
   new HTTPObject(TNBSessionInterface);

   TNBSessionInterface.buffer = "";
   TNBSessionInterface.handled = 0;
   TNBSessionInterface.setHeader("Accept", "text/plain");
   TNBSessionInterface.setHeader("Content-Type", "application/x-www-form-urlencoded");

   // POST, because the certificate is about 1,200 characters and a query string
   // is the wrong place for it. The URI carries nothing: this build's
   // HTTPObject ignores the third argument of get() -- verified against the
   // live server, where passing the query there produced "ERR: No GUID
   // specified." -- and post() puts the body where a body belongs.
   //
   // $TNB::Host, not a separate auth host. The session is negotiated with the
   // community backend now, which is the same server that holds the data.
   $TNB::SessionBusy = 1;
   $TNB::SessionSentAt = getSimTime();

   TNBSessionInterface.post($TNB::Host, $TNB::SessionURI, "", %query);
}

//-----------------------------------------------------------------------------
// Transport callbacks
//-----------------------------------------------------------------------------

function TNBSessionInterface::onLine(%this, %line)
{
   %line = trim(%line);
   if (%line $= "")
      return;                    // blank separator line before the body

   %this.handled = 1;

   if ($TNB::Debug)
      echo("TNBrowser session <- " @ %line);

   if (getSubStr(%line, 0, 11) $= "CHALLENGE: ")
   {
      $TNB::Errors = 0;
      $TNB::Challenge = getSubStr(%line, 11, strlen(%line));
      // Answer on the next tick rather than inline, so the decrypt does not
      // happen inside the network callback.
      $TNB::Schedule = schedule(200, 0, "TNBSessionStart");
      return;
   }

   if (getSubStr(%line, 0, 6) $= "UUID: ")
   {
      $TNB::Errors = 0;
      $TNB::UUID = getSubStr(%line, 6, strlen(%line));
      $TNB::Challenge = "";
      $TNB::Nonce = "";
      echo("TNBrowser: session established");
      $TNB::Schedule = schedule($TNB::SessionRefresh * 1000, 0, "TNBSessionStart");
      TNBSessionFlushWaiters();
      return;
   }

   if (getSubStr(%line, 0, 9) $= "REFRESHED")
   {
      $TNB::Errors = 0;
      $TNB::Schedule = schedule($TNB::SessionRefresh * 1000, 0, "TNBSessionStart");
      return;
   }

   if (getSubStr(%line, 0, 7) $= "TIMEOUT")
   {
      // The session lapsed; drop it and negotiate a fresh one.
      $TNB::UUID = "";
      $TNB::Challenge = "";
      $TNB::Errors = 0;
      $TNB::Schedule = schedule(200, 0, "TNBSessionStart");
      return;
   }

   if (getSubStr(%line, 0, 5) $= "ERR: ")
   {
      TNBSessionFail(getSubStr(%line, 5, strlen(%line)));
      return;
   }
}

// This build's libcurl-backed HTTPObject does NOT call onConnectFailed or
// onDNSFailed -- verified by pointing one at a dead port, where the only script
// callback that fires is onDisconnect (libcurl prints its own diagnostic to the
// console and closes). So a connection failure is "disconnected without having
// understood a single line", and that is what has to be detected here.
//
// Without this the session simply never calls back: every queued request stays
// parked behind a negotiation that will never finish, which looks exactly like
// a hang.
function TNBSessionInterface::onDisconnect(%this)
{
   // Every transfer ends here, understood or not, which makes this the one
   // place the in-flight flag can be cleared. Before the early return, because
   // the successful case takes it -- and a flag left set after a *successful*
   // negotiation would block the next one just as thoroughly as one left set
   // after a failure.
   //
   // The next step of the handshake is scheduled 200ms out (onLine, above), so
   // it always finds this cleared.
   $TNB::SessionBusy = "";

   if (%this.handled)
      return;

   TNBSessionFail("Could not reach the TribesNext community server.");
}

// Kept for completeness in case a future patch starts emitting them; they are
// not the path that fires today.
function TNBSessionInterface::onConnectFailed(%this)
{
   TNBSessionFail("Could not reach the TribesNext community server.");
}

function TNBSessionInterface::onDNSFailed(%this)
{
   TNBSessionFail("Could not resolve the TribesNext community server.");
}

//-----------------------------------------------------------------------------
// Failure handling
//-----------------------------------------------------------------------------

function TNBSessionFail(%reason)
{
   error("TNBrowser: session error -- " @ %reason);

   $TNB::SessionBusy = "";
   $TNB::UUID = "";
   $TNB::Challenge = "";
   $TNB::LastError = %reason;

   // Forget the cached identity, so the next attempt asks the account
   // subsystem again instead of retrying the same wrong answer forever.
   //
   // TNBSessionGuid reads $LoginCertificate and returns early when it already
   // holds a GUID (:84), so the first answer is kept for the whole run. That is
   // fine when the first answer is taken after login and fatal when it is taken
   // before: on a machine with more than one stored account,
   // t2csri_getAccountCertificate() with no argument can hand back an account
   // the player is not logged in as, and the backend then answers "That account
   // certificate was not accepted" -- forever, because nothing ever re-read it.
   // Mail and the browser both wait on that session, so both stay dead for the
   // rest of the session.
   //
   // Observed exactly that against the live backend on a client with two stored
   // accounts. It stayed invisible locally because a server run with
   // -dev-trust-guid accepts a bare GUID without looking at the certificate at
   // all, so the wrong identity works there.
   //
   // Cleared on every failure rather than only on that message: re-reading
   // costs one call and returns the same answer in the ordinary case, and a
   // session layer that cannot correct a bad identity is one bad read away from
   // taking every pane down with it.
   $LoginCertificate = "";

   TNBSessionFailWaiters(%reason);
   TNBSessionRetry();
}

// Quadratic backoff, capped near fifteen minutes, matching the reference
// client. Keeps a server-side outage from turning into a request flood.
function TNBSessionRetry()
{
   $TNB::Errors++;
   if ($TNB::Errors > 66)
      $TNB::Errors = 66;

   %delay = 200 * ($TNB::Errors * $TNB::Errors);
   if (isEventPending($TNB::Schedule))
      cancel($TNB::Schedule);
   $TNB::Schedule = schedule(%delay, 0, "TNBSessionStart");
}

// Drop the session and stop all scheduled work. Used when the browser closes
// and on shutdown.
function TNBSessionEnd()
{
   if (isEventPending($TNB::Schedule))
      cancel($TNB::Schedule);
   $TNB::Schedule = "";
   $TNB::UUID = "";
   $TNB::Challenge = "";
   $TNB::Nonce = "";
   $TNB::Errors = 0;
   $TNB::WaitCount = 0;

   // Whatever was in flight is abandoned along with everything else, so the
   // next caller must not be told to wait for it. The suites end a session and
   // immediately start another, which is the one path that would notice.
   $TNB::SessionBusy = "";
}

echo("TNBrowser: session.cs loaded");
