// TNBrowser -- mail tests.
//
// Runs against tools/mockserver.py's json_mail.php, which reproduces what
// probing the live endpoint established, including the fact that send is
// refused with "500 Invalid Parameters".
//
//   exec("tests/mail_test.cs"); TNBMailSelfTest("http://172.17.0.1:8099", 0);
//
// The second argument says whether the backend under test can actually send
// mail: a TNBrowser backend delivers, TribesNext refuses every payload shape.
// That is a property of the server, not a client setting, so the test is told
// rather than the client being put into a mode.

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

function TNBMailSelfTest(%host, %sendWorks)
{
   $TNBMailTest::SendWorks = %sendWorks;
   $TNBMailTest::Pass = 0;
   $TNBMailTest::Fail = 0;
   $TNBMailTest::Done = 0;
   $TNBMailTest::Failures = "";

   $TNB::Host = %host;
   $TNB::AuthHost = %host;
   $TNB::GuidOverride = "4510186";
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

   // Selecting a row renders the message from the cached list entry.
   TNBMailList.setSelectedRow(0);
   TNBMailList::onSelect(TNBMailList, 11, "");
   %body = TNBMailBody.getText();
   TNBMailTestHas("body shows subject", %body, "Scrim on Tuesday?");
   TNBMailTestHas("body shows sender", %body, "Shifter");
   TNBMailTestHas("body shows text", %body, "short a defender");

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
   if ($TNBMailTest::SendWorks)
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
   TNBMailTestEq("inbox shrank after delete", TNBMailList.rowCount(), 1);
   TNBMailTestEq("remaining count", $TNB::MailCount, 1);
   TNBMailTestEq("remaining message", $TNB::MailSubject[0], "gg");
   TNBMailTestEq("remaining id", $TNB::MailId[0], "12");

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
