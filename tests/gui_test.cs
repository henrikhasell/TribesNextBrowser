// TNBrowser -- GUI-level test.
//
// Drives the real controls against tools/mockserver.py and asserts on what
// ends up in them: pane text, roster rows, tab strip, search results. It is a
// step machine rather than a straight line because every load is asynchronous;
// each step schedules the next, and $TNBGuiTest::Done marks the end so a runner
// can wait on it.
//
//   exec("tests/gui_test.cs"); TNBGuiSelfTest("http://172.17.0.1:8099", 0);
//
// %isBackend says which server is behind it. The mock stands in for TribesNext,
// which has no buddy list, so the views built on one are asserted only when a
// real backend is answering -- the same split mail_test.cs already uses.

function TNBGuiEq(%name, %got, %want)
{
   if (%got $= %want)
   {
      $TNBGuiTest::Pass++;
      echo("PASS " @ %name);
   }
   else
   {
      $TNBGuiTest::Fail++;
      // Also recorded, not just echoed: the steps run from schedule() callbacks,
      // so their console output lands between a runner's polls and is lost.
      $TNBGuiTest::Failures = $TNBGuiTest::Failures @ %name @
         " (got [" @ %got @ "] want [" @ %want @ "])\n";
      error("FAIL " @ %name @ " -- got [" @ %got @ "] want [" @ %want @ "]");
   }
}

// Substring assertion, for the marked-up pane text.
function TNBGuiHas(%name, %haystack, %needle)
{
   if (strstr(%haystack, %needle) >= 0)
   {
      $TNBGuiTest::Pass++;
      echo("PASS " @ %name);
   }
   else
   {
      $TNBGuiTest::Fail++;
      $TNBGuiTest::Failures = $TNBGuiTest::Failures @ %name @
         " (missing [" @ %needle @ "])\n";
      error("FAIL " @ %name @ " -- [" @ %needle @ "] not found in [" @
            getSubStr(%haystack, 0, 200) @ "]");
   }
}

function TNBGuiSelfTest(%host, %isBackend)
{
   $TNBGuiTest::Pass = 0;
   $TNBGuiTest::Fail = 0;
   $TNBGuiTest::Done = 0;
   $TNBGuiTest::Failures = "";
   $TNBGuiTest::Backend = %isBackend;

   $TNB::Host = %host;
   $TNB::AuthHost = %host;
   $TNB::GuidOverride = "4510186";
   TNBSessionEnd();
   TNBApiInit();

   echo("--- gui against " @ %host @ " ---");

   // Pure functions first: no network, no waiting.
   TNBGuiEq("date epoch zero", TNBFormatDate(0), "unknown");
   TNBGuiEq("date 2023-11-14", TNBFormatDate(1700000000), "2023-11-14");
   TNBGuiEq("date 2001-03-26", TNBFormatDate(985564800), "2001-03-26");
   TNBGuiEq("tag prefixed", TNBTaggedName("Bob", "[TC]", 0), "[TC]Bob");
   TNBGuiEq("tag appended", TNBTaggedName("Bob", "[TC]", 1), "Bob[TC]");
   TNBGuiEq("no tag", TNBTaggedName("Bob", "", 0), "Bob");
   TNBGuiEq("rank name", TNBRankName(4), "Tribe Admin 1");
   TNBGuiEq("rank name out of range", TNBRankName(9), "Unknown");

   TNBrowserOpen();
   TNBOpenPlayer("4510186");

   // The wait cursor goes up the moment something is queued, as the original
   // screens did. The engine has no getCursor, so the flag the helper keeps is
   // the only way to see it from here.
   TNBGuiEq("wait cursor while a request is out", $TNB::CursorBusy, 1);

   schedule(2500, 0, "TNBGuiStep2");
}

function TNBGuiStep2()
{
   // ...and comes back down once the queue drains. The assertions below all
   // depend on the response having arrived, so this is not a race.
   TNBGuiEq("cursor restored once the queue drains", $TNB::CursorBusy, "");

   TNBGuiEq("own profile loaded", $TNB::PlayerName, "orange01");
   TNBGuiEq("title shows tagged name", TNBTitle.getValue(), "[TC]orange01");
   TNBGuiEq("player pane visible", TNBPlayerPane.isVisible(), 1);
   TNBGuiEq("clan pane hidden", TNBClanPane.isVisible(), 0);

   %text = TNBPlayerText.getText();
   TNBGuiHas("profile shows name", %text, "orange01");
   TNBGuiHas("profile shows website", %text, "www.tribesnext.com");
   TNBGuiHas("profile shows clan", %text, "Test Clan");
   TNBGuiHas("profile shows body", %text, "Testing the in-game browser.");
   // The body carries no edit affordances: stock's did not, and editing has one
   // route, through ADMIN. Asserted as an absence so a second one cannot creep
   // back in unnoticed.
   TNBGuiEq("profile body offers no edit link",
            (strstr(%text, "Edit my") >= 0) ? 1 : 0, 0);

   TNBGuiEq("cached clan count", $TNB::PlayerClanCount, 2);
   TNBGuiEq("cached own rank", $TNB::MyRank[7], 4);

   // Tab strip seeded with our profile plus one tab per clan.
   TNBGuiEq("tab count", TNBTabView.tabCount(), 3);

   // TRIBES sub-tab fills the side list. The strip is stock's five buttons in
   // stock's slots, so the button has to be there at all.
   TNBGuiEq("tribes button present", isObject(TNBPlayerTabClans), 1);
   TNBPlayerPane.selectTab(2);
   TNBGuiEq("clan list rows", TNBPlayerClans.rowCount(), 2);
   TNBGuiEq("list flagged as tribes", TNBPlayerClans.CID, 0);

   TNBOpenClan("7");
   schedule(2000, 0, "TNBGuiStep3");
}

function TNBGuiStep3()
{
   TNBGuiEq("clan loaded", $TNB::ClanName, "Test Clan");
   TNBGuiEq("clan pane visible", TNBClanPane.isVisible(), 1);
   TNBGuiEq("my rank in clan", TNBMyRankInCurrentClan(), 4);

   %text = TNBClanText.getText();
   TNBGuiHas("clan profile name", %text, "Test Clan");
   TNBGuiHas("clan profile recruiting", %text, "Currently recruiting");
   TNBGuiHas("clan profile members", %text, "2 members");
   TNBGuiEq("clan body offers no edit link",
            (strstr(%text, "Edit clan profile") >= 0) ? 1 : 0, 0);

   // ROSTER sub-tab.
   TNBClanPane.selectTab(1);
   TNBGuiEq("roster rows", TNBRoster.rowCount(), 2);
   TNBGuiEq("list flagged as roster", TNBRoster.CID, 0);
   TNBGuiEq("roster first member", getField(TNBRoster.getRowText(0), 0), "orange01");
   TNBGuiEq("roster title column", getField(TNBRoster.getRowText(1), 1), "Officer");
   TNBGuiEq("roster rank column", getField(TNBRoster.getRowText(1), 2), "2");

   // ADMIN sub-tab: we are the leader, so the dangerous options appear.
   TNBClanPane.selectTab(4);
   %admin = TNBClanText.getText();
   TNBGuiHas("admin offers invite", %admin, "Invite a player");
   TNBGuiHas("admin offers properties", %admin, "Clan properties");

   // Disband and tag editing live in the recreated properties dialog, which
   // shows or hides them by rank. We are the leader of clan 7.
   TNBClanPropsOpen();
   TNBGuiEq("props dialog open", isObject(TNBClanPropsDlg), 1);
   TNBGuiEq("leader sees disband", TNBPropsDisbandBtn.isVisible(), 1);
   TNBGuiEq("leader sees tag change", TNBPropsChangeTagBtn.isVisible(), 1);
   TNBGuiEq("security pane hidden", TNBPropsSecurityPane.isVisible(), 0);
   TNBGuiEq("props tag prefilled", TNBPropsNewTag.getValue(), "[TC]");
   TNBGuiEq("props preview", TNBPropsPreviewTag.getValue(), "[TC]orange01");
   TNBGuiEq("props recruiting", TNBPropsRecruitYes.getValue(), 1);
   TNBClanPropsClose();

   // OPTIONS sub-tab.
   TNBClanPane.selectTab(2);
   TNBGuiHas("options offers tag", TNBClanText.getText(), "Wear this clan's tag");

   // INVITES sub-tab: a list in the same control as the roster, which is why
   // the CID flag has to move with it.
   TNBClanPane.selectTab(3);
   schedule(2000, 0, "TNBGuiStep3b");
}

function TNBGuiStep3b()
{
   // The two backends seed a different number of invitations, so this asserts
   // the shape of the view rather than the fixture: it is a list, it is flagged
   // as invites so a double-click does not reach for the rank editor, and the
   // INVITED column has something in it.
   TNBGuiEq("invites listed as rows", (TNBRoster.rowCount() >= 1), 1);
   TNBGuiEq("list flagged as invites", TNBRoster.CID, 1);
   TNBGuiEq("invited column filled",
            (getField(TNBRoster.getRowText(0), 1) !$= ""), 1);

   TNBGuiStep4();
}

function TNBGuiStep4()
{
   // Search dialog.
   TNBSearchOpen("player");
   $TNB::SearchText = "oran";
   TNBSearchSubmit();
   schedule(2000, 0, "TNBGuiStep5");
}

function TNBGuiStep5()
{
   TNBGuiEq("search result rows", TNBSearchResults.rowCount(), 2);
   TNBGuiEq("search first row", TNBSearchResults.getRowText(0), "[TC]orange01");
   TNBGuiEq("search row id is guid", TNBSearchResults.getRowId(0), "4510186");
   Canvas.popDialog(TNBSearchDlg);

   // A clan search should switch the result set over.
   TNBSearchOpen("clan");
   $TNB::SearchText = "casual";
   TNBSearchSubmit();
   schedule(2000, 0, "TNBGuiStep6");
}

function TNBGuiStep6()
{
   TNBGuiEq("clan search rows", TNBSearchResults.rowCount(), 1);
   TNBGuiEq("clan search name", TNBSearchResults.getRowText(0), "Casual Alliance");
   Canvas.popDialog(TNBSearchDlg);

   // Following a link opens a tab for another clan.
   TNBHandleLink("clan", "9");
   schedule(2000, 0, "TNBGuiStep7");
}

function TNBGuiStep7()
{
   TNBGuiEq("link opened new tab", TNBTabView.tabCount(), 3);
   TNBGuiEq("link switched clan", $TNB::CurrentClan, 9);
   TNBGuiEq("rank in second clan", TNBMyRankInCurrentClan(), 1);

   // A non-leader must not be offered the destructive controls.
   TNBClanPane.selectTab(4);
   %admin = TNBClanText.getText();
   TNBGuiEq("member denied admin",
            strstr(%admin, "do not have permission") >= 0, 1);

   // Invitations for our account.
   TNBOpenPlayer("4510186");
   schedule(1500, 0, "TNBGuiStep8");
}

// Slot 3 is stock's BUDDYLIST and shows the buddy list, which the mock -- a
// TribesNext stand-in -- cannot serve.
function TNBGuiStep8()
{
   if (!$TNBGuiTest::Backend)
   {
      TNBGuiStep8b();
      return;
   }
   TNBPlayerPane.selectTab(3);
   schedule(2000, 0, "TNBGuiStep8b");
}

function TNBGuiStep8b()
{
   if ($TNBGuiTest::Backend)
   {
      TNBGuiEq("list flagged as buddies", TNBPlayerClans.CID, 1);
      TNBGuiHas("buddy view offers add", TNBPlayerText.getText(), "Add a buddy");
   }

   // Invitations have no button: stock's player pane never had one, so they
   // hang off the profile instead.
   TNBHandleLink("invites", "");
   schedule(2000, 0, "TNBGuiStep9");
}

function TNBGuiStep9()
{
   %text = TNBPlayerText.getText();
   TNBGuiHas("invites listed", %text, "Pending invitations");
   TNBGuiHas("invite has accept link", %text, "Accept");
   TNBGuiHas("invite names clan", %text, "Test Clan");

   // Editor prefills with the current profile text.
   TNBEditOpen("userinfo", "");
   TNBGuiEq("editor prefilled", TNBEditText.getValue(), $TNB::PlayerInfo);
   Canvas.popDialog(TNBEditInfoDlg);

   // Prompt dialog round trip.
   TNBPromptOpen("Clan website", "www.old.example", "TNBGuiCapturePrompt");
   TNBGuiEq("prompt prefilled", TNBPromptField.getValue(), "www.old.example");
   TNBPromptField.setValue("www.new.example");
   TNBPromptAccept();
   TNBGuiEq("prompt applied value", $TNBGuiTest::Prompt, "www.new.example");

   // Rank editor reflects the selected member.
   $TNB::CurrentClan = 7;
   TNBMemberAdminOpen("4120041", "Shifter", "Officer", 2);
   TNBGuiEq("rank dialog who", TNBRankWho.getValue(), "Shifter");
   TNBGuiEq("rank dialog title", TNBRankTitle.getValue(), "Officer");
   TNBGuiEq("rank radio 2 set", TNBRank2.getValue(), 1);
   TNBGuiEq("rank radio 4 clear", TNBRank4.getValue(), 0);
   TNBRankSetUI(3);
   TNBGuiEq("rank radio moves", TNBRank3.getValue(), 1);
   TNBGuiEq("rank radio 2 cleared", TNBRank2.getValue(), 0);
   TNBGuiEq("rank value follows ui", $TNB::RankValue, 3);

   // Clicking a radio goes through TNBRankSelect. It must record the choice
   // without writing back to the controls -- doing so recurses through the
   // radio's own command and crashes the engine.
   TNBRankSelect(1);
   TNBGuiEq("click records rank", $TNB::RankValue, 1);
   Canvas.popDialog(TNBMemberAdminDlg);

   // Kicking lives on the roster's right-click menu, as it did in the stock UI.
   TNBEnsureRosterPopup();
   TNBGuiEq("roster popup exists", isObject(TNBRosterPopup), 1);
   $TNB::RankTargetName = "Shifter";
   $TNB::RankTarget = "4120041";
   $TNB::CurrentClan = 7;
   $TNB::MyRank[7] = 4;
   TNBRosterPopupDlg::onWake(TNBRosterPopupDlg);
   TNBGuiEq("popup offers kick to a leader",
            (strstr(TNBRosterPopup.getTextById(3), "Kick") >= 0), 1);

   // A plain member must not be offered it.
   $TNB::MyRank[7] = 1;
   TNBRosterPopupDlg::onWake(TNBRosterPopupDlg);
   TNBGuiEq("popup hides kick from a member", TNBRosterPopup.getTextById(3), "");
   $TNB::MyRank[7] = 4;

   // Create-clan preview follows the append toggle.
   TNBCreateClanOpen();
   $TNB::NewClanTag = "[XX]";
   $TNB::NewClanAppend = 0;
   TNBUpdateTagPreview();
   TNBGuiEq("preview prefixed", TNBNewClanPreview.getValue(), "[XX]orange01");
   $TNB::NewClanAppend = 1;
   TNBUpdateTagPreview();
   TNBGuiEq("preview appended", TNBNewClanPreview.getValue(), "orange01[XX]");
   Canvas.popDialog(TNBCreateClanDlg);

   echo("");
   echo("TNBGUIRESULT pass=" @ $TNBGuiTest::Pass @ " fail=" @ $TNBGuiTest::Fail);
   $TNBGuiTest::Done = 1;
}

function TNBGuiCapturePrompt(%value)
{
   $TNBGuiTest::Prompt = %value;
}

echo("TNBrowser: gui_test.cs loaded");
