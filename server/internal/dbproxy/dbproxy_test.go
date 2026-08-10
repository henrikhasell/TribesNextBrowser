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

// The status field the client tests first has to be a code, and the sentence
// after it has to be a sentence: webemail.cs:551, webbrowser.cs:927 and
// webbrowser.cs:1446 all put field 1 straight into a MessageBoxOK.
func TestStatusShape(t *testing.T) {
	// The fallback, for the array forms whose status nothing displays. Field 1
	// still has to be non-empty: the client tests it and so does the sweep.
	if got := okStatus(""); got != "0\tOK" {
		t.Errorf("okStatus(\"\") = %q, want %q", got, "0\tOK")
	}

	// The message is field 1 and the payload follows it, so a handler with
	// something to say never displaces a field the pane parses.
	if got := okStatus("Player Harabec has been kicked from Big Sucka Fishes."); got !=
		"0\tPlayer Harabec has been kicked from Big Sucka Fishes." {
		t.Errorf("okStatus with a message = %q", got)
	}
	if got := okStatus("Here it is.", "9001", "Big Sucka Fishes"); got !=
		"0\tHere it is.\t9001\tBig Sucka Fishes" {
		t.Errorf("okStatus with payload = %q", got)
	}

	// okMessage puts the same sentence in both places a pane might read it.
	m := okMessage("You have left Big Sucka Fishes.")
	if m.Status != "0\tYou have left Big Sucka Fishes." ||
		m.Result != "You have left Big Sucka Fishes." {
		t.Errorf("okMessage = %+v", m)
	}

	f := fail("There is no tribe by that name.")
	if code := strings.SplitN(f.Status, "\t", 2)[0]; code == "0" {
		t.Errorf("fail() produced a success code: %q", f.Status)
	}
	if msg := strings.SplitN(f.Status, "\t", 2)[1]; !strings.HasSuffix(msg, ".") {
		t.Errorf("fail() message is not a sentence: %q", msg)
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
	row := withBody("head", "one\ntwo")
	f := strings.Split(row, "\t")
	if f[1] != "2" || len(f) != 4 {
		t.Errorf("withBody = %q, want head, 2, one, two", row)
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

// fields("") is no fields, not one empty field. Several ordinals take no
// arguments at all and would otherwise see a phantom first one.
func TestEmptyArgsIsNoFields(t *testing.T) {
	if got := fields(""); len(got) != 0 {
		t.Errorf("fields(%q) = %#v, want none", "", got)
	}
	if got := field("", 0); got != "" {
		t.Errorf("field(%q, 0) = %q", "", got)
	}
	if got := field("a\tb", 5); got != "" {
		t.Errorf("field past the end = %q, want empty", got)
	}
}

// An unknown ordinal is refused loudly. The alternative -- an empty success --
// renders exactly like "there is nothing here" and hides the gap.
func TestUnknownOrdinalIsRefusedNotEmpty(t *testing.T) {
	answer, err := Dispatch(&Ctx{}, Scalar, "999", "")
	if err != nil {
		t.Fatalf("Dispatch returned a fault for an unknown ordinal: %v", err)
	}
	if strings.HasPrefix(answer.Status, "0\t") {
		t.Errorf("unknown ordinal answered success: %q", answer.Status)
	}
	if len(answer.Rows) != 0 {
		t.Errorf("unknown ordinal answered rows: %#v", answer.Rows)
	}
}
