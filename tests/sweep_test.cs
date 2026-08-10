// TNBrowser -- the ordinal sweep.
//
// Issues all 61 (form, ordinal) pairs the five shipped community scripts can
// reach, in their shipped call form, through the real DatabaseQuery -- and
// checks that each one comes back through the client's own delivery path.
//
//   exec("tests/sweep_test.cs"); TNBSweepSelfTest("http://172.17.0.1:8099");
//
// What this proves and what it does not:
//
// It proves the framing and the routing. Every ordinal reaches a handler, every
// handler answers, and the answer arrives as an onDatabaseQueryResult followed
// by the rows -- which is the only contract the shipped panes have.
//
// It does not prove a single field index. A row with its fields in the wrong
// order sweeps exactly as cleanly as a correct one; only rendering it does
// that, which is what browser_test.cs and mail_test.cs are for. The two are
// complementary and neither is sufficient.
//
// The assertion is deliberately not "status 0". Several ordinals correctly
// REFUSE for this fixture account -- it is not WON staff, so the moderation
// ordinals say so, and it is already a member of Test Clan, so asking that
// tribe for an invitation is refused. A refusal is a real answer. What must
// never happen is the "does not implement" status, or no answer at all.

function TNBSweepEq(%name, %got, %want)
{
   if (%got $= %want)
   {
      $TNBSweep::Pass++;
      echo("PASS " @ %name);
   }
   else
   {
      $TNBSweep::Fail++;
      $TNBSweep::Failures = $TNBSweep::Failures @ %name @
         " (got [" @ %got @ "] want [" @ %want @ "])\n";
      error("FAIL " @ %name @ " -- got [" @ %got @ "] want [" @ %want @ "]");
   }
}

//-----------------------------------------------------------------------------
// The probe table
//-----------------------------------------------------------------------------
//
// Each entry is the call form the shipped script uses, not a convenient one, so
// a transcript of this sweep never shows a request the game does not make.

function TNBSweepAdd(%form, %ordinal, %args)
{
   %n = $TNBSweep::Count;
   $TNBSweep::Form[%n] = %form;
   $TNBSweep::Ordinal[%n] = %ordinal;
   $TNBSweep::Args[%n] = %args;
   $TNBSweep::Count = %n + 1;
}

function TNBSweepBuild()
{
   $TNBSweep::Count = 0;

   // -- news (webnews.cs). No controls exist for this pane in a retail
   //    install, so the sweep is the only thing that will ever call these.
   TNBSweepAdd("scalar", 0, "");
   TNBSweepAdd("scalar", 1, "105" TAB "Sweep headline" TAB "Sweep body");
   TNBSweepAdd("scalar", 2, "501" TAB "105" TAB "Edited" TAB "Body");
   TNBSweepAdd("scalar", 3, "501");
   TNBSweepAdd("scalar", 4, "Swept.");
   TNBSweepAdd("array", 0, "1" TAB "105");
   TNBSweepAdd("array", 100, "0" TAB "0" TAB "105");

   // -- web links (weblinks.cs). Same: driven by a script, defined in no .gui.
   TNBSweepAdd("array", 15, "WEBLINK");

   // -- email (webemail.cs)
   TNBSweepAdd("array", 1, "0");
   TNBSweepAdd("array", 2, "");
   TNBSweepAdd("array", 14, "");
   TNBSweepAdd("scalar", 5, "Shifter" TAB "" TAB "Sweep" TAB "Hello from the sweep.");
   TNBSweepAdd("scalar", 9, "Ravage");
   TNBSweepAdd("scalar", 8, "Ravage");
   TNBSweepAdd("scalar", 6, "12");
   TNBSweepAdd("scalar", 35, "9");

   // -- shared between the two panes
   TNBSweepAdd("array", 3, "orange" TAB "0" TAB "100" TAB "1");
   TNBSweepAdd("array", 5, "");
   TNBSweepAdd("array", 6, "Test Clan");
   TNBSweepAdd("scalar", 10, "Ravage");
   TNBSweepAdd("scalar", 11, "Ravage");
   TNBSweepAdd("scalar", 69, "4510186" TAB "4120041");

   // -- browser (webbrowser.cs)
   TNBSweepAdd("array", 4, "Test" TAB "0" TAB "100" TAB "0");
   TNBSweepAdd("array", 10, "Test Clan");
   TNBSweepAdd("array", 11, "Test Clan");
   TNBSweepAdd("array", 12, "orange01");
   TNBSweepAdd("array", 13, "Shifter");
   TNBSweepAdd("scalar", 22, "Test Clan");
   TNBSweepAdd("scalar", 23, "orange01");
   TNBSweepAdd("scalar", 15, "Test Clan" TAB "1" TAB "Swept description.");
   TNBSweepAdd("scalar", 16, "Sweep Tribe" TAB "[SW]" TAB "0");
   TNBSweepAdd("scalar", 17, "Swept warrior description.");
   TNBSweepAdd("scalar", 18, "Casual Alliance");
   TNBSweepAdd("scalar", 19, "Shifter" TAB "Test Clan" TAB "0");
   TNBSweepAdd("scalar", 20, "Recruiting" TAB "Test Clan" TAB "1");
   TNBSweepAdd("scalar", 21, "Test Clan" TAB "Shifter" TAB "2" TAB "Officer");
   TNBSweepAdd("scalar", 24, "Casual Alliance");
   TNBSweepAdd("scalar", 25, "Test Clan");
   TNBSweepAdd("scalar", 26, "clearBuddy");
   TNBSweepAdd("scalar", 27, "Test Clan" TAB "Ravage");
   TNBSweepAdd("scalar", 28, "cancel" TAB "Test Clan" TAB "Ravage");
   TNBSweepAdd("scalar", 29, "Test Clan" TAB "texticons/twb/twb_Laserrifle.jpg");
   TNBSweepAdd("scalar", 30, "Test Clan" TAB "[TC]");
   TNBSweepAdd("scalar", 31, "texticons/twb/twb_Missilelauncher.jpg");
   TNBSweepAdd("scalar", 32, "www.example.com");
   TNBSweepAdd("scalar", 33, "sweptname");
   TNBSweepAdd("scalar", 34, "Test Clan");
   TNBSweepAdd("scalar", 63, "0" TAB "swept");

   // -- forums (webforums.cs)
   TNBSweepAdd("array", 7, "");
   TNBSweepAdd("array", 8, "1");
   TNBSweepAdd("array", 9, "71" TAB "0");
   TNBSweepAdd("scalar", 12, "1" TAB "71" TAB "0" TAB "Sweep" TAB "Body");
   TNBSweepAdd("scalar", 13, "900" TAB "Sweep" TAB "Body");
   TNBSweepAdd("scalar", 14, "900");
   TNBSweepAdd("scalar", 60, "1" TAB "71" TAB "0");
   TNBSweepAdd("scalar", 61, "1" TAB "71" TAB "900" TAB "4510186");
   TNBSweepAdd("scalar", 62, "0" TAB "71" TAB "0");
   TNBSweepAdd("scalar", 66, "71" TAB "Swept");
   TNBSweepAdd("scalar", 67, "71");
   TNBSweepAdd("scalar", 68, "71" TAB "1" TAB "General Discussion");
}

//-----------------------------------------------------------------------------
// The probe object
//-----------------------------------------------------------------------------
//
// Deliberately the same shape as ProxyEchoTest (webstuff.cs:64), because that
// is what the shipped scripts prove a proxy object has to be: a ScriptObject
// with onDatabaseQueryResult and onDatabaseRow.

function TNBSweepProbe::onDatabaseQueryResult(%this, %status, %result, %key)
{
   $TNBSweep::Status[%key] = %status;
   $TNBSweep::Result[%key] = %result;
   $TNBSweep::Answered[%key] = 1;
   $TNBSweep::Answers++;
}

function TNBSweepProbe::onDatabaseRow(%this, %row, %isLast, %key)
{
   $TNBSweep::Rows[%key]++;
   if (%isLast)
      $TNBSweep::LastSeen[%key]++;

   // Keep the first row of each so a failure says what came back rather than
   // only that something did.
   if ($TNBSweep::Rows[%key] == 1)
      $TNBSweep::FirstRow[%key] = %row;
}

//-----------------------------------------------------------------------------

function TNBSweepSelfTest(%host)
{
   $TNBSweep::Pass = 0;
   $TNBSweep::Fail = 0;
   $TNBSweep::Done = 0;
   $TNBSweep::Failures = "";
   $TNBSweep::Answers = 0;

   $TNB::Host = %host;
   $TNB::AuthHost = %host;

   // A ScriptObject must never be constructed at file scope -- that is global
   // scope, where %locals do not exist and `new` takes the engine down.
   if (!isObject(TNBSweepProbe))
      new ScriptObject(TNBSweepProbe) { class = TNBSweepProbe; };

   TNBSweepBuild();
   // 60 here plus the one no-proxy call below, which cannot carry a key and so
   // cannot be tracked in this table.
   TNBSweepEq("the table covers every trackable ordinal", $TNBSweep::Count, 60);

   for (%i = 0; %i < $TNBSweep::Count; %i++)
   {
      $TNBSweep::Answered[%i] = 0;
      $TNBSweep::Rows[%i] = 0;
      $TNBSweep::LastSeen[%i] = 0;
      $TNBSweep::Status[%i] = "";

      if ($TNBSweep::Form[%i] $= "scalar")
         DatabaseQuery($TNBSweep::Ordinal[%i], $TNBSweep::Args[%i],
                       TNBSweepProbe, %i);
      else
         DatabaseQueryArray($TNBSweep::Ordinal[%i], 0, $TNBSweep::Args[%i],
                            TNBSweepProbe, %i);
   }

   // The one call form that passes neither a proxy object nor a key
   // (webemail.cs:1328). Reproduced because it is the shipped form, and
   // because a shim that assumed a proxy object would crash on exactly this
   // one query and nothing else.
   DatabaseQuery(7, "11");

   TNBSweepWait(0);
}

// The queue runs one request at a time, so the sweep finishes when the answers
// arrive rather than after any fixed delay.
function TNBSweepWait(%ticks)
{
   if ($TNBSweep::Answers >= $TNBSweep::Count)
   {
      TNBSweepCheck();
      return;
   }
   if (%ticks > 200)
   {
      TNBSweepEq("every ordinal answered", $TNBSweep::Answers, $TNBSweep::Count);
      TNBSweepCheck();
      return;
   }
   schedule(250, 0, "TNBSweepWait", %ticks + 1);
}

function TNBSweepCheck()
{
   %answered = 0;
   %wellFormed = 0;
   %unimplemented = "";
   %lastFlagWrong = "";
   %bareOK = "";

   for (%i = 0; %i < $TNBSweep::Count; %i++)
   {
      %what = $TNBSweep::Form[%i] SPC $TNBSweep::Ordinal[%i];
      %status = $TNBSweep::Status[%i];

      if ($TNBSweep::Answered[%i])
         %answered++;

      // Field 0 is the code every shipped handler tests first, and it has to
      // be there whether the answer succeeded or refused.
      if (getFieldCount(%status) >= 2 && getField(%status, 1) !$= "")
         %wellFormed++;

      if (strstr(%status, "does not implement") >= 0)
         %unimplemented = %unimplemented @ %what @ " ";

      // Field 1 of a success is the sentence seven shipped handlers put
      // straight into a MessageBoxOK with no wording of their own
      // (webbrowser.cs:927, :1725, :1781, :1784, :1808, webemail.cs:704,
      // :706). It was the literal word "OK", so confirming an invitation, a
      // graphic, a web address or a buddy each produced a dialog reading "OK"
      // and nothing else.
      //
      // Scalars only: an array's status is never displayed -- the pane goes to
      // the row count -- and inventing prose per list would be words nobody
      // reads, so those keep the bare fallback deliberately.
      if ($TNBSweep::Form[%i] $= "scalar" && getField(%status, 0) == 0 &&
          getField(%status, 1) $= "OK")
         %bareOK = %bareOK @ %what @ " ";

      // isLast marks the end of a result set, so it must fire exactly once for
      // anything that returned rows -- and not at all for anything that did
      // not, which is what the shipped client does.
      if ($TNBSweep::Rows[%i] > 0 && $TNBSweep::LastSeen[%i] != 1)
         %lastFlagWrong = %lastFlagWrong @ %what @ " ";
      if ($TNBSweep::Rows[%i] == 0 && $TNBSweep::LastSeen[%i] != 0)
         %lastFlagWrong = %lastFlagWrong @ %what @ "(empty) ";
   }

   TNBSweepEq("every ordinal answered", %answered, $TNBSweep::Count);
   TNBSweepEq("every answer carries a code and a message",
              %wellFormed, $TNBSweep::Count);
   TNBSweepEq("no ordinal is unimplemented", %unimplemented, "");
   TNBSweepEq("no scalar answers a bare OK where a sentence is shown",
              %bareOK, "");
   TNBSweepEq("isLast fires exactly once per non-empty result",
              %lastFlagWrong, "");

   // Spot-check the two ordinals that carry their whole payload in the status
   // rather than in rows, because a shim that dropped status fields 2.. would
   // otherwise sweep clean and render two blank profiles.
   TNBSweepEq("the tribe profile carries its header in the status",
              getFieldCount(TNBSweepStatusFor("scalar", 22)) >= 8, 1);
   TNBSweepEq("the warrior profile carries its header in the status",
              getFieldCount(TNBSweepStatusFor("scalar", 23)) >= 10, 1);

   $TNBSweep::Done = 1;
   echo("TNBSWEEPRESULT pass=" @ $TNBSweep::Pass @ " fail=" @ $TNBSweep::Fail);
}

function TNBSweepStatusFor(%form, %ordinal)
{
   for (%i = 0; %i < $TNBSweep::Count; %i++)
      if ($TNBSweep::Form[%i] $= %form && $TNBSweep::Ordinal[%i] == %ordinal)
         return $TNBSweep::Status[%i];
   return "";
}

echo("TNBrowser: sweep_test.cs loaded");
