// The public website: a read-only view of the same community the game screens
// show, plus somewhere to get the mod.
//
// Everything here is unauthenticated, and for the reason handleAuthInfo already
// gives: a warrior name, a tribe tag and a roster are on the scoreboard of
// every server those players join, so a login here would guard nothing while
// costing a credential to distribute. What it must not touch is everything
// else -- mail, buddy lists, blocks and invitations are private, and no handler
// below can reach them.
//
// The other rule is that none of this may borrow the client's plumbing.
// writeJSON, fatal and closeAfterResponse in api.go are shaped by Torque's
// HTTPObject: a blank first line before the body, an HTML error page the mod
// sniffs for "401", and a closed connection because the engine has no other
// signal that a transfer finished. All three are wrong for a browser -- the
// last one especially, since closing after every asset would turn one page load
// into a dozen handshakes. So the site writes its own.
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/henrik/tnbrowser-server/internal/model"
	"github.com/henrik/tnbrowser-server/internal/store"
	"github.com/henrik/tnbrowser-server/web"
)

//-----------------------------------------------------------------------------
// The JSON the React app reads
//-----------------------------------------------------------------------------

// page is the envelope both directories answer with. The client needs the total
// to draw a pager, and its own page number back to know which of several
// requests in flight it is looking at.
type page[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
	Page  int `json:"page"`
	Pages int `json:"pages"`
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	counts, err := s.Store.Counts(r.Context())
	if err != nil {
		s.Log.Error("site stats", "err", err)
		siteError(w, http.StatusInternalServerError, "the community database is not answering")
		return
	}
	siteJSON(w, counts)
}

func (s *Server) handleWarriors(w http.ResponseWriter, r *http.Request) {
	q, num, limit, offset := pageParams(r)

	items, total, err := s.Store.Warriors(r.Context(), q, limit, offset)
	if err != nil {
		s.Log.Error("site warriors", "err", err)
		siteError(w, http.StatusInternalServerError, "the community database is not answering")
		return
	}
	siteJSON(w, page[model.DirectoryWarrior]{
		Items: items, Total: total, Page: num, Pages: pages(total, limit),
	})
}

func (s *Server) handleTribes(w http.ResponseWriter, r *http.Request) {
	q, num, limit, offset := pageParams(r)

	items, total, err := s.Store.Tribes(r.Context(), q, limit, offset)
	if err != nil {
		s.Log.Error("site tribes", "err", err)
		siteError(w, http.StatusInternalServerError, "the community database is not answering")
		return
	}
	siteJSON(w, page[model.DirectoryTribe]{
		Items: items, Total: total, Page: num, Pages: pages(total, limit),
	})
}

// warriorPage is a profile and its audit trail in one reply. Two round trips
// from the browser would only ever be made together.
type warriorPage struct {
	Warrior *model.User          `json:"warrior"`
	History []model.HistoryEntry `json:"history"`
}

func (s *Server) handleWarrior(w http.ResponseWriter, r *http.Request) {
	guid := r.PathValue("guid")

	user, err := s.Store.UserView(r.Context(), guid)
	if errors.Is(err, store.ErrNotFound) {
		siteError(w, http.StatusNotFound, "no warrior with that id")
		return
	}
	if err != nil {
		s.Log.Error("site warrior", "guid", guid, "err", err)
		siteError(w, http.StatusInternalServerError, "the community database is not answering")
		return
	}

	// A profile without its history is still a profile: a failure here is worth
	// logging but not worth turning a working page into an error.
	history, err := s.Store.UserHistory(r.Context(), guid)
	if err != nil {
		s.Log.Error("site warrior history", "guid", guid, "err", err)
		history = []model.HistoryEntry{}
	}
	siteJSON(w, warriorPage{Warrior: user, History: history})
}

type tribePage struct {
	Tribe   *model.Clan          `json:"tribe"`
	History []model.HistoryEntry `json:"history"`
}

func (s *Server) handleTribe(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		siteError(w, http.StatusNotFound, "no tribe with that id")
		return
	}

	clan, err := s.Store.ClanView(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		siteError(w, http.StatusNotFound, "no tribe with that id")
		return
	}
	if err != nil {
		s.Log.Error("site tribe", "id", id, "err", err)
		siteError(w, http.StatusInternalServerError, "the community database is not answering")
		return
	}

	// ClanView does not filter on active, because the client follows links to
	// disbanded tribes out of old history and mail. The directory does filter,
	// so a disbanded tribe is unreachable by browsing -- and should be
	// unreachable by guessing an id too.
	if clan.Active != "1" {
		siteError(w, http.StatusNotFound, "that tribe has disbanded")
		return
	}

	history, err := s.Store.ClanHistory(r.Context(), id)
	if err != nil {
		s.Log.Error("site tribe history", "id", id, "err", err)
		history = []model.HistoryEntry{}
	}
	siteJSON(w, tribePage{Tribe: clan, History: history})
}

func (s *Server) handleLatestRelease(w http.ResponseWriter, r *http.Request) {
	// Get never fails: with GitHub unreachable it answers the permanent
	// download URLs, so the page always has two working buttons.
	siteJSON(w, s.Releases.Get(r.Context()))
}

//-----------------------------------------------------------------------------
// Query parameters
//-----------------------------------------------------------------------------

// pageParams reads ?q=, ?page= and ?size= and clamps them with the same rules
// the store uses, so the reply can report the page it actually served rather
// than the one that was asked for.
func pageParams(r *http.Request) (q string, num, limit, offset int) {
	q = r.URL.Query().Get("q")
	num, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if num < 1 {
		num = 1
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	limit, offset = store.Page(size, num)
	return q, num, limit, offset
}

func pages(total, limit int) int {
	if total <= 0 || limit <= 0 {
		return 0
	}
	return (total + limit - 1) / limit
}

//-----------------------------------------------------------------------------
// Writing a reply to a browser
//-----------------------------------------------------------------------------

// siteJSON is writeJSON without any of the client's quirks: no leading blank
// line, no Connection: close, and a short cache window so a visitor clicking
// between pages does not re-query the database for each step.
func siteJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=30")
	w.WriteHeader(http.StatusOK)

	// HTML escaping off for the reason writeJSON gives: profile text and
	// history lines legitimately carry markup, and React escapes what it
	// renders anyway.
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// siteError is fatal's counterpart: JSON, because the only thing reading it is
// a fetch() that wants a message to show.
func siteError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{msg})
}

//-----------------------------------------------------------------------------
// The single-page app
//-----------------------------------------------------------------------------

// site serves the built React app out of the binary.
//
// Two behaviours matter. Unknown paths get index.html rather than a 404,
// because /warriors/4510186 is a route the browser resolves and not a file
// anybody built -- without this, a refresh or a pasted link is a dead page. And
// unknown /api/ paths are excluded from that, since answering a mistyped
// endpoint with a page of HTML makes a typo look like a parse failure.
type site struct {
	files fs.FS
	index []byte
}

func newSite() *site {
	sub, err := fs.Sub(web.FS, "dist")
	if err != nil {
		// Only reachable if the embed directive itself is wrong, which is a
		// build-time mistake rather than a runtime condition.
		return &site{}
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		// The placeholder build: dist holds nothing but .gitkeep. Serving a
		// sentence that says so beats a 404 that looks like a routing bug.
		return &site{files: sub}
	}
	return &site{files: sub, index: index}
}

func (s *site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.index == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("The web UI was not built into this binary.\n" +
			"Run `npm run build` in server/web, or build the image, which does it for you.\n"))
		return
	}

	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		s.serveIndex(w)
		return
	}

	if !s.isFile(name) {
		// Under assets/ a miss is a miss. Everything there is referenced by a
		// fingerprinted name out of index.html, so a request for one that is
		// not here means a deploy shipped a shell pointing at files it did not
		// ship -- and answering that with the shell itself turns a plain 404
		// into "unexpected token '<'" in a console somewhere.
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
		s.serveIndex(w)
		return
	}

	// Vite fingerprints everything under assets/, so those bytes can never
	// change under a name that has already been handed out. Anything else --
	// a favicon, a loose file -- is served without that promise.
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=300")
	}
	// Existence is settled above, so this only has to do the parts worth not
	// reimplementing: content types, conditional requests and ranges.
	http.FileServerFS(s.files).ServeHTTP(w, r)
}

// isFile reports whether the embedded tree holds a regular file at that path.
// A directory is not one: without this check the file server would answer a
// bare directory with an index listing of the build output.
func (s *site) isFile(name string) bool {
	f, err := s.files.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()

	info, err := f.Stat()
	return err == nil && !info.IsDir()
}

// serveIndex answers with the app shell. no-cache rather than no-store: the
// browser may keep it, but must revalidate, or a deploy that changes the asset
// hashes leaves visitors on a shell pointing at files that no longer exist.
func (s *site) serveIndex(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(s.index)
}
