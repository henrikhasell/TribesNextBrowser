// TNBrowser -- player properties dialog logic
//
// Drives TNBPlayerPropsDlg, derived from the stock WarriorPropertiesDlg -- the
// player-side mirror of the clan properties dialog. Its PROFILE pane maps
// cleanly onto the browser API:
//
//   DESCRIPTION          -> userinfo
//   URL / home address   -> usersite
//   CHANGE NAME          -> username  (refused by the backend during the beta)
//
// The GFX pane is hidden: the API has clanpicture but no user-picture method,
// so those controls would have nothing to write to. Same treatment as the clan
// dialog's SECURITY pane.

function TNBPlayerPropsOpen()
{
   // Only your own profile is editable.
   if ($TNB::CurrentPlayer !$= TNBSessionGuid())
   {
      TNBError("You can only edit your own profile.");
      return;
   }

   TNBEnsureGuis();
   Canvas.pushDialog(TNBPlayerPropsDlg);

   TNBPPGfxBtn.setVisible(0);
   TNBPPGfxPane.setVisible(0);

   TNBPlayerPropsShowPane(0);
   TNBPlayerPropsRefresh();
}

// Repopulate from the cached profile, so the dialog always shows server truth
// rather than whatever was last typed.
function TNBPlayerPropsRefresh()
{
   if (!isObject(TNBPlayerPropsDlg))
      return;

   TNBPPDescription.setText($TNB::PlayerInfo $= ""
      ? "<spush><color:808080>No description.<spop>" : $TNB::PlayerInfo);

   TNBPPUrlEdit.setValue($TNB::PlayerSite);
   TNBPPCurrentName.setText($TNB::PlayerName);
   TNBPPNewName.setValue("");
}

function TNBPlayerPropsShowPane(%pane)
{
   TNBPPProfilePane.setVisible(%pane == 0);
   TNBPPGfxPane.setVisible(0);      // never shown; see file header
}

function TNBPlayerPropsClose()
{
   Canvas.popDialog(TNBPlayerPropsDlg);
}

function TNBPlayerPropsUnsupported()
{
   MessageBoxOK("PLAYER PROPERTIES",
      "The TribesNext API has no player picture, so this cannot be changed.");
}

//-----------------------------------------------------------------------------
// Description
//-----------------------------------------------------------------------------

function TNBPlayerPropsEditDesc()
{
   TNBEditOpen("userinfo", "");
}

function TNBPlayerPropsClearDesc()
{
   MessageBoxYesNo("CLEAR DESCRIPTION", "Clear your profile description?",
                   "TNBPlayerPropsConfirmClear();", "");
}

function TNBPlayerPropsConfirmClear()
{
   TNBApiSetInfo("", "TNBPlayerPropsChanged", "");
}

//-----------------------------------------------------------------------------
// Website
//-----------------------------------------------------------------------------

function TNBPlayerPropsChangeUrl()
{
   TNBApiSetWebsite(trim(TNBPPUrlEdit.getValue()), "TNBPlayerPropsChanged", "");
}

//-----------------------------------------------------------------------------
// Account name
//
// Wired in full. The backend rejects renames during the beta, so in practice
// this surfaces the server's refusal -- which is the point: the control is not
// silently missing, and it starts working the day the method is enabled.
//-----------------------------------------------------------------------------

function TNBPlayerPropsChangeName()
{
   %name = trim(TNBPPNewName.getValue());
   if (%name $= "")
   {
      TNBError("Enter the new name first.");
      return;
   }
   TNBApiChangeName(%name, "TNBPlayerPropsChanged", "");
}

//-----------------------------------------------------------------------------
// Change completion
//-----------------------------------------------------------------------------

function TNBPlayerPropsChanged(%ctx, %status, %result)
{
   if (%status $= "error")
   {
      TNBError(%result);
      return;
   }
   TNBApiUserView(TNBSessionGuid(), "TNBPlayerPropsReloaded", TNBSessionGuid());
}

function TNBPlayerPropsReloaded(%guid, %status, %result)
{
   TNBPlayerLoaded(%guid, %status, %result);
   TNBPlayerPropsRefresh();
}

echo("TNBrowser: playerprops.cs loaded");
