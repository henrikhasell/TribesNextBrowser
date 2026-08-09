// TNBrowser -- self-test for the JSON parser.
//
// Run from the telnet console:
//    exec("tnbrowser/json.cs"); exec("tests/json_test.cs"); TNBJsonSelfTest();
//
// Every case prints PASS or FAIL with the expectation, so a run is readable in
// the console log without a test harness.

function TNBTestEq(%name, %got, %want)
{
   if (%got $= %want)
   {
      $TNBTest::Pass++;
      echo("PASS " @ %name);
   }
   else
   {
      $TNBTest::Fail++;
      error("FAIL " @ %name @ " -- got [" @ %got @ "] want [" @ %want @ "]");
   }
}

// Engine primitives the parser leans on. If any of these are wrong the parser
// is built on sand, so check them explicitly rather than inferring from a
// downstream failure.
function TNBTestPrimitives()
{
   echo("--- engine primitives ---");
   TNBTestEq("strcmp gives codepoint", strcmp("A", ""), 65);
   TNBTestEq("strstr finds index", strstr("0123456789abcdef", "c"), 12);
   TNBTestEq("strstr missing is -1", strstr("abc", "z"), -1);
   TNBTestEq("ternary works", (1 ? "yes" : "no"), "yes");
   TNBTestEq("hex to dec", TNBHexToDec("ff"), 255);
   TNBTestEq("hex to dec 4 digit", TNBHexToDec("0041"), 65);
   TNBTestEq("DecToHex round trip", TNBHexToDec(DecToHex(1234)), 1234);

   // Unnamed ScriptObject with a class field -- the node representation.
   %n = TNBJsonNewNode("string");
   TNBTestEq("node is object", isObject(%n), 1);
   %n.key[0] = "alpha";
   %n.val[0] = 77;
   TNBTestEq("array field key", %n.key[0], "alpha");
   TNBTestEq("array field val", %n.val[0], 77);
   %n.delete();
}

function TNBTestScalars()
{
   echo("--- scalars ---");

   %r = TNBJsonParse("\"hello\"");
   TNBTestEq("string type", TNBJsonType(%r), "string");
   TNBTestEq("string value", TNBJsonValue(%r), "hello");
   TNBJsonFree(%r);

   %r = TNBJsonParse("42");
   TNBTestEq("number value", TNBJsonValue(%r), "42");
   TNBJsonFree(%r);

   %r = TNBJsonParse("-3.5e2");
   TNBTestEq("negative exponent number", TNBJsonValue(%r), "-3.5e2");
   TNBJsonFree(%r);

   %r = TNBJsonParse("true");
   TNBTestEq("bool true", TNBJsonValue(%r), 1);
   TNBJsonFree(%r);

   %r = TNBJsonParse("null");
   TNBTestEq("null type", TNBJsonType(%r), "null");
   TNBJsonFree(%r);
}

function TNBTestObjects()
{
   echo("--- objects and arrays ---");

   %r = TNBJsonParse("{\"status\":\"success\"}");
   TNBTestEq("simple member", TNBJsonStr(%r, "status"), "success");
   TNBTestEq("missing member is empty", TNBJsonStr(%r, "nope"), "");
   TNBJsonFree(%r);

   %r = TNBJsonParse("{}");
   TNBTestEq("empty object count", TNBJsonCount(%r), 0);
   TNBJsonFree(%r);

   %r = TNBJsonParse("[]");
   TNBTestEq("empty array count", TNBJsonCount(%r), 0);
   TNBTestEq("empty array type", TNBJsonType(%r), "array");
   TNBJsonFree(%r);

   %r = TNBJsonParse("[1,2,3]");
   TNBTestEq("array count", TNBJsonCount(%r), 3);
   TNBTestEq("array element", TNBJsonValue(TNBJsonIndex(%r, 1)), "2");
   TNBTestEq("out of range index", TNBJsonIndex(%r, 9), 0);
   TNBJsonFree(%r);

   // Whitespace between every token.
   %r = TNBJsonParse("  {  \"a\" :  [ 1 , { \"b\" : \"c\" } ]  }  ");
   %inner = TNBJsonIndex(TNBJsonGet(%r, "a"), 1);
   TNBTestEq("nested through whitespace", TNBJsonStr(%inner, "b"), "c");
   TNBJsonFree(%r);
}

function TNBTestEscapes()
{
   echo("--- escapes ---");

   %r = TNBJsonParse("\"a\\\"b\"");
   TNBTestEq("escaped quote", TNBJsonValue(%r), "a\"b");
   TNBJsonFree(%r);

   %r = TNBJsonParse("\"a\\\\b\"");
   TNBTestEq("escaped backslash", TNBJsonValue(%r), "a\\b");
   TNBJsonFree(%r);

   %r = TNBJsonParse("\"a\\/b\"");
   TNBTestEq("escaped solidus", TNBJsonValue(%r), "a/b");
   TNBJsonFree(%r);

   %r = TNBJsonParse("\"line\\nbreak\"");
   TNBTestEq("newline escape", TNBJsonValue(%r), "line\nbreak");
   TNBJsonFree(%r);

   %r = TNBJsonParse("\"\\u0041\\u0042\"");
   TNBTestEq("unicode ascii", TNBJsonValue(%r), "AB");
   TNBJsonFree(%r);

   %r = TNBJsonParse("\"\\u00e9\"");
   TNBTestEq("unicode non-ascii becomes ?", TNBJsonValue(%r), "?");
   TNBJsonFree(%r);

   // A run of plain text on both sides of an escape, exercising the
   // copy-the-run-in-one-go path.
   %r = TNBJsonParse("\"prefix text \\\" suffix text\"");
   TNBTestEq("runs around escape", TNBJsonValue(%r), "prefix text \" suffix text");
   TNBJsonFree(%r);
}

function TNBTestErrors()
{
   echo("--- malformed input ---");

   TNBTestEq("unterminated string", TNBJsonParse("\"abc"), 0);
   TNBTestEq("missing brace", TNBJsonParse("{\"a\":1"), 0);
   TNBTestEq("bare word", TNBJsonParse("notjson"), 0);
   TNBTestEq("trailing garbage", TNBJsonParse("{} junk"), 0);
   TNBTestEq("empty input", TNBJsonParse(""), 0);

   // An HTML error page is the realistic failure the backend produces.
   TNBTestEq("html error page", TNBJsonParse("<h1>Fatal Error</h1>"), 0);
}

// The shapes the GUI actually consumes, taken from json_browser.phps.
function TNBTestRealPayloads()
{
   echo("--- real API shapes ---");

   %userview = "{\"guid\":\"4510186\",\"name\":\"orange01\",\"tag\":\"[TN]\"," @
               "\"append\":\"0\",\"creation\":\"1300000000\"," @
               "\"website\":\"www.example.com\",\"info\":\"Hello \\\"world\\\"\"," @
               "\"online\":\"1\",\"memberships\":[" @
               "{\"id\":\"7\",\"name\":\"Test Clan\",\"rank\":\"4\",\"title\":\"Leader\"," @
               "\"tag\":\"[TC]\",\"append\":\"0\"}," @
               "{\"id\":\"9\",\"name\":\"Other\",\"rank\":\"1\",\"title\":\"Member\"," @
               "\"tag\":\"-O-\",\"append\":\"1\"}]}";

   %r = TNBJsonParse(%userview);
   TNBTestEq("userview name", TNBJsonStr(%r, "name"), "orange01");
   TNBTestEq("userview info with quotes", TNBJsonStr(%r, "info"), "Hello \"world\"");
   TNBTestEq("userview online bool", TNBJsonBool(%r, "online"), 1);
   %m = TNBJsonGet(%r, "memberships");
   TNBTestEq("membership count", TNBJsonCount(%m), 2);
   TNBTestEq("second membership title", TNBJsonStr(TNBJsonIndex(%m, 1), "title"), "Member");
   TNBJsonFree(%r);

   %search = "[{\"guid\":\"1\",\"name\":\"aaa\",\"tag\":\"\",\"append\":\"0\"}," @
             "{\"guid\":\"2\",\"name\":\"aab\",\"tag\":\"x\",\"append\":\"1\"}]";
   %r = TNBJsonParse(%search);
   TNBTestEq("search result count", TNBJsonCount(%r), 2);
   TNBTestEq("search empty tag", TNBJsonStr(TNBJsonIndex(%r, 0), "tag"), "");
   TNBJsonFree(%r);

   // The shape the *live* backend actually returns, which differs from the
   // documentation in ways worth pinning down: absent strings come back as
   // JSON null rather than "", and "online" is a bare number rather than a
   // quoted string. Captured from a real userview against tribesnext.thyth.com.
   %live = "{\"guid\":\"4510186\",\"name\":\"orange01\",\"tag\":null,\"append\":null," @
           "\"creation\":\"1786127938\",\"website\":\"www.tribesnext.com\"," @
           "\"info\":\"\",\"memberships\":[],\"online\":1}";
   %r = TNBJsonParse(%live);
   TNBTestEq("live name", TNBJsonStr(%r, "name"), "orange01");
   TNBTestEq("live null tag reads empty", TNBJsonStr(%r, "tag"), "");
   TNBTestEq("live null tag type", TNBJsonType(TNBJsonGet(%r, "tag")), "null");
   TNBTestEq("live null append is falsey", TNBJsonBool(%r, "append"), 0);
   TNBTestEq("live numeric online is true", TNBJsonBool(%r, "online"), 1);
   TNBTestEq("live empty memberships", TNBJsonCount(TNBJsonGet(%r, "memberships")), 0);
   TNBTestEq("live tagged name falls back to plain",
             TNBTaggedName(TNBJsonStr(%r, "name"), TNBJsonStr(%r, "tag"),
                           TNBJsonBool(%r, "append")), "orange01");
   TNBJsonFree(%r);

   // The live server prefixes its body with a blank line; the API layer trims
   // before parsing, but the parser must cope with leading whitespace anyway.
   %r = TNBJsonParse("\n{\"status\":\"success\"}");
   TNBTestEq("leading newline tolerated", TNBJsonStr(%r, "status"), "success");
   TNBJsonFree(%r);

   %err = "{\"status\":\"error\",\"msg\":\"not authorized\"}";
   %r = TNBJsonParse(%err);
   TNBTestEq("error status", TNBJsonStr(%r, "status"), "error");
   TNBTestEq("error message", TNBJsonStr(%r, "msg"), "not authorized");
   TNBJsonFree(%r);
}

// A profile body of a few kilobytes, which is the realistic worst case for the
// string scanner.
function TNBTestLargeBody()
{
   echo("--- large payload ---");

   %chunk = "The quick brown fox jumps over the lazy dog. ";
   %body = "";
   for (%i = 0; %i < 100; %i++)
      %body = %body @ %chunk;

   %doc = "{\"info\":\"" @ %body @ "\"}";
   %start = getSimTime();
   %r = TNBJsonParse(%doc);
   %elapsed = getSimTime() - %start;

   TNBTestEq("large body length", strlen(TNBJsonStr(%r, "info")), strlen(%body));
   echo("   (" @ strlen(%doc) @ " bytes parsed in " @ %elapsed @ " ms)");
   TNBJsonFree(%r);
}

// TNBJsonFree must delete the whole tree, not just the root. Object ids are
// never recycled in this engine, so the check is that each recorded node stops
// being an object -- not that ids get reused.
function TNBTestNoLeak()
{
   echo("--- leak check ---");

   %doc = "{\"a\":[{\"b\":\"c\"},{\"d\":[1,2,3]}]}";
   %r = TNBJsonParse(%doc);

   %arr        = TNBJsonGet(%r, "a");
   %firstElem  = TNBJsonIndex(%arr, 0);
   %secondElem = TNBJsonIndex(%arr, 1);
   %deepArray  = TNBJsonGet(%secondElem, "d");
   %deepScalar = TNBJsonIndex(%deepArray, 2);

   TNBTestEq("deep scalar alive before free", isObject(%deepScalar), 1);

   TNBJsonFree(%r);

   TNBTestEq("root freed", isObject(%r), 0);
   TNBTestEq("array child freed", isObject(%arr), 0);
   TNBTestEq("first element freed", isObject(%firstElem), 0);
   TNBTestEq("nested array freed", isObject(%deepArray), 0);
   TNBTestEq("deepest scalar freed", isObject(%deepScalar), 0);

   // And repeated parse/free cycles must stay stable.
   for (%i = 0; %i < 50; %i++)
   {
      %n = TNBJsonParse(%doc);
      TNBJsonFree(%n);
   }
   TNBTestEq("survives repeated cycles", isObject(%n), 0);
}

function TNBJsonSelfTest()
{
   $TNBTest::Pass = 0;
   $TNBTest::Fail = 0;

   TNBTestPrimitives();
   TNBTestScalars();
   TNBTestObjects();
   TNBTestEscapes();
   TNBTestErrors();
   TNBTestRealPayloads();
   TNBTestLargeBody();
   TNBTestNoLeak();

   echo("");
   echo("TNBJSONRESULT pass=" @ $TNBTest::Pass @ " fail=" @ $TNBTest::Fail);
}

echo("TNBrowser: json_test.cs loaded");
