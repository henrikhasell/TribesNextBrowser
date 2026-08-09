// TNBrowser -- community certificate
//
// This is what actually makes a clan tag appear in your in-game name.
//
// Game servers never talk to a central system, so the client has to present a
// signed *community certificate* issued by the DCE. That certificate annotates
// the bare account certificate with the current name, clan, tag and membership,
// which is where getAuthInfo() -- and therefore the name servers display -- gets
// them from. It is time-limited on purpose, so the annotations cannot go stale.
// See t2csri/clientSideClans.cs in the TribesNext package.
//
// The gap this fills: the shipped TribesNext client *sends*
// $T2CSRI::CommunityCertificate to servers (clientSide.cs) but never fetches
// it -- the only reference in t2csri.vl2 is the read. Only the 2017 tournament
// client ever set it. On a stock install that global is empty,
// t2csri_sendCommunityCert() returns early, and no clan tag can ever show up.
//
// So setting your active clan with userclan is only half the job: it records the
// choice on the community server, and this fetch is what carries it into the
// game.
//
// Response is tab-delimited, not JSON:
//   CEC <TAB> <community certificate>
//   DCE <TAB> <DCE certificate>          (zero or more, cached by index)
//   ERR: <message>
//
// Status today: the live DCE answers
//   ERR: Signer validity period has expired.
// to an authenticated request -- its signing certificate has lapsed, so no
// client can obtain a community certificate at present and no tag can appear.
// Verified with a valid session (the same session's json_browser calls return
// 200). Nothing here can work around that; it needs renewing server-side. The
// code is complete so it starts working the moment it is.

if ($TNB::RobotBrowserURI $= "")
   $TNB::RobotBrowserURI = "/tn/robot/robot_browser.php";

//-----------------------------------------------------------------------------

function TNBCertFetch(%announce)
{
   if (isEventPending($TNB::CertSchedule))
      cancel($TNB::CertSchedule);
   $TNB::CertSchedule = "";

   TNBApiEnqueueRawOn($TNB::AuthHost, $TNB::RobotBrowserURI, "cert", "",
                      "TNBCertLoaded", %announce, 0, 1);
}

function TNBCertLoaded(%announce, %status, %body)
{
   if (%status $= "error")
   {
      TNBCertFailed(%body, %announce);
      return;
   }

   %got = 0;
   %count = getRecordCount(%body);

   for (%i = 0; %i < %count; %i++)
   {
      %line = trim(getRecord(%body, %i));
      if (%line $= "")
         continue;

      %tag = getField(%line, 0);

      if (%tag $= "CEC")
      {
         // collapseEscape, as the reference client does: the certificate is
         // transported with escaped separators.
         $T2CSRI::CommunityCertificate = collapseEscape(getField(%line, 1));
         %got = 1;
         TNBCertScheduleRefresh();
      }
      else if (%tag $= "DCE")
      {
         %dce = collapseEscape(getField(%line, 1));
         $T2CSRI::ClientDCESupport::DCECert[getField(%dce, 1)] = %dce;
      }
      else if (getSubStr(%line, 0, 5) $= "ERR: ")
      {
         TNBCertFailed(getSubStr(%line, 5, strlen(%line)), %announce);
         return;
      }
   }

   if (!%got)
   {
      TNBCertFailed("The community server did not return a certificate.", %announce);
      return;
   }

   echo("TNBrowser: community certificate updated");
   if (%announce)
      MessageBoxOK("CLAN TAG",
         "Your clan tag is now active. It will show in your name on servers " @
         "you join from now on.");
}

function TNBCertFailed(%reason, %announce)
{
   error("TNBrowser: could not obtain a community certificate -- " @ %reason);
   $TNB::CertLastError = %reason;

   if (!%announce)
      return;

   // Only surface this when the user did something that expected it to work,
   // and say plainly that it is the server's side that is broken.
   MessageBoxOK("CLAN TAG",
      "Your active clan was saved, but the community server would not issue " @
      "the certificate that carries your tag into the game:\n\n" @ %reason @
      "\n\nUntil that is fixed server-side, no tag can appear in your name.");
}

// The certificate carries its expiry in field 2. Refresh a minute before it, so
// the tag never lapses mid-session.
function TNBCertScheduleRefresh()
{
   %expire = getField($T2CSRI::CommunityCertificate, 2);
   if (%expire $= "")
      return;

   %seconds = %expire - TNBCertNow() - 60;
   if (%seconds < 60)
      %seconds = 60;
   if (%seconds > 3600)
      %seconds = 3600;

   $TNB::CertSchedule = schedule(%seconds * 1000, 0, "TNBCertFetch", 0);
}

// The engine has no wall clock. getSimTime() is milliseconds since start, so
// anchor it against the issue time in the certificate we just received: field 1
// is the issued epoch, which by definition is "about now".
function TNBCertNow()
{
   %issued = getField($T2CSRI::CommunityCertificate, 1);
   if (%issued $= "")
      return 0;
   return %issued;
}

echo("TNBrowser: cert.cs loaded");
