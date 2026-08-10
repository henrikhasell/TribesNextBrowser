// TNBrowser -- the Tribe & Warrior Browser, driven through its own controls.
//
//   exec("tests/browser_test.cs"); TNBBrowserSelfTest("http://172.17.0.1:8099");
//
// Where sweep_test.cs proves the framing, this proves the field indices. A row
// whose fields are in the wrong order sweeps perfectly and renders as a
// plausible pane with the wrong data in it, so the only way to catch one is to
// let the shipped parser read it and then look at what ended up in the control.
//
// Every object named below is the shipped one, out of
// gui/TribeAndWarriorBrowserGui.gui. This mod ships no .gui file at all -- that
// is the point of it -- so if any of these assertions has to change to
// accommodate a screen, the screen is wrong.
//
// A step machine rather than a straight line, because every load is
// asynchronous: each step schedules the next and $TNBBrowserTest::Done marks
// the end so a runner can wait on it.

function TNBBrowserEq(%name, %got, %want)
{
   if (%got $= %want)
   {
      $TNBBrowserTest::Pass++;
      echo("PASS " @ %name);
   }
   else
   {
      $TNBBrowserTest::Fail++;
      $TNBBrowserTest::Failures = $TNBBrowserTest::Failures @ %name @
         " (got [" @ %got @ "] want [" @ %want @ "])\n";
      error("FAIL " @ %name @ " -- got [" @ %got @ "] want [" @ %want @ "]");
   }
}

function TNBBrowserHas(%name, %haystack, %needle)
{
   if (strstr(%haystack, %needle) >= 0)
   {
      $TNBBrowserTest::Pass++;
      echo("PASS " @ %name);
   }
   else
   {
      $TNBBrowserTest::Fail++;
      $TNBBrowserTest::Failures = $TNBBrowserTest::Failures @ %name @
         " (missing [" @ %needle @ "])\n";
      error("FAIL " @ %name @ " -- [" @ %needle @ "] not in [" @
            getSubStr(%haystack, 0, 200) @ "]");
   }
}

// The certificate record for one tribe, by name. Record 1 is the count and the
// tribe records follow it (webbrowser.cs:101, :1909-1926).
function TNBCertTribe(%name)
{
   %count = getRecord($TNB::Cert, 1);
   for (%i = 0; %i < %count; %i++)
   {
      %rec = getRecord($TNB::Cert, %i + 2);
      if (getField(%rec, 0) $= %name)
         return %rec;
   }
   return "";
}

function TNBBrowserSelfTest(%host)
{
   $TNBBrowserTest::Pass = 0;
   $TNBBrowserTest::Fail = 0;
   $TNBBrowserTest::Done = 0;
   $TNBBrowserTest::Failures = "";

   $TNB::Host = %host;
   $TNB::AuthHost = %host;
   $TNB::Cert = "";

   TNBCertRefresh("TNBBrowserCertLoaded", "");
}

// Nothing can be asserted before the identity lands: TribeAndWarriorBrowserGui
// reads the warrior name out of the certificate on wake and opens that
// warrior's page, so opening it early queries the empty string.
function TNBBrowserCertLoaded(%ctx, %status, %result)
{
   TNBBrowserEq("the certificate arrives", %status, "ok");
   TNBBrowserEq("record 0 is the warrior quad",
                getField(getRecord($TNB::Cert, 0), 0), "orange01");
   TNBBrowserEq("record 0 field 3 is the GUID",
                getField(getRecord($TNB::Cert, 0), 3), "4510186");
   TNBBrowserEq("record 1 is the tribe count", getRecord($TNB::Cert, 1), 2);
   // Found by name rather than by position. Nothing in the shipped scripts
   // depends on the order of these records -- webbrowser.cs:1909-1926 walks
   // all of them -- so asserting on record 2 would be testing something the
   // client does not read, and did fail once for no better reason than the
   // two backends sorting differently.
   %tc = TNBCertTribe("Test Clan");
   TNBBrowserEq("a tribe record carries its id in field 3", getField(%tc, 3), 7);
   TNBBrowserEq("a tribe record carries the admin level in field 4",
                getField(%tc, 4), 4);
   TNBBrowserEq("a tribe record carries the title in field 5",
                getField(%tc, 5), "Leader");

   // webemail.cs computes this at file scope, before any identity exists.
   TNBBrowserEq("the mail cache is namespaced by GUID",
                $EmailCachePath, "webcache/4510186/");

   // The tab strip is built once, by onWake, when it finds no tabs -- so this
   // suite asserts on a freshly booted client and does not try to reset it.
   // (An earlier version looped on TWBTabView.removeTab(), which does not
   // exist: tabCount() never fell and the engine hung with a live console,
   // which looks exactly like a network stall.)
   Canvas.setContent(TribeAndWarriorBrowserGui);
   schedule(2500, 0, "TNBBrowserStep1");
}

// onWake opened the player's own page, which issues scalar 23.
function TNBBrowserStep1()
{
   TNBBrowserEq("the shipped browser is the content",
                Canvas.getContent().getName(), "TribeandWarriorBrowserGui");

   // One tab for the warrior plus one per tribe in the certificate. The tribe
   // tabs are built from the certificate, never from a query.
   TNBBrowserEq("the tab strip is warrior plus certificate tribes",
                TWBTabView.tabCount(), 3);

   %text = W_Text.getText();
   TNBBrowserHas("the profile shows the registration date", %text, "Registered:");
   TNBBrowserHas("status field 6 reached the registration line", %text, "2025-06-20");
   TNBBrowserHas("status field 8 reached the website line", %text, "www.tribesnext.com");
   TNBBrowserHas("the resultString reached the description",
                 %text, "Testing the in-game browser");

   // Field 9 of the status. An empty one renders a permanently blank picture,
   // because the client's own fallback ($PlayerGfx) is assigned by no shipped
   // script.
   TNBBrowserEq("status field 9 reached the graphic",
                PlayerPix.bitmap, "texticons/twb/twb_Missilelauncher.jpg");

   // TRIBES: array 13 for another warrior.
   W_MemberList.clear();
   PlayerPane.key = LaunchGui.key++;
   PlayerPane.state = "getWarriorTribeList";
   DatabaseQueryArray(13, 0, "Shifter", PlayerPane, PlayerPane.key);
   schedule(2000, 0, "TNBBrowserStep2");
}

function TNBBrowserStep2()
{
   TNBBrowserEq("a warrior's tribe list fills the member list",
                W_MemberList.rowCount(), 1);
   TNBBrowserHas("field 0 of a tribe row is the tribe name",
                 W_MemberList.getRowText(0), "Test Clan");
   TNBBrowserHas("field 5 of a tribe row is the title",
                 W_MemberList.getRowText(0), "Officer");

   // ROSTER: array 6, through the tribe pane rather than the warrior one.
   MemberList.clear();
   MemberList.clearColumns();
   TribePane.key = LaunchGui.key++;
   TribePane.state = "getTribeRoster";
   TProfileHdr.tribeName = "Test Clan";
   DatabaseQueryArray(6, 0, "Test Clan", TribePane, TribePane.key);
   schedule(2000, 0, "TNBBrowserStep3");
}

function TNBBrowserStep3()
{
   TNBBrowserEq("the roster fills the member list", MemberList.rowCount(), 2);

   // The three columns the ROSTER button adds beforehand line up with fields
   // 0, 4 and 5 -- which is what identifies array 6 as the roster.
   %row = MemberList.getRowText(0);
   TNBBrowserHas("field 0 of a roster row is the member", %row, "orange01");
   TNBBrowserHas("field 4 of a roster row is the title", %row, "Leader");
   TNBBrowserHas("field 5 of a roster row is the admin level", %row, "4");

   // The tribe profile, whose whole payload rides in the status.
   TribePane.key = LaunchGui.key++;
   TribePane.state = "getTribeProfile";
   DatabaseQuery(22, "Test Clan", TribePane, TribePane.key);
   schedule(2000, 0, "TNBBrowserStep4");
}

function TNBBrowserStep4()
{
   TNBBrowserEq("status field 2 reached the tribe id", TProfileHdr.tribeId, 7);
   TNBBrowserEq("status field 3 reached the tribe name",
                TProfileHdr.tribeName, "Test Clan");
   TNBBrowserEq("status field 4 reached the tribe tag", TProfileHdr.tribeTag, "[TC]");
   TNBBrowserHas("the resultString reached the tribe description",
                 TProfileHdr.Desc, "We are a test clan");

   // Search, which both search ordinals reach through the same pane.
   BrowserSearchPane.query = 4;
   BrowserSearchPane.key = LaunchGui.key++;
   BrowserSearchPane.state = "tribe";
   BrowserSearchMatchList.clear();
   BrowserSearchPane.rowNum = 0;
   DatabaseQueryArray(4, 0, "Test" TAB 0 TAB 100 TAB 0,
                      BrowserSearchPane, BrowserSearchPane.key);
   schedule(2000, 0, "TNBBrowserStep5");
}

function TNBBrowserStep5()
{
   TNBBrowserEq("a tribe search fills the match list",
                BrowserSearchMatchList.rowCount(), 1);
   // getTribeName (webbrowser.cs:316) renders fields 1 and 2 as
   // "<name> - <tag>", so both have to be in the right places for this to read.
   TNBBrowserHas("getTribeName renders name and tag together",
                 BrowserSearchMatchList.getRowText(0), "Test Clan");
   TNBBrowserHas("the tribe tag survives into the search row",
                 BrowserSearchMatchList.getRowText(0), "[TC]");

   // Founding a tribe, through the dialog's own controls.
   //
   // The description is the point. It is the fourth thing the create dialog
   // collects and it rides in the same ordinal as the name (scalar 16, six
   // fields: name, tag, append, recruiting, line count, description), so a
   // backend that reads only the first few founds the tribe correctly and
   // silently drops what the player typed -- and the pane the client lands on
   // straight afterwards is the profile that would have shown it. Nothing
   // reports an error, which is why this needs a test rather than a look.
   $TNBBrowserTest::NewTribe = "Fixture Clan";
   $TNBBrowserTest::NewDesc = "Founded by the test suite.";

   CreateTribe();                        // loads and pushes the dialog
   $CreateTribeName = $TNBBrowserTest::NewTribe;
   $CreateTribeTag = "[FX]";
   $CreateTribeAppend = false;
   $CreateTribeRecruiting = true;
   CreateTribeDescription.setValue($TNBBrowserTest::NewDesc);
   CreateTribeProcess();

   schedule(3000, 0, "TNBBrowserStep6");
}

function TNBBrowserStep6()
{
   // Read it back the way the tribe pane does, rather than trusting the
   // create call's own answer -- which is just the name echoed back.
   TribePane.key = LaunchGui.key++;
   TribePane.state = "getTribeProfile";
   TProfileHdr.Desc = "";
   DatabaseQuery(22, $TNBBrowserTest::NewTribe, TribePane, TribePane.key);

   schedule(2500, 0, "TNBBrowserStep7");
}

function TNBBrowserStep7()
{
   TNBBrowserEq("a tribe created through the dialog exists",
                TProfileHdr.tribeName, $TNBBrowserTest::NewTribe);
   TNBBrowserEq("and kept its tag", TProfileHdr.tribeTag, "[FX]");
   TNBBrowserHas("and kept the description it was created with",
                 TProfileHdr.Desc, $TNBBrowserTest::NewDesc);

   // Put the tab strip back. Creating a tribe opens a tab for it
   // (TWBTabView.view, webbrowser.cs:774) and the strip is built only once,
   // when onWake finds it empty -- so a leftover tab is still there on the next
   // run of this suite in the same client, and step 1's count fails against a
   // reseeded backend. The shipped leaveTribe handler tidies up the same way
   // (webbrowser.cs:1737-1741).
   for (%i = 0; %i < TWBTabView.tabCount(); %i++)
   {
      if (TWBTabView.getTabText(%i) $= $TNBBrowserTest::NewTribe)
         TWBTabView.removeTabByIndex(%i);
   }
   TWBTabView.setSelectedByIndex(0);

   $TNBBrowserTest::Done = 1;
   echo("TNBBROWSERRESULT pass=" @ $TNBBrowserTest::Pass @
        " fail=" @ $TNBBrowserTest::Fail);
}

echo("TNBrowser: browser_test.cs loaded");
