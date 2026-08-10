// Package api is the front door: four routes, and nothing that knows what an
// ordinal means.
//
//	POST /db                  one stored-procedure ordinal (internal/dbproxy)
//	POST /cert                the identity WONGetAuthInfo() hands the scripts
//	GET  /tn/server/authinfo  the game-server mod's clan lookup
//	GET  /healthz
//
// The interesting thing about this file is what is no longer in it. It used to
// serve json_browser.php and json_mail.php -- TribesNext's method-and-JSON
// protocol -- because the mod was a set of hand-built screens that spoke it.
// The mod now runs the shipped screens instead, and the shipped screens speak
// ordinals, so the whole method table went with them.
//
// Authentication did not change and is not ours: a player proves who they are
// to TribesNext with their RSA key, and internal/auth asks TribesNext whether
// the resulting token is real. This server holds no passwords and no keys. The
// one place json_browser.php survives is inside that check, as somebody else's
// verification oracle -- which is a different thing from a protocol we speak.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/henrik/tnbrowser-server/internal/auth"
	"github.com/henrik/tnbrowser-server/internal/dbproxy"
	"github.com/henrik/tnbrowser-server/internal/store"
)

type Server struct {
	Store    *store.Store
	Verifier *auth.Verifier
	Log      *slog.Logger
}

// request is the decoded `payload` parameter of /db.
//
// Args stays the single tab-joined string the call site assembled. Splitting it
// client-side would mean deciding a field count, and three ordinals genuinely
// vary theirs -- scalar 14 sends one field or three depending on which of its
// call sites fired.
//
// MaxRows is carried and not used. It is nominally a row cap, but three call
// sites put a page number in that slot instead (webnews.cs:467,
// webforums.cs:602, :920) and a fourth puts a real limit there (:935), so it
// cannot be read as a cap without capping the wrong thing. Ordinals that need a
// bound pick their own.
type request struct {
	Form    string `json:"form"`
	Ordinal string `json:"ordinal"`
	MaxRows string `json:"maxRows"`
	Args    string `json:"args"`
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/db", s.handleDB)
	mux.HandleFunc("/cert", s.handleCert)
	mux.HandleFunc("/tn/server/authinfo", s.handleAuthInfo)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	return s.logRequests(mux)
}

//-----------------------------------------------------------------------------
// The database proxy
//-----------------------------------------------------------------------------

func (s *Server) handleDB(w http.ResponseWriter, r *http.Request) {
	c, ok := s.authenticate(w, r)
	if !ok {
		return
	}

	var req request
	if raw := r.FormValue("payload"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &req); err != nil {
			writeJSON(w, dbproxy.Answer{
				Status: "1\tThe community server could not read that request.",
				Result: "0",
				Rows:   []string{},
			})
			return
		}
	}

	if req.Form != dbproxy.Scalar && req.Form != dbproxy.Array {
		writeJSON(w, dbproxy.Answer{
			Status: "1\tUnknown query form " + req.Form + ".",
			Result: "0",
			Rows:   []string{},
		})
		return
	}

	answer, err := dbproxy.Dispatch(c, req.Form, req.Ordinal, req.Args)
	if err != nil {
		// A fault, not a refusal. Dispatch has already turned everything the
		// player could have caused into a well-formed non-zero status, so
		// reaching here means we are broken and should say so as a 500 rather
		// than dress it up as a rejected request.
		s.Log.Error("ordinal failed", "form", req.Form, "ordinal", req.Ordinal, "err", err)
		fatal(w, http.StatusInternalServerError, "500 Internal Server Error")
		return
	}

	if answer.Rows == nil {
		answer.Rows = []string{}
	}
	writeJSON(w, answer)
}

//-----------------------------------------------------------------------------
// The certificate
//-----------------------------------------------------------------------------

func (s *Server) handleCert(w http.ResponseWriter, r *http.Request) {
	c, ok := s.authenticate(w, r)
	if !ok {
		return
	}

	cert, err := dbproxy.Certificate(c)
	if err != nil {
		s.Log.Error("certificate failed", "guid", c.GUID, "err", err)
		fatal(w, http.StatusInternalServerError, "500 Internal Server Error")
		return
	}
	writeJSON(w, struct {
		Cert string `json:"cert"`
	}{cert})
}

// handleAuthInfo is the same record, unauthenticated and as plain text.
//
// Deliberately open. A game server has no player token and needs none: a
// warrior name and a clan tag are on the scoreboard of every server that player
// joins, so guarding this would only add a shared secret to distribute and
// rotate, for data anyone can read by joining a game.
//
// It answers in the layout the game's auth-info format wants, so the server mod
// can drop it into %client.t2csri_authInfo without reformatting -- which is the
// same layout WONGetAuthInfo() hands the client scripts, because it is the same
// record. One producer, two encodings.
func (s *Server) handleAuthInfo(w http.ResponseWriter, r *http.Request) {
	guid := r.FormValue("guid")
	if guid == "" {
		fatal(w, http.StatusNotImplemented, "501 Not Implemented")
		return
	}

	cert, err := dbproxy.Certificate(&dbproxy.Ctx{
		Ctx: r.Context(), Store: s.Store, GUID: guid,
	})
	if err != nil {
		// An unknown player is not an error: they simply have no tag here, and
		// the mod should leave their name alone.
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("\n"))
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("\n" + cert + "\n"))
}

//-----------------------------------------------------------------------------
// Shared front-door work
//-----------------------------------------------------------------------------

// authenticate parses the request, verifies the session upstream and returns
// the context a handler runs in. It writes the failure itself and reports
// false, so a caller is a two-line guard.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (*dbproxy.Ctx, bool) {
	if err := r.ParseForm(); err != nil {
		fatal(w, http.StatusBadRequest, "400 Bad Request")
		return nil, false
	}

	guid := r.FormValue("guid")
	uuid := r.FormValue("uuid")
	if guid == "" || uuid == "" {
		fatal(w, http.StatusUnauthorized, "401 Authentication Required")
		return nil, false
	}

	id, err := s.Verifier.Verify(r.Context(), guid, uuid)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorised) {
			fatal(w, http.StatusUnauthorized, "401 Authentication Required")
			return nil, false
		}
		// Upstream is broken, not the player. 503 keeps that distinction, and
		// the client reports it as a server problem rather than silently
		// dropping the session.
		s.Log.Error("upstream verification failed", "err", err)
		fatal(w, http.StatusServiceUnavailable, "503 Service Unavailable")
		return nil, false
	}

	if err := s.Store.EnsureAccount(r.Context(), id.GUID, id.Name); err != nil {
		s.Log.Error("ensure account", "err", err, "guid", id.GUID)
		fatal(w, http.StatusInternalServerError, "500 Internal Server Error")
		return nil, false
	}

	return &dbproxy.Ctx{
		Ctx:   r.Context(),
		Store: s.Store,
		GUID:  id.GUID,
		Name:  id.Name,
	}, true
}

// logRequests writes one line per request: what was asked, by whom, what came
// back, and how long it took.
//
// The uuid is deliberately never logged. It is a bearer credential that works
// against TribesNext itself and not just here, so a log file holding one would
// be as sensitive as a password store -- and logs get pasted into bug reports.
// The guid is logged: it identifies the player but grants nothing, and without
// it a log cannot answer "did that player's request arrive?".
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		// Read the form only after the handler has run. Calling ParseForm here
		// would consume a POST body and, because ParseForm caches, would stop
		// authenticate's own call from ever reporting a malformed one as 400.
		field := func(k string) string {
			if r.Form != nil {
				return r.Form.Get(k)
			}
			return r.URL.Query().Get(k)
		}

		attrs := []any{
			"verb", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"dur", time.Since(start).Round(time.Millisecond).String(),
			"remote", r.RemoteAddr,
		}
		if p := field("payload"); p != "" {
			// The ordinal, not the arguments: those carry mail bodies and
			// profile text.
			var req request
			if err := json.Unmarshal([]byte(p), &req); err == nil {
				attrs = append(attrs, "q", req.Form+" "+req.Ordinal)
			}
		}
		if g := field("guid"); g != "" {
			attrs = append(attrs, "guid", g)
		}

		// A 5xx is ours to fix; a 4xx is the caller's and is routine here --
		// the client treats 401 as "session expired" and re-logs in.
		switch {
		case rec.status >= 500:
			s.Log.Error("request", attrs...)
		case rec.status >= 400:
			s.Log.Warn("request", attrs...)
		default:
			s.Log.Info("request", attrs...)
		}
	})
}

// statusRecorder remembers what the handler actually sent. net/http offers no
// way to read back a status once written.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// writeJSON emits the body, preceded by the blank line the live TribesNext
// server sends. The client trims it either way; reproducing it keeps a curl
// session against either backend byte-comparable.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	closeAfterResponse(w)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("\n"))

	// HTML escaping off: Go would turn < and > into </>, and rows
	// legitimately carry markup the game renders -- every invitation mail has
	// an <a:acceptinvite...> link in its body, and a warrior profile can have
	// one too.
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// fatal reproduces the HTML error body the PHP backend produced, so the
// client's existing error handling (which sniffs for "401"/"Authentication")
// behaves identically against either server.
// closeAfterResponse ends the connection once the body is written.
//
// Not an optimisation -- a correctness requirement, and the client's, not
// ours. Torque's HTTPObject reports a completed transfer as onDisconnect and
// has no other completion signal, so the mod's request queue advances only
// when the socket closes. Serve a keep-alive response and the queue stops dead
// with the answer already in its buffer: $TNB::Busy stays 1, and every
// subsequent request waits behind a transfer that finished minutes ago.
//
// Found by sweeping the ordinals against this server -- the client answered 10
// and then hung, while the same sweep against tools/mockserver.py (which sends
// Connection: close) passed. Go's keep-alive is the only difference between
// the two.
func closeAfterResponse(w http.ResponseWriter) {
	w.Header().Set("Connection", "close")
}

func fatal(w http.ResponseWriter, code int, text string) {
	w.Header().Set("Content-Type", "text/html")
	closeAfterResponse(w)
	w.WriteHeader(code)
	_, _ = w.Write([]byte("<h1>Fatal Error</h1><h2>" + text + "</h2>"))
}
