// TNBrowser -- the Email pane, driven through its own controls.
//
//   exec("tests/mail_test.cs"); TNBMailSelfTest("http://172.17.0.1:8099");
//
// Every object named below is the shipped one, out of gui/EmailGui.gui. This
// mod ships no .gui file, so an assertion that has to change to accommodate a
// screen means the screen is wrong.
//
// EM_Browser is display-only -- GuiEmailBrowser::getRowText returns "" -- which
// is why the shipped client keeps its text in a separate EmailMessageVector,
// and why the assertions below read that instead. Only the row COUNT can be
// read back off the control itself.

function TNBMailEq(%name, %got, %want)
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

function TNBMailHas(%name, %haystack, %needle)
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
            getSubStr(%haystack, 0, 300) @ "]");
   }
}

function TNBMailSelfTest(%host)
{
   $TNBMailTest::Pass = 0;
   $TNBMailTest::Fail = 0;
   $TNBMailTest::Done = 0;
   $TNBMailTest::Failures = "";
   $TNBMailTest::Rows = "";

   $TNB::Host = %host;
   $TNB::AuthHost = %host;
   $TNB::Cert = "";

   // Step 5 switches identity to read the invited warrior's mailbox, so start
   // from no session rather than whatever guid the last run finished on.
   $TNB::GuidOverride = "4510186";
   $TNB::UUID = "";

   // Never at file scope: that is global scope, where `new` takes the engine
   // down rather than failing.
   if (!isObject(TNBMailProbe))
      new ScriptObject(TNBMailProbe) { class = TNBMailProbe; };
   if (!isObject(TNBMailRowProbe))
      new ScriptObject(TNBMailRowProbe) { class = TNBMailRowProbe; };

   TNBCertRefresh("TNBMailCertLoaded", "");
}

function TNBMailCertLoaded(%ctx, %status, %result)
{
   TNBMailEq("the certificate arrives", %status, "ok");

   // Start from an empty mailbox and a zero high-water mark, so the first poll
   // is a first poll rather than whatever a previous suite left behind.
   EmailMessageVector.clear();
   EM_Browser.clear();
   $EmailNextSeq = 0;
   EmailGui.cacheLoaded = true;
   EmailGui.checkingEmail = "";
   EmailGui.soundPlayed = false;

   Canvas.setContent(EmailGui);

   // onWake's own fetch is guarded by EmailGui.checkingEmail and a pending
   // schedule, both of which survive a previous suite in the same session, so
   // the poll is asked for explicitly -- the same call the GET MAIL button
   // makes.
   schedule(1000, 0, "TNBMailFetch");
   schedule(4000, 0, "TNBMailStep1");
}

function TNBMailFetch()
{
   EmailGui.checkingEmail = "";
   EmailGui.btnClicked = true;
   CheckEmail(false);
}

function TNBMailStep1()
{
   TNBMailEq("the shipped mail window is the content",
             Canvas.getContent().getName(), "EmailGui");
   TNBMailEq("both inbox messages arrived", EM_Browser.rowCount(), 2);
   TNBMailEq("and reached the message vector",
             EmailMessageVector.getNumLines(), 2);

   // The client folds a row into a record-separated message (webemail.cs:1149)
   // and EmailMessageAddRow then feeds records 1, 6, 3 and 2 to
   // GuiEmailBrowser::addRow. Reading those back is what pins the field
   // indices of the row itself.
   %msg = EmailMessageVector.getLineText(0);
   TNBMailEq("record 0 is the message id", getRecord(%msg, 0), 11);
   TNBMailEq("record 1 is the sender quad, name first",
             getField(getRecord(%msg, 1), 0), "Shifter");
   TNBMailEq("the sender quad carries the GUID in field 3",
             getField(getRecord(%msg, 1), 3), "4120041");
   TNBMailEq("record 2 is the read flag", getRecord(%msg, 2), 0);
   TNBMailEq("record 3 is the received date", getRecord(%msg, 3), "2026-07-25");
   TNBMailEq("record 6 is the subject", getRecord(%msg, 6), "Scrim on Tuesday?");
   TNBMailHas("records 7.. are the body", EmailGetBody(%msg),
              "We are short a defender");

   // The high-water mark. The pane sends the highest id it has cached, and a
   // server that ignores it hands the same messages back on every poll -- the
   // inbox then grows without bound, which is how the argument's meaning was
   // established in the first place.
   TNBMailEq("the poll advanced the sequence", $EmailNextSeq, 12);
   EmailGui.checkingEmail = "";
   CheckEmail(false);
   schedule(3000, 0, "TNBMailStep2");
}

function TNBMailStep2()
{
   TNBMailEq("a second poll adds nothing", EM_Browser.rowCount(), 2);

   // The deleted folder: a different ordinal with the same row schema, and the
   // branch that fills the vector directly rather than through the arrival
   // path.
   EmailGui.key = LaunchGui.key++;
   EmailGui.state = "DeletedMail";
   EmailMessageVector.clear();
   DatabaseQueryArray(14, 0, "DeletedMail", EmailGui, EmailGui.key);
   schedule(3000, 0, "TNBMailStep3");
}

function TNBMailStep3()
{
   TNBMailEq("the deleted folder has its own message",
             EmailMessageVector.getNumLines(), 1);
   TNBMailEq("a deleted message keeps its subject",
             getRecord(EmailMessageVector.getLineText(0), 6), "Old news");

   // The block list. getTextName consumes fields 0..4 but reads only four of
   // them; field 4 is then re-read as the count of mail the block has actually
   // turned away, which is the column the stock dialog shows.
   DatabaseQueryArray(2, 0, "", TNBMailProbe, "blocks");
   schedule(3000, 0, "TNBMailStep4");
}

function TNBMailStep4()
{
   TNBMailHas("a block row names the blocked warrior",
              $TNBMailTest::LastRow, "Ravage");
   TNBMailEq("field 4 of a block row is the hit count",
             getField($TNBMailTest::LastRow, 4), 3);

   // An invitation is delivered BY MAIL, because the client has no query that
   // lists a player's own invitations. The link in that mail is therefore the
   // only way an invitation can ever be answered.
   DatabaseQuery(27, "Test Clan" TAB "Ravage", TNBMailProbe, "invite");
   schedule(3000, 0, "TNBMailStep5");
}

function TNBMailStep5()
{
   TNBMailEq("inviting a warrior succeeds",
             getField($TNBMailTest::LastStatus, 0), 0);

   // Read it back as the INVITED warrior, because that is whose mailbox it
   // landed in -- and re-negotiate the session, since the guid travels with
   // every request.
   $TNB::GuidOverride = "4200999";
   $TNB::UUID = "";

   // Look at the body the way the pane does. The server wrote newlines; the
   // row split them into fields; getFields(%row,17) rejoins them TAB-separated
   // into one record; EmailGetBody prints that verbatim, and
   // GuiMLTextCtrl::onURL splits the URL on TAB. So a newline on the server is
   // the TAB the link needs here.
   $TNBMailTest::Rows = "";
   DatabaseQueryArray(1, 0, "0", TNBMailRowProbe, "rows");
   schedule(3000, 0, "TNBMailStep6");
}

function TNBMailStep6()
{
   TNBMailHas("an accept link survives the round trip",
              $TNBMailTest::Rows, "<a:acceptinvite");
   TNBMailHas("the tribe is the link's first argument",
              $TNBMailTest::Rows, "acceptinvite" TAB "Test Clan");
   TNBMailHas("the warrior is its second",
              $TNBMailTest::Rows, "Test Clan" TAB "Ravage>");
   TNBMailHas("a reject link is offered beside it",
              $TNBMailTest::Rows, "<a:rejectinvite");

   $TNB::GuidOverride = "4510186";
   $TNB::UUID = "";

   $TNBMailTest::Done = 1;
   echo("TNBMAILRESULT pass=" @ $TNBMailTest::Pass @
        " fail=" @ $TNBMailTest::Fail);
}

//-----------------------------------------------------------------------------
// Probes, for the ordinals whose shipped consumer is a dialog this suite does
// not open.
//-----------------------------------------------------------------------------

function TNBMailProbe::onDatabaseQueryResult(%this, %status, %result, %key)
{
   $TNBMailTest::LastStatus = %status;
}

function TNBMailProbe::onDatabaseRow(%this, %row, %isLast, %key)
{
   $TNBMailTest::LastRow = %row;
}

function TNBMailRowProbe::onDatabaseQueryResult(%this, %status, %result, %key)
{
}

function TNBMailRowProbe::onDatabaseRow(%this, %row, %isLast, %key)
{
   // Exactly what webemail.cs:1147 does.
   $TNBMailTest::Rows = $TNBMailTest::Rows @ getFields(%row, 17) @ "\n";
}

echo("TNBrowser: mail_test.cs loaded");
