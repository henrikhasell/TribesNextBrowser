package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/henrik/tnbrowser-server/internal/model"
)

// mailMethods is json_mail.php.
//
// The method names and shapes are the ones probing the live server established
// (count returns the number as a JSON string, read with no payload lists and
// with an id fetches one, delete takes an id). Two differences from TribesNext,
// both deliberate:
//
//   - send actually works. Theirs refuses every payload shape tried.
//   - read and delete take an optional folder, restoring the Sent and Deleted
//     tabs the original EmailGui had.
func (s *Server) mailMethods() map[string]handler {
	return map[string]handler{
		"count": func(ctx context.Context, guid string, p payload) (any, error) {
			n, err := s.Store.MailCount(ctx, guid)
			if err != nil {
				return nil, err
			}
			// A JSON string, not a number -- matching the live server, whose
			// count answered "0" rather than 0.
			return strconv.Itoa(n), nil
		},
		"read": func(ctx context.Context, guid string, p payload) (any, error) {
			if strings.TrimSpace(p.ID) == "" {
				return s.Store.MailList(ctx, guid, p.Folder)
			}
			id, err := strconv.ParseInt(p.ID, 10, 64)
			if err != nil {
				return []model.Message{}, nil
			}
			return s.Store.MailRead(ctx, guid, id)
		},
		"delete": func(ctx context.Context, guid string, p payload) (any, error) {
			id, err := strconv.ParseInt(p.ID, 10, 64)
			if err != nil {
				return model.Status{Status: "error", Msg: "no such message"}, nil
			}
			return model.OK(), s.Store.MailDelete(ctx, guid, id)
		},
		"send": func(ctx context.Context, guid string, p payload) (any, error) {
			return model.OK(), s.Store.MailSend(ctx, guid, p.To, p.Subject, p.Body)
		},
	}
}

// handleAuthInfo serves the game-server mod.
//
// Unauthenticated on purpose. A dedicated server has no player token -- it is
// asking about other people -- and everything this returns is public: a name
// and a clan tag are on the scoreboard of every server that player joins.
// Guarding it would only add a shared secret to distribute and rotate, for
// data anyone can read by joining a game.
//
// It returns exactly the record the game's auth-info format wants, so the mod
// can drop it into %client.t2csri_authInfo without reformatting:
//
//	Name <TAB> ActiveTag <TAB> Prepend(0)/Append(1) <TAB> guid
//	NumberOfClans
//	ClanName <TAB> Tag <TAB> Append <TAB> clanid <TAB> rank <TAB> title
func (s *Server) handleAuthInfo(w http.ResponseWriter, r *http.Request) {
	guid := r.FormValue("guid")
	if guid == "" {
		fatal(w, http.StatusNotImplemented, "501 Not Implemented")
		return
	}

	name, tag, append_, memberships, err := s.Store.ClanTagFor(r.Context(), guid)
	if err != nil {
		// An unknown player is not an error: they simply have no tag here, and
		// the mod should leave their name alone.
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("\n"))
		return
	}

	var b strings.Builder
	b.WriteString(name + "\t" + tag + "\t" + model.Bool(append_) + "\t" + guid + "\n")
	b.WriteString(strconv.Itoa(len(memberships)) + "\n")
	for _, m := range memberships {
		b.WriteString(m.Name + "\t" + m.Tag + "\t" + m.Append + "\t" +
			m.ID + "\t" + m.Rank + "\t" + m.Title + "\n")
	}

	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("\n" + b.String()))
}
