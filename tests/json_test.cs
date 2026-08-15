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

   // How the accessors read a member that is not the type the caller assumed.
   // A parser has to answer these whatever the backend of the day sends, and a
   // reader that treats a null as a missing string, or a bare number as false,
   // fails in a way that looks like missing data rather than a type error.
   %mixed = "{\"a\":null,\"n\":1,\"z\":0,\"s\":\"x\",\"e\":[]}";
   %r = TNBJsonParse(%mixed);
   TNBTestEq("null member reads as empty", TNBJsonStr(%r, "a"), "");
   TNBTestEq("null member keeps its type", TNBJsonType(TNBJsonGet(%r, "a")), "null");
   TNBTestEq("null member is falsey", TNBJsonBool(%r, "a"), 0);
   TNBTestEq("bare number is true when nonzero", TNBJsonBool(%r, "n"), 1);
   TNBTestEq("bare zero is false", TNBJsonBool(%r, "z"), 0);
   TNBTestEq("string member is unaffected", TNBJsonStr(%r, "s"), "x");
   TNBTestEq("empty array member counts zero", TNBJsonCount(TNBJsonGet(%r, "e")), 0);
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

   // Not JSON at all. A proxy, a captive portal or an edge error page is the
   // realistic way this happens -- the backend itself always answers JSON.
   TNBTestEq("html error page", TNBJsonParse("<h1>Fatal Error</h1>"), 0);
}

// The shapes that actually cross the wire, and the one decode built on top of
// them.
function TNBTestRealPayloads()
{
   echo("--- real API shapes ---");

   // Every /db answer. The status is tab-separated: field 0 is the code the
   // shipped onDatabaseQueryResult tests, field 1 a sentence a pane may show.
   // Rows are opaque strings, themselves tab-separated, and the parser must
   // hand them back with the tabs intact.
   %answer = "{\"status\":\"0\tOK\",\"result\":\"2\",\"rows\":[" @
             "\"7\tTest Clan\t[TC]\",\"9\tCasual Alliance\t-CA-\"]}";

   %r = TNBJsonParse(%answer);
   TNBTestEq("answer status code", getField(TNBJsonStr(%r, "status"), 0), "0");
   TNBTestEq("answer status text", getField(TNBJsonStr(%r, "status"), 1), "OK");
   TNBTestEq("answer result", TNBJsonStr(%r, "result"), "2");
   %rows = TNBJsonGet(%r, "rows");
   TNBTestEq("answer row count", TNBJsonCount(%rows), 2);
   TNBTestEq("a row keeps its tabs",
             getField(TNBJsonValue(TNBJsonIndex(%rows, 0)), 1), "Test Clan");
   TNBJsonFree(%r);

   // An answer with no rows, which is how most scalars reply. The array must
   // still parse and count zero rather than come back as nothing at all.
   %empty = "{\"status\":\"0\tOK\",\"result\":\"0\",\"rows\":[]}";
   %r = TNBJsonParse(%empty);
   TNBTestEq("empty rows count", TNBJsonCount(TNBJsonGet(%r, "rows")), 0);
   TNBJsonFree(%r);

   // A transport failure arrives in that same shape, with the HTTP code in
   // field 0. api.cs reads exactly this to decide a session has lapsed.
   %fault = "{\"status\":\"401\tSession expired. Please try again.\"," @
            "\"result\":\"0\",\"rows\":[]}";
   %r = TNBJsonParse(%fault);
   TNBTestEq("fault code", getField(TNBJsonStr(%r, "status"), 0), "401");
   TNBTestEq("fault sentence", getField(TNBJsonStr(%r, "status"), 1),
             "Session expired. Please try again.");
   TNBJsonFree(%r);

   // /cert, the identity record WONGetAuthInfo() hands the shipped scripts.
   // Newlines inside the string are real content: the record is line-based.
   %cert = "{\"cert\":\"orange01\t[TC]\t0\t4510186\n1\nTest Clan\t[TC]\t0\t7\t4\tLeader\"}";
   %r = TNBJsonParse(%cert);
   TNBTestEq("cert first line names the warrior",
             getField(getRecord(TNBJsonStr(%r, "cert"), 0), 0), "orange01");
   TNBTestEq("cert membership count", getRecord(TNBJsonStr(%r, "cert"), 1), "1");
   TNBJsonFree(%r);

   // Decorating a name with its tribe tag is not this mod's job: rows carry an
   // identity quad and the shipped getTextName (webstuff.cs:19) decodes it,
   // putting the tag after the name when append is set and before it otherwise.
   // Asserted here because the quad is what every row schema is built around.
   TNBTestEq("an untagged quad decodes to the bare name",
             getField(getTextName("orange01" TAB "" TAB 0 TAB 4510186), 0),
             "orange01");
   TNBTestEq("append false puts the tag first",
             getField(getTextName("orange01" TAB "[TC]" TAB 0 TAB 4510186), 0),
             "[TC]orange01");
   TNBTestEq("append true puts the tag last",
             getField(getTextName("orange01" TAB "[TC]" TAB 1 TAB 4510186), 0),
             "orange01[TC]");
   TNBTestEq("the quad's uid survives the decode",
             getField(getTextName("orange01" TAB "[TC]" TAB 1 TAB 4510186), 1),
             4510186);

   // api.cs trims before parsing, but the parser must cope with surrounding
   // whitespace on its own -- a body arrives in however many pieces the socket
   // delivered it in.
   %r = TNBJsonParse("\n  {\"status\":\"0\tOK\",\"result\":\"0\",\"rows\":[]}  ");
   TNBTestEq("surrounding whitespace tolerated",
             getField(TNBJsonStr(%r, "status"), 0), "0");
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
