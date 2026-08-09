// TNBrowser -- GUI-level test.
//
// Drives the real controls against tools/mockserver.py and asserts on what
// ends up in them: pane text, roster rows, tab strip, search results. It is a
// step machine rather than a straight line because every load is asynchronous;
// each step schedules the next, and $TNBGuiTest::Done marks the end so a runner
// can wait on it.
//
//   exec("tests/gui_test.cs"); TNBGuiSelfTest("http://172.17.0.1:8099");

// The five tab buttons on each pane are one strip, so exactly one is lit at a
// time. They came from the stock GUI split across two radio groups (4 and 5),
// which meant one from each group stayed lit and two read as selected at once.
function TNBGuiPlayerTabsLit()
{
   return TNBPlayerTabProfile.getValue() + TNBPlayerTabHistory.getValue()
        + TNBPlayerTabClans.getValue() + TNBPlayerTabInvites.getValue()
        + TNBPlayerTabEdit.getValue();
}

function TNBGuiClanTabsLit()
{
   return TNBClanTabProfile.getValue() + TNBClanTabRoster.getValue()
        + TNBClanTabOptions.getValue() + TNBClanTabInvites.getValue()
        + TNBClanTabAdmin.getValue();
}

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

function TNBGuiSelfTest(%host)
{
   $TNBGuiTest::Pass = 0;
   $TNBGuiTest::Fail = 0;
   $TNBGuiTest::Done = 0;
   $TNBGuiTest::Failures = "";

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
   TNBGuiEq("rank name", TNBRankName(4), "Leader");
   TNBGuiEq("rank name out of range", TNBRankName(9), "Unknown");

   TNBrowserOpen();
   TNBOpenPlayer("4510186");
   schedule(2500, 0, "TNBGuiStep2");
}

function TNBGuiStep2()
{
   TNBGuiEq("own profile loaded", $TNB::PlayerName, "orange01");
   TNBGuiEq("title shows tagged name", TNBTitle.getValue(), "[TC]orange01");
   TNBGuiEq("player pane visible", TNBPlayerPane.isVisible(), 1);
   TNBGuiEq("clan pane hidden", TNBClanPane.isVisible(), 0);

   %text = TNBPlayerText.getText();
   TNBGuiHas("profile shows name", %text, "orange01");
   TNBGuiHas("profile shows website", %text, "www.tribesnext.com");
   TNBGuiHas("profile shows clan", %text, "Test Clan");
   TNBGuiHas("profile shows body", %text, "Testing the in-game browser.");
   TNBGuiHas("own profile offers edit", %text, "Edit my profile");

   TNBGuiEq("cached clan count", $TNB::PlayerClanCount, 2);
   TNBGuiEq("cached own rank", $TNB::MyRank[7], 4);

   // Tab strip seeded with our profile plus one tab per clan.
   TNBGuiEq("tab count", TNBTabView.tabCount(), 3);

   // CLANS sub-tab fills the side list.
   TNBPlayerPane.selectTab(2);
   TNBGuiEq("clan list rows", TNBPlayerClans.rowCount(), 2);
   TNBGuiEq("one player tab lit on CLANS", TNBGuiPlayerTabsLit(), 1);
   TNBGuiEq("and it is CLANS", TNBPlayerTabClans.getValue(), 1);

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
   TNBGuiHas("leader offered edit", %text, "Edit clan profile");

   // ROSTER sub-tab.
   TNBClanPane.selectTab(1);
   TNBGuiEq("roster rows", TNBRoster.rowCount(), 2);
   TNBGuiEq("one clan tab lit on ROSTER", TNBGuiClanTabsLit(), 1);
   TNBGuiEq("and it is ROSTER", TNBClanTabRoster.getValue(), 1);
   TNBGuiEq("roster first member", getField(TNBRoster.getRowText(0), 0), "orange01");
   TNBGuiEq("roster title column", getField(TNBRoster.getRowText(1), 1), "Officer");
   TNBGuiEq("roster rank column", getField(TNBRoster.getRowText(1), 2), "2");

   // ADMIN sub-tab: we are the leader, so the dangerous options appear.
   TNBClanPane.selectTab(4);
   TNBGuiEq("one clan tab lit on ADMIN", TNBGuiClanTabsLit(), 1);
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

function TNBGuiStep8()
{
   TNBPlayerPane.selectTab(3);
   TNBGuiEq("one player tab lit on INVITES", TNBGuiPlayerTabsLit(), 1);
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
