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

   // Start from an empty mailbox ON DISK as well as in memory.
   //
   // EmailGui::getCache reads webcache/<guid>/email1 on every wake. Setting
   // EmailGui.cacheFile does NOT skip that read -- it only takes the branch
   // that skips the GUID check (webemail.cs:1203) -- so the previous run's
   // messages come back, including the deleted-folder one step 3 pushes into
   // the vector and a later dumpCache persists. That made this suite poison its
   // own next run: green once, then three messages where two were expected.
   //
   // Truncating the file to a valid empty cache is the fix, and it has to be
   // the real path, since $EmailCachePath is derived from the certificate.
   TNBMailClearCache();

   Canvas.setContent(EmailGui);

   // The pane fetches on wake now (the mod's EmailGui::onWake), but only when
   // this setContent actually wakes it -- and a previous suite in the same
   // session may have left EmailGui as the content already, in which case
   // setContent does nothing at all. So the poll is still asked for explicitly,
   // and TNBMailFetch stands down if the wake got there first.
   schedule(1000, 0, "TNBMailFetch");
   schedule(4000, 0, "TNBMailStep1");
}

// A valid cache with no messages in it: the GUID the client will compare
// against, a count of zero, and nothing after. Both of getCache's branches then
// load nothing, whatever EmailGui.cacheFile happens to be left over as.
function TNBMailClearCache()
{
   %file = new FileObject();
   if (%file.openForWrite($EmailCachePath @ $EmailFileName))
   {
      %file.writeLine(getField(WONGetAuthInfo(), 3));
      %file.writeLine("0");
      %file.close();
   }
   %file.delete();

   EmailGui.cacheFile = "";
   EmailGui.messageCount = 0;
}

function TNBMailFetch()
{
   // Clearing the flag and firing regardless would issue a SECOND query with
   // the same $EmailNextSeq of 0 while the wake's is still out, and both
   // answers carry both messages -- four rows where step 1 expects two.
   if (EmailGui.checkingEmail)
      return;

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
   // Read the flag off the SECOND message, not the first. EM_Browser::onSelect
   // issues markMailRead (webemail.cs:1328) whenever a row is selected, so the
   // pane marks whatever it lands on as read just by showing it -- which makes
   // an assertion on row 0's flag a race against the control. The fixture's
   // second message is read to begin with and nothing selects it.
   TNBMailEq("record 2 is the read flag",
             getRecord(EmailMessageVector.getLineText(1), 2), 1);
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

   // The cache path, which is where a mailbox goes to die.
   //
   // webemail.cs:15 computes $EmailCachePath at file scope, during boot, with
   // nobody logged in -- so it bakes to "webcache//". If it is only corrected
   // when the community certificate lands, it MOVES while the pane is using it,
   // and EmailGui::onWake then reads a file that does not exist. onWake starts
   // by clearing EM_Browser and the message vector, so the inbox goes to zero
   // rows with nothing due to poll again for five minutes. That is the whole of
   // the "opens on EMAIL and no mail appears" report.
   $TNB::Cert = "";
   $EmailCachePath = "webcache//";
   TNBCachePathSync();
   TNBMailEq("the cache path is settled without a certificate",
             $EmailCachePath, "webcache/4510186/");

   schedule(500, 0, "TNBMailStep7");
}

// A poll that fails must not cost the session its mail.
//
// CheckEmail (webemail.cs:369) clears checkSchedule and sets checkingEmail
// before issuing the query, and only its success branches restore either. The
// error branch stops at a MessageBoxOK -- so without the mod's override,
// checkingEmail stays set, every later CheckEmail returns at its first line,
// and no timer is armed. Nothing polls again, ever, and GET MAIL takes the same
// early return.
function TNBMailStep7()
{
   $TNBMailTest::HostWas = $TNB::Host;
   $TNB::Host = "http://172.17.0.1:9";      // nothing listens here

   EmailGui.checkingEmail = "";
   EmailGui.checkSchedule = "";
   EmailGui.state = "";
   CheckEmail(false);

   schedule(4000, 0, "TNBMailStep8");
}

function TNBMailStep8()
{
   TNBMailEq("a failed poll reports failure", EmailGui.state, "error");
   TNBMailEq("a failed poll releases the in-progress flag",
             EmailGui.checkingEmail, 0);
   TNBMailEq("and arms another attempt",
             isEventPending(EmailGui.checkSchedule), 1);

   // Leave nothing armed: it would fire in the middle of another suite.
   if (isEventPending(EmailGui.checkSchedule))
      cancel(EmailGui.checkSchedule);
   EmailGui.checkSchedule = "";
   $TNB::Host = $TNBMailTest::HostWas;

   schedule(500, 0, "TNBMailStep9");
}

// Opening the pane fetches, whatever is in the cache.
//
// The shipped wake only fetches through rbInbox when the cache is empty
// (webemail.cs:995), so by this point -- two messages cached -- stock would do
// nothing and the mailbox would sit until the five-minute poll came round. The
// mod's EmailGui::onWake is what closes that.
//
// onWake() directly rather than Canvas.setContent: EmailGui is already the
// content, and setting the content to what it already is wakes nothing.
function TNBMailStep9()
{
   EmailGui.checkingEmail = "";
   EmailGui.checkSchedule = "";
   EmailGui.state = "";

   EmailGui.onWake();

   TNBMailEq("opening the mail pane fetches", EmailGui.checkingEmail, 1);

   schedule(3000, 0, "TNBMailStep10");
}

// Tribe membership changes are delivered by mail.
//
// The browser panes show no history, so the party who did not perform a change
// has no way to learn of it: a promoted or kicked member sees a tab that has
// silently changed or gone, and a tribe never hears that somebody left. The
// backends mail both directions, and this is the guard on that -- from the
// affected warrior's own mailbox, which is the only place it can be observed.
//
// Read through the row probe rather than the pane: EmailGui holds one mailbox
// at a time and these steps switch identity, which is also why $TNB::UUID is
// cleared each time -- the session travels with every request.
function TNBMailStep10()
{
   // That fetch armed the next poll on its way out; leave nothing behind.
   if (isEventPending(EmailGui.checkSchedule))
      cancel(EmailGui.checkSchedule);
   EmailGui.checkSchedule = "";

   // As the leader of Test Clan: promote Shifter, an officer of it.
   $TNBMailTest::LastStatus = "";
   DatabaseQuery(21, "Test Clan" TAB "Shifter" TAB "Senior Member" TAB 3,
                 TNBMailProbe, "promote");
   schedule(3000, 0, "TNBMailStep11");
}

function TNBMailStep11()
{
   TNBMailEq("promoting a member succeeds",
             getField($TNBMailTest::LastStatus, 0), 0);

   TNBMailReadAs("4120041");
   schedule(3000, 0, "TNBMailStep12");
}

function TNBMailStep12()
{
   TNBMailHas("a promoted member is told", $TNBMailTest::Raw,
              "Rank changed in Test Clan");
   TNBMailHas("and by whom, to what", $TNBMailTest::Raw,
              "orange01 has promoted you to Senior Member in Test Clan.");

   // Still as Shifter: leave the tribe, which its administrators hear about.
   $TNBMailTest::LastStatus = "";
   DatabaseQuery(24, "Test Clan", TNBMailProbe, "leave");
   schedule(3000, 0, "TNBMailStep13");
}

function TNBMailStep13()
{
   TNBMailEq("leaving a tribe succeeds",
             getField($TNBMailTest::LastStatus, 0), 0);

   TNBMailReadAs("4510186");
   schedule(3000, 0, "TNBMailStep14");
}

function TNBMailStep14()
{
   TNBMailHas("a tribe's administrators are told when a member leaves",
              $TNBMailTest::Raw, "Member left Test Clan");
   TNBMailHas("and who left", $TNBMailTest::Raw,
              "Shifter has left Test Clan.");

   $TNBMailTest::Done = 1;
   echo("TNBMAILRESULT pass=" @ $TNBMailTest::Pass @
        " fail=" @ $TNBMailTest::Fail);
}

// Read a mailbox as somebody else. The high-water mark is 0, so this is the
// whole inbox rather than what has arrived since the last poll.
function TNBMailReadAs(%guid)
{
   $TNB::GuidOverride = %guid;
   $TNB::UUID = "";

   $TNBMailTest::Raw = "";
   DatabaseQueryArray(1, 0, "0", TNBMailRowProbe, "rows");
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
   // And the whole row beside it, for the steps that read a subject (field 15)
   // as well as a body.
   $TNBMailTest::Raw = $TNBMailTest::Raw @ %row @ "\n";
}

echo("TNBrowser: mail_test.cs loaded");
