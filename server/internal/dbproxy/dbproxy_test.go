package dbproxy

import (
	"strings"
	"testing"
)

// The table and the handlers have to agree, in both directions.
//
// An entry with no handler answers "this server does not implement it", which
// reads to a player like a broken server rather than a missing feature. A
// handler with no entry is worse: it works, and nothing records what it is or
// which call site issues it, so the next person to touch it has to re-derive
// the row schema from the shipped scripts.
func TestEveryOrdinalHasAHandler(t *testing.T) {
	const want = 61
	if len(Table) != want {
		t.Fatalf("table has %d entries, want %d -- all %d (form, ordinal) pairs "+
			"reachable from the five community scripts", len(Table), want, want)
	}

	for key, o := range Table {
		if _, found := registry[key]; !found {
			t.Errorf("%s %s (%s) is in the table with no handler", key.Form, key.Ordinal, o.Name)
		}
	}
	for key := range registry {
		if _, found := Table[key]; !found {
			t.Errorf("%s %s has a handler but no table entry", key.Form, key.Ordinal)
		}
	}
}

// Every entry has to say where it came from. A row schema with no call site
// behind it is a guess, and the whole value of this table is that none of it
// is.
func TestEveryOrdinalCitesItsCallSite(t *testing.T) {
	for key, o := range Table {
		if o.Name == "" {
			t.Errorf("%s %s has no name", key.Form, key.Ordinal)
		}
		if !strings.Contains(o.Where, ".cs:") {
			t.Errorf("%s %s (%s) cites no call site: %q",
				key.Form, key.Ordinal, o.Name, o.Where)
		}
		if o.Args == "" {
			t.Errorf("%s %s (%s) records no argument tuple", key.Form, key.Ordinal, o.Name)
		}
	}
}

// The code the client tests first has to be a code, and the message after it
// has to be a sentence: webemail.cs:551, webbrowser.cs:927 and
// webbrowser.cs:1446 all put the message straight into a MessageBoxOK.
func TestAnswerShape(t *testing.T) {
	// The fallback, for the array forms whose message nothing displays. It still
	// has to be non-empty: the client tests it and so does the sweep.
	empty := okRows(nil)
	if empty.Code != 0 || empty.Message != "OK" {
		t.Errorf("okRows(nil) = %+v, want code 0 and a message", empty)
	}
	if empty.Rows == nil {
		t.Error("rows is null; the client iterates it without checking")
	}

	// A handler with something to say puts it in the message, and the payload
	// the two profile ordinals carry goes in fields -- so neither displaces the
	// other, which is what the old single tab-separated status risked.
	got := okFields("Here it is.", []string{"9001", "Big Sucka Fishes"}, "")
	if got.Message != "Here it is." {
		t.Errorf("message = %q", got.Message)
	}
	if len(got.Fields) != 2 || got.Fields[1] != "Big Sucka Fishes" {
		t.Errorf("fields = %+v", got.Fields)
	}

	// A refusal is a non-zero code and a sentence.
	bad := fail("Player %s is not a member.", "Harabec")
	if bad.Code == 0 {
		t.Error("a refusal answered code 0")
	}
	if bad.Message != "Player Harabec is not a member." {
		t.Errorf("refusal message = %q", bad.Message)
	}
}

// A row keeps its fields separate and its types intact. The indices are the
// shipped parsers' contract, so an empty value must still occupy its position.
func TestRowKeepsEveryField(t *testing.T) {
	r := row(7, "Test Clan", "", true, "")
	if len(r) != 5 {
		t.Fatalf("row has %d fields, want 5 -- an empty value is not an omission", len(r))
	}
	if r[0] != 7 || r[3] != true {
		t.Errorf("row lost its types: %+v", r)
	}
}

// An empty body is one empty line, not zero. A zero-line body makes every field
// after it land one place early, which shows up as a plausible pane with the
// wrong data in it rather than as an error.
func TestBodyLinesNeverEmpty(t *testing.T) {
	if got := bodyLines(""); len(got) != 1 || got[0] != "" {
		t.Errorf("bodyLines(%q) = %#v, want one empty line", "", got)
	}
	if got := bodyLines("one\ntwo"); len(got) != 2 {
		t.Errorf("bodyLines(two lines) = %#v", got)
	}

	// The count in the row has to match the lines that follow it.
	r := withBody(row("head"), "one\ntwo")
	if len(r) != 4 || r[1] != 2 || r[2] != "one" || r[3] != "two" {
		t.Errorf("withBody = %#v, want head, 2, one, two", r)
	}
}

// mlLink writes newlines where the client will see tabs. That is not a bug and
// it is the precondition for a server-sent working hyperlink, which is the only
// channel a tribe invitation has.
func TestInviteLinkUsesNewlineSeparators(t *testing.T) {
	got := mlLink("Accept", "acceptinvite", "Big Sucka Fishes", "Harabec")
	want := "<a:acceptinvite\nBig Sucka Fishes\nHarabec>Accept</a>"
	if got != want {
		t.Errorf("mlLink() = %q, want %q", got, want)
	}
	if strings.Contains(got, "\t") {
		t.Error("mlLink wrote a literal tab; the round trip through the mail " +
			"body is what turns the newline into one")
	}
}

// Reading past the end of the argument list is empty, not a panic. Three
// ordinals genuinely vary their argument count, so a handler asking for an
// argument the caller did not send is normal rather than exceptional.
//
// Arguments arrive as a real list now, so "no arguments" is an empty list and
// cannot be confused with one empty argument -- which is the ambiguity a
// tab-joined string had.
func TestReadingPastTheArgumentsIsEmpty(t *testing.T) {
	if got := field(nil, 0); got != "" {
		t.Errorf("field(nil, 0) = %q", got)
	}
	if got := field([]string{"a", "b"}, 5); got != "" {
		t.Errorf("field past the end = %q, want empty", got)
	}
	if got := field([]string{"a", ""}, 1); got != "" {
		t.Errorf("an empty argument = %q, want empty", got)
	}
}

// An unknown ordinal is refused loudly. The alternative -- an empty success --
// renders exactly like "there is nothing here" and hides the gap.
func TestUnknownOrdinalIsRefusedNotEmpty(t *testing.T) {
	answer, err := Dispatch(&Ctx{}, Scalar, "999", nil)
	if err != nil {
		t.Fatalf("Dispatch returned a fault for an unknown ordinal: %v", err)
	}
	if answer.Code == 0 {
		t.Errorf("unknown ordinal answered success: %+v", answer)
	}
	if answer.Message == "" {
		t.Error("a refusal with no sentence; the pane shows this one")
	}
	if len(answer.Rows) != 0 {
		t.Errorf("unknown ordinal answered rows: %#v", answer.Rows)
	}
}

// The history is the one ordinal whose rows are display text rather than
// fields, which is what makes a link possible in it at all -- and what makes
// the separator a real tab rather than the newline mlLink writes.
func TestLinkRefsBuildsLinksTheClientFollows(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Joined {clan:Test Clan}", "Joined <a:tribe\tTest Clan>Test Clan</a>"},
		{"{warrior:Shifter} joined", "<a:player\tShifter>Shifter</a> joined"},
		{"to {web:example.org}", "to <a:wwwlink\texample.org>example.org</a>"},
		{"{clan:A} and {clan:B}",
			"<a:tribe\tA>A</a> and <a:tribe\tB>B</a>"},

		// Rows written before any of this, and rows naming a kind this does not
		// know, are left alone rather than mangled or dropped.
		{"Changed profile text", "Changed profile text"},
		{"a {mystery:thing} here", "a {mystery:thing} here"},
		{"unbalanced {clan:A", "unbalanced {clan:A"},
	}
	for _, c := range cases {
		if got := linkRefs(c.in); got != c.want {
			t.Errorf("linkRefs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
