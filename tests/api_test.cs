// TNBrowser -- integration test for the session and API layers.
//
// Runs against tools/mockserver.py, not the live backend, so it can exercise
// write methods and error paths without touching real accounts.
//
//   ./tools/mockserver.py --port 8099 &
//   exec("tests/api_test.cs"); TNBApiSelfTest("http://172.17.0.1:8099");
//
// The calls are asynchronous, so each step's callback launches the next one and
// the run ends by printing TNBAPIRESULT. A step that never completes simply
// never prints it, which is the signal that something hung.

function TNBApiTestEq(%name, %got, %want)
{
   if (%got $= %want)
   {
      $TNBApiTest::Pass++;
      echo("PASS " @ %name);
   }
   else
   {
      $TNBApiTest::Fail++;
      // Recorded as well as echoed: later steps run from network callbacks, so
      // their console output lands between a runner's polls and is lost.
      $TNBApiTest::Failures = $TNBApiTest::Failures @ %name @
         " (got [" @ %got @ "] want [" @ %want @ "])\n";
      error("FAIL " @ %name @ " -- got [" @ %got @ "] want [" @ %want @ "]");
    }
}

function TNBApiSelfTest(%host)
{
   $TNBApiTest::Pass = 0;
   $TNBApiTest::Fail = 0;
   $TNBApiTest::Failures = "";

   $TNB::Host = %host;
   $TNB::AuthHost = %host;
   $TNBApiTest::Host = %host;
   $TNB::GuidOverride = "4510186";   // pretend to be orange01
   TNBSessionEnd();
   TNBApiInit();

   echo("--- api integration against " @ %host @ " ---");
   TNBApiUserView("4510186", "TNBApiTestStep1", "");
}

// Reading our own profile also proves the session negotiated, since the first
// request transparently establishes one.
function TNBApiTestStep1(%ctx, %status, %result)
{
   TNBApiTestEq("userview status", %status, "ok");
   TNBApiTestEq("session established", TNBSessionReady(), 1);
   TNBApiTestEq("userview name", TNBJsonStr(%result, "name"), "orange01");
   TNBApiTestEq("userview website", TNBJsonStr(%result, "website"), "www.tribesnext.com");

   %m = TNBJsonGet(%result, "memberships");
   TNBApiTestEq("membership count", TNBJsonCount(%m), 2);
   TNBApiTestEq("first clan name", TNBJsonStr(TNBJsonIndex(%m, 0), "name"), "Test Clan");
   TNBApiTestEq("first clan rank", TNBJsonStr(TNBJsonIndex(%m, 0), "rank"), "4");

   TNBApiClanView("7", "TNBApiTestStep2", "");
}

function TNBApiTestStep2(%ctx, %status, %result)
{
   TNBApiTestEq("clanview status", %status, "ok");
   TNBApiTestEq("clan name", TNBJsonStr(%result, "name"), "Test Clan");
   TNBApiTestEq("clan recruiting", TNBJsonBool(%result, "recruiting"), 1);

   %members = TNBJsonGet(%result, "members");
   TNBApiTestEq("roster size", TNBJsonCount(%members), 2);
   TNBApiTestEq("member title", TNBJsonStr(TNBJsonIndex(%members, 1), "title"), "Officer");

   TNBApiUserSearch("oran", "TNBApiTestStep3", "");
}

function TNBApiTestStep3(%ctx, %status, %result)
{
   TNBApiTestEq("usersearch status", %status, "ok");
   TNBApiTestEq("usersearch is array", TNBJsonType(%result), "array");
   TNBApiTestEq("usersearch hits", TNBJsonCount(%result), 2);

   TNBApiClanSearch("test", "TNBApiTestStep4", "");
}

function TNBApiTestStep4(%ctx, %status, %result)
{
   TNBApiTestEq("clansearch hits", TNBJsonCount(%result), 1);
   TNBApiTestEq("clansearch id", TNBJsonStr(TNBJsonIndex(%result, 0), "id"), "7");

   // A write, via POST, with text that needs both URL and JSON escaping.
   $TNBApiTest::Marker = "Edited by the test: \"quoted\" & 100% done";
   TNBApiSetInfo($TNBApiTest::Marker, "TNBApiTestStep5", "");
}

function TNBApiTestStep5(%ctx, %status, %result)
{
   TNBApiTestEq("setinfo status", %status, "ok");
   // Read it back to prove the POST body reached the server intact.
   TNBApiUserView("4510186", "TNBApiTestStep6", "");
}

function TNBApiTestStep6(%ctx, %status, %result)
{
   TNBApiTestEq("profile text round trip",
                TNBJsonStr(%result, "info"), $TNBApiTest::Marker);

   TNBApiInvitePlayer("7", "4200999", "TNBApiTestStep7", "");
}

function TNBApiTestStep7(%ctx, %status, %result)
{
   TNBApiTestEq("invite status", %status, "ok");
   TNBApiSetRank("7", "4120041", 3, "Warlord", "TNBApiTestStep8", "");
}

function TNBApiTestStep8(%ctx, %status, %result)
{
   TNBApiTestEq("setrank status", %status, "ok");
   // Rank above our own must be refused by the server.
   TNBApiSetRank("7", "4120041", 9, "Impossible", "TNBApiTestStep9", "");
}

function TNBApiTestStep9(%ctx, %status, %result)
{
   TNBApiTestEq("invalid rank refused", %status, "error");
   TNBApiTestEq("invalid rank message", %result, "rank must be an integer 0 to 4");

   // Beta-disabled method: must surface the server's refusal, not vanish.
   TNBApiChangeName("newname", "TNBApiTestStep10", "");
}

function TNBApiTestStep10(%ctx, %status, %result)
{
   // Both backends refuse renames, for different reasons and in their own
   // words -- TribesNext disables the method during the beta, a self-hosted
   // backend does not own the name. Assert the refusal, not the wording.
   TNBApiTestEq("name change refused", %status, "error");
   TNBApiTestEq("name change explains itself", (%result !$= ""), 1);

   TNBApiUserInvites("TNBApiTestStep11", "");
}

function TNBApiTestStep11(%ctx, %status, %result)
{
   TNBApiTestEq("userinvites status", %status, "ok");
   TNBApiTestEq("one pending invite", TNBJsonCount(%result), 1);
   %clan = TNBJsonGet(TNBJsonIndex(%result, 0), "clan");
   TNBApiTestEq("invite clan name", TNBJsonStr(%clan, "name"), "Test Clan");

   // Queue several at once: they must run in order over one connection.
   $TNBApiTest::Order = "";
   TNBApiUserView("4120041", "TNBApiTestOrder", "a");
   TNBApiUserView("4200999", "TNBApiTestOrder", "b");
   TNBApiUserView("4300777", "TNBApiTestOrder", "c");
}

function TNBApiTestOrder(%ctx, %status, %result)
{
   $TNBApiTest::Order = $TNBApiTest::Order @ %ctx;
   if (%ctx $= "c")
   {
      TNBApiTestEq("queued requests keep order", $TNBApiTest::Order, "abc");
      TNBApiTestEq("last queued result", TNBJsonStr(%result, "name"), "orangeade");

      // Finally, an unreachable host must fail cleanly rather than hang.
      $TNB::Host = "http://127.0.0.1:9";
      TNBSessionEnd();
      TNBApiInit();
      TNBApiUserView("4510186", "TNBApiTestUnreachable", "");
   }
}

function TNBApiTestUnreachable(%ctx, %status, %result)
{
   TNBApiTestEq("unreachable host errors", %status, "error");
   echo("   (error was: " @ %result @ ")");

   // The waiter table must survive a client that has never ended a session.
   // $TNB::WaitCount starts unset, and "" is not 0 as an array index: the first
   // waiter used to land at $TNB::WaitFn[""] while the count advanced to 1, so
   // the flush called index 0, found nothing, and dropped it. That cost the
   // first request of the session -- the mail window sat on "Checking for
   // mail..." until the folder was clicked again.
   //
   // Every suite calls TNBSessionEnd, which resets the count, so this is forced
   // by hand: it is the one state the tests could not otherwise reach.
   $TNB::WaitCount = "";
   $TNB::WaitFn[0] = "";
   $TNB::UUID = "";
   TNBSessionOnReady("TNBApiTestWaiterProbe", "probe");
   TNBApiTestEq("waiter lands at a numeric index",
                $TNB::WaitFn[0], "TNBApiTestWaiterProbe");
   TNBApiTestEq("and the count advances from it", $TNB::WaitCount, 1);

   // TribesNext is the identity provider and nothing else. The login is the
   // only URI that may be resolved against $TNB::AuthHost; everything else --
   // browser, clan, mail -- belongs to the backend. Asserted here because it is
   // a rule about the whole client rather than any one call, and the community
   // certificate that used to break it was removed for exactly this reason.
   TNBApiTestEq("login is the only AuthHost URI",
                $TNB::LoginURI, "/tn/robot/robot_login.php");

   echo("");
   echo("TNBAPIRESULT pass=" @ $TNBApiTest::Pass @ " fail=" @ $TNBApiTest::Fail);
}

// Never called; it only has to exist so the probe above stores a real name.
function TNBApiTestWaiterProbe(%ctx, %status, %reason) { }

echo("TNBrowser: api_test.cs loaded");
