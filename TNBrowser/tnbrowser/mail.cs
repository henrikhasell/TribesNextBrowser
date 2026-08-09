// TNBrowser -- TribesNext mail (tmail)
//
// The mail API lives at /tn/json/json_mail.php and follows exactly the same
// conventions as the browser API: guid + uuid + method + JSON payload, JSON
// response. It is not covered by the forum post and its .phps source is not
// published, so the method set below was established empirically against the
// live server (unknown methods answer 501, valid ones 200):
//
//   count    -> "0"   the number of messages, as a JSON string
//   read     -> []    with no payload, the message list
//                     with {"id":N}, that single message
//   delete   -> []    {"id":N}
//   send     -> 500 "Invalid Parameters" for every payload shape tried
//
// Two consequences shape this file:
//
//   * Sending is disabled server-side. Around thirty payload and parameter
//     spellings were tried across both json_mail.php and the older
//     robot_mail.php (which answers INVALID_RECIP just as uniformly), with a
//     real authenticated session. The compose window is still wired up, and
//     reports whatever the server says rather than pretending to succeed --
//     the same treatment the beta-disabled "username" browser method gets.
//   * The message item fields could not be observed, because the account's
//     inbox is empty and no message can be created without send. So the list
//     and read paths accept any of the plausible field spellings instead of
//     hard-coding one, and fall back to showing the raw record. If a real
//     message ever arrives and uses different names, TNBMailField is the one
//     place to adjust.

if ($TNB::MailURI $= "")
   $TNB::MailURI = "/tn/json/json_mail.php";

//-----------------------------------------------------------------------------
// API
//
// Routed through the same queue as the browser methods, so mail and browser
// requests cannot overlap on the shared connection object.
//-----------------------------------------------------------------------------

function TNBMailApiCount(%cb, %ctx)
{
   TNBApiEnqueueOn($TNB::MailURI, "count", "", %cb, %ctx, 0);
}

function TNBMailApiList(%cb, %ctx)
{
   // The folder is only meaningful to a TNBrowser backend; TribesNext ignores
   // it, so sending it is harmless either way.
   %folder = ($TNB::MailFolder $= "" ? "inbox" : $TNB::MailFolder);
   TNBApiEnqueueOn($TNB::MailURI, "read",
                   TNBJsonObject("folder", %folder), %cb, %ctx, 0);
}

function TNBMailApiRead(%id, %cb, %ctx)
{
   TNBApiEnqueueOn($TNB::MailURI, "read", TNBJsonObject("id", %id), %cb, %ctx, 0);
}

function TNBMailApiDelete(%id, %cb, %ctx)
{
   TNBApiEnqueueOn($TNB::MailURI, "delete", TNBJsonObject("id", %id), %cb, %ctx, 0);
}

function TNBMailApiSend(%to, %subject, %body, %cb, %ctx)
{
   TNBApiEnqueueOn($TNB::MailURI, "send",
                   TNBJsonObject("to", %to, "subject", %subject, "body", %body),
                   %cb, %ctx, 1);
}

//-----------------------------------------------------------------------------
// Field access
//
// The server's field names for a message are unobserved, so every accessor
// tries the plausible spellings in turn. Cheap, and it means a wrong guess
// degrades to a blank column rather than a broken screen.
//-----------------------------------------------------------------------------

function TNBMailField(%node, %names)
{
   %count = getWordCount(%names);
   for (%i = 0; %i < %count; %i++)
   {
      %v = TNBJsonStr(%node, getWord(%names, %i));
      if (%v !$= "")
         return %v;
   }
   return "";
}

function TNBMailId(%node)      { return TNBMailField(%node, "id mid msgid message"); }
function TNBMailFrom(%node)    { return TNBMailField(%node, "from sender name fromname author"); }
function TNBMailFromGuid(%node){ return TNBMailField(%node, "fromguid senderguid guid from"); }
function TNBMailSubject(%node) { return TNBMailField(%node, "subject subj title"); }
function TNBMailBodyText(%node){ return TNBMailField(%node, "body text message content msg"); }
function TNBMailDate(%node)    { return TNBMailField(%node, "date time sent received timestamp"); }
function TNBMailUnread(%node)  { return TNBMailField(%node, "unread new status"); }
function TNBMailToGuid(%node)  { return TNBMailField(%node, "toguid recipientguid to"); }

// Who the *other* party is. In the inbox that is the sender; in Sent it is the
// recipient, because there the sender is you -- which is how blocking used to
// answer "you cannot block yourself" instead of blocking anyone.
function TNBMailOtherParty(%fromGuid, %toGuid)
{
   if (%fromGuid $= TNBSessionGuid() && %toGuid !$= "")
      return %toGuid;
   return %fromGuid;
}

// Render a date for display. Formatting happens here and nowhere else, so a
// value can never be formatted twice -- which matters because TNBJsonIsNumber
// accepts "-" and would happily treat an already-formatted "2026-07-25" as a
// number and mangle it. Only an all-digit string is treated as an epoch.
function TNBMailDisplayDate(%raw)
{
   if (%raw $= "")
      return "";

   for (%i = 0; %i < strlen(%raw); %i++)
   {
      %c = getSubStr(%raw, %i, 1);
      if (%c < "0" || %c > "9")
         return %raw;            // already formatted, or not a timestamp
   }
   return TNBFormatDate(%raw);
}

//-----------------------------------------------------------------------------
// Window
//-----------------------------------------------------------------------------

function TNBMailEnsureGuis()
{
   // Same deferred load as the browser: at autoexec time the shell profiles do
   // not exist yet and the controls would render unstyled.
   if (isObject(TNBMailGui))
      return;

   exec("tnbrowser/gui/TNBMailGui.gui");
   exec("tnbrowser/gui/TNBComposeDlg.gui");
}

function TNBMailOpen()
{
   TNBMailEnsureGuis();
   LaunchTabView.viewTab("EMAIL", TNBMailGui, 0);
}

function TNBMailGui::onWake(%this)
{
   Canvas.pushDialog(LaunchToolbarDlg);
   TNBMailHideUnsupported();

   // Open on whichever folder was last used, the inbox the first time. Say it
   // in both places -- the folder the next request asks for, and the tab the
   // user sees lit -- because TNBMailApiList falls back to "inbox" on an empty
   // folder, so leaving the tab implicit still showed the right messages under
   // no tab at all, which reads as a broken screen.
   //
   // $TNB::MailFolder is the memory as well as the request argument, so the two
   // cannot drift apart. Anything that is not a folder we know falls back
   // rather than lighting nothing.
   //
   // setValue does not fire the control's command on this build (measured), so
   // this lights the tab without also triggering TNBMailShowFolder and a second
   // list request.
   %folder = $TNB::MailFolder;
   if (%folder !$= "sent" && %folder !$= "deleted")
      %folder = "inbox";
   $TNB::MailFolder = %folder;

   TNBMailTabInbox.setValue(%folder $= "inbox");
   TNBMailTabSent.setValue(%folder $= "sent");
   TNBMailTabDeleted.setValue(%folder $= "deleted");

   TNBMailList.ClearColumns();
   TNBMailList.addColumn(0, "", 24, 24, 24, "center");
   TNBMailList.addColumn(1, "From", 120, 60, 220);
   TNBMailList.addColumn(2, "Subject", 200, 80, 320);
   TNBMailList.addColumn(3, "Received", 90, 60, 160);

   TNBMailRefresh();
}

function TNBMailGui::onSleep(%this)
{
   Canvas.popDialog(LaunchToolbarDlg);
}

// The launch shell calls these on whatever GUI it hosts; without setKey it
// aborts with "Unknown command setKey" and the window never appears.
function TNBMailGui::setKey(%this, %key) { %this.key = %key; }
function TNBMailGui::onClose(%this, %key) { }
function TNBMailGui::connectionTerminated(%this, %key) { }

// Hide only what has no counterpart at all.
//
// Everything else is offered unconditionally. The backend is a TNBrowser
// backend -- TribesNext is contacted for the login and nothing else -- so there
// is no capability left to switch on, and a mode flag that has to be set
// correctly for the UI to be honest is a poor way to express something the
// server already answers for itself. A method a backend cannot serve reports
// its own refusal, which is how this client treats every other method.
function TNBMailHideUnsupported()
{
   // Set both ways, not just the hiding. GUI objects outlive a single open --
   // they are created once and reused -- so a control this function does not
   // explicitly show stays however it was last left, and anything hidden
   // earlier in the session would never come back.
   TNBMailBlockListBtn.setVisible(1);
   TNBMailBlockBtn.setVisible(1);
   TNBMailForwardBtn.setVisible(1);
   TNBMailReplyAllBtn.setVisible(1);
   TNBMailTabSent.setVisible(1);
   TNBMailTabDeleted.setVisible(1);

   // Sender tracking was a WON buddy-list shortcut. The buddy list itself lives
   // on the player pane, so these two have nothing to call.
   TNBMailTrackBtn.setVisible(0);
   TNBMailTrackListBtn.setVisible(0);
}

// Folder tabs. INBOX/SENT/DELETED map to the folder argument the backend takes.
function TNBMailShowFolder(%folder)
{
   $TNB::MailFolder = %folder;
   TNBMailRefresh();
}

function TNBMailBlockSender()
{
   if ($TNB::MailReplyTo $= "")
   {
      MessageBoxOK("EMAIL", "Select a message first.");
      return;
   }
   if ($TNB::MailReplyTo $= TNBSessionGuid())
   {
      MessageBoxOK("EMAIL", "That message is between you and yourself.");
      return;
   }
   MessageBoxYesNo("BLOCK SENDER",
      "Block mail from this player?", "TNBMailConfirmBlock();", "");
}

function TNBMailConfirmBlock()
{
   TNBApiEnqueue("blockadd", TNBJsonObject("to", $TNB::MailReplyTo),
                 "TNBMailAfterBlock", "", 0);
}

function TNBMailAfterBlock(%ctx, %status, %result)
{
   if (%status $= "error")
   {
      MessageBoxOK("EMAIL", %result);
      return;
   }
   MessageBoxOK("EMAIL", "Blocked. Their mail will no longer arrive.");
}

// The block list, rendered into the message pane rather than a dialog of its
// own -- the stock BLOCK LIST button opened a WON-backed list this mod has no
// .gui for, and the pane is already a GuiMLTextCtrl, which the TNBrowserLinks
// package makes clickable everywhere.
//
// Blocking itself happens from a message (TNBMailBlockSender). This is the
// other half: seeing who is blocked, and undoing it.
function TNBMailShowBlockList()
{
   TNBMailSetBody("<just:center>\n\nLoading your block list...");
   TNBApiEnqueue("blocklist", "", "TNBMailBlockListLoaded", "", 0);
}

function TNBMailBlockListLoaded(%ctx, %status, %result)
{
   if (%status $= "error")
   {
      TNBMailSetBody("<just:center>\n\nCould not load your block list.\n\n" @ %result);
      return;
   }

   %count = TNBJsonCount(%result);
   %text = "<font:Univers Bold:16>Blocked players<font:Univers:14>\n\n";
   if (%count == 0)
      %text = %text @ "<spush><color:808080>You have not blocked anyone.<spop>\n";

   for (%i = 0; %i < %count; %i++)
   {
      %b = TNBJsonIndex(%result, %i);
      %guid = TNBJsonStr(%b, "guid");
      %text = %text @ "  " @
              TNBTaggedName(TNBJsonStr(%b, "name"), TNBJsonStr(%b, "tag"),
                            TNBJsonBool(%b, "append")) @
              "   <a:tnb\tunblock\t" @ %guid @ ">[ unblock ]</a>\n";
   }

   %text = %text @ "\nBlocked players cannot send you mail.";
   TNBMailSetBody(%text);
}

function TNBMailAfterUnblock(%ctx, %status, %result)
{
   if (%status $= "error")
   {
      MessageBoxOK("EMAIL", %result);
      return;
   }
   TNBMailShowBlockList();
}

function TNBMailUnsupported()
{
   MessageBoxOK("EMAIL",
      "The TribesNext mail API has no equivalent for that feature.");
}

//-----------------------------------------------------------------------------
// Inbox
//-----------------------------------------------------------------------------

function TNBMailRefresh()
{
   // Opening the window wakes it twice -- the launch bar adds the tab, which
   // selects it, and then selects it again -- and each wake refreshes. Collapse
   // that into one request rather than paying a round trip twice on every open.
   // Keyed on the folder so switching tabs while one is in flight still fetches.
   %folder = ($TNB::MailFolder $= "" ? "inbox" : $TNB::MailFolder);
   if ($TNB::MailListPending $= %folder)
      return;
   $TNB::MailListPending = %folder;

   TNBMailSetBody("<just:center>\n\nChecking for mail...");
   TNBMailApiList("TNBMailListLoaded", "");
}

function TNBMailSetBody(%text)
{
   TNBMailBody.setText(%text);
   TNBMailBodyScroll.scrollToTop();
}

function TNBMailListLoaded(%ctx, %status, %result)
{
   // Cleared first: this runs for failures too, and a stuck flag would block
   // every later refresh of that folder.
   $TNB::MailListPending = "";

   %wasSelected = $TNB::MailCurrent;

   $TNB::MailRebuilding = 1;
   TNBMailList.clear();
   $TNB::MailCount = 0;

   if (%status $= "error")
   {
      $TNB::MailRebuilding = "";
      $TNB::MailCurrent = "";
      TNBMailSetBody("<just:center>\n\nCould not fetch mail.\n\n" @ %result);
      return;
   }

   %count = TNBJsonCount(%result);
   if (%count == 0)
   {
      $TNB::MailRebuilding = "";
      $TNB::MailCurrent = "";
      TNBMailSetBody("<just:center>\n\nYour inbox is empty.");
      return;
   }

   for (%i = 0; %i < %count; %i++)
   {
      %m = TNBJsonIndex(%result, %i);

      // The list may be an array of ids rather than of objects; cope with both.
      if (TNBJsonType(%m) !$= "object")
         %id = TNBJsonValue(%m);
      else
         %id = TNBMailId(%m);
      if (%id $= "")
         %id = %i;

      $TNB::MailId[%i] = %id;
      $TNB::MailFrom[%i] = TNBMailFrom(%m);
      $TNB::MailSubject[%i] = TNBMailSubject(%m);
      $TNB::MailBody[%i] = TNBMailBodyText(%m);
      $TNB::MailDate[%i] = TNBMailDate(%m);
      $TNB::MailUnread[%i] = (TNBMailUnread(%m) ? 1 : 0);
      $TNB::MailFromGuid[%i] = TNBMailFromGuid(%m);
      $TNB::MailToGuid[%i] = TNBMailToGuid(%m);

      TNBMailAddRow(%i);
   }

   $TNB::MailCount = %count;

   // Keep the selection across a refresh when the message survived it -- the
   // list is rebuilt on every folder switch and after every delete, and losing
   // the selection each time means the next action reports "select a message
   // first" for a row still highlighted on screen.
   %row = TNBMailIndexOfId(%wasSelected);
   if (%wasSelected !$= "" && %row >= 0)
      TNBMailList.setSelectedRow(%row);
   else
      $TNB::MailCurrent = "";

   $TNB::MailRebuilding = "";

   TNBMailSetBody("<just:center>\n\n" @ %count @ " message" @
                  (%count == 1 ? "" : "s") @ " -- select one to read it.");
}

// One row, from the cached arrays.
function TNBMailAddRow(%i)
{
   // GuiEmailBrowser wants exactly four values after the row id, and they land
   // in the From, Subject and Received columns -- the leading Status column is
   // the envelope icon, which the control draws itself from the fourth value.
   // Passing three values adds no row at all; passing a status string first
   // shifts every column one place left.
   //
   // That fourth value is the READ flag, not an unread flag: stock
   // webemail.cs stores it as record 2 and does `if (!getRecord(%text, 2))` to
   // mean "not yet read", then sets it to 1 on opening. Sending `unread` there
   // draws every envelope backwards -- which is exactly what this used to do.
   TNBMailList.addRow($TNB::MailId[%i],
                      ($TNB::MailFrom[%i] $= "" ? "(unknown)" : $TNB::MailFrom[%i]),
                      ($TNB::MailSubject[%i] $= "" ? "(no subject)" : $TNB::MailSubject[%i]),
                      TNBMailDisplayDate($TNB::MailDate[%i]),
                      ($TNB::MailUnread[%i] ? 0 : 1));
}

// Selecting a row shows the body. If the list entry already carried one there is
// nothing to fetch, so it renders from the cache; otherwise read by id.
function TNBMailList::onSelect(%this, %id, %text)
{
   // GuiEmailBrowser fires this while the list is being rebuilt -- clear() on a
   // populated list reports a selection, with whatever id happened to be there
   // before. Acting on that ran the whole open-a-message path against a stale
   // row: after deleting a message, $TNB::MailCurrent came back pointing at a
   // different one, so the next delete acted on a message the user never chose,
   // and an emptied list left it pointing at nothing.
   if ($TNB::MailRebuilding)
      return;

   $TNB::MailCurrent = %id;

   %index = TNBMailIndexOfId(%id);
   if (%index >= 0 && $TNB::MailBody[%index] !$= "")
   {
      // Set the same state the by-id path sets. Without this, reply and block
      // acted on whatever was left from a previous message -- and since the
      // list carries bodies, this branch is the one that normally runs, so they
      // acted on nothing at all.
      $TNB::MailReplyTo = TNBMailOtherParty($TNB::MailFromGuid[%index],
                                            $TNB::MailToGuid[%index]);
      $TNB::MailReplySubject = $TNB::MailSubject[%index];

      TNBMailShow($TNB::MailFrom[%index], $TNB::MailSubject[%index],
                  $TNB::MailDate[%index], $TNB::MailBody[%index]);
      TNBMailMarkRead(%index, %id);
      return;
   }

   TNBMailSetBody("<just:center>\n\nLoading message...");
   TNBMailApiRead(%id, "TNBMailReadLoaded", %id);
}

// Clearing the unread flag is a side effect of reading a message *by id*: that
// is the only call the backend treats as "opened" (json_mail read with an id;
// store.MailRead then does UPDATE mail SET unread = FALSE).
//
// The list arrives with bodies attached, so the cached path above answers
// without a request and nothing would ever be marked read -- the envelope
// stayed shut no matter how many times you opened a message. So send the read
// for its side effect, and drop the envelope locally rather than refetching the
// list to learn what we already know.
function TNBMailMarkRead(%index, %id)
{
   if (!$TNB::MailUnread[%index])
      return;

   $TNB::MailUnread[%index] = 0;

   // setRowFlags(id, 1) is how the stock client updates the envelope without
   // rebuilding the list, and it takes the same read flag addRow does.
   TNBMailList.setRowFlags(%id, 1);

   TNBMailApiRead(%id, "TNBMailMarkedRead", %id);
}

function TNBMailMarkedRead(%ctx, %status, %result)
{
   // Nothing to display -- the body is already on screen from the cache. A
   // failure is worth a console line but not a dialog: the envelope simply
   // comes back on the next refresh, which is the truth of it.
   if (%status $= "error")
      error("TNBrowser: could not mark message " @ %ctx @ " read -- " @ %result);
}

function TNBMailIndexOfId(%id)
{
   for (%i = 0; %i < $TNB::MailCount; %i++)
   {
      if ($TNB::MailId[%i] $= %id)
         return %i;
   }
   return -1;
}

function TNBMailReadLoaded(%id, %status, %result)
{
   if (%status $= "error")
   {
      TNBMailSetBody("<just:center>\n\nCould not read that message.\n\n" @ %result);
      return;
   }

   // read may answer the message directly or wrapped in a single-element array.
   %m = %result;
   if (TNBJsonType(%result) $= "array")
   {
      if (TNBJsonCount(%result) == 0)
      {
         TNBMailSetBody("<just:center>\n\nThat message is no longer available.");
         return;
      }
      %m = TNBJsonIndex(%result, 0);
   }

   $TNB::MailReplyTo = TNBMailOtherParty(TNBMailFromGuid(%m), TNBMailToGuid(%m));
   $TNB::MailReplySubject = TNBMailSubject(%m);

   TNBMailShow(TNBMailFrom(%m), TNBMailSubject(%m), TNBMailDate(%m),
               TNBMailBodyText(%m));

   // This request marked it read server-side, so the row is stale. The reply
   // still reports the message as it was when opened, which is why the local
   // flag is cleared here rather than read back from %m.
   %index = TNBMailIndexOfId(%id);
   if (%index >= 0 && $TNB::MailUnread[%index])
   {
      $TNB::MailUnread[%index] = 0;
      TNBMailList.setRowFlags(%id, 1);
   }
}

function TNBMailShow(%from, %subject, %date, %body)
{
   %text = "<font:Univers Bold:16>" @
           (%subject $= "" ? "(no subject)" : %subject) @ "<font:Univers:14>\n";
   %text = %text @ "From: " @ (%from $= "" ? "(unknown)" : %from);
   %date = TNBMailDisplayDate(%date);
   if (%date !$= "")
      %text = %text @ "   |   " @ %date;
   %text = %text @ "\n\n";
   %text = %text @ (%body $= "" ? "<spush><color:808080>(empty message)<spop>" : %body);

   TNBMailSetBody(%text);
}

//-----------------------------------------------------------------------------
// Delete
//-----------------------------------------------------------------------------

function TNBMailDelete()
{
   if ($TNB::MailCurrent $= "")
   {
      MessageBoxOK("EMAIL", "Select a message first.");
      return;
   }
   MessageBoxYesNo("DELETE MESSAGE", "Delete this message?",
                   "TNBMailConfirmDelete();", "");
}

function TNBMailConfirmDelete()
{
   TNBMailApiDelete($TNB::MailCurrent, "TNBMailDeleted", "");
}

function TNBMailDeleted(%ctx, %status, %result)
{
   if (%status $= "error")
   {
      MessageBoxOK("EMAIL", %result);
      return;
   }
   $TNB::MailCurrent = "";
   TNBMailRefresh();
}

//-----------------------------------------------------------------------------
// Compose
//
// Wired up in full even though the server currently refuses every send, so the
// screen is complete and the failure is reported honestly.
//-----------------------------------------------------------------------------

function TNBMailCompose()
{
   TNBMailEnsureGuis();
   Canvas.pushDialog(TNBComposeDlg);
   TNBComposeTo.setValue("");
   TNBComposeCC.setValue("");
   TNBComposeSubject.setValue("");
   TNBComposeBody.setValue("");
   $TNB::ComposeSubject = "";
}

function TNBMailReply()
{
   if ($TNB::MailCurrent $= "")
   {
      MessageBoxOK("EMAIL", "Select a message to reply to.");
      return;
   }

   TNBMailCompose();

   %subject = $TNB::MailReplySubject;
   if (%subject !$= "" && getSubStr(%subject, 0, 4) !$= "Re: ")
      %subject = "Re: " @ %subject;

   TNBComposeTo.setValue($TNB::MailReplyTo);
   TNBComposeSubject.setValue(%subject);
   $TNB::ComposeSubject = %subject;
}

// Forwarding needs a working send, so it shares the compose path and simply
// prefills the body with the message being forwarded.
function TNBMailForward()
{
   %index = TNBMailIndexOfId($TNB::MailCurrent);
   TNBMailCompose();
   if (%index >= 0)
   {
      TNBComposeSubject.setValue("Fwd: " @ $TNB::MailSubject[%index]);
      TNBComposeBody.setValue("\n\n--- Forwarded ---\n" @ $TNB::MailBody[%index]);
   }
}

// The stock dialog's TO:/CC: buttons opened an address book. There is no
// address-book API, so reuse the player search instead.
function TNBComposePick()
{
   TNBSearchOpen("mailto");
}

function TNBComposeSend()
{
   %to = trim(TNBComposeTo.getValue());
   %subject = trim(TNBComposeSubject.getValue());
   %body = TNBComposeBody.getValue();

   if (%to $= "")
   {
      MessageBoxOK("EMAIL", "Enter a recipient.");
      return;
   }

   Canvas.popDialog(TNBComposeDlg);
   TNBMailApiSend(%to, %subject, %body, "TNBMailSent", "");
}

function TNBMailSent(%ctx, %status, %result)
{
   if (%status $= "error")
   {
      // Expected today: the server refuses every send. Report what it said
      // rather than claiming the message went out.
      MessageBoxOK("EMAIL",
         "The community server would not send that message.\n\n" @ %result);
      return;
   }
   MessageBoxOK("EMAIL", "Message sent.");
   TNBMailRefresh();
}

echo("TNBrowser: mail.cs loaded");
