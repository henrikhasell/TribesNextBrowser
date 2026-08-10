package dbproxy

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/henrik/tnbrowser-server/internal/store"
)

// Answer is what one ordinal produces, and it is the client's contract rather
// than ours.
//
// Status is tab-separated. Field 0 is the code every onDatabaseQueryResult
// tests with getField(%status,0)==0. Field 1 is a sentence several handlers put
// straight into a MessageBoxOK -- webemail.cs:551, webbrowser.cs:927,
// webbrowser.cs:1446 -- so a failure message has to read like prose, not like
// an error code. Fields 2.. carry the entire payload for the two profile
// ordinals, which return no rows at all.
//
// Result is a row count for the array form. Several scalars overload it with a
// payload instead: the MOTD, a tribe description, a tribe name.
type Answer struct {
	Status string   `json:"status"`
	Result string   `json:"result"`
	Rows   []string `json:"rows"`
}

// Ctx is everything a handler may touch.
type Ctx struct {
	Ctx   context.Context
	Store *store.Store
	// GUID and Name identify the player asking, as TribesNext vouched for them.
	GUID string
	Name string
}

// Handler answers one ordinal. args is the raw tab-separated string the call
// site assembled; splitting it is the handler's business, because several
// ordinals are ambiguous about their own field count.
type Handler func(c *Ctx, args string) (Answer, error)

// registry is populated by the on() calls in the sibling files.
var registry = map[Key]Handler{}

func on(form, ordinal string, h Handler) {
	key := k(form, ordinal)
	if _, dup := registry[key]; dup {
		panic(fmt.Sprintf("two handlers for %s %s", form, ordinal))
	}
	registry[key] = h
}

// Dispatch answers a request.
//
// A refusal the player caused comes back as a well-formed Answer with a
// non-zero status, because that is a normal outcome the panes render. A fault
// on our side comes back as an error, so the front door can log it and answer
// 500 rather than dressing a broken server up as a rejected request.
func Dispatch(c *Ctx, form, ordinal, args string) (Answer, error) {
	h, found := registry[k(form, ordinal)]
	if !found {
		// Loud, never a silently plausible pane. Most paths raise status
		// field 1 in the client's own dialog, so an unknown ordinal says so
		// rather than answering an empty success -- which renders exactly like
		// "there is nothing here" and hides the gap.
		return fail("This server does not implement %s ordinal %s.", form, ordinal), nil
	}

	answer, err := h(c, args)
	if err != nil {
		var ue *store.UserError
		if errors.As(err, &ue) {
			return fail("%s", ue.Msg), nil
		}
		return Answer{}, err
	}
	return answer, nil
}

//-----------------------------------------------------------------------------
// Status and row helpers
//-----------------------------------------------------------------------------

// okStatus is a well-formed success. Field 1 gets a real word because several
// handlers show it on the success path too (EMailBlockDlg, AddressDlg).
func okStatus(extra ...string) string {
	return strings.Join(append([]string{"0", "OK"}, extra...), "\t")
}

func ok(rows ...string) Answer {
	return Answer{Status: okStatus(), Result: strconv.Itoa(len(rows)), Rows: rows}
}

// okRows keeps an explicit result string, for the array ordinals whose count
// the pane tests before it looks at anything else.
func okRows(rows []string) Answer {
	if rows == nil {
		rows = []string{}
	}
	return Answer{Status: okStatus(), Result: strconv.Itoa(len(rows)), Rows: rows}
}

// okResult is the shape the scalars that overload resultString need: a payload
// where an array form would put a count.
func okResult(result string) Answer {
	return Answer{Status: okStatus(), Result: result, Rows: []string{}}
}

// okWith replaces the whole status, for the two profile ordinals that carry
// their payload in status fields 2...
func okWith(status, result string) Answer {
	return Answer{Status: status, Result: result, Rows: []string{}}
}

// fail is a refusal the player will see. Field 0 is non-zero, which every
// handler tests first, and field 1 is the sentence it shows.
func fail(format string, args ...any) Answer {
	return Answer{
		Status: "1\t" + fmt.Sprintf(format, args...),
		Result: "0",
		Rows:   []string{},
	}
}

// tab joins row fields. Every row on the wire is tab-separated and the indices
// are load-bearing, so a field that has no value is an empty string and never
// an omission.
func tab(fields ...any) string {
	out := make([]string, len(fields))
	for i, f := range fields {
		switch v := f.(type) {
		case string:
			out[i] = v
		case bool:
			out[i] = boolField(v)
		default:
			out[i] = fmt.Sprint(v)
		}
	}
	return strings.Join(out, "\t")
}

// boolField is how the client reads a flag: getField gives it a string and it
// tests truthiness, so 1 and 0 are the only two spellings that behave.
func boolField(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// fields splits an argument string. An empty argument is no fields, not one
// empty field -- several ordinals take "(none)" and would otherwise see a
// phantom first argument.
func fields(args string) []string {
	if args == "" {
		return nil
	}
	return strings.Split(args, "\t")
}

// field reads one argument, tolerating a caller that sent fewer than the call
// site's tuple suggests. Three ordinals genuinely vary their field count.
func field(args string, n int) string {
	f := fields(args)
	if n < 0 || n >= len(f) {
		return ""
	}
	return f[n]
}

func atoi(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// fieldsFrom rejoins everything from field n onward with newlines.
//
// Three ordinals put free text after a fixed prefix -- a tribe description
// after its line count, a forum body after its subject -- and the client sent
// it as one field per line. Rejoining with newlines is the inverse of what
// bodyLines does on the way out, so text survives a round trip through the
// database unchanged.
func fieldsFrom(args string, n int) string {
	f := fields(args)
	if n >= len(f) {
		return ""
	}
	return strings.Join(f[n:], "\n")
}

// date renders a timestamp for a control that will display it verbatim.
//
// Every date the client shows arrives as an opaque string -- it parses none of
// them, sorts on none of them, and puts each straight into a list column or a
// text control. So the format is a presentation choice, and ISO is the one that
// stays unambiguous in a column too narrow to read twice.
func date(unix int64) string {
	if unix <= 0 {
		return ""
	}
	return time.Unix(unix, 0).UTC().Format("2006-01-02")
}

// bodyLines splits a stored body the way every row schema that carries one
// expects: a line count followed by that many fields.
//
// An empty body is ONE empty line, not zero. A zero-line body makes every field
// after the body land at the wrong index for any parser that reads past it --
// and mail, news, forum posts and tribe news all do.
func bodyLines(text string) []string {
	if text == "" {
		return []string{""}
	}
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}

// withBody appends "<line count> <line> <line> ..." to a row.
func withBody(prefix string, body string) string {
	lines := bodyLines(body)
	parts := make([]any, 0, len(lines)+2)
	parts = append(parts, prefix, len(lines))
	for _, l := range lines {
		parts = append(parts, l)
	}
	return tab(parts...)
}

// mlLink builds an <a:...> link for a body the client renders in a
// GuiMLTextCtrl.
//
// The separator written here is a NEWLINE and the client sees a TAB, which
// looks wrong until the round trip is followed through:
//
//   - GuiMLTextCtrl::onURL splits the URL on TAB -- verb at getField(%url,0),
//     arguments at fields 1.. (webbrowser.cs:1063-1067) -- so
//     <a:acceptinvite<TAB>Tribe<TAB>Player> is the only shape that parses;
//   - a stored body is split on newlines into row fields by bodyLines, and the
//     client puts them back with getFields(%row,17) (webemail.cs:1147), which
//     rejoins them TAB-separated into one record;
//   - EmailGetBody (webemail.cs:167) prints records 7.. verbatim and never
//     touches tabs.
//
// So every newline in a stored body arrives at the control as a TAB. That is
// also why a mail body cannot contain a real line break -- a property of the
// shipped client, not of this server. It is what makes a server-sent working
// hyperlink possible, and for tribe invitations and join requests it is the
// only channel there is.
func mlLink(label, verb string, args ...string) string {
	return "<a:" + strings.Join(append([]string{verb}, args...), "\n") + ">" +
		label + "</a>"
}
