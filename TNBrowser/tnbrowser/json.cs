// TNBrowser -- JSON parser for TorqueScript
//
// The TribesNext browser API answers in JSON and this engine has no notion of
// it, so this is a hand-written recursive-descent parser.
//
// A parsed document is a tree of ScriptObjects (class TNBJsonNode):
//
//   type    "object" | "array" | "string" | "number" | "bool" | "null"
//   count   number of members/elements (containers only)
//   key[i]  member name          (objects only)
//   val[i]  child node id        (containers only)
//   value   the scalar itself    (scalars only)
//
// Every tree you parse must be released with TNBJsonFree, or the ScriptObjects
// leak for the life of the session.
//
// Two engine constraints shape this file:
//
//   * `new ScriptObject(...)` at file scope crashes this build outright, so
//     every allocation happens inside a function and nothing runs at load time
//     beyond assigning globals.
//   * There are no reference parameters, so the scan position lives in
//     $TNBJson::Pos. That makes the parser non-reentrant, which is fine: it
//     runs to completion inside a single callback and never yields.

// Printable ASCII, indexed by (codepoint - 32). Used to turn \uXXXX escapes
// back into characters, since the engine has no chr().
$TNBJson::Ascii = " !\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~";
$TNBJson::HexDigits = "0123456789abcdef";

//-----------------------------------------------------------------------------
// Entry point
//-----------------------------------------------------------------------------

// Parse %text. Returns the root node, or 0 on failure with $TNBJson::Error set.
function TNBJsonParse(%text)
{
   $TNBJson::Src = %text;
   $TNBJson::Len = strlen(%text);
   $TNBJson::Pos = 0;
   $TNBJson::Error = "";

   %root = TNBJsonParseValue();

   if ($TNBJson::Error !$= "")
   {
      if (isObject(%root))
         TNBJsonFree(%root);
      return 0;
   }

   // Trailing garbage means we did not understand the document, which usually
   // means the server sent an HTML error page instead of JSON.
   TNBJsonSkipSpace();
   if ($TNBJson::Pos < $TNBJson::Len)
   {
      $TNBJson::Error = "trailing content at offset " @ $TNBJson::Pos;
      TNBJsonFree(%root);
      return 0;
   }

   return %root;
}

//-----------------------------------------------------------------------------
// Node construction and teardown
//-----------------------------------------------------------------------------

function TNBJsonNewNode(%type)
{
   return new ScriptObject()
   {
      class = "TNBJsonNode";
      type = %type;
      count = 0;
      value = "";
   };
}

// Recursively delete a node and everything under it.
function TNBJsonFree(%node)
{
   if (!isObject(%node))
      return;

   if (%node.type $= "object" || %node.type $= "array")
   {
      for (%i = 0; %i < %node.count; %i++)
         TNBJsonFree(%node.val[%i]);
   }
   %node.delete();
}

//-----------------------------------------------------------------------------
// Accessors
//
// All of these tolerate being handed 0/"" so callers can chain lookups through
// missing fields without guarding every step.
//-----------------------------------------------------------------------------

// Member of an object by name. Returns the child node, or 0.
function TNBJsonGet(%node, %key)
{
   if (!isObject(%node) || %node.type !$= "object")
      return 0;

   for (%i = 0; %i < %node.count; %i++)
   {
      if (%node.key[%i] $= %key)
         return %node.val[%i];
   }
   return 0;
}

// Element of an array (or object) by position. Returns the child node, or 0.
function TNBJsonIndex(%node, %i)
{
   if (!isObject(%node))
      return 0;
   if (%i < 0 || %i >= %node.count)
      return 0;
   return %node.val[%i];
}

function TNBJsonKey(%node, %i)
{
   if (!isObject(%node) || %i < 0 || %i >= %node.count)
      return "";
   return %node.key[%i];
}

function TNBJsonCount(%node)
{
   if (!isObject(%node))
      return 0;
   return %node.count;
}

function TNBJsonType(%node)
{
   if (!isObject(%node))
      return "";
   return %node.type;
}

// Scalar value of a node itself.
function TNBJsonValue(%node)
{
   if (!isObject(%node))
      return "";
   return %node.value;
}

// Convenience: scalar value of a named member. This is what almost all the
// GUI code uses -- TNBJsonStr(%profile, "name").
//
// The API is loose about types (it returns "1"/"true"/1 for booleans across
// different methods), so a bool comes back as "1"/"0" and everything else as
// its literal text.
function TNBJsonStr(%node, %key)
{
   %child = TNBJsonGet(%node, %key);
   if (!isObject(%child))
      return "";
   return %child.value;
}

// Convenience: truthiness of a named member, normalising the several spellings
// the backend uses for yes.
function TNBJsonBool(%node, %key)
{
   %v = strlwr(TNBJsonStr(%node, %key));
   return (%v $= "1" || %v $= "true" || %v $= "yes");
}

//-----------------------------------------------------------------------------
// Scanner
//-----------------------------------------------------------------------------

function TNBJsonPeek()
{
   if ($TNBJson::Pos >= $TNBJson::Len)
      return "";
   return getSubStr($TNBJson::Src, $TNBJson::Pos, 1);
}

function TNBJsonSkipSpace()
{
   while ($TNBJson::Pos < $TNBJson::Len)
   {
      %c = getSubStr($TNBJson::Src, $TNBJson::Pos, 1);
      if (%c $= " " || %c $= "\t" || %c $= "\n" || %c $= "\r")
         $TNBJson::Pos++;
      else
         return;
   }
}

//-----------------------------------------------------------------------------
// Grammar
//-----------------------------------------------------------------------------

function TNBJsonParseValue()
{
   if ($TNBJson::Error !$= "")
      return 0;

   TNBJsonSkipSpace();
   %c = TNBJsonPeek();

   if (%c $= "")
   {
      $TNBJson::Error = "unexpected end of input";
      return 0;
   }
   if (%c $= "{")
      return TNBJsonParseObject();
   if (%c $= "[")
      return TNBJsonParseArray();
   if (%c $= "\"")
   {
      %node = TNBJsonNewNode("string");
      %node.value = TNBJsonParseString();
      return %node;
   }
   return TNBJsonParseLiteral();
}

function TNBJsonParseObject()
{
   %node = TNBJsonNewNode("object");
   $TNBJson::Pos++;              // consume {

   TNBJsonSkipSpace();
   if (TNBJsonPeek() $= "}")
   {
      $TNBJson::Pos++;
      return %node;
   }

   while (true)
   {
      TNBJsonSkipSpace();
      if (TNBJsonPeek() !$= "\"")
      {
         $TNBJson::Error = "expected member name at offset " @ $TNBJson::Pos;
         return %node;
      }
      %key = TNBJsonParseString();
      if ($TNBJson::Error !$= "")
         return %node;

      TNBJsonSkipSpace();
      if (TNBJsonPeek() !$= ":")
      {
         $TNBJson::Error = "expected ':' at offset " @ $TNBJson::Pos;
         return %node;
      }
      $TNBJson::Pos++;

      %child = TNBJsonParseValue();
      if ($TNBJson::Error !$= "")
      {
         TNBJsonFree(%child);
         return %node;
      }

      %n = %node.count;
      %node.key[%n] = %key;
      %node.val[%n] = %child;
      %node.count = %n + 1;

      TNBJsonSkipSpace();
      %c = TNBJsonPeek();
      if (%c $= ",")
      {
         $TNBJson::Pos++;
         continue;
      }
      if (%c $= "}")
      {
         $TNBJson::Pos++;
         return %node;
      }
      $TNBJson::Error = "expected ',' or '}' at offset " @ $TNBJson::Pos;
      return %node;
   }
}

function TNBJsonParseArray()
{
   %node = TNBJsonNewNode("array");
   $TNBJson::Pos++;              // consume [

   TNBJsonSkipSpace();
   if (TNBJsonPeek() $= "]")
   {
      $TNBJson::Pos++;
      return %node;
   }

   while (true)
   {
      %child = TNBJsonParseValue();
      if ($TNBJson::Error !$= "")
      {
         TNBJsonFree(%child);
         return %node;
      }

      %n = %node.count;
      %node.key[%n] = %n;
      %node.val[%n] = %child;
      %node.count = %n + 1;

      TNBJsonSkipSpace();
      %c = TNBJsonPeek();
      if (%c $= ",")
      {
         $TNBJson::Pos++;
         continue;
      }
      if (%c $= "]")
      {
         $TNBJson::Pos++;
         return %node;
      }
      $TNBJson::Error = "expected ',' or ']' at offset " @ $TNBJson::Pos;
      return %node;
   }
}

// true / false / null / number. Anything else is an error.
function TNBJsonParseLiteral()
{
   %start = $TNBJson::Pos;
   while ($TNBJson::Pos < $TNBJson::Len)
   {
      %c = getSubStr($TNBJson::Src, $TNBJson::Pos, 1);
      if (%c $= "," || %c $= "}" || %c $= "]" || %c $= " " ||
          %c $= "\t" || %c $= "\n" || %c $= "\r")
         break;
      $TNBJson::Pos++;
   }

   %text = getSubStr($TNBJson::Src, %start, $TNBJson::Pos - %start);
   %lower = strlwr(%text);

   if (%lower $= "true")
   {
      %node = TNBJsonNewNode("bool");
      %node.value = 1;
      return %node;
    }
   if (%lower $= "false")
   {
      %node = TNBJsonNewNode("bool");
      %node.value = 0;
      return %node;
   }
   if (%lower $= "null")
   {
      %node = TNBJsonNewNode("null");
      %node.value = "";
      return %node;
   }

   if (!TNBJsonIsNumber(%text))
   {
      $TNBJson::Error = "unexpected token '" @ %text @ "' at offset " @ %start;
      return 0;
   }

   %node = TNBJsonNewNode("number");
   %node.value = %text;
   return %node;
}

// The engine coerces any string to 0, so "abc" == 0 is true and cannot be used
// to test for numbers. Check the characters instead.
function TNBJsonIsNumber(%text)
{
   %len = strlen(%text);
   if (%len == 0)
      return false;

   %seenDigit = false;
   for (%i = 0; %i < %len; %i++)
   {
      %c = getSubStr(%text, %i, 1);
      if (%c $= "0" || %c $= "1" || %c $= "2" || %c $= "3" || %c $= "4" ||
          %c $= "5" || %c $= "6" || %c $= "7" || %c $= "8" || %c $= "9")
      {
         %seenDigit = true;
         continue;
      }
      if (%c $= "-" || %c $= "+" || %c $= "." || %c $= "e" || %c $= "E")
         continue;
      return false;
   }
   return %seenDigit;
}

// Parse a quoted string starting at the opening quote. Returns the decoded
// text with the position left just past the closing quote.
//
// Plain runs are copied in one getSubStr rather than a character at a time, so
// a multi-kilobyte profile body costs a handful of concatenations instead of
// thousands.
function TNBJsonParseString()
{
   $TNBJson::Pos++;              // consume opening quote
   %out = "";
   %runStart = $TNBJson::Pos;

   while (true)
   {
      if ($TNBJson::Pos >= $TNBJson::Len)
      {
         $TNBJson::Error = "unterminated string";
         return %out;
      }

      %c = getSubStr($TNBJson::Src, $TNBJson::Pos, 1);

      if (%c $= "\"")
      {
         %out = %out @ getSubStr($TNBJson::Src, %runStart, $TNBJson::Pos - %runStart);
         $TNBJson::Pos++;
         return %out;
      }

      if (%c $= "\\")
      {
         %out = %out @ getSubStr($TNBJson::Src, %runStart, $TNBJson::Pos - %runStart);
         $TNBJson::Pos++;
         %out = %out @ TNBJsonParseEscape();
         %runStart = $TNBJson::Pos;
         continue;
      }

      $TNBJson::Pos++;
   }
}

function TNBJsonParseEscape()
{
   if ($TNBJson::Pos >= $TNBJson::Len)
   {
      $TNBJson::Error = "unterminated escape";
      return "";
   }

   %e = getSubStr($TNBJson::Src, $TNBJson::Pos, 1);
   $TNBJson::Pos++;

   switch$ (%e)
   {
      case "\"": return "\"";
      case "\\": return "\\";
      case "/":  return "/";
      case "n":  return "\n";
      case "t":  return "\t";
      case "r":  return "\r";
      case "b":  return "";      // backspace has no meaning in a GUI control
      case "f":  return "";      // ditto form feed
      case "u":  return TNBJsonParseUnicode();
   }

   $TNBJson::Error = "unknown escape '\\" @ %e @ "'";
   return "";
}

// \uXXXX. Anything outside printable ASCII becomes '?', because the engine has
// no way to build an arbitrary byte and the shell fonts are ASCII anyway.
function TNBJsonParseUnicode()
{
   if ($TNBJson::Pos + 4 > $TNBJson::Len)
   {
      $TNBJson::Error = "truncated \\u escape";
      return "";
   }

   %hex = getSubStr($TNBJson::Src, $TNBJson::Pos, 4);
   $TNBJson::Pos += 4;

   %code = TNBHexToDec(%hex);
   if (%code < 32 || %code > 126)
      return "?";

   return getSubStr($TNBJson::Ascii, %code - 32, 1);
}

// Hex string to decimal. The patch ships DecToHex but no inverse.
function TNBHexToDec(%hex)
{
   %hex = strlwr(%hex);
   %value = 0;
   for (%i = 0; %i < strlen(%hex); %i++)
   {
      %digit = strstr($TNBJson::HexDigits, getSubStr(%hex, %i, 1));
      if (%digit < 0)
         return %value;
      %value = %value * 16 + %digit;
   }
   return %value;
}

//-----------------------------------------------------------------------------
// Percent-encoding, needed to put payloads into a request-URI
//-----------------------------------------------------------------------------

// Encode %text for use as a query-string value. Conservative on purpose:
// anything outside the unreserved set is escaped.
function TNBUrlEncode(%text)
{
   %out = "";
   %len = strlen(%text);
   for (%i = 0; %i < %len; %i++)
   {
      %c = getSubStr(%text, %i, 1);
      if (TNBIsUnreserved(%c))
         %out = %out @ %c;
      else
      {
         // strcmp against "" yields the character's numeric value; this is the
         // same trick the stock TribesNext scripts use to get a codepoint.
         %code = strcmp(%c, "");
         if (%code < 0)
            %code = %code + 256;
         %out = %out @ "%" @ TNBHexPad2(DecToHex(%code));
      }
   }
   return %out;
}

function TNBIsUnreserved(%c)
{
   if (%c $= "-" || %c $= "_" || %c $= "." || %c $= "~")
      return true;
   %code = strcmp(%c, "");
   if (%code >= 48 && %code <= 57)     // 0-9
      return true;
   if (%code >= 65 && %code <= 90)     // A-Z
      return true;
   if (%code >= 97 && %code <= 122)    // a-z
      return true;
   return false;
}

function TNBHexPad2(%hex)
{
   if (strlen(%hex) < 2)
      return "0" @ %hex;
   return %hex;
}

//-----------------------------------------------------------------------------
// Minimal JSON emission, for building request payloads
//-----------------------------------------------------------------------------

// Escape %text for inclusion in a JSON string literal.
function TNBJsonEscape(%text)
{
   %out = "";
   %len = strlen(%text);
   for (%i = 0; %i < %len; %i++)
   {
      %c = getSubStr(%text, %i, 1);
      if (%c $= "\"")
         %out = %out @ "\\\"";
      else if (%c $= "\\")
         %out = %out @ "\\\\";
      else if (%c $= "\n")
         %out = %out @ "\\n";
      else if (%c $= "\t")
         %out = %out @ "\\t";
      else if (%c $= "\r")
         %out = %out @ "\\r";
      else
         %out = %out @ %c;
   }
   return %out;
}

// Build {"k":"v", ...} from alternating key/value arguments. The API never
// needs nested request payloads, so this deliberately only emits string values.
function TNBJsonObject(%k1, %v1, %k2, %v2, %k3, %v3, %k4, %v4)
{
   %out = "{";
   %sep = "";
   for (%i = 1; %i <= 4; %i++)
   {
      %k = (%i == 1 ? %k1 : (%i == 2 ? %k2 : (%i == 3 ? %k3 : %k4)));
      %v = (%i == 1 ? %v1 : (%i == 2 ? %v2 : (%i == 3 ? %v3 : %v4)));
      if (%k $= "")
         continue;
      %out = %out @ %sep @ "\"" @ TNBJsonEscape(%k) @ "\":\"" @ TNBJsonEscape(%v) @ "\"";
      %sep = ",";
   }
   return %out @ "}";
}

echo("TNBrowser: json.cs loaded");
