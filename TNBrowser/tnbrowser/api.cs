// TNBrowser -- the HTTP request queue
//
// One queue, one connection object, one request in flight. The engine gives
// each HTTPObject a single set of callbacks, so two overlapping transfers would
// interleave their response bodies; everything that talks to the backend goes
// through here for that reason. dbproxy.cs sits on top of it and is the only
// caller that matters -- it turns the shipped scripts' DatabaseQuery() into a
// request and the answer back into rows.
//
// Callbacks are invoked as
//
//     call(%callback, %context, %status, %result)
//
//   %status  "ok" or "error"
//   %result  on ok, the parsed JSON root node; on error, a message
//
// The result tree is freed as soon as the callback returns, so a callback must
// copy anything it wants to keep rather than store the node.
//
// Authentication is the guid/uuid pair from session.cs. The session is
// established lazily -- enqueuing with no session negotiates one first and then
// runs the request.

//-----------------------------------------------------------------------------
// Queue
//-----------------------------------------------------------------------------

function TNBApiInit()
{
   $TNB::QHead = 0;
   $TNB::QTail = 0;
   $TNB::Busy = 0;
   $TNB::AwaitingSession = 0;
   TNBApiCursorIdle();
}

// The wait cursor, as the original screens had it.
//
// Stock raised it at every query site and dropped it in the result handlers --
// webbrowser.cs has 24 of the former against 7 of the latter, webemail.cs
// :366 and :1106 -- which is exactly why it leaked: any path that returned
// early left the hourglass up.
//
// This client has something stock did not: one queue that mail, the browser and
// the robot login all pass through. So the same behaviour comes from two places
// instead of thirty, and cannot be left behind. A chain of requests holds the
// cursor for the whole chain rather than flickering between the links, because
// the drop waits for the queue to empty rather than firing per response.
//
// The session keepalive is deliberately outside this: it re-runs TNBSessionStart
// on a ten-minute timer over its own connection object (session.cs), and stock
// only ever showed the cursor for a query the player had asked for.
//
// $TNB::CursorBusy is the state as well as the guard -- the engine has no
// getCursor, so it is the only way to observe this from a test.
function TNBApiCursorBusy()
{
   if ($TNB::CursorBusy)
      return;
   $TNB::CursorBusy = 1;

   // Absent on a dedicated server, which loads this file but never calls it.
   if (isObject(Canvas))
      Canvas.setCursor(ArrowWaitCursor);
}

function TNBApiCursorIdle()
{
   if (!$TNB::CursorBusy)
      return;
   $TNB::CursorBusy = "";

   if (isObject(Canvas))
      Canvas.setCursor(DefaultCursor);
}

function TNBApiEnqueueOn(%uri, %method, %payload, %callback, %ctx, %usePost)
{
   TNBApiEnqueueRaw(%uri, %method, %payload, %callback, %ctx, %usePost, 0);
}

// %raw hands the response body to the callback untouched instead of parsing it
// as JSON. The robot endpoints answer tab-delimited lines, not JSON, but must
// still share this queue -- two transfers on the one connection object would
// interleave their bodies.
function TNBApiEnqueueRaw(%uri, %method, %payload, %callback, %ctx, %usePost, %raw)
{
   TNBApiEnqueueRawOn("", %uri, %method, %payload, %callback, %ctx, %usePost, %raw);
}

// %host overrides the data host for one request. The session and the community
// certificate live on TribesNext even when browser data is served from a
// self-hosted backend, so those calls name their host explicitly; everything
// else passes "" and follows $TNB::Host.
function TNBApiEnqueueRawOn(%host, %uri, %method, %payload, %callback, %ctx, %usePost, %raw)
{
   %t = $TNB::QTail;
   $TNB::QHost[%t] = %host;
   $TNB::QRaw[%t] = %raw;
   $TNB::QUri[%t] = %uri;
   $TNB::QMethod[%t] = %method;
   $TNB::QPayload[%t] = %payload;
   $TNB::QCallback[%t] = %callback;
   $TNB::QCtx[%t] = %ctx;
   $TNB::QPost[%t] = %usePost;
   $TNB::QTail = %t + 1;

   TNBApiCursorBusy();

   // Always deferred, never inline: callers routinely chain a second call from
   // inside the first one's callback (a clan edit re-reading the clan, say),
   // and starting a transfer from within a network callback wedges it. See
   // TNBApiPumpSoon.
   if (!$TNB::Busy)
      TNBApiPumpSoon();
}

// Start the next request on a fresh tick rather than inline.
//
// Issuing an HTTP request from inside another HTTPObject's callback leaves it
// stuck in "Connecting" forever against the live server: the libcurl multi
// handle is mid-iteration on the connection whose callback we are in, and the
// new transfer never gets driven. Verified the hard way -- the same request
// that hangs when fired from the session's onLine callback completes in half a
// second when fired one tick later.
//
// Anything reached from a network callback must therefore go through here.
function TNBApiPumpSoon()
{
   schedule(32, 0, "TNBApiPump");
}

function TNBApiPump()
{
   if ($TNB::Busy)
      return;
   if ($TNB::QHead >= $TNB::QTail)
   {
      // Queue drained; reset the indices so they cannot grow without bound
      // across a long session.
      $TNB::QHead = 0;
      $TNB::QTail = 0;
      TNBApiCursorIdle();
      return;
   }

   if (!TNBSessionReady())
   {
      // Negotiate first, then come back for this same queue entry.
      //
      // At most one waiter, ever. Pump runs on a timer and on every enqueue, so
      // without this guard each pump while the session is down registers
      // another waiter -- and on failure the session layer then calls back once
      // per waiter. The first TNBApiFailAll runs the queued callbacks (which
      // routinely enqueue a retry) and the second promptly clears the queue
      // again, silently discarding it. That is a request accepted and never
      // sent, with a caller waiting for a callback that never comes.
      if (!$TNB::AwaitingSession)
      {
         $TNB::AwaitingSession = 1;
         TNBSessionOnReady("TNBApiSessionReady", "");
      }
      return;
   }

   $TNB::Busy = 1;
   TNBApiSend();
}

// Session callback. A failure here fails every queued request rather than
// leaving them stuck behind a session that will not come up.
function TNBApiSessionReady(%ctx, %status, %reason)
{
   $TNB::AwaitingSession = 0;

   if (%status $= "error")
   {
      TNBApiFailAll(%reason);
      return;
   }
   TNBApiPumpSoon();
}

// Fail every queued request.
//
// The pending entries are copied out and the queue cleared *before* any callback
// runs. Callbacks routinely enqueue new work -- a retry, or the next step of a
// flow -- and clearing afterwards would silently discard whatever they added,
// leaving a request that was accepted but never sent and a caller waiting for a
// callback that can never arrive.
function TNBApiFailAll(%reason)
{
   %count = 0;
   for (%h = $TNB::QHead; %h < $TNB::QTail; %h++)
   {
      $TNB::FailCb[%count] = $TNB::QCallback[%h];
      $TNB::FailCtx[%count] = $TNB::QCtx[%h];
      %count++;
   }

   $TNB::QHead = 0;
   $TNB::QTail = 0;
   $TNB::Busy = 0;

   // Dropped before the callbacks, not after: one of them enqueuing a retry must
   // be able to raise it again.
   TNBApiCursorIdle();

   for (%i = 0; %i < %count; %i++)
      call($TNB::FailCb[%i], $TNB::FailCtx[%i], "error", %reason);
}

//-----------------------------------------------------------------------------
// Transport
//-----------------------------------------------------------------------------

function TNBApiSend()
{
   %h = $TNB::QHead;
   %host = $TNB::QHost[%h];
   if (%host $= "")
      %host = $TNB::Host;
   %uri = $TNB::QUri[%h];
   %method = $TNB::QMethod[%h];
   %payload = $TNB::QPayload[%h];

   if (isObject(TNBApiInterface))
      TNBApiInterface.delete();
   new HTTPObject(TNBApiInterface);

   TNBApiInterface.buffer = "";
   TNBApiInterface.rawbuffer = "";
   TNBApiInterface.setHeader("Accept", "application/json");

   %auth = "guid=" @ TNBSessionGuid() @
           "&uuid=" @ $TNB::UUID @
           "&method=" @ %method;

   if ($TNB::QPost[%h])
   {
      // User-authored text (profile bodies, clan info, titles) can be long and
      // contains characters that are awkward in a URI, so it travels as a form
      // body. PHP reads both through $_REQUEST, so the split is invisible to
      // the server.
      TNBApiInterface.setHeader("Content-Type", "application/x-www-form-urlencoded");
      TNBApiInterface.post(%host, %uri @ "?" @ %auth, "",
                           "payload=" @ TNBUrlEncode(%payload));
   }
   else
   {
      %full = %uri @ "?" @ %auth;
      if (%payload !$= "")
         %full = %full @ "&payload=" @ TNBUrlEncode(%payload);
      TNBApiInterface.get(%host, %full, "");
   }

   if ($TNB::Debug)
      echo("TNBrowser api -> " @ %method @ " " @ %payload);
}

function TNBApiInterface::onLine(%this, %line)
{
   // Two buffers on purpose.
   //
   // JSON: json_encode never emits newlines, and a long body can still be split
   // across callbacks mid-token, so the pieces must be concatenated with
   // nothing between them or the document is corrupted.
   //
   // Raw: the robot endpoints answer several tab-delimited lines (DCE, CEC,
   // ERR), and those line boundaries are the record separators the caller needs.
   // Concatenating them the JSON way glues the records together and getRecord()
   // then sees one line instead of three.
   %this.buffer = %this.buffer @ %line;
   %this.rawbuffer = %this.rawbuffer @ %line @ "\n";
}

function TNBApiInterface::onDisconnect(%this)
{
   TNBApiComplete($TNB::QRaw[$TNB::QHead] ? %this.rawbuffer : %this.buffer);
}

function TNBApiInterface::onConnectFailed(%this)
{
   TNBApiComplete("");
}

function TNBApiInterface::onDNSFailed(%this)
{
   TNBApiComplete("");
}

function TNBApiComplete(%body)
{
   %h = $TNB::QHead;
   if (%h >= $TNB::QTail)
      return;                    // stray callback after the queue was flushed

   $TNB::QHead = %h + 1;
   $TNB::Busy = 0;

   %callback = $TNB::QCallback[%h];
   %ctx = $TNB::QCtx[%h];

   %body = trim(%body);

   if ($TNB::Debug)
      echo("TNBrowser api <- " @ getSubStr(%body, 0, 400));

   if ($TNB::QRaw[%h])
   {
      call(%callback, %ctx, (%body $= "" ? "error" : "ok"),
           (%body $= "" ? "No response from the community server." : %body));
      TNBApiPumpSoon();
      return;
   }

   if (%body $= "")
   {
      call(%callback, %ctx, "error", "No response from the community server.");
      TNBApiPumpSoon();
      return;
   }

   %root = TNBJsonParse(%body);

   if (!%root)
   {
      // The backend answers 401 with an HTML page, which is the usual reason
      // to land here: the session lapsed between negotiation and this request.
      if (strstr(%body, "401") >= 0 || strstr(%body, "Authentication") >= 0)
      {
         $TNB::UUID = "";
         call(%callback, %ctx, "error", "Session expired. Please try again.");
      }
      else
         call(%callback, %ctx, "error", "Unreadable response from the community server.");
      TNBApiPumpSoon();
      return;
   }

   // Methods that mutate something answer {"status":...}; readers answer their
   // payload directly, with no status field.
   if (TNBJsonStr(%root, "status") $= "error")
   {
      %msg = TNBJsonStr(%root, "msg");
      if (%msg $= "")
         %msg = "The community server rejected that request.";
      call(%callback, %ctx, "error", %msg);
   }
   else
      call(%callback, %ctx, "ok", %root);

   TNBJsonFree(%root);
   TNBApiPumpSoon();
}

echo("TNBrowser: api.cs loaded");
