package dbproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/henrik/tnbrowser-server/internal/store"
)

// Answer is what one ordinal produces, and it is the client's contract rather
// than ours.
//
// Code is the outcome the shipped onDatabaseQueryResult tests first: 0
// succeeded, non-zero was refused. Message is the sentence several handlers put
// straight into a MessageBoxOK -- webemail.cs:551, webbrowser.cs:927,
// webbrowser.cs:1446 -- so a refusal has to read like prose, not like an error
// code.
//
// Fields carries the payload the two profile ordinals return instead of rows;
// the shim appends it after the message when it rebuilds the status string the
// shipped scripts parse. Empty for everything else.
//
// Result is a row count for the array form. Several scalars overload it with a
// payload instead: the MOTD, a tribe description, a tribe name. It stays a
// string because it is genuinely one or the other depending on the ordinal, and
// pretending otherwise would be a lie with a type on it.
//
// Rows are arrays of fields rather than tab-joined strings. The indices are
// load-bearing -- a field exists because a shipped parser reads that position --
// so a field with no value is an empty string and never an omission.
type Answer struct {
	Code    int      `json:"code"`
	Message string   `json:"message"`
	Fields  []string `json:"fields,omitempty"`
	Result  string   `json:"result"`
	Rows    [][]any  `json:"rows"`
}

// Ctx is everything a handler may touch.
type Ctx struct {
	Ctx   context.Context
	Store *store.Store
	// GUID and Name identify the player asking, as TribesNext vouched for them.
	GUID string
	Name string
	// Log is where a failure that must not fail the request goes. May be nil;
	// use c.log() rather than reaching for it directly.
	Log *slog.Logger
}

func (c *Ctx) log() *slog.Logger {
	if c.Log == nil {
		return slog.Default()
	}
	return c.Log
}

// notify sends one player a message from the player whose action caused it.
//
// Deliberately not fatal, and the reason is the same one the welcome mail gives
// (api.go:217): the mutation has already been committed by the time anything
// gets mailed about it, so answering an error here would tell a player that the
// kick, promotion or disband they just performed had failed when it had not --
// and the client's only response to a failed write is to show the sentence in a
// MessageBoxOK and leave the pane as it was. A notification nobody receives is
// worth a log line, not a lie.
func (c *Ctx) notify(to, subject, body string) {
	if to == "" || to == c.GUID {
		return
	}
	if err := c.Store.Deliver(c.Ctx, to, c.GUID, c.Name, subject, body); err != nil {
		c.log().Error("notification mail", "err", err, "to", to, "subject", subject)
	}
}

// notifyAll is notify over a list, for the events a whole roster hears about.
func (c *Ctx) notifyAll(to []string, subject, body string) {
	for _, guid := range to {
		c.notify(guid, subject, body)
	}
}

// Handler answers one ordinal. args is the raw tab-separated string the call
// site assembled; splitting it is the handler's business, because several
// ordinals are ambiguous about their own field count.
type Handler func(c *Ctx, args []string) (Answer, error)

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
func Dispatch(c *Ctx, form, ordinal string, args []string) (Answer, error) {
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

// message is the sentence the client shows on the success path.
//
// It shows it on quite a few of them -- webbrowser.cs:927, :1725, :1781, :1784,
// :1808 and webemail.cs:704, :706 all put it straight into a MessageBoxOK with
// no wording of their own. This used to be the literal word "OK", which is how
// confirming an invitation, a graphic, a web address or a buddy each produced a
// dialog reading "OK" and nothing else. A message that names what happened, and
// to whom, costs the same bytes.
//
// The blank fallback is for the array forms. Their message is never displayed
// (the pane goes straight to the row count) and inventing prose per list would
// be words nobody reads, but it must still be non-empty: the client tests it,
// and so does the sweep.
func message(msg string) string {
	if msg == "" {
		return "OK"
	}
	return msg
}

func ok(rows ...[]any) Answer {
	return okRows(rows)
}

// okRows is the array form: the count the pane tests before it looks at
// anything else, and the rows themselves.
func okRows(rows [][]any) Answer {
	if rows == nil {
		rows = [][]any{}
	}
	return Answer{
		Code:    0,
		Message: message(""),
		Result:  strconv.Itoa(len(rows)),
		Rows:    rows,
	}
}

// okMessage answers with a sentence and nothing else -- the shape of every
// write that changes something and returns no data.
//
// The sentence goes in the result as well, because that is where it has always
// been, and a handful of ordinals are read one way rather than the other
// depending on which of the shipped panes issued them.
func okMessage(msg string) Answer {
	return Answer{Code: 0, Message: message(msg), Result: msg, Rows: [][]any{}}
}

// okResult is the shape the scalars that overload the result need: a payload
// where an array form would put a count, and the sentence beside it. Both
// matter -- the payload is data the pane parses, the sentence is what it shows.
func okResult(msg, result string) Answer {
	return Answer{Code: 0, Message: message(msg), Result: result, Rows: [][]any{}}
}

// okFields is the two profile ordinals, which carry their payload after the
// message rather than in rows at all.
func okFields(msg string, extra []string, result string) Answer {
	return Answer{
		Code:    0,
		Message: message(msg),
		Fields:  extra,
		Result:  result,
		Rows:    [][]any{},
	}
}

// withRows attaches rows to an answer built by okFields -- the two ordinals
// that carry an extra status field AND a list.
func (a Answer) withRows(rows [][]any) Answer {
	if rows == nil {
		rows = [][]any{}
	}
	a.Rows = rows
	return a
}

// Refuse is a refusal built outside the handler table -- an unreadable request
// or an unknown query form, which the front door catches before dispatch.
func Refuse(msg string) Answer { return fail("%s", msg) }

// fail is a refusal the player will see. A non-zero code is what every handler
// tests first, and the message is the sentence it shows.
func fail(format string, args ...any) Answer {
	return Answer{
		Code:    1,
		Message: fmt.Sprintf(format, args...),
		Result:  "0",
		Rows:    [][]any{},
	}
}

// row is one row of an answer.
//
// The values keep their types -- an id stays a number, a flag stays a boolean --
// because the client joins them with tabs at the last moment and the engine
// reads a JSON true as 1, which is the spelling the shipped getField callers
// want. What it must never do is drop a field: the indices are the shipped
// parsers' contract, so a value that has nothing in it is an empty string.
func row(values ...any) []any {
	out := make([]any, len(values))
	copy(out, values)
	return out
}

// flag is how a boolean is spelled inside the status fields, which are strings
// rather than typed row values: the client reads them with getField and tests
// truthiness, so 1 and 0 are the only two spellings that behave.
func flag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// field reads one argument, tolerating a caller that sent fewer than the call
// site's tuple suggests. Three ordinals genuinely vary their argument count.
func field(args []string, n int) string {
	if n < 0 || n >= len(args) {
		return ""
	}
	return args[n]
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
func fieldsFrom(args []string, n int) string {
	if n >= len(args) {
		return ""
	}
	return strings.Join(args[n:], "\n")
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

// withBody appends "<line count> <line> <line> ..." to a row already built.
func withBody(head []any, body string) []any {
	lines := bodyLines(body)
	out := make([]any, 0, len(head)+len(lines)+1)
	out = append(out, head...)
	out = append(out, len(lines))
	for _, l := range lines {
		out = append(out, l)
	}
	return out
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

// clanLink names a tribe in a mail body the way the history already names one:
// as a link to its profile.
//
// The verb and the argument are the same, because scalar 22 takes a tribe name
// rather than an id (ordinals.go, "<tribeName>") -- so the thing the player
// reads is also the handle the pane queries with. linkRefs writes the identical
// link for a {clan:...} marker, with a real tab instead of the newline, because
// a history row is appended verbatim rather than rejoined out of fields.
//
// Every mail this server sends about a tribe goes through here. A tribe name
// containing a tab or a newline would break the URL, and nothing escapes one --
// but such a name cannot exist, because the ordinal protocol itself is
// tab-separated and the name would not have survived being created.
func clanLink(name string) string {
	return mlLink(name, "tribe", name)
}
