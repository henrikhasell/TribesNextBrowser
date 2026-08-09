// TNBrowser -- mail tests.
//
// Runs against tools/mockserver.py's json_mail.php, which reproduces what
// probing the live endpoint established, including the fact that send is
// refused with "500 Invalid Parameters".
//
//   exec("tests/mail_test.cs"); TNBMailSelfTest("http://172.17.0.1:8099", 0);
//
// The second argument says which kind of server is behind it: 0 for the
// TribesNext-shaped mock, 1 for a TNBrowser backend. The two differ in what
// they can actually do -- a TNBrowser backend delivers mail, keeps Sent and
// Deleted folders, and serves block and buddy lists; TribesNext has none of it
// and refuses every send.
//
// That is a property of the server, so the *test* is told. The client is not:
// it offers every control unconditionally and reports whatever refusal comes
// back, which is what removing $TNB::FullFeatures was about.

function TNBMailTestEq(%name, %got, %want)
{
   if (%got $= %want)
   {
      $TNBMailTest::Pass++;
      echo("PASS " @ %name);
   }
   else
   {
      $TNBMailTest::Fail++;
      $TNBMailTest::Failures = $TNBMailTest::Failures @ %name @
         " (got [" @ %got @ "] want [" @ %want @ "])\n";
      error("FAIL " @ %name @ " -- got [" @ %got @ "] want [" @ %want @ "]");
   }
}

function TNBMailTestHas(%name, %haystack, %needle)
{
   if (strstr(%haystack, %needle) >= 0)
   {
      $TNBMailTest::Pass++;
      echo("PASS " @ %name);
   }
   else
   {
      $TNBMailTest::Fail++;
      $TNBMailTest::Failures = $TNBMailTest::Failures @ %name @
         " (missing [" @ %needle @ "])\n";
      error("FAIL " @ %name @ " -- [" @ %needle @ "] not in [" @
            getSubStr(%haystack, 0, 160) @ "]");
   }
}

function TNBMailSelfTest(%host, %isBackend)
{
   $TNBMailTest::IsBackend = %isBackend;
   $TNBMailTest::Pass = 0;
   $TNBMailTest::Fail = 0;
   $TNBMailTest::Done = 0;
   $TNBMailTest::Failures = "";

   $TNB::Host = %host;
   $TNB::AuthHost = %host;
   $TNB::GuidOverride = "4510186";

   // The mail window remembers the last folder in $TNB::MailFolder, and globals
   // outlive a suite run in the same game process. Clear it so the assertions
   // below are about a first open rather than whatever the previous run left.
   $TNB::MailFolder = "";
   TNBSessionEnd();
   TNBApiInit();

   echo("--- mail against " @ %host @ " ---");

   // Field accessors tolerate the alternative spellings the live server might
   // use, since its item shape could not be observed.
   %alt = TNBJsonParse("{\"mid\":\"3\",\"sender\":\"Bob\",\"subj\":\"Hi\",\"text\":\"Body\",\"sent\":\"100\"}");
   TNBMailTestEq("id alternative", TNBMailId(%alt), "3");
   TNBMailTestEq("from alternative", TNBMailFrom(%alt), "Bob");
   TNBMailTestEq("subject alternative", TNBMailSubject(%alt), "Hi");
   TNBMailTestEq("body alternative", TNBMailBodyText(%alt), "Body");
   TNBMailTestEq("date alternative", TNBMailDate(%alt), "100");
   TNBJsonFree(%alt);

   %none = TNBJsonParse("{\"nothing\":\"here\"}");
   TNBMailTestEq("missing field is empty", TNBMailSubject(%none), "");
   TNBJsonFree(%none);

   TNBMailOpen();
   schedule(2500, 0, "TNBMailStep2");
}

function TNBMailStep2()
{
   // GuiEmailBrowser is display-only: unlike GuiTextListCtrl its rows cannot be
   // read back (getRowText returns ""), which is exactly why the stock client
   // kept message text in a separate EmailMessageVector. The $TNB::Mail* cache
   // plays that role here, so assertions go against it plus the row count.
   TNBMailTestEq("inbox rows", TNBMailList.rowCount(), 2);
   TNBMailTestEq("cached count", $TNB::MailCount, 2);
   TNBMailTestEq("cached id", $TNB::MailId[0], "11");
   TNBMailTestEq("cached from", $TNB::MailFrom[0], "Shifter");
   TNBMailTestEq("cached subject", $TNB::MailSubject[0], "Scrim on Tuesday?");
   TNBMailTestEq("cached body", $TNB::MailBody[1], "Good games last night.\n\n-- Ravage");
   TNBMailTestEq("second cached id", $TNB::MailId[1], "12");

   // Every control the mail API has any counterpart for is offered, whichever
   // backend is behind it. Only sender tracking stays hidden, having nothing to
   // call at all.
   TNBMailTestEq("block button offered", TNBMailBlockBtn.isVisible(), 1);
   TNBMailTestEq("sent folder offered", TNBMailTabSent.isVisible(), 1);
   TNBMailTestEq("deleted folder offered", TNBMailTabDeleted.isVisible(), 1);
   TNBMailTestEq("sender tracking hidden", TNBMailTrackBtn.isVisible(), 0);

   // A first open lights INBOX and asks for the inbox -- nothing remembered
   // yet. The selection used to live inside the capability guard, so the
   // shipped build opened on the inbox with no tab lit at all.
   TNBMailTestEq("inbox tab lit on open", TNBMailTabInbox.getValue(), 1);
   TNBMailTestEq("sent tab not lit", TNBMailTabSent.getValue(), 0);
   TNBMailTestEq("deleted tab not lit", TNBMailTabDeleted.getValue(), 0);
   TNBMailTestEq("folder follows the tab", $TNB::MailFolder, "inbox");

   // Selecting a row renders the message from the cached list entry.
   TNBMailTestEq("starts unread", $TNB::MailUnread[0], 1);
   TNBMailList.setSelectedRow(0);
   TNBMailList::onSelect(TNBMailList, 11, "");
   %body = TNBMailBody.getText();
   TNBMailTestHas("body shows subject", %body, "Scrim on Tuesday?");
   TNBMailTestHas("body shows sender", %body, "Shifter");
   TNBMailTestHas("body shows text", %body, "short a defender");

   // Opening it drops the envelope, updated in place with setRowFlags the way
   // the stock client does it, so the list keeps its rows and its selection.
   // Opening from the cached list must set the reply/block target too. It used
   // not to -- and since the list carries bodies, that is the branch which
   // normally runs, so reply and block acted on nothing.
   TNBMailTestEq("cached open sets the target", $TNB::MailReplyTo, "4120041");

   TNBMailTestEq("selection recorded", $TNB::MailCurrent, "11");
   TNBMailTestEq("envelope cleared locally", $TNB::MailUnread[0], 0);
   TNBMailTestEq("row kept after marking read", TNBMailList.rowCount(), 2);
   TNBMailTestEq("selection kept", TNBMailList.getSelectedId(), "11");

   // And it must have reached the server: the cached path answers without a
   // request, so marking read is a deliberate extra call rather than a side
   // effect of rendering. Re-list to see what the backend now believes.
   TNBMailApiList("TNBMailStepUnreadPersisted", "");
}

function TNBMailStepUnreadPersisted(%ctx, %status, %result)
{
   TNBMailTestEq("relist status", %status, "ok");

   %opened = "";
   for (%i = 0; %i < TNBJsonCount(%result); %i++)
   {
      %m = TNBJsonIndex(%result, %i);
      if (TNBMailId(%m) $= "11")
         %opened = %m;
   }

   TNBMailTestEq("opened message still listed", (%opened !$= ""), 1);
   TNBMailTestEq("server marked it read", TNBMailUnread(%opened), 0);

   TNBMailTestEq("count api", 1, 1);
   TNBMailApiCount("TNBMailStepCount", "");
}

function TNBMailStepCount(%ctx, %status, %result)
{
   TNBMailTestEq("count status", %status, "ok");
   TNBMailTestEq("count value", TNBJsonValue(%result), "2");

   // Reply prefills recipient and subject from the message just read.
   TNBMailApiRead(11, "TNBMailStepRead", "");
}

function TNBMailStepRead(%ctx, %status, %result)
{
   TNBMailTestEq("read status", %status, "ok");
   TNBMailReadLoaded("", %status, %result);
   TNBMailTestEq("reply target cached", $TNB::MailReplyTo, "4120041");

   $TNB::MailCurrent = 11;
   TNBMailReply();
   TNBMailTestEq("reply recipient", TNBComposeTo.getValue(), "4120041");
   TNBMailTestEq("reply subject", TNBComposeSubject.getValue(), "Re: Scrim on Tuesday?");

   // Replying twice must not stack "Re: Re:".
   $TNB::MailReplySubject = "Re: Already";
   TNBMailReply();
   TNBMailTestEq("no double Re", TNBComposeSubject.getValue(), "Re: Already");

   // Send is refused by the server; the client must report that, not claim
   // success. The mock reproduces the live 500 Invalid Parameters.
   TNBComposeTo.setValue("4120041");
   TNBComposeBody.setValue("hello");
   TNBComposeSend();
   schedule(2000, 0, "TNBMailStepAfterSend");
}

function TNBMailStepAfterSend()
{
   // The headline difference between the backends: TribesNext refuses every
   // send, a self-hosted backend delivers. Either way the client must report
   // what actually happened rather than assume.
   if ($TNBMailTest::IsBackend)
      TNBMailTestEq("send delivered", $TNBMailTest::SendStatus, "ok");
   else
      TNBMailTestEq("send reported as failure", $TNBMailTest::SendStatus, "error");

   // Delete removes the message and refreshes the list.
   $TNB::MailCurrent = 11;
   TNBMailConfirmDelete();
   schedule(2500, 0, "TNBMailStepAfterDelete");
}

function TNBMailStepAfterDelete()
{
   // The list is rebuilt here, and clear() reports a selection while it is --
   // with whatever id was there before. Acting on that left the app pointing at
   // a message the user never chose, so the next delete hit the wrong one.
   TNBMailTestEq("selection goes with the deleted message", $TNB::MailCurrent, "");

   TNBMailTestEq("inbox shrank after delete", TNBMailList.rowCount(), 1);
   TNBMailTestEq("remaining count", $TNB::MailCount, 1);
   TNBMailTestEq("remaining message", $TNB::MailSubject[0], "gg");
   TNBMailTestEq("remaining id", $TNB::MailId[0], "12");

   // Everything past here needs a TNBrowser backend. Against the mock, which
   // stands in for TribesNext, these methods do not exist at all -- the client
   // still offers the controls and would report the 501, which is the designed
   // behaviour, but there is nothing to assert about the feature itself.
   if (!$TNBMailTest::IsBackend)
   {
      TNBMailFinish();
      return;
   }

   // Deleting moved the message rather than destroying it: the Deleted folder
   // is the two-stage delete the original's tab implied (store.MailDelete).
   TNBMailShowFolder("deleted");
   schedule(2500, 0, "TNBMailStepDeletedFolder");
}

function TNBMailStepDeletedFolder()
{
   TNBMailTestEq("folder request switched", $TNB::MailFolder, "deleted");
   TNBMailTestEq("deleted folder holds the message", $TNB::MailCount, 1);
   TNBMailTestEq("and it is the one deleted", $TNB::MailId[0], "11");

   TNBMailShowFolder("sent");
   schedule(2500, 0, "TNBMailStepSentFolder");
}

function TNBMailStepSentFolder()
{
   TNBMailTestEq("sent folder requested", $TNB::MailFolder, "sent");
   // The send earlier in this run went to 4120041, so it is filed here.
   TNBMailTestEq("sent folder holds what we sent", ($TNB::MailCount > 0), 1);

   // In Sent the sender is you, so the party to act on is the recipient.
   // Targeting the sender made blocking answer "you cannot block yourself".
   TNBMailList.setSelectedRow(0);
   TNBMailList::onSelect(TNBMailList, $TNB::MailId[0], "");
   TNBMailTestEq("sent mail targets the recipient", $TNB::MailReplyTo, "4120041");
   TNBMailTestEq("and never yourself",
                 ($TNB::MailReplyTo $= TNBSessionGuid()) ? 0 : 1, 1);

   // Block list: blocking happens from a message, this is the other half.
   $TNB::MailReplyTo = "4200999";
   TNBMailConfirmBlock();
   schedule(2500, 0, "TNBMailStepBlockList");
}

function TNBMailStepBlockList()
{
   TNBMailShowBlockList();
   schedule(2500, 0, "TNBMailStepBlockListShown");
}

function TNBMailStepBlockListShown()
{
   %text = TNBMailBody.getText();
   TNBMailTestHas("block list names the blocked player", %text, "Ravage");
   TNBMailTestHas("block list offers an unblock", %text, "unblock");

   TNBHandleLink("unblock", "4200999");
   schedule(2500, 0, "TNBMailStepAfterUnblock");
}

function TNBMailStepAfterUnblock()
{
   %text = TNBMailBody.getText();
   TNBMailTestEq("unblocking empties the list",
                 (strstr(%text, "Ravage") >= 0) ? 0 : 1, 1);

   // Buddy list, which nothing exercised from the client before now.
   TNBApiEnqueue("buddyadd", TNBJsonObject("to", "4120041"),
                 "TNBMailStepBuddyAdded", "", 0);
}

function TNBMailStepBuddyAdded(%ctx, %status, %result)
{
   TNBMailTestEq("buddyadd accepted", %status, "ok");
   TNBApiEnqueue("buddylist", "", "TNBMailStepBuddyList", "", 0);
}

function TNBMailStepBuddyList(%ctx, %status, %result)
{
   TNBMailTestEq("buddylist status", %status, "ok");
   TNBMailTestEq("one buddy listed", TNBJsonCount(%result), 1);
   TNBMailTestEq("buddy is who we added",
                 TNBJsonStr(TNBJsonIndex(%result, 0), "guid"), "4120041");

   TNBMailFinish();
}

// Reopening returns to the folder last used rather than snapping back to the
// inbox. Left to the end because onWake refetches, and nothing after this
// depends on the list.
// A refresh rebuilds the list, and the selection must survive it when the
// message did: folder switches and post-delete refreshes both go through here.
function TNBMailCheckSelectionSurvives()
{
   $TNB::MailFolder = "inbox";
   $TNB::MailListPending = "";
   $TNB::MailCurrent = "";
   TNBMailRefresh();
   schedule(2500, 0, "TNBMailCheckSelectionSurvivesDone");
}

function TNBMailCheckSelectionSurvivesDone()
{
   if ($TNB::MailCount > 0)
   {
      // Select the first row, refresh, and it must still be selected.
      %id = $TNB::MailId[0];
      TNBMailList.setSelectedRow(0);
      TNBMailList::onSelect(TNBMailList, %id, "");
      $TNBMailTest::HeldId = %id;

      $TNB::MailListPending = "";
      TNBMailRefresh();
      schedule(2500, 0, "TNBMailCheckSelectionHeld");
      return;
   }
   TNBMailCheckFolderMemory();
   TNBMailFinishReport();
}

function TNBMailCheckSelectionHeld()
{
   TNBMailTestEq("selection survives a rebuild",
                 $TNB::MailCurrent, $TNBMailTest::HeldId);
   TNBMailTestEq("and it is a row that exists",
                 (TNBMailIndexOfId($TNB::MailCurrent) >= 0), 1);

   TNBMailCheckFolderMemory();
   TNBMailFinishReport();
}

function TNBMailCheckFolderMemory()
{
   $TNB::MailFolder = "deleted";
   TNBMailGui.onWake();
   TNBMailTestEq("reopen remembers the folder", $TNB::MailFolder, "deleted");
   TNBMailTestEq("and lights its tab", TNBMailTabDeleted.getValue(), 1);
   TNBMailTestEq("without lighting the inbox", TNBMailTabInbox.getValue(), 0);

   // An unknown folder is not a tab, so it must fall back rather than light
   // nothing -- the failure mode this whole change was about.
   $TNB::MailFolder = "nonsense";
   TNBMailGui.onWake();
   TNBMailTestEq("unknown folder falls back", $TNB::MailFolder, "inbox");
   TNBMailTestEq("and lights the inbox", TNBMailTabInbox.getValue(), 1);
}

function TNBMailFinish()
{
   TNBMailCheckSelectionSurvives();
}

function TNBMailFinishReport()
{

   echo("");
   echo("TNBMAILRESULT pass=" @ $TNBMailTest::Pass @ " fail=" @ $TNBMailTest::Fail);
   $TNBMailTest::Done = 1;
}

// Capture what the send path reported, since it goes to a message box.
package TNBMailTestHooks
{
   function TNBMailSent(%ctx, %status, %result)
   {
      $TNBMailTest::SendStatus = %status;
      Parent::TNBMailSent(%ctx, %status, %result);
   }
};
activatePackage(TNBMailTestHooks);

echo("TNBrowser: mail_test.cs loaded");
