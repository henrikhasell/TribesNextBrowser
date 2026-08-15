// TNBrowser -- the database proxy, over HTTP
//
// This is the whole of the interception. The shipped community scripts --
// webbrowser.cs, webemail.cs, webnews.cs, webforums.cs, weblinks.cs -- reach
// their backend through exactly two functions:
//
//     DatabaseQuery(ordinal, args, proxyObject, key)              webstuff.cs:218
//     DatabaseQueryArray(ordinal, maxRows, args, proxyObject, key)         :223
//
// Both are TorqueScript, not native, so replacing them in a package is all it
// takes to move every community pane onto a different transport while leaving
// the panes -- and every .gui behind them -- completely untouched.
//
// What the shipped pair did was wrap DatabaseQueryi (:181), which framed the
// call as "dbqax <id> <ordinal> <maxRows> :<args>" and pushed it down the WON
// chat socket. That route has two problems here and only one of them is that
// WON is gone: DatabaseQueryi refuses to send anything unless
// $IRCClient::state $= IDIRC_CONNECTED (:183), and answers a *fabricated*
// "1\tORA-04061" when it is not. Overriding the two public entry points steps
// over that gate entirely, which is why nothing in this mod has to talk to a
// chat server to make the browser work.
//
// The ordinals themselves are unchanged, and that is the point. They were
// Oracle stored-procedure selectors -- the client tests for ORA-04061 by name
// in seven handlers -- and the argument tuples and row schemas belong to the
// shipped parsers, not to us. The table lives on the server side, in
// server/internal/dbproxy/ordinals.go, with each entry cited to the call site
// that issues it and the handler that reads the answer back.
//
// Delivery order is the contract, and it is HandleDatabaseProxyResponse's
// (webstuff.cs:85):
//
//     proxyObject.onDatabaseQueryResult(status, resultString, key)   once
//     proxyObject.onDatabaseRow(row, isLast, key)                    per row
//
// status is tab-separated: field 0 is the code every handler tests with
// getField(%status,0)==0, field 1 is the sentence several of them show verbatim
// in a MessageBoxOK, and fields 2.. carry the entire payload for the two
// profile ordinals. resultString is a row count for the array form and a
// payload for the scalars that overload it.

//-----------------------------------------------------------------------------
// The shipped entry points
//-----------------------------------------------------------------------------

// Both forms select different stored procedures for the same number: on the
// wire DatabaseQuery(1,..) sent "1 0" and DatabaseQueryArray(1,..) sent
// "C1 <maxRows>", and four ordinals collide across the two (0, 1, 10, 11) with
// unrelated meanings each time -- scalar 10 is addBuddy, array 10 is a tribe
// roster. So the form travels with the ordinal and the server keys on the pair.
function TNBDbQuery(%ordinal, %args, %proxyObject, %key)
{
   return TNBDbSend("scalar", %ordinal, 0, %args, %proxyObject, %key);
}

function TNBDbQueryArray(%ordinal, %maxRows, %args, %proxyObject, %key)
{
   return TNBDbSend("array", %ordinal, %maxRows, %args, %proxyObject, %key);
}

function TNBDbSend(%form, %ordinal, %maxRows, %args, %proxyObject, %key)
{
   %id = $TNB::DbNextId + 1;
   $TNB::DbNextId = %id;

   $TNB::DbProxy[%id] = %proxyObject;
   $TNB::DbKey[%id] = %key;
   $TNB::DbCancelled[%id] = 0;

   if ($TNB::Debug)
      echo("TNBrowser db -> " @ %form SPC %ordinal SPC "[" @ %args @ "]");

   // POST, always, with a JSON body. Arguments are user-authored text often
   // enough -- a profile body, a mail body, a clan description -- that a query
   // string is the wrong place for them, and the two forms would otherwise
   // differ for no reason.
   //
   // %maxRows is not sent. It was nominally a row cap, but three of the shipped
   // call sites put a page number in that slot and a fourth a real limit, so it
   // could never be read as one; ordinals that need a bound pick their own.
   TNBApiEnqueueOn($TNB::DbURI, "db",
                   TNBJsonObjectWith("form", %form, "ordinal", %ordinal,
                                     "args", TNBJsonFieldArray(%args)),
                   "TNBDbDone", %id, 1);
   return %id;
}

// Nothing in the five shipped scripts actually calls this, but the shipped
// function exists and taking the override half-way would leave a caller sending
// "dbqc" down a socket that is not there.
function TNBDbQueryCancel(%id)
{
   $TNB::DbCancelled[%id] = 1;
   $TNB::DbProxy[%id] = "";
}

//-----------------------------------------------------------------------------
// The answer
//-----------------------------------------------------------------------------

function TNBDbDone(%id, %status, %result)
{
   %proxy = $TNB::DbProxy[%id];
   %key = $TNB::DbKey[%id];

   $TNB::DbProxy[%id] = "";
   $TNB::DbKey[%id] = "";

   if ($TNB::DbCancelled[%id])
      return;

   if (%status $= "error")
   {
      TNBDbFail(%proxy, %key, %result);
      return;
   }

   // Rebuild the status string the shipped scripts parse. The wire carries a
   // real code, a message and -- for the two profile ordinals -- the payload
   // fields that used to trail the status. Joining them here is the whole job
   // of a shim: the packed format lives for one line, at the last moment,
   // instead of on the wire.
   %code = TNBJsonStr(%result, "code");
   if (%code $= "")
   {
      TNBDbFail(%proxy, %key, "The community server sent an answer with no code.");
      return;
   }

   %statusField = %code TAB TNBJsonStr(%result, "message");

   %extra = TNBJsonGet(%result, "fields");
   %extraCount = TNBJsonCount(%extra);
   for (%i = 0; %i < %extraCount; %i++)
      %statusField = %statusField TAB TNBJsonValue(TNBJsonIndex(%extra, %i));

   %resultString = TNBJsonStr(%result, "result");
   %rows = TNBJsonGet(%result, "rows");

   // A query with neither proxy object nor key is a real call form, not a
   // mistake: DatabaseQuery(7, %id) at webemail.cs:1328 marks a message read
   // and is the only site in all five scripts that passes neither, so its
   // answer is reassembled and discarded. Guard rather than assume.
   if (%proxy $= "")
      return;

   %proxy.onDatabaseQueryResult(%statusField, %resultString, %key);

   // Each row arrives as an array of fields and leaves as the tab-joined string
   // onDatabaseRow has always been handed. The engine reads a JSON true as 1,
   // so a boolean field joins as the spelling getField callers test for.
   %count = TNBJsonCount(%rows);
   for (%i = 0; %i < %count; %i++)
      %proxy.onDatabaseRow(TNBDbJoinRow(TNBJsonIndex(%rows, %i)),
                           (%i == %count - 1), %key);

   // No rows means no onDatabaseRow at all, and therefore no isLast. That
   // matches the shipped client exactly -- HandleDatabaseProxyResponse's row
   // loop (webstuff.cs:150) only emits a row when it finds a trailing
   // separator, so an empty result set signals completion through the status
   // handler and nowhere else. Synthesising an empty final row here would look
   // tidier and would put a blank line in every list the panes build.
}

// A transport failure is answered exactly as the shipped client answers a
// missing connection: DatabaseQueryi (webstuff.cs:186) hands the proxy object
// "1\tORA-04061" when the chat socket is down. Seven handlers special-case that
// string by name and show "please wait a few moments and try again", which is
// both the right message and one the player has UI for. A failure our server
// described in words is passed through instead, because it says more.
// One row's fields, tab-joined. The indices are the shipped parsers' contract,
// so every field is emitted including the empty ones.
function TNBDbJoinRow(%row)
{
   %n = TNBJsonCount(%row);
   %out = "";
   for (%i = 0; %i < %n; %i++)
   {
      if (%i > 0)
         %out = %out @ "\t";
      %out = %out @ TNBJsonValue(TNBJsonIndex(%row, %i));
   }
   return %out;
}

function TNBDbFail(%proxy, %key, %reason)
{
   if (%proxy $= "")
      return;

   if (%reason $= "")
      %reason = "ORA-04061: existing state of packages has been discarded";

   %proxy.onDatabaseQueryResult("1" TAB %reason, "", %key);
}

//-----------------------------------------------------------------------------
// The certificate
//-----------------------------------------------------------------------------
//
// WONGetAuthInfo() is the identity the whole Community section reads -- 45
// call sites across the five scripts, everything from "is this me?" through
// "may I administer this tribe?" to the path the mail cache is written to. On
// a TribesNext-patched client the native is simply absent (it answers "Unable
// to find function"), so unlike a retail install there is nothing to shadow:
// defining it is the whole job.
//
// The layout is pinned by the scripts that read it. Records are newline
// separated, fields tab separated:
//
//     record 0    name TAB tag TAB append TAB guid
//     record 1    <tribe count>
//     record 2+n  name TAB tag TAB append TAB tribeId TAB adminLevel TAB title
//
// Field 0 of record 0 is the warrior name (GameGui.cs:1324) and field 3 the
// GUID (webemail.cs:1276); webbrowser.cs:271 compares that field against field
// 3 of a row quad, which is what makes record 0 a quad rather than four
// unrelated fields. Record 1 field 0 is the tribe count (webbrowser.cs:101),
// and the tribe records are pinned by webbrowser.cs:1909-1926, where viewing
// your own tribe list renders straight from here instead of querying.
function TNBCertGet()
{
   return $TNB::Cert;
}

function TNBCertRefresh(%callback, %ctx)
{
   $TNB::CertCb = %callback;
   $TNB::CertCbCtx = %ctx;
   TNBApiEnqueueOn($TNB::CertURI, "cert", "", "TNBCertLoaded", "", 0);
}

function TNBCertLoaded(%ctx, %status, %result)
{
   if (%status $= "ok")
   {
      $TNB::Cert = TNBCertRecord(%result);
      TNBCachePathSync();
   }

   %cb = $TNB::CertCb;
   $TNB::CertCb = "";

   if (%cb !$= "")
      call(%cb, $TNB::CertCbCtx, %status,
           (%status $= "ok" ? $TNB::Cert : %result));
}

// Render the identity into the record layout WONGetAuthInfo() has to answer
// with.
//
// The server sends this structured -- real booleans, a nested tribe list -- and
// this is where it becomes the tab-and-newline layout 45 shipped call sites
// read:
//
//     record 0    name TAB tag TAB append TAB guid
//     record 1    <tribe count>
//     record 2+n  name TAB tag TAB append TAB tribeId TAB rank TAB title
//
// Keeping the packed form here rather than on the wire is the point of a shim:
// it exists for exactly one reader, and it is built one line before that reader
// sees it.
function TNBCertRecord(%root)
{
   %out = TNBJsonStr(%root, "name") TAB
          TNBJsonStr(%root, "tag") TAB
          (TNBJsonBool(%root, "append") ? 1 : 0) TAB
          TNBJsonStr(%root, "guid");

   %tribes = TNBJsonGet(%root, "tribes");
   %n = TNBJsonCount(%tribes);
   %out = %out NL %n;

   for (%i = 0; %i < %n; %i++)
   {
      %t = TNBJsonIndex(%tribes, %i);
      %out = %out NL
             TNBJsonStr(%t, "name") TAB
             TNBJsonStr(%t, "tag") TAB
             (TNBJsonBool(%t, "append") ? 1 : 0) TAB
             TNBJsonStr(%t, "id") TAB
             TNBJsonStr(%t, "rank") TAB
             TNBJsonStr(%t, "title");
   }
   return %out;
}

// Point the mail cache at the account whose mail it holds.
//
// webemail.cs:15 computes $EmailCachePath at file scope --
//
//    $EmailCachePath = "webcache/" @ getField(getRecord(wonGetAuthInfo(),0),3) @ "/";
//
// -- which on a WON client ran after the login, because the login was the first
// thing the shell did. Here it runs during boot, with nobody logged in, and
// bakes to "webcache//".
//
// Leaving it to be repaired when the community certificate lands is too late,
// and the failure is worse than a stale cache: EmailGui reads and writes that
// global on every wake, and loadCache (webemail.cs:1229) is destructive -- it
// clears EM_Browser and EmailMessageVector and repopulates them only from the
// file it reads. Move the path after mail has been written under the old one
// and the next wake empties the inbox, with nothing due to poll again for five
// minutes. Observed: EmailGui.onWake() takes a two-message inbox to zero rows.
//
// Nothing had to wait for the certificate anyway. TNBSessionGuid reads the
// TribesNext account certificate directly (session.cs), so the GUID is known
// synchronously from the moment the player is logged in -- and it is the same
// GUID, because the backend issues a certificate for the account its session
// was negotiated with. Calling this again on arrival recomputes the same value.
//
// The session guid rather than WONGetAuthInfo's, because it is the identity
// every request is actually made as: it honours $TNB::GuidOverride, so a client
// driven against the mock caches under the account it is pretending to be
// rather than not at all.
function TNBCachePathSync()
{
   %guid = TNBSessionGuid();
   if (%guid $= "")
      %guid = getField(getRecord(WONGetAuthInfo(), 0), 3);
   if (%guid $= "")
      return;                    // nothing to compute it from yet

   $EmailCachePath = "webcache/" @ %guid @ "/";
}

// True once the identity is known. Every pane reads the warrior name out of the
// certificate on wake, so opening the browser before this is true renders a
// blank warrior and queries the empty string.
function TNBCertReady()
{
   return getField(getRecord($TNB::Cert, 0), 3) !$= "";
}

echo("TNBrowser: dbproxy.cs loaded");
