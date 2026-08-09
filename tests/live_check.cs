// TNBrowser -- live backend check.
//
// The one path the mock cannot cover: answering a real RSA challenge (which
// needs the account private key, and therefore a real password login) and then
// spending the resulting session on the documented JSON browser API.
//
// Results are stashed in globals rather than only echoed, because the steps
// complete from network callbacks long after the console command that started
// them has returned.
//
// Driven by tools/live-check.sh.

function TNBLiveReset()
{
   $TNBLive::Stage = "idle";
   $TNBLive::Status = "";
   $TNBLive::Detail = "";
   $TNBLive::Name = "";
   $TNBLive::Clans = "";
}

// Step 1: negotiate a session against the live robot endpoint.
function TNBLiveStart()
{
   TNBLiveReset();

   // Use the real account certificate, not the mock override.
   $TNB::GuidOverride = "";
   $TNB::Host = "https://tribesnext.thyth.com";
   $TNB::Debug = 1;

   TNBSessionEnd();
   TNBApiInit();

   $TNBLive::Stage = "session";
   $TNBLive::Guid = getField($LoginCertificate, 1);

   TNBSessionOnReady("TNBLiveSessionReady", "");
}

function TNBLiveSessionReady(%ctx, %status, %reason)
{
   if (%status $= "error")
   {
      $TNBLive::Stage = "done";
      $TNBLive::Status = "session-failed";
      $TNBLive::Detail = %reason;
      return;
   }

   $TNBLive::Stage = "browser";

   // Step 2: the interop question -- does a robot-issued UUID authorise the
   // JSON browser API? Both sides validate through the same challenge.php
   // session library, so it should, but this is the only way to know.
   TNBApiUserView($TNBLive::Guid, "TNBLiveProfileLoaded", "");
}

function TNBLiveProfileLoaded(%ctx, %status, %result)
{
   $TNBLive::Stage = "done";

   if (%status $= "error")
   {
      $TNBLive::Status = "browser-failed";
      $TNBLive::Detail = %result;
      return;
   }

   $TNBLive::Status = "ok";
   $TNBLive::Name = TNBJsonStr(%result, "name");

   %memberships = TNBJsonGet(%result, "memberships");
   %count = TNBJsonCount(%memberships);
   $TNBLive::Clans = %count;
   for (%i = 0; %i < %count; %i++)
   {
      $TNBLive::Detail = $TNBLive::Detail @
         TNBJsonStr(TNBJsonIndex(%memberships, %i), "name") @ " ";
   }
}

echo("TNBrowser: live_check.cs loaded");
