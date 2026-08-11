// TNBrowser -- chat, driven through the shipped IRC client
//
//   exec("tests/chat_test.cs"); TNBChatSelfTest("http://172.17.0.1:8099");
//
// What this proves is that the shipped chat client works over an HTTPS stream
// it knows nothing about. Every assertion below reads shipped state -- the
// ChannelVector for a room, the person list, the flags IRCClient::findChannel
// sets -- rather than anything this mod owns, because the mod owning the
// transport is the entire claim.
//
// The last assertion is the one to keep. $IRCClient::connectwait is a counter
// raised by IRCClient::connecting() and lowered by IRCClient::connected(), and
// several client actions raise it: a JOIN, a LIST, a WHO, a ban-list request.
// Only specific replies lower it again, so a backend that answers a LIST
// without a 323 leaves the chat panes titled "CONNECTING" for the rest of the
// session with no way back. A non-zero count at the end means we emitted a
// request the backend did not finish.
//
// A step machine rather than a straight line, because every exchange is a round
// trip: each step schedules the next and $TNBChatTest::Done marks the end so a
// runner can wait on it.

function TNBChatEq(%name, %got, %want)
{
   if (%got $= %want)
   {
      $TNBChatTest::Pass++;
      echo("PASS " @ %name);
   }
   else
   {
      $TNBChatTest::Fail++;
      $TNBChatTest::Failures = $TNBChatTest::Failures @ %name @
         " (got [" @ %got @ "] want [" @ %want @ "])\n";
      echo("FAIL " @ %name @ " (got [" @ %got @ "] want [" @ %want @ "])");
   }
}

function TNBChatTrue(%name, %got)
{
   TNBChatEq(%name, %got ? 1 : 0, 1);
}

// How many lines in a room's pane contain this text.
//
// A count and not a search, because the failure worth catching here is a
// message arriving twice: IRCClient::send2 echoes what a player sends as it
// sends it, so a backend that reflects it back shows it once for each.
function TNBChatCount(%channel, %text)
{
   %c = IRCClient::findChannel(%channel);
   if (!isObject(%c))
      return -1;

   %hits = 0;
   for (%i = 0; %i < %c.getNumLines(); %i++)
      if (strstr(%c.getLineText(%i), %text) != -1)
         %hits++;

   return %hits;
}

function TNBChatSelfTest(%host)
{
   $TNBChatTest::Pass = 0;
   $TNBChatTest::Fail = 0;
   $TNBChatTest::Failures = "";
   $TNBChatTest::Done = 0;

   if (%host !$= "")
      $TNB::Host = %host;
   $TNB::ChatRoom = "#Tribes2";

   // Two callers, one negotiation.
   //
   // This is here rather than in a session suite because chat is what made it
   // matter: until chat existed the request queue was the only thing that ever
   // asked for a session, and its own guard was enough. Chat asks on a timer,
   // so two starts can overlap -- and a second start mints a new nonce, which
   // makes the CHALLENGE already in flight fail its replay check
   // (session.cs:281). The exchange then restarts forever and every pane in the
   // mod is dead, because mail, the browser and chat all wait on the same
   // token.
   //
   // Invisible against a backend started with -dev-trust-guid, which answers
   // the first request with a session and never issues a challenge at all. That
   // is why this asserts on the nonce rather than on the outcome.
   TNBSessionEnd();
   TNBSessionStart();
   $TNBChatTest::Nonce = $TNB::Nonce;
   TNBSessionStart();
   TNBChatEq("a second start does not replace the nonce",
             $TNB::Nonce, $TNBChatTest::Nonce);

   // A clean slate: the suite may be re-run against a client that is already
   // connected, and the mock forgets a connection the moment its stream ends.
   IRCClient::disconnect();
   IRCClient::init();

   // Both halves of the real path. ChatGui.cs calls connect() at boot, which
   // only arms the flag; TNBCertEnsure calls the nudge when a pane is opened
   // with an account in hand, and that is what opens the stream.
   IRCClient::connect();
   TNBChatNudge();

   $TNBChatTest::Waited = 0;
   $TNBChatTest::Step = schedule(500, 0, "TNBChatStep1");
}

// Connected, with an identity.
//
// Polled rather than waited out: connecting can mean negotiating a session
// first, and a fixed delay long enough for that on a slow day is a fixed delay
// wasted on every other run.
function TNBChatStep1()
{
   if ($IRCClient::state !$= IDIRC_CONNECTED && $TNBChatTest::Waited < 40)
   {
      $TNBChatTest::Waited++;
      $TNBChatTest::Step = schedule(500, 0, "TNBChatStep1");
      return;
   }

   TNBChatEq("state is connected", $IRCClient::state, IDIRC_CONNECTED);

   %me = $IRCClient::people.getObject(0);

   // The wire identity is the triple; the rendered one is not. Both matter:
   // findPerson matches on displayName, and a private message has to be
   // addressed to it.
   TNBChatEq("wire nick", %me.displayName, "orange01^[TC]^0");

   // append is 0 for this tribe, so the tag renders in front of the name.
   TNBChatEq("rendered nick", %me.nick, "[TC]orange01");
   TNBChatEq("tagged nick", %me.tagged,
             "<tribe:1>[TC]</tribe><tribe:0>orange01</tribe>");

   $TNBChatTest::Step = schedule(1500, 0, "TNBChatStep2");
}

// The room named in $TNB::ChatRoom was joined on connecting.
function TNBChatStep2()
{
   %c = IRCClient::findChannel("#Tribes2");
   TNBChatTrue("auto-joined the default room", isObject(%c));

   // That *we* are in it, not that we are alone in it. The hub is in-process
   // and outlives a suite run, so a backend somebody else is connected to --
   // another container, a curl session, a developer -- would otherwise fail a
   // count that has nothing to do with what is being tested.
   TNBChatTrue("and is in it",
               %c.findMember($IRCClient::people.getObject(0)) >= 0);
   TNBChatEq("shown without its hash", IRCClient::displayChannel("#Tribes2"),
             "Tribes2");

   IRCClient::send2("shazbot", "#Tribes2");
   $TNBChatTest::Step = schedule(1500, 0, "TNBChatStep3");
}

// A message the player sent appears exactly once.
function TNBChatStep3()
{
   TNBChatEq("own message shown once", TNBChatCount("#Tribes2", "shazbot"), 1);

   // A tribe room, addressed by the tribe's name with its spaces escaped --
   // which is what JoinPublicTribeChannel builds, through
   // IRCClient::channelName.
   IRCClient::join(IRCClient::channelName("Test Clan") @ "_Public");
   $TNBChatTest::Step = schedule(1500, 0, "TNBChatStep4");
}

// The tribe room exists, is flagged as one, and carries the tribe's name.
function TNBChatStep4()
{
   %c = IRCClient::findChannel("#Test_-_01Clan_Public");
   TNBChatTrue("joined the tribe room", isObject(%c));
   TNBChatEq("flagged as a tribe room", %c.tribe, 1);
   TNBChatEq("topic is the tribe", %c.topic, "Test Clan");

   // The escaped name is unescaped for display, so a tribe with a space in it
   // is not shown with "_-_01" in the middle of it.
   TNBChatEq("shown unescaped",
             IRCClient::displayChannel("#Test_-_01Clan_Public"),
             "Test Clan_Public");

   // A tribe room belonging to a tribe this warrior is not in. The refusal is
   // a 473, which IRCClient::onChannelInviteOnly turns into a status message --
   // and, importantly, clears the wait state the JOIN raised.
   IRCClient::join("#Somebody_-_01Else_Private");
   $TNBChatTest::Step = schedule(1500, 0, "TNBChatStep5");
}

function TNBChatStep5()
{
   TNBChatTrue("refused a room we are not a member of",
               !isObject(IRCClient::findChannel("#Somebody_-_01Else_Private")));

   // The channel list the JOIN CHAT dialog is built from.
   $IRCClient::numChannels = 0;
   IRCClient::requestChannelList();
   $TNBChatTest::Step = schedule(1500, 0, "TNBChatStep6");
}

function TNBChatStep6()
{
   TNBChatTrue("channel list arrived", $IRCClient::numChannels > 0);

   // Every wait state raised above was cleared. See the note at the top: this
   // is the assertion that catches a missing terminator numeric, and nothing
   // else in the suite would notice one.
   TNBChatEq("no wait state left behind", $IRCClient::connectwait, 0);

   echo("TNBCHATRESULT pass=" @ $TNBChatTest::Pass @
        " fail=" @ $TNBChatTest::Fail);
   $TNBChatTest::Done = 1;
}

echo("TNBrowser: chat_test.cs loaded");
