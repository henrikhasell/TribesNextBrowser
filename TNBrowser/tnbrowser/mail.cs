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

// Whether the backend serves the WON-era extras: Sent/Deleted folders, block
// lists, working send. TribesNext serves none of them, a self-hosted TNBrowser
// backend serves all of them, so the controls follow this rather than being
// hidden outright.
//
// Left off by default so pointing at TribesNext behaves exactly as before.
if ($TNB::FullFeatures $= "")
   $TNB::FullFeatures = 0;

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

// Show only what the backend behind us can actually do.
//
// Against TribesNext the mail API is count/read/delete/send-that-refuses, with
// no folders and no block list, so those controls stay hidden. Against a
// TNBrowser backend they all work and are shown.
function TNBMailHideUnsupported()
{
   %full = $TNB::FullFeatures;

   TNBMailBlockListBtn.setVisible(%full);
   TNBMailBlockBtn.setVisible(%full);
   TNBMailTabSent.setVisible(%full);
   TNBMailTabDeleted.setVisible(%full);

   // Forward and reply-all both need a working send, which only a TNBrowser
   // backend has.
   TNBMailForwardBtn.setVisible(%full);
   TNBMailReplyAllBtn.setVisible(%full);

   // Sender tracking was a WON buddy-list shortcut; the buddy list itself lives
   // on the player pane, so this button stays retired either way.
   TNBMailTrackBtn.setVisible(0);
   TNBMailTrackListBtn.setVisible(0);

   if (!%full)
      TNBMailTabInbox.setValue(1);
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
   TNBMailList.clear();
   $TNB::MailCount = 0;

   if (%status $= "error")
   {
      TNBMailSetBody("<just:center>\n\nCould not fetch mail.\n\n" @ %result);
      return;
   }

   %count = TNBJsonCount(%result);
   if (%count == 0)
   {
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

      %date = TNBMailDisplayDate($TNB::MailDate[%i]);

      // GuiEmailBrowser wants exactly four values after the row id, and they
      // land in the From, Subject and Received columns -- the leading Status
      // column is the envelope icon, which the control draws itself from the
      // fourth value. Passing three values adds no row at all; passing a
      // status string first shifts every column one place left.
      TNBMailList.addRow(%id,
                         ($TNB::MailFrom[%i] $= "" ? "(unknown)" : $TNB::MailFrom[%i]),
                         ($TNB::MailSubject[%i] $= "" ? "(no subject)" : $TNB::MailSubject[%i]),
                         %date,
                         (TNBMailUnread(%m) ? 1 : 0));
   }

   $TNB::MailCount = %count;
   TNBMailSetBody("<just:center>\n\n" @ %count @ " message" @
                  (%count == 1 ? "" : "s") @ " -- select one to read it.");
}

// Selecting a row shows the body. If the list entry already carried one there
// is nothing to fetch; otherwise read the message by id.
function TNBMailList::onSelect(%this, %id, %text)
{
   $TNB::MailCurrent = %id;

   %index = TNBMailIndexOfId(%id);
   if (%index >= 0 && $TNB::MailBody[%index] !$= "")
   {
      TNBMailShow($TNB::MailFrom[%index], $TNB::MailSubject[%index],
                  $TNB::MailDate[%index], $TNB::MailBody[%index]);
      return;
   }

   TNBMailSetBody("<just:center>\n\nLoading message...");
   TNBMailApiRead(%id, "TNBMailReadLoaded", %id);
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

   $TNB::MailReplyTo = TNBMailFromGuid(%m);
   $TNB::MailReplySubject = TNBMailSubject(%m);

   TNBMailShow(TNBMailFrom(%m), TNBMailSubject(%m), TNBMailDate(%m),
               TNBMailBodyText(%m));
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
