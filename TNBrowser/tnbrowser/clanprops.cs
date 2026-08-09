// TNBrowser -- clan properties dialog logic
//
// Drives TNBClanPropsDlg, derived from the stock TribePropertiesDlg: the
// biggest screen in the original clan UI. Three panes behind three buttons:
//
//   PROFILE   description, clan tag and which side it sits on, recruiting,
//             and the disband authorisation
//   GFX       the clan picture
//   SECURITY  hidden -- see below
//
// The stock SECURITY pane configured which rank was allowed to perform which
// action. TribesNext has no such concept: a member has a rank 0-4 and a
// free-text title, and the server decides permissions itself. Rather than show
// controls that cannot do anything, the pane and its button are hidden, the
// same treatment the unsupported mail features get.

//-----------------------------------------------------------------------------
// Opening
//-----------------------------------------------------------------------------

function TNBClanPropsOpen()
{
   if ($TNB::CurrentClan $= "")
      return;

   if (TNBMyRankInCurrentClan() < $TNB::RankToEditInfo)
   {
      TNBError("You do not have permission to administer this clan.");
      return;
   }

   TNBEnsureGuis();
   Canvas.pushDialog(TNBClanPropsDlg);

   TNBPropsSecurityBtn.setVisible(0);
   TNBPropsSecurityPane.setVisible(0);

   TNBClanPropsShowPane(0);
   TNBClanPropsRefresh();
}

// Repopulate every control from the cached clan record. Called on open and
// after any change is accepted, so the dialog always shows server truth rather
// than what the user typed.
function TNBClanPropsRefresh()
{
   if (!isObject(TNBClanPropsDlg))
      return;

   TNBPropsDescription.setText($TNB::ClanInfo $= ""
      ? "<spush><color:808080>No description.<spop>" : $TNB::ClanInfo);

   TNBPropsCurrentTag.setText($TNB::ClanTag $= "" ? "(none)" : $TNB::ClanTag);
   TNBPropsNewTag.setValue($TNB::ClanTag);

   TNBPropsPrefixBtn.setValue(!$TNB::ClanAppend);
   TNBPropsAppendBtn.setValue($TNB::ClanAppend);

   TNBPropsRecruitYes.setValue($TNB::ClanRecruiting);
   TNBPropsRecruitNo.setValue(!$TNB::ClanRecruiting);
   TNBPropsRecruitLabel.setText($TNB::ClanRecruiting ? "RECRUITING" : "CLOSED");

   // Only a leader may authorise a disband, and only leaders may rename/retag.
   %rank = TNBMyRankInCurrentClan();
   TNBPropsDisbandBtn.setVisible(%rank >= $TNB::RankToDisband);
   TNBPropsChangeTagBtn.setVisible(%rank >= $TNB::RankToRename);

   TNBClanPropsRefreshTag();
}

function TNBClanPropsShowPane(%pane)
{
   TNBPropsProfilePane.setVisible(%pane == 0);
   TNBPropsGfxPane.setVisible(%pane == 1);
   TNBPropsSecurityPane.setVisible(0);   // never shown; see file header

   if (%pane == 1)
      TNBClanPropsLoadGfx();
}

function TNBClanPropsClose()
{
   Canvas.popDialog(TNBClanPropsDlg);
}

//-----------------------------------------------------------------------------
// Description
//-----------------------------------------------------------------------------

function TNBClanPropsEditDesc()
{
   TNBEditOpen("claninfo", $TNB::CurrentClan);
}

function TNBClanPropsClearDesc()
{
   MessageBoxYesNo("CLEAR DESCRIPTION",
      "Clear the description for " @ $TNB::ClanName @ "?",
      "TNBClanPropsConfirmClear();", "");
}

function TNBClanPropsConfirmClear()
{
   TNBApiSetClanInfo($TNB::CurrentClan, "", "TNBClanPropsChanged", "");
}

//-----------------------------------------------------------------------------
// Tag
//-----------------------------------------------------------------------------

// Live preview of how the tag will read against the local player's name, which
// is what the stock dialog showed.
function TNBClanPropsRefreshTag()
{
   %tag = TNBPropsNewTag.getValue();
   %append = TNBPropsAppendBtn.getValue();
   %name = ($TNB::PlayerName $= "" ? "Player" : $TNB::PlayerName);

   TNBPropsPreviewTag.setText(TNBTaggedName(%name, %tag, %append));
}

function TNBClanPropsChangeTag()
{
   %tag = trim(TNBPropsNewTag.getValue());
   if (%tag $= "")
   {
      TNBError("Enter a tag first.");
      return;
   }

   TNBApiSetClanTag($TNB::CurrentClan, %tag, TNBPropsAppendBtn.getValue(),
                    "TNBClanPropsChanged", "");
}

//-----------------------------------------------------------------------------
// Recruiting and disband
//-----------------------------------------------------------------------------

function TNBClanPropsChangeRecruiting()
{
   TNBApiSetClanRecruiting($TNB::CurrentClan, TNBPropsRecruitYes.getValue(),
                           "TNBClanPropsChanged", "");
}

function TNBClanPropsDisband()
{
   MessageBoxYesNo("DISBAND TRIBE",
      "Authorise disbanding " @ $TNB::ClanName @ "?\n\nThis is how a clan is " @
      "permanently removed once enough leaders agree. You can withdraw the " @
      "authorisation while the clan still exists.",
      "TNBClanPropsConfirmDisband();", "");
}

function TNBClanPropsConfirmDisband()
{
   TNBApiDisbandClan($TNB::CurrentClan, 1, "TNBClanPropsChanged", "");
}

//-----------------------------------------------------------------------------
// Picture
//
// The stock GFX pane uploaded a JPEG to WON. clanpicture instead stores a path
// to an image the game already has, so the list is filled from the shipped
// clan artwork rather than from disk uploads.
//-----------------------------------------------------------------------------

function TNBClanPropsLoadGfx()
{
   TNBPropsGfxList.clear();
   TNBPropsGfxList.ClearColumns();
   TNBPropsGfxList.addColumn(0, "Picture", 200, 100, 320);

   $TNB::GfxCount = 0;
   TNBClanPropsAddGfxMatching("texticons/twb/*.jpg");
   TNBClanPropsAddGfxMatching("texticons/twb/*.png");

   if ($TNB::GfxCount == 0)
   {
      // Nothing found on the search path: still offer the stock default so the
      // pane is usable.
      TNBClanPropsAddGfx("texticons/twb/twb_Lineup.jpg");
   }

   if ($TNB::ClanPicture !$= "")
      TNBPropsGfxPreview.setBitmap($TNB::ClanPicture);
}

function TNBClanPropsAddGfxMatching(%pattern)
{
   for (%file = findFirstFile(%pattern); %file !$= ""; %file = findNextFile(%pattern))
      TNBClanPropsAddGfx(%file);
}

function TNBClanPropsAddGfx(%path)
{
   %n = $TNB::GfxCount;
   $TNB::GfxPath[%n] = %path;
   TNBPropsGfxList.addRow(%n, %path);
   $TNB::GfxCount = %n + 1;
}

function TNBClanPropsGfxSelect()
{
   %row = TNBPropsGfxList.getSelectedRow();
   if (%row < 0)
      return;

   %path = $TNB::GfxPath[TNBPropsGfxList.getRowId(%row)];
   if (%path $= "")
      return;

   $TNB::GfxChosen = %path;
   TNBPropsGfxPreview.setBitmap(%path);
}

function TNBClanPropsSetPicture()
{
   if ($TNB::GfxChosen $= "")
   {
      TNBError("Select a picture first.");
      return;
   }
   TNBApiSetClanPicture($TNB::CurrentClan, $TNB::GfxChosen,
                        "TNBClanPropsChanged", "");
}

//-----------------------------------------------------------------------------
// Change completion
//-----------------------------------------------------------------------------

// Every accepted change re-reads the clan, then repopulates the dialog from the
// fresh record. TNBClanLoaded does the re-render of the pane behind it.
function TNBClanPropsChanged(%ctx, %status, %result)
{
   if (%status $= "error")
   {
      TNBError(%result);
      return;
   }
   TNBApiClanView($TNB::CurrentClan, "TNBClanPropsReloaded", $TNB::CurrentClan);
}

function TNBClanPropsReloaded(%clanId, %status, %result)
{
   TNBClanLoaded(%clanId, %status, %result);
   TNBClanPropsRefresh();
}

echo("TNBrowser: clanprops.cs loaded");
