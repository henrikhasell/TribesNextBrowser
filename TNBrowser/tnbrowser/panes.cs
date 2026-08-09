// TNBrowser -- GUI logic for the profile and clan browser
//
// Drives the controls declared in tnbrowser/gui/*.gui. The window keeps a tab
// per open subject (a player or a clan) exactly as the stock browser did, and
// each pane has its own row of sub-tabs.
//
//   Player pane   0 PROFILE   1 HISTORY   2 CLANS    3 INVITES   4 EDIT
//   Clan pane     0 PROFILE   1 ROSTER    2 OPTIONS  3 INVITES   4 ADMIN
//
// All data arrives asynchronously. Every load therefore renders a placeholder
// first and fills the controls from an API callback, so a slow server shows
// "Loading..." rather than a blank pane that looks broken.

$TNB::RankName[0] = "Recruit";
$TNB::RankName[1] = "Member";
$TNB::RankName[2] = "Officer";
$TNB::RankName[3] = "Senior Admin";
$TNB::RankName[4] = "Leader";

// Ranks required to perform each clan action, matching what the backend
// enforces. Used only to decide which controls to show -- the server remains
// the authority and its refusal is always surfaced.
$TNB::RankToEditInfo = 2;
$TNB::RankToInvite = 2;
$TNB::RankToPromote = 3;
$TNB::RankToRename = 3;
$TNB::RankToDisband = 4;

//-----------------------------------------------------------------------------
// Helpers
//-----------------------------------------------------------------------------

function TNBRankName(%rank)
{
   if (%rank < 0 || %rank > 4)
      return "Unknown";
   return $TNB::RankName[%rank];
}

// UNIX timestamp to YYYY-MM-DD. The engine has no date formatting, so this is
// the standard days-to-civil conversion.
function TNBFormatDate(%epoch)
{
   if (%epoch $= "" || %epoch <= 0)
      return "unknown";

   %days = mFloor(%epoch / 86400) + 719468;
   %era = mFloor(%days / 146097);
   %doe = %days - %era * 146097;
   %yoe = mFloor((%doe - mFloor(%doe / 1460) + mFloor(%doe / 36524) - mFloor(%doe / 146096)) / 365);
   %y = %yoe + %era * 400;
   %doy = %doe - (365 * %yoe + mFloor(%yoe / 4) - mFloor(%yoe / 100));
   %mp = mFloor((5 * %doy + 2) / 153);
   %d = %doy - mFloor((153 * %mp + 2) / 5) + 1;
   %m = %mp + (%mp < 10 ? 3 : -9);
   if (%m <= 2)
      %y++;

   return %y @ "-" @ (%m < 10 ? "0" : "") @ %m @ "-" @ (%d < 10 ? "0" : "") @ %d;
}

// A player's display name with their tag placed on the correct side.
function TNBTaggedName(%name, %tag, %append)
{
   if (%tag $= "")
      return %name;
   if (%append)
      return %name @ %tag;
   return %tag @ %name;
}

function TNBOnlineText(%online)
{
   return (%online ? "Online" : "Offline");
}

// Report a failed call in the same way the stock browser did.
function TNBError(%message)
{
   MessageBoxOK("COMMUNITY BROWSER", %message);
}

//-----------------------------------------------------------------------------
// Opening the browser
//-----------------------------------------------------------------------------

// The .gui files are loaded here rather than from scripts/autoexec, and that
// timing is load-bearing.
//
// autoexec runs before the shell control profiles (ShellPaneProfile,
// ShellRadioProfile, ShellButtonProfile, ...) are created. A control built
// against a profile that does not exist yet still *constructs* -- isObject()
// says 1 and every method works -- but it silently falls back to a default
// look: no pane frame, no title bar, collapsed radio buttons and buttons. The
// window appears to work while looking nothing like the game.
//
// Deferring to first open means the profiles are guaranteed to exist.
// Checked per file rather than behind one guard on TNBrowserGui: a single guard
// means adding a screen later silently never loads it in any session where the
// browser window already exists, which is every session after the first open.
function TNBEnsureGuis()
{
   TNBEnsureGui(TNBrowserGui, "tnbrowser/gui/TNBrowserGui.gui");
   TNBEnsureGui(TNBSearchDlg, "tnbrowser/gui/TNBSearchDlg.gui");
   TNBEnsureGui(TNBEditInfoDlg, "tnbrowser/gui/TNBEditInfoDlg.gui");
   TNBEnsureGui(TNBMemberAdminDlg, "tnbrowser/gui/TNBMemberAdminDlg.gui");
   TNBEnsureGui(TNBCreateClanDlg, "tnbrowser/gui/TNBCreateClanDlg.gui");
   TNBEnsureGui(TNBPromptDlg, "tnbrowser/gui/TNBPromptDlg.gui");
   TNBEnsureGui(TNBClanPropsDlg, "tnbrowser/gui/TNBClanPropsDlg.gui");
   TNBEnsureGui(TNBPlayerPropsDlg, "tnbrowser/gui/TNBPlayerPropsDlg.gui");
}

function TNBEnsureGui(%obj, %file)
{
   if (!isObject(%obj))
      exec(%file);
}

// The roster's right-click menu. The stock browser built this in script rather
// than shipping it as a .gui (TribePane::onAdd), and so does this -- which is
// also why it is constructed inside a function: `new GuiControl(...)` at file
// scope crashes this engine outright.
function TNBEnsureRosterPopup()
{
   if (isObject(TNBRosterPopupDlg))
      return;

   new GuiControl(TNBRosterPopupDlg)
   {
      profile = "GuiModelessDialogProfile";
      horizSizing = "width";
      vertSizing = "height";
      position = "0 0";
      extent = "640 480";
      minExtent = "8 8";
      visible = "1";
      setFirstResponder = "0";
      modal = "1";

      new ShellPopupMenu(TNBRosterPopup)
      {
         profile = "ShellPopupProfile";
         position = "0 0";
         extent = "0 0";
         minExtent = "0 0";
         visible = "1";
         maxPopupHeight = "200";
         noButtonStyle = "1";
      };
   };
}

function TNBrowserOpen()
{
   TNBEnsureGuis();
   LaunchTabView.viewTab("BROWSER", TNBrowserGui, 0);
}

function TNBrowserGui::onWake(%this)
{
   TNBRoster.ClearColumns();
   TNBRoster.Clear();
   TNBPlayerClans.ClearColumns();
   TNBPlayerClans.Clear();

   Canvas.pushDialog(LaunchToolbarDlg);

   if (TNBTabView.tabCount() == 0)
   {
      %guid = TNBSessionGuid();
      if (%guid $= "")
      {
         TNBSetPlayerText("<just:center>\n\nYou are not logged in to a TribesNext account.");
         return;
      }
      // Open our own profile; its membership list then supplies a tab per clan.
      TNBOpenPlayer(%guid);
   }
}

function TNBrowserGui::onSleep(%this)
{
   Canvas.popDialog(LaunchToolbarDlg);
}

// LaunchTabView::onSelect calls setKey on whatever GUI it hosts, and the launch
// shell also expects onClose and connectionTerminated. They are empty stubs in
// the stock browser too, but they must exist: without setKey the shell aborts
// with "Unknown command setKey" the moment the tab is selected, and the browser
// never appears.
function TNBrowserGui::setKey(%this, %key)
{
   %this.key = %key;
}

function TNBrowserGui::onClose(%this, %key)
{
}

function TNBrowserGui::connectionTerminated(%this, %key)
{
}

//-----------------------------------------------------------------------------
// Tab strip
//-----------------------------------------------------------------------------

function TNBTabView::onAdd(%this)
{
   // Tab set 1 is drawn with the horizontal tab bitmap, as in the stock GUI.
   %this.addSet(1, "gui/shll_horztabbuttonB", "5 5 5", "50 50 0", "5 5 5");
}

// Show a subject, adding a tab for it if it is not already open.
// %type is "player" or "clan"; %key is the guid or clan id.
function TNBTabView::view(%this, %label, %type, %key)
{
   %set = (%type $= "clan" ? 1 : 0);

   for (%i = 0; %i < %this.tabCount(); %i++)
   {
      if (%this.getTabText(%i) $= %label && %set == %this.getTabSet(%i))
      {
         %this.setSelectedByIndex(%i);
         return;
      }
   }

   %index = %this.tabCount();
   %this.addTab(%index, %label, %set);
   %this.tabType[%index] = %type;
   %this.tabKey[%index] = %key;
   %this.setSelectedByIndex(%index);
}

function TNBTabView::onSelect(%this, %id, %text)
{
   %type = %this.tabType[%id];
   %key = %this.tabKey[%id];

   if (%type $= "clan")
      TNBOpenClan(%key);
   else
      TNBOpenPlayer(%key);
}

function TNBTabView::closeCurrentPane(%this)
{
   if (%this.tabCount() <= 1)
   {
      // Closing the last tab would leave an empty window with no way back.
      LaunchTabView.closeCurrentTab();
      return;
   }
   %this.removeTab(%this.getSelectedId());
}

//-----------------------------------------------------------------------------
// Pane switching
//-----------------------------------------------------------------------------

function TNBShowPlayerPane()
{
   TNBPlayerPane.setVisible(1);
   TNBClanPane.setVisible(0);
}

function TNBShowClanPane()
{
   TNBPlayerPane.setVisible(0);
   TNBClanPane.setVisible(1);
}

function TNBSetPlayerText(%text)
{
   TNBPlayerText.setText(%text);
   TNBPlayerScroll.scrollToTop();
}

function TNBSetClanText(%text)
{
   TNBClanText.setText(%text);
   TNBClanScroll.scrollToTop();
}

//-----------------------------------------------------------------------------
// Player profile
//-----------------------------------------------------------------------------

function TNBOpenPlayer(%guid)
{
   $TNB::CurrentPlayer = %guid;
   TNBShowPlayerPane();
   TNBPlayerTabProfile.setValue(1);
   $TNB::PlayerTab = 0;

   TNBSetPlayerText("<just:center>\n\nLoading profile...");
   TNBApiUserView(%guid, "TNBPlayerLoaded", %guid);
}

function TNBPlayerLoaded(%guid, %status, %result)
{
   if (%status $= "error")
   {
      TNBSetPlayerText("<just:center>\n\nCould not load this profile.\n\n" @ %result);
      return;
   }

   // Cache the fields the other sub-tabs and the edit dialog need, because the
   // result tree is freed as soon as this callback returns.
   $TNB::PlayerName = TNBJsonStr(%result, "name");
   $TNB::PlayerTag = TNBJsonStr(%result, "tag");
   $TNB::PlayerAppend = TNBJsonBool(%result, "append");
   $TNB::PlayerSite = TNBJsonStr(%result, "website");
   $TNB::PlayerInfo = TNBJsonStr(%result, "info");
   $TNB::PlayerOnline = TNBJsonBool(%result, "online");
   $TNB::PlayerCreated = TNBJsonStr(%result, "creation");

   %display = TNBTaggedName($TNB::PlayerName, $TNB::PlayerTag, $TNB::PlayerAppend);
   TNBTitle.setText(%display);
   TNBTitle.name = $TNB::PlayerName;

   // Remember our clan ranks so the clan panes can decide which admin controls
   // to offer without a second round trip.
   %memberships = TNBJsonGet(%result, "memberships");
   $TNB::PlayerClanCount = TNBJsonCount(%memberships);
   for (%i = 0; %i < $TNB::PlayerClanCount; %i++)
   {
      %m = TNBJsonIndex(%memberships, %i);
      $TNB::PlayerClanId[%i] = TNBJsonStr(%m, "id");
      $TNB::PlayerClanName[%i] = TNBJsonStr(%m, "name");
      $TNB::PlayerClanRank[%i] = TNBJsonStr(%m, "rank");
      $TNB::PlayerClanTitle[%i] = TNBJsonStr(%m, "title");

      if (%guid $= TNBSessionGuid())
         $TNB::MyRank[$TNB::PlayerClanId[%i]] = $TNB::PlayerClanRank[%i];
   }

   TNBPlayerPix.setBitmap($TNB::PlayerOnline ? "texticons/twb/twb_Lineup.jpg"
                                             : "texticons/twb/twb_Lineup.jpg");

   TNBRenderPlayerTab();

   // The first load of our own profile seeds the tab strip.
   if (TNBTabView.tabCount() == 0)
   {
      TNBTabView.view(%display, "player", %guid);
      for (%i = 0; %i < $TNB::PlayerClanCount; %i++)
         TNBTabView.addTabFor($TNB::PlayerClanName[%i], "clan", $TNB::PlayerClanId[%i]);
   }
}

// Add a tab without selecting it, used to seed the clan tabs at startup.
function TNBTabView::addTabFor(%this, %label, %type, %key)
{
   %index = %this.tabCount();
   %this.addTab(%index, %label, (%type $= "clan" ? 1 : 0));
   %this.tabType[%index] = %type;
   %this.tabKey[%index] = %key;
}

function TNBPlayerPane::selectTab(%this, %tab)
{
   $TNB::PlayerTab = %tab;

   if (%tab == 3)
   {
      TNBSetPlayerText("<just:center>\n\nLoading invitations...");
      TNBApiUserInvites("TNBPlayerInvitesLoaded", "");
      return;
   }
   if (%tab == 1)
   {
      TNBSetPlayerText("<just:center>\n\nLoading history...");
      TNBApiUserHistory($TNB::CurrentPlayer, "TNBHistoryLoaded", "player");
      return;
   }
   if (%tab == 4)
   {
      TNBPlayerPropsOpen();
      TNBPlayerTabProfile.setValue(1);
      $TNB::PlayerTab = 0;
      return;
   }
   TNBRenderPlayerTab();
}

function TNBRenderPlayerTab()
{
   if ($TNB::PlayerTab == 2)
   {
      TNBRenderPlayerClans();
      return;
   }
   TNBRenderPlayerProfile();
}

function TNBRenderPlayerProfile()
{
   %isSelf = ($TNB::CurrentPlayer $= TNBSessionGuid());

   %text = "<font:Univers Bold:18>" @
           TNBTaggedName($TNB::PlayerName, $TNB::PlayerTag, $TNB::PlayerAppend) @
           "<font:Univers:14>\n";
   %text = %text @ TNBOnlineText($TNB::PlayerOnline) @
           "   |   Member since " @ TNBFormatDate($TNB::PlayerCreated) @ "\n";

   if ($TNB::PlayerSite !$= "")
      %text = %text @ "<a:tnb\tweb\t" @ $TNB::PlayerSite @ ">" @ $TNB::PlayerSite @ "</a>\n";

   %text = %text @ "\n";

   if ($TNB::PlayerClanCount > 0)
   {
      %text = %text @ "<font:Univers Bold:14>Clans<font:Univers:14>\n";
      for (%i = 0; %i < $TNB::PlayerClanCount; %i++)
      {
         %text = %text @ "  <a:tnb\tclan\t" @ $TNB::PlayerClanId[%i] @ ">" @
                 $TNB::PlayerClanName[%i] @ "</a> -- " @
                 $TNB::PlayerClanTitle[%i] @
                 " (rank " @ $TNB::PlayerClanRank[%i] @ ")\n";
      }
      %text = %text @ "\n";
   }

   if ($TNB::PlayerInfo $= "")
      %text = %text @ "<spush><color:808080>This player has not written a profile."
                    @ "<spop>\n";
   else
   {
      // Profile bodies may contain the markup the original browser supported,
      // so they are rendered rather than escaped.
      %text = %text @ $TNB::PlayerInfo @ "\n";
   }

   if (%isSelf)
   {
      %text = %text @ "\n<a:tnb\teditinfo\t>[ Edit my profile ]</a>" @
              "   <a:tnb\teditsite\t>[ Edit my website ]</a>";
   }
   %text = %text @ "\n<a:tnb\tuserhistory\t>[ View history ]</a>";

   TNBSetPlayerText(%text);
}

function TNBRenderPlayerClans()
{
   TNBPlayerClans.Clear();
   TNBPlayerClans.ClearColumns();
   // Same width budget as the roster: this list is the stock W_MemberList.
   TNBPlayerClans.addColumn(0, "Clan", 110, 70, 200);
   TNBPlayerClans.addColumn(1, "Title", 65, 45, 160);
   TNBPlayerClans.addColumn(2, "Rank", 40, 30, 60, "numeric center");

   for (%i = 0; %i < $TNB::PlayerClanCount; %i++)
   {
      TNBPlayerClans.AddRow($TNB::PlayerClanId[%i],
         $TNB::PlayerClanName[%i] TAB $TNB::PlayerClanTitle[%i] TAB
         $TNB::PlayerClanRank[%i]);
   }

   TNBSetPlayerText("<just:center>\n\nSelect a clan from the list to open it.");
}

function TNBPlayerClansActivate()
{
   %row = TNBPlayerClans.getSelectedRow();
   if (%row < 0)
      return;
   %clanId = TNBPlayerClans.getRowId(%row);
   TNBOpenClanTab(%clanId, getField(TNBPlayerClans.getRowText(%row), 0));
}

function TNBPlayerInvitesLoaded(%ctx, %status, %result)
{
   if (%status $= "error")
   {
      TNBSetPlayerText("<just:center>\n\nCould not load invitations.\n\n" @ %result);
      return;
   }

   %count = TNBJsonCount(%result);
   if (%count == 0)
   {
      TNBSetPlayerText("<just:center>\n\nYou have no pending clan invitations.");
      return;
   }

   %text = "<font:Univers Bold:16>Pending invitations<font:Univers:14>\n\n";
   for (%i = 0; %i < %count; %i++)
   {
      %inv = TNBJsonIndex(%result, %i);
      %clan = TNBJsonGet(%inv, "clan");
      %sender = TNBJsonGet(%inv, "sender");
      %clanId = TNBJsonStr(%clan, "id");

      %text = %text @ "<a:tnb\tclan\t" @ %clanId @ ">" @
              TNBJsonStr(%clan, "name") @ "</a>\n";
      %text = %text @ "  invited by " @ TNBJsonStr(%sender, "name") @ "\n";
      %text = %text @ "  <a:tnb\taccept\t" @ %clanId @ ">[ Accept ]</a>   " @
              "<a:tnb\treject\t" @ %clanId @ ">[ Decline ]</a>\n\n";
   }
   TNBSetPlayerText(%text);
}

function TNBHistoryLoaded(%which, %status, %result)
{
   if (%status $= "error")
   {
      %msg = "<just:center>\n\nCould not load history.\n\n" @ %result;
      if (%which $= "clan")
         TNBSetClanText(%msg);
      else
         TNBSetPlayerText(%msg);
      return;
   }

   %count = TNBJsonCount(%result);
   %text = "<font:Univers Bold:16>History<font:Univers:14>\n\n";
   if (%count == 0)
      %text = %text @ "Nothing recorded.";

   for (%i = 0; %i < %count; %i++)
   {
      %row = TNBJsonIndex(%result, %i);
      %text = %text @ TNBFormatDate(TNBJsonStr(%row, "time")) @ "  " @
              TNBJsonStr(%row, "event") @ "\n";
   }

   if (%which $= "clan")
      TNBSetClanText(%text);
   else
      TNBSetPlayerText(%text);
}

//-----------------------------------------------------------------------------
// Clan profile
//-----------------------------------------------------------------------------

function TNBOpenClanTab(%clanId, %label)
{
   TNBTabView.view(%label, "clan", %clanId);
}

function TNBOpenClan(%clanId)
{
   $TNB::CurrentClan = %clanId;
   TNBShowClanPane();
   TNBClanTabProfile.setValue(1);
   $TNB::ClanTab = 0;

   TNBSetClanText("<just:center>\n\nLoading clan...");
   TNBApiClanView(%clanId, "TNBClanLoaded", %clanId);
}

function TNBClanLoaded(%clanId, %status, %result)
{
   if (%status $= "error")
   {
      TNBSetClanText("<just:center>\n\nCould not load this clan.\n\n" @ %result);
      return;
   }

   $TNB::ClanName = TNBJsonStr(%result, "name");
   $TNB::ClanTag = TNBJsonStr(%result, "tag");
   $TNB::ClanAppend = TNBJsonBool(%result, "append");
   $TNB::ClanRecruiting = TNBJsonBool(%result, "recruiting");
   $TNB::ClanSite = TNBJsonStr(%result, "website");
   $TNB::ClanInfo = TNBJsonStr(%result, "info");
   $TNB::ClanCreated = TNBJsonStr(%result, "creation");
   $TNB::ClanPicture = TNBJsonStr(%result, "picture");
   $TNB::ClanActive = TNBJsonBool(%result, "active");

   TNBTitle.setText($TNB::ClanName);
   TNBTitle.name = $TNB::ClanName;

   %members = TNBJsonGet(%result, "members");
   $TNB::ClanMemberCount = TNBJsonCount(%members);
   $TNB::MyRank[%clanId] = -1;

   for (%i = 0; %i < $TNB::ClanMemberCount; %i++)
   {
      %m = TNBJsonIndex(%members, %i);
      $TNB::ClanMemberGuid[%i] = TNBJsonStr(%m, "guid");
      $TNB::ClanMemberName[%i] = TNBJsonStr(%m, "name");
      $TNB::ClanMemberRank[%i] = TNBJsonStr(%m, "rank");
      $TNB::ClanMemberTitle[%i] = TNBJsonStr(%m, "title");
      $TNB::ClanMemberOnline[%i] = TNBJsonBool(%m, "online");

      if ($TNB::ClanMemberGuid[%i] $= TNBSessionGuid())
         $TNB::MyRank[%clanId] = $TNB::ClanMemberRank[%i];
   }

   if ($TNB::ClanPicture !$= "")
      TNBClanPix.setBitmap($TNB::ClanPicture);

   TNBRenderClanTab();
}

function TNBClanPane::selectTab(%this, %tab)
{
   $TNB::ClanTab = %tab;

   if (%tab == 3)
   {
      TNBSetClanText("<just:center>\n\nLoading invitations...");
      TNBApiClanViewInvites($TNB::CurrentClan, "TNBClanInvitesLoaded", "");
      return;
   }
   if (%tab == 2)
   {
      TNBRenderClanOptions();
      return;
   }
   if (%tab == 4)
   {
      TNBRenderClanAdmin();
      return;
   }
   TNBRenderClanTab();
}

function TNBRenderClanTab()
{
   if ($TNB::ClanTab == 1)
   {
      TNBRenderRoster();
      return;
   }
   TNBRenderClanProfile();
}

function TNBMyRankInCurrentClan()
{
   %r = $TNB::MyRank[$TNB::CurrentClan];
   if (%r $= "")
      return -1;
   return %r;
}

function TNBRenderClanProfile()
{
   %text = "<font:Univers Bold:18>" @ $TNB::ClanName @ "<font:Univers:14>\n";
   %text = %text @ "Tag: " @ ($TNB::ClanTag $= "" ? "(none)" : $TNB::ClanTag) @
           ($TNB::ClanAppend ? "  (appended)" : "  (prefixed)") @ "\n";
   %text = %text @ "Founded " @ TNBFormatDate($TNB::ClanCreated) @
           "   |   " @ $TNB::ClanMemberCount @ " member" @
           ($TNB::ClanMemberCount == 1 ? "" : "s") @ "\n";
   %text = %text @ ($TNB::ClanRecruiting ? "Currently recruiting"
                                         : "Not recruiting") @ "\n";

   if (!$TNB::ClanActive)
      %text = %text @ "<spush><color:c04040>This clan has been disbanded.<spop>\n";

   if ($TNB::ClanSite !$= "")
      %text = %text @ "<a:tnb\tweb\t" @ $TNB::ClanSite @ ">" @ $TNB::ClanSite @ "</a>\n";

   %text = %text @ "\n";

   if ($TNB::ClanInfo $= "")
      %text = %text @ "<spush><color:808080>This clan has not written a profile.<spop>";
   else
      %text = %text @ $TNB::ClanInfo;

   %rank = TNBMyRankInCurrentClan();
   if (%rank >= $TNB::RankToEditInfo)
      %text = %text @ "\n\n<a:tnb\teditclaninfo\t>[ Edit clan profile ]</a>";

   TNBSetClanText(%text);
}

function TNBRenderRoster()
{
   // The roster list inherits the stock geometry and is only 217px wide, so the
   // columns have to add up to about that. Online state is shown with the row
   // style rather than a fourth column, which is what the original did -- a
   // "Status" column simply does not fit.
   TNBRoster.Clear();
   TNBRoster.ClearColumns();
   TNBRoster.addColumn(0, "Member", 110, 70, 200);
   TNBRoster.addColumn(1, "Title", 65, 45, 160);
   TNBRoster.addColumn(2, "Rank", 40, 30, 60, "numeric center");

   for (%i = 0; %i < $TNB::ClanMemberCount; %i++)
   {
      TNBRoster.AddRow($TNB::ClanMemberGuid[%i],
         $TNB::ClanMemberName[%i] TAB
         $TNB::ClanMemberTitle[%i] TAB
         $TNB::ClanMemberRank[%i]);

      TNBRoster.setRowStylebyID($TNB::ClanMemberGuid[%i],
                                !$TNB::ClanMemberOnline[%i]);
   }

   %rank = TNBMyRankInCurrentClan();
   if (%rank >= $TNB::RankToPromote)
      TNBSetClanText("<just:center>\n\nDouble-click a member to change their rank or remove them.");
   else
      TNBSetClanText("<just:center>\n\nDouble-click a member to open their profile.");
}

// Double-click in the roster: officers get the rank editor, everyone else the
// member's profile.
function TNBRoster::onRightMouseDown(%this, %column, %row, %mousePos)
{
   %this.setSelectedRow(%row);

   %guid = %this.getRowId(%row);
   if (%guid $= "")
      return;

   $TNB::RankTarget = %guid;
   $TNB::RankTargetName = getField(%this.getRowText(%row), 0);
   $TNB::RankTargetTitle = getField(%this.getRowText(%row), 1);
   $TNB::RankTargetRank = getField(%this.getRowText(%row), 2);

   TNBEnsureRosterPopup();
   TNBRosterPopup.position = %mousePos;
   Canvas.pushDialog(TNBRosterPopupDlg);
   TNBRosterPopupDlg.onWake();
   TNBRosterPopup.forceOnAction();
}

function TNBRosterPopupDlg::onWake(%this)
{
   TNBRosterPopup.clear();
   TNBRosterPopup.add(strUpr($TNB::RankTargetName), -1);
   TNBRosterPopup.add("--------------------------------", -1);
   TNBRosterPopup.add("View Profile", 0);
   TNBRosterPopup.add("Send Mail", 1);

   // Officer actions, shown only to those the server would accept them from.
   // Buddy and block lists are omitted: the API has neither.
   if (TNBMyRankInCurrentClan() >= $TNB::RankToPromote &&
       $TNB::RankTarget !$= TNBSessionGuid())
   {
      TNBRosterPopup.add("................................", -1);
      TNBRosterPopup.add("Edit Rank and Title", 2);
      TNBRosterPopup.add("Kick from Clan", 3);
   }
}

function TNBRosterPopup::onSelect(%this, %id, %text)
{
   Canvas.popDialog(TNBRosterPopupDlg);

   switch (%id)
   {
      case 0:
         TNBTabView.view($TNB::RankTargetName, "player", $TNB::RankTarget);

      case 1:
         TNBMailCompose();
         TNBComposeTo.setValue($TNB::RankTarget);

      case 2:
         TNBMemberAdminOpen($TNB::RankTarget, $TNB::RankTargetName,
                            $TNB::RankTargetTitle, $TNB::RankTargetRank);

      case 3:
         TNBKickSelected();
   }
}

function TNBRosterPopupDlg::onSleep(%this)
{
}

function TNBRosterActivate()
{
   %row = TNBRoster.getSelectedRow();
   if (%row < 0)
      return;

   %guid = TNBRoster.getRowId(%row);
   %text = TNBRoster.getRowText(%row);

   if (TNBMyRankInCurrentClan() >= $TNB::RankToPromote &&
       %guid !$= TNBSessionGuid())
   {
      TNBMemberAdminOpen(%guid, getField(%text, 0), getField(%text, 1),
                         getField(%text, 2));
      return;
   }
   TNBTabView.view(getField(%text, 0), "player", %guid);
}

function TNBClanInvitesLoaded(%ctx, %status, %result)
{
   if (%status $= "error")
   {
      TNBSetClanText("<just:center>\n\nCould not load invitations.\n\n" @ %result);
      return;
   }

   // clanviewinvites wraps its list in {"status":..., "payload":[...]}.
   %list = TNBJsonGet(%result, "payload");
   if (!%list)
      %list = %result;

   %count = TNBJsonCount(%list);
   if (%count == 0)
   {
      TNBSetClanText("<just:center>\n\nThis clan has no outstanding invitations.");
      return;
   }

   %text = "<font:Univers Bold:16>Outstanding invitations<font:Univers:14>\n\n";
   for (%i = 0; %i < %count; %i++)
   {
      %inv = TNBJsonIndex(%list, %i);
      %guid = TNBJsonStr(%inv, "guid");
      %text = %text @ "  <a:tnb\tplayer\t" @ %guid @ ">" @
              TNBJsonStr(%inv, "name") @ "</a>\n";
   }
   TNBSetClanText(%text);
}

//-----------------------------------------------------------------------------
// Clan options and administration
//-----------------------------------------------------------------------------

function TNBRenderClanOptions()
{
   %rank = TNBMyRankInCurrentClan();

   if (%rank < 0)
   {
      TNBSetClanText("<just:center>\n\nYou are not a member of this clan.");
      return;
   }

   %text = "<font:Univers Bold:16>My membership<font:Univers:14>\n\n";
   %text = %text @ "Rank: " @ %rank @ " (" @ TNBRankName(%rank) @ ")\n\n";
   %text = %text @ "<a:tnb\tweartag\t" @ $TNB::CurrentClan @ ">[ Wear this clan's tag ]</a>\n";
   %text = %text @ "<a:tnb\tcleartag\t>[ Wear no tag ]</a>\n\n";
   %text = %text @ "<a:tnb\tleave\t" @ $TNB::CurrentClan @ ">[ Leave this clan ]</a>\n";

   TNBSetClanText(%text);
}

function TNBRenderClanAdmin()
{
   %rank = TNBMyRankInCurrentClan();

   if (%rank < $TNB::RankToEditInfo)
   {
      TNBSetClanText("<just:center>\n\nYou do not have permission to administer this clan.");
      return;
   }

   %text = "<font:Univers Bold:16>Clan administration<font:Univers:14>\n\n";
   %text = %text @ "<a:tnb\tclanprops\t>[ Clan properties ]</a>" @
           "   description, tag, recruiting, picture, disband\n";
   %text = %text @ "<a:tnb\tinvite\t>[ Invite a player ]</a>\n";
   %text = %text @ "<a:tnb\tclanhistory\t>[ View clan history ]</a>\n";

   if (%rank >= $TNB::RankToRename)
   {
      %text = %text @ "\n<font:Univers Bold:14>Identity<font:Univers:14>\n";
      %text = %text @ "<a:tnb\trenameclan\t>[ Rename clan ]</a>\n";
      %text = %text @ "<a:tnb\tclansite\t>[ Set clan website ]</a>\n";
   }

   if (%rank >= $TNB::RankToDisband)
   {
      %text = %text @ "\n<font:Univers Bold:14>Danger<font:Univers:14>\n";
      %text = %text @ "<a:tnb\tundisband\t>[ Withdraw disband authorisation ]</a>\n";
   }

   TNBSetClanText(%text);
}

//-----------------------------------------------------------------------------
// Link handling
//
// The stock webbrowser.cs already defines GuiMLTextCtrl::onURL for the old WON
// browser, so this is a package override rather than a redefinition: TNBrowser
// URLs start with a "tnb" field and everything else is handed to the original
// implementation, leaving the shipped GUI working.
//-----------------------------------------------------------------------------

package TNBrowserLinks
{
   function GuiMLTextCtrl::onURL(%this, %url)
   {
      if (getField(%url, 0) !$= "tnb")
      {
         Parent::onURL(%this, %url);
         return;
      }
      TNBHandleLink(getField(%url, 1), getField(%url, 2));
   }
};

function TNBHandleLink(%action, %arg)
{
   switch$ (%action)
   {
      case "player":
         TNBApiUserView(%arg, "TNBOpenPlayerTabFromLink", %arg);

      case "clan":
         TNBApiClanView(%arg, "TNBOpenClanTabFromLink", %arg);

      case "web":
         gotoWebPage(%arg);

      case "editinfo":
         TNBEditOpen("userinfo", "");

      case "editclaninfo":
         TNBEditOpen("claninfo", $TNB::CurrentClan);

      case "clanprops":
         TNBClanPropsOpen();

      case "editsite":
         TNBPromptOpen("My website", $TNB::PlayerSite, "TNBApplyUserSite");

      case "clanpicture":
         TNBPromptOpen("Clan picture path", $TNB::ClanPicture, "TNBApplyClanPicture");

      case "clanhistory":
         TNBSetClanText("<just:center>\n\nLoading history...");
         TNBApiClanHistory($TNB::CurrentClan, "TNBHistoryLoaded", "clan");

      case "userhistory":
         TNBSetPlayerText("<just:center>\n\nLoading history...");
         TNBApiUserHistory($TNB::CurrentPlayer, "TNBHistoryLoaded", "player");

      case "accept":
         TNBApiAcceptInvite(%arg, "TNBAfterInviteAction", "accepted");

      case "reject":
         TNBApiRejectInvite(%arg, "TNBAfterInviteAction", "declined");

      case "weartag":
         TNBApiSetActiveClan(%arg, "TNBAfterTagChange", "");

      case "cleartag":
         TNBApiSetActiveClan(-1, "TNBAfterTagChange", "");

      case "leave":
         MessageBoxYesNo("LEAVE CLAN",
            "Leave " @ $TNB::ClanName @ "?",
            "TNBConfirmLeave(" @ %arg @ ");", "");

      case "recruit":
         TNBApiSetClanRecruiting($TNB::CurrentClan, %arg, "TNBAfterClanChange", "");

      case "invite":
         TNBSearchOpen("invite");

      case "renameclan":
         TNBPromptOpen("Rename clan", $TNB::ClanName, "TNBApplyClanName");

      case "retag":
         TNBPromptOpen("New clan tag", $TNB::ClanTag, "TNBApplyClanTag");

      case "clansite":
         TNBPromptOpen("Clan website", $TNB::ClanSite, "TNBApplyClanSite");

      case "disband":
         MessageBoxYesNo("DISBAND CLAN",
            "Authorise disbanding " @ $TNB::ClanName @ "? This is how a clan is "
            @ "permanently removed once enough leaders agree.",
            "TNBApiDisbandClan($TNB::CurrentClan, 1, \"TNBAfterClanChange\", \"\");", "");

      case "undisband":
         TNBApiDisbandClan($TNB::CurrentClan, 0, "TNBAfterClanChange", "");
   }
}

function TNBConfirmLeave(%clanId)
{
   TNBApiLeaveClan(%clanId, "TNBAfterClanChange", "");
}

function TNBOpenPlayerTabFromLink(%guid, %status, %result)
{
   if (%status $= "error")
   {
      TNBError(%result);
      return;
   }
   TNBTabView.view(TNBTaggedName(TNBJsonStr(%result, "name"),
                                 TNBJsonStr(%result, "tag"),
                                 TNBJsonBool(%result, "append")),
                   "player", %guid);
}

function TNBOpenClanTabFromLink(%clanId, %status, %result)
{
   if (%status $= "error")
   {
      TNBError(%result);
      return;
   }
   TNBTabView.view(TNBJsonStr(%result, "name"), "clan", %clanId);
}

function TNBAfterInviteAction(%what, %status, %result)
{
   if (%status $= "error")
   {
      TNBError(%result);
      return;
   }
   MessageBoxOK("COMMUNITY BROWSER", "Invitation " @ %what @ ".");
   TNBApiUserInvites("TNBPlayerInvitesLoaded", "");
}

// Anything that changes clan or membership state re-reads the affected view so
// the GUI reflects what the server actually did rather than what we assumed.
// Changing which tag you wear records the choice on the community server; the
// tag only reaches game servers via a fresh community certificate, so fetch one.
function TNBAfterTagChange(%ctx, %status, %result)
{
   if (%status $= "error")
   {
      TNBError(%result);
      return;
   }
   TNBCertFetch(1);
   TNBAfterClanChange(%ctx, %status, %result);
}

function TNBAfterClanChange(%ctx, %status, %result)
{
   if (%status $= "error")
   {
      TNBError(%result);
      return;
   }
   if ($TNB::CurrentClan !$= "")
      TNBApiClanView($TNB::CurrentClan, "TNBClanLoaded", $TNB::CurrentClan);
}

//-----------------------------------------------------------------------------
// Search dialog
//-----------------------------------------------------------------------------

// %mode is "player", "clan" or "invite" (a player search whose result invites).
function TNBSearchOpen(%mode)
{
   $TNB::SearchMode = %mode;
   $TNB::SearchText = "";
   Canvas.pushDialog(TNBSearchDlg);
   TNBSearchResults.clear();
}

function TNBSearchDlg::onWake(%this)
{
   TNBSearchPane.setText($TNB::SearchMode $= "clan" ? "CLAN SEARCH" : "PLAYER SEARCH");
   TNBSearchField.makeFirstResponder(1);
}

function TNBSearchSubmit()
{
   %query = trim($TNB::SearchText);
   if (%query $= "")
      return;

   TNBSearchResults.clear();
   TNBSearchResults.addRow(-1, "Searching...");

   if ($TNB::SearchMode $= "clan")
      TNBApiClanSearch(%query, "TNBSearchResultsLoaded", "");
   else
      TNBApiUserSearch(%query, "TNBSearchResultsLoaded", "");
}

function TNBSearchResultsLoaded(%ctx, %status, %result)
{
   TNBSearchResults.clear();

   if (%status $= "error")
   {
      TNBSearchResults.addRow(-1, "Error: " @ %result);
      return;
   }

   %count = TNBJsonCount(%result);
   if (%count == 0)
   {
      TNBSearchResults.addRow(-1, "No matches.");
      return;
   }

   for (%i = 0; %i < %count; %i++)
   {
      %row = TNBJsonIndex(%result, %i);
      if ($TNB::SearchMode $= "clan")
         TNBSearchResults.addRow(TNBJsonStr(%row, "id"), TNBJsonStr(%row, "name"));
      else
         TNBSearchResults.addRow(TNBJsonStr(%row, "guid"),
            TNBTaggedName(TNBJsonStr(%row, "name"), TNBJsonStr(%row, "tag"),
                          TNBJsonBool(%row, "append")));
   }
}

function TNBSearchAccept()
{
   %row = TNBSearchResults.getSelectedRow();
   if (%row < 0)
      return;

   %id = TNBSearchResults.getRowId(%row);
   if (%id < 0)
      return;                    // a status line, not a result

   %label = TNBSearchResults.getRowText(%row);
   Canvas.popDialog(TNBSearchDlg);

   if ($TNB::SearchMode $= "clan")
      TNBTabView.view(%label, "clan", %id);
   else if ($TNB::SearchMode $= "invite")
      TNBApiInvitePlayer($TNB::CurrentClan, %id, "TNBAfterClanChange", "");
   else if ($TNB::SearchMode $= "mailto")
      TNBComposeTo.setValue(%id);      // the compose dialog wants a GUID
   else
      TNBTabView.view(%label, "player", %id);
}

//-----------------------------------------------------------------------------
// Description editor
//-----------------------------------------------------------------------------

function TNBEditOpen(%target, %clanId)
{
   $TNB::EditTarget = %target;
   $TNB::EditClanId = %clanId;
   Canvas.pushDialog(TNBEditInfoDlg);
   TNBEditText.setValue(%target $= "claninfo" ? $TNB::ClanInfo : $TNB::PlayerInfo);
}

function TNBEditApply()
{
   %text = TNBEditText.getValue();
   Canvas.popDialog(TNBEditInfoDlg);

   if ($TNB::EditTarget $= "claninfo")
      TNBApiSetClanInfo($TNB::EditClanId, %text, "TNBAfterClanChange", "");
   else
      TNBApiSetInfo(%text, "TNBAfterProfileChange", "");
}

function TNBAfterProfileChange(%ctx, %status, %result)
{
   if (%status $= "error")
   {
      TNBError(%result);
      return;
   }
   TNBApiUserView($TNB::CurrentPlayer, "TNBPlayerLoaded", $TNB::CurrentPlayer);
}

//-----------------------------------------------------------------------------
// Member rank editor
//-----------------------------------------------------------------------------

function TNBMemberAdminOpen(%guid, %name, %title, %rank)
{
   $TNB::RankTarget = %guid;
   $TNB::RankTargetName = %name;

   Canvas.pushDialog(TNBMemberAdminDlg);
   TNBRankWho.setValue(%name);
   TNBRankTitle.setValue(%title);
   TNBRankSetUI(%rank);
}

// Command handler for the rank radio buttons. It only records the choice, and
// deliberately does not write back to the controls: setValue(1) on a radio
// fires that radio's own command, so a handler that re-set the radios would
// call itself forever and take the engine down with a stack overflow. (The
// stock TAM_OnAction is a one-liner for exactly this reason.)
function TNBRankSelect(%rank)
{
   $TNB::RankValue = %rank;
}

// Push a rank into the controls, e.g. when opening the dialog. The guard makes
// the setValue-fires-command feedback harmless rather than merely unlikely.
function TNBRankSetUI(%rank)
{
   if ($TNB::RankUpdating)
      return;

   $TNB::RankUpdating = 1;
   $TNB::RankValue = %rank;

   TNBRank0.setValue(%rank == 0);
   TNBRank1.setValue(%rank == 1);
   TNBRank2.setValue(%rank == 2);
   TNBRank3.setValue(%rank == 3);
   TNBRank4.setValue(%rank == 4);

   $TNB::RankUpdating = 0;
   $TNB::RankValue = %rank;
}

function TNBRankApply()
{
   %title = TNBRankTitle.getValue();
   Canvas.popDialog(TNBMemberAdminDlg);
   TNBApiSetRank($TNB::CurrentClan, $TNB::RankTarget, $TNB::RankValue, %title,
                 "TNBAfterClanChange", "");
}

// Bound to the REMOVE button in the rank dialog. The dialog closes first so the
// confirmation is not stacked underneath it.
function TNBKickSelected()
{
   %name = $TNB::RankTargetName;

   MessageBoxYesNo("REMOVE MEMBER",
      "Remove " @ %name @ " from " @ $TNB::ClanName @ "?",
      "TNBConfirmKick();", "");
}

function TNBConfirmKick()
{
   TNBApiKickMember($TNB::CurrentClan, $TNB::RankTarget,
                    "TNBAfterClanChange", "");
}

//-----------------------------------------------------------------------------
// Clan creation
//-----------------------------------------------------------------------------

function TNBCreateClanOpen()
{
   $TNB::NewClanName = "";
   $TNB::NewClanTag = "";
   $TNB::NewClanAppend = 0;
   $TNB::NewClanRecruiting = 0;
   Canvas.pushDialog(TNBCreateClanDlg);
   TNBUpdateTagPreview();
}

function TNBUpdateTagPreview()
{
   %name = ($TNB::PlayerName $= "" ? "Player" : $TNB::PlayerName);
   TNBNewClanPreview.setValue(
      TNBTaggedName(%name, $TNB::NewClanTag, $TNB::NewClanAppend));
}

function TNBCreateClanSubmit()
{
   %name = trim($TNB::NewClanName);
   %tag = trim($TNB::NewClanTag);

   if (%name $= "" || %tag $= "")
   {
      TNBError("A new clan needs both a name and a tag.");
      return;
   }

   Canvas.popDialog(TNBCreateClanDlg);
   TNBApiCreateClan(%tag, ($TNB::NewClanAppend ? "yes" : "no"), %name,
                    "TNBAfterCreateClan", "");
}

function TNBAfterCreateClan(%ctx, %status, %result)
{
   if (%status $= "error")
   {
      TNBError(%result);
      return;
   }
   MessageBoxOK("COMMUNITY BROWSER", "Clan created.");
   // Re-read our profile so the new clan appears in the tab strip.
   TNBApiUserView(TNBSessionGuid(), "TNBPlayerLoaded", TNBSessionGuid());
}

//-----------------------------------------------------------------------------
// Small single-field prompt, reused for renames, tags and websites
//-----------------------------------------------------------------------------

// commonDialogs.cs has no text-collecting message box, so this drives our own
// TNBPromptDlg instead.
function TNBPromptOpen(%title, %value, %applyFn)
{
   $TNB::PromptApply = %applyFn;
   $TNB::PromptValue = %value;
   Canvas.pushDialog(TNBPromptDlg);
   TNBPromptPane.setText(%title);
   TNBPromptField.setValue(%value);
   TNBPromptField.makeFirstResponder(1);
}

function TNBPromptAccept()
{
   %value = trim(TNBPromptField.getValue());
   Canvas.popDialog(TNBPromptDlg);

   if (%value $= "")
      return;

   call($TNB::PromptApply, %value);
}

function TNBApplyClanName(%value)
{
   TNBApiSetClanName($TNB::CurrentClan, %value, "TNBAfterClanChange", "");
}

function TNBApplyClanTag(%value)
{
   TNBApiSetClanTag($TNB::CurrentClan, %value, $TNB::ClanAppend,
                    "TNBAfterClanChange", "");
}

function TNBApplyClanSite(%value)
{
   TNBApiSetClanWebsite($TNB::CurrentClan, %value, "TNBAfterClanChange", "");
}

function TNBApplyClanPicture(%value)
{
   TNBApiSetClanPicture($TNB::CurrentClan, %value, "TNBAfterClanChange", "");
}

function TNBApplyUserSite(%value)
{
   TNBApiSetWebsite(%value, "TNBAfterProfileChange", "");
}

echo("TNBrowser: panes.cs loaded");
