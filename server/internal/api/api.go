// Package api is the front door, and nothing here knows what an ordinal means.
//
// The game client:
//
//	POST /session             negotiate a session (internal/auth)
//	POST /db                  one stored-procedure ordinal (internal/dbproxy)
//	POST /cert                the identity WONGetAuthInfo() hands the scripts
//	POST /clancert            the same record, signed, for a game server to check
//	GET  /tn/server/authinfo  the game-server mod's clan lookup
//	GET  /healthz
//
// The website, which is read-only, unauthenticated and shares nothing with the
// above but the store underneath it -- see site.go:
//
//	GET  /api/stats           three numbers for the landing page
//	GET  /api/warriors        the warrior directory
//	GET  /api/tribes          the tribe directory
//	GET  /api/releases/latest where to get the newest .vl2 (internal/release)
//	GET  /                    the built React app (server/web)
//
// One quirk in here is deliberate and load-bearing: every answer to the game
// closes the connection. Torque's HTTPObject reports a completed transfer only
// as onDisconnect and has no other completion signal, so a keep-alive response
// leaves the mod's request queue waiting forever on a transfer that finished.
// See closeAfterResponse.
//
// Identity comes from TribesNext and is checked here rather than asked about:
// the player's account certificate is verified against TribesNext's pinned
// signing key, and they are challenged to prove they hold the private half.
// That is what every game server does, and for the same reason -- the proof is
// in the certificate, so the issuer does not have to be reachable. Nothing in
// this server makes an outbound call to authenticate anybody. See internal/auth.
package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/henrik/tnbrowser-server/internal/auth"
	"github.com/henrik/tnbrowser-server/internal/clancert"
	"github.com/henrik/tnbrowser-server/internal/dbproxy"
	"github.com/henrik/tnbrowser-server/internal/release"
	"github.com/henrik/tnbrowser-server/internal/store"
)

type Server struct {
	Store    *store.Store
	Sessions *auth.Sessions
	Log      *slog.Logger

	// ClanCerts signs the clan record a game server renders into a name. Nil
	// when no key was configured, which is a supported deployment: /clancert
	// answers 404 and players simply carry no tag.
	ClanCerts *clancert.Signer

	// Releases backs the website's download page. Nil is safe -- Get is written
	// for a nil receiver and answers the permanent GitHub download URLs -- so a
	// server assembled without one still serves working buttons.
	Releases *release.Cache

	// TrustGUID accepts a bare guid with no proof of anything, for driving the
	// in-game test suites: the containers they run in hold no account key
	// material, so a client in one cannot answer a challenge. Never set outside
	// a test run -- with this on, anybody can be anybody.
	TrustGUID bool
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

	// The game client. These five are a wire protocol and their paths, methods
	// and bodies are fixed -- see the package comment.
	mux.HandleFunc("/session", s.handleSession)
	mux.HandleFunc("/db", s.handleDB)
	mux.HandleFunc("/cert", s.handleCert)
	mux.HandleFunc("/clancert", s.handleClanCert)
	mux.HandleFunc("/tn/server/authinfo", s.handleAuthInfo)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	// The website. Read-only and unauthenticated; see site.go. A registration
	// above is a more specific pattern than anything here, so adding "/" cannot
	// shadow one of the client's paths.
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/warriors", s.handleWarriors)
	mux.HandleFunc("GET /api/warriors/{guid}", s.handleWarrior)
	mux.HandleFunc("GET /api/tribes", s.handleTribes)
	mux.HandleFunc("GET /api/tribes/{id}", s.handleTribe)
	mux.HandleFunc("GET /api/releases/latest", s.handleLatestRelease)

	// Anything under /api/ that got this far is a mistyped endpoint. It must
	// answer as JSON rather than fall through to the app shell below, or a typo
	// arrives at the caller looking like a parse failure.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		siteError(w, http.StatusNotFound, "no such endpoint")
	})

	mux.Handle("/", newSite())

	return s.logRequests(mux)
}

//-----------------------------------------------------------------------------
// The session
//-----------------------------------------------------------------------------

// handleSession is the whole of authentication, in three shapes distinguished
// by which parameter arrived:
//
//	cert + nonce  ->  "CHALLENGE: <hex>"   prove you hold the key
//	response      ->  "UUID: <token>"      you did
//	uuid          ->  "REFRESHED"          keepalive, or "TIMEOUT" if it lapsed
//
// Plain text lines rather than JSON, and deliberately. This is the login path:
// it runs before anything else works, it has exactly five possible answers, and
// keeping it to one line each means the session layer can read it with
// getSubStr and no parser at all. The client's backoff and retry rules
// (session.cs:261-316) are built on that, and there is nothing JSON would buy
// them.
//
// Every failure answers "ERR: <sentence>", which the client shows and then
// retries with a quadratic backoff. No failure here distinguishes "no such
// account" from "wrong key" -- there is nothing to gain by helping someone find
// out which.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		sessionLine(w, "ERR: That request could not be read.")
		return
	}

	guid := r.FormValue("guid")
	if guid == "" {
		sessionLine(w, "ERR: No GUID specified.")
		return
	}

	switch {
	case r.FormValue("cert") != "":
		blob, id, err := s.Sessions.Challenge(r.FormValue("cert"), r.FormValue("nonce"))
		if err != nil {
			s.Log.Warn("session challenge refused", "guid", guid, "err", err)
			sessionLine(w, "ERR: That account certificate was not accepted.")
			return
		}
		// The certificate names the account; a guid parameter disagreeing with
		// it is either confusion or an attempt to have the challenge answered
		// under somebody else's name.
		if id.GUID != guid {
			sessionLine(w, "ERR: That certificate is for a different account.")
			return
		}
		sessionLine(w, "CHALLENGE: "+blob)

	case r.FormValue("response") != "":
		token, id, err := s.Sessions.Answer(guid, r.FormValue("response"))
		if err != nil {
			sessionLine(w, "ERR: That challenge response was not accepted.")
			return
		}
		s.Log.Info("session established", "guid", id.GUID, "name", id.Name)
		sessionLine(w, "UUID: "+token)

	case r.FormValue("uuid") != "":
		if _, ok := s.Sessions.Lookup(guid, r.FormValue("uuid")); !ok {
			// Not an error: the client answers this by negotiating a fresh
			// session (session.cs:301-309), which is exactly right after a
			// restart dropped the table.
			sessionLine(w, "TIMEOUT")
			return
		}
		sessionLine(w, "REFRESHED")

	case s.TrustGUID:
		// The bypass, and the only way a client with no account subsystem can
		// get a session at all: it has no certificate to send and no key to
		// answer a challenge with. Off in any deployment that matters.
		token, err := s.Sessions.Grant(auth.Identity{GUID: guid})
		if err != nil {
			s.Log.Error("granting a bypass session", "err", err)
			sessionLine(w, "ERR: That request could not be completed.")
			return
		}
		s.Log.Warn("session granted without proof (-dev-trust-guid)", "guid", guid)
		sessionLine(w, "UUID: "+token)

	default:
		sessionLine(w, "ERR: Nothing to do.")
	}
}

// sessionLine writes one plain-text line: the whole of a session response.
func sessionLine(w http.ResponseWriter, line string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, line+"\n")
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
		gameFault(w, http.StatusInternalServerError,
			"The community server failed on that request.")
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
		gameFault(w, http.StatusInternalServerError,
			"The community server could not build your identity record.")
		return
	}
	writeJSON(w, struct {
		Cert string `json:"cert"`
	}{cert})
}

// handleClanCert is the same record again, signed, for the player to carry.
//
// The client fetches this and hands it to game servers that ask; a game server
// running TNBrowserServer checks the signature against the public half and
// renders the tag, with no HTTP request of its own on the connect path. See
// internal/clancert for why it is a certificate rather than the lookup
// /tn/server/authinfo still serves.
//
// Session-authenticated, unlike that lookup, because this one is issued *to* a
// player: it says who they are, so it may only be handed to them. The 404 for
// an unconfigured key comes before authentication, so a deployment without one
// does no database work to answer a request it cannot serve.
func (s *Server) handleClanCert(w http.ResponseWriter, r *http.Request) {
	if s.ClanCerts == nil {
		gameFault(w, http.StatusNotFound,
			"This community server does not issue tribe certificates.")
		return
	}

	c, ok := s.authenticate(w, r)
	if !ok {
		return
	}

	record, err := dbproxy.Certificate(c)
	if err != nil {
		s.Log.Error("clan certificate record", "guid", c.GUID, "err", err)
		gameFault(w, http.StatusInternalServerError,
			"The community server could not build your tribe record.")
		return
	}

	cert, err := s.ClanCerts.Sign(c.GUID, record, time.Now())
	if err != nil {
		s.Log.Error("signing a clan certificate", "guid", c.GUID, "err", err)
		gameFault(w, http.StatusInternalServerError,
			"The community server could not sign your tribe record.")
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
		gameFault(w, http.StatusNotImplemented, "That lookup needs a guid.")
		return
	}

	cert, err := dbproxy.Certificate(&dbproxy.Ctx{
		Ctx: r.Context(), Store: s.Store, GUID: guid,
	})
	if err != nil {
		// An unknown player is not an error: they simply have no tag here, and
		// a reader should leave their name alone.
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("\n"))
		return
	}

	// The record and nothing before it. It used to be preceded by a blank line,
	// which was framing rather than layout -- a leading newline puts an empty
	// string where field 0 should be, and field 0 is the warrior's name.
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(cert + "\n"))
}

//-----------------------------------------------------------------------------
// Shared front-door work
//-----------------------------------------------------------------------------

// authenticate parses the request, verifies the session upstream and returns
// the context a handler runs in. It writes the failure itself and reports
// false, so a caller is a two-line guard.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (*dbproxy.Ctx, bool) {
	if err := r.ParseForm(); err != nil {
		gameFault(w, http.StatusBadRequest, "That request could not be read.")
		return nil, false
	}

	guid := r.FormValue("guid")
	uuid := r.FormValue("uuid")
	if guid == "" {
		gameFault(w, http.StatusUnauthorized, "Session expired. Please try again.")
		return nil, false
	}

	id, ok := s.Sessions.Lookup(guid, uuid)
	if !ok {
		if !s.TrustGUID {
			gameFault(w, http.StatusUnauthorized, "Session expired. Please try again.")
			return nil, false
		}
		// The test bypass. A bare GUID carries no name, so it is read back out
		// of our own records below rather than invented -- EnsureAccount keeps
		// whatever the account already had, and several ordinals put the
		// caller's name in the sentence they answer with.
		id = auth.Identity{GUID: guid}
	}

	created, err := s.Store.EnsureAccount(r.Context(), id.GUID, id.Name, id.Created)
	if err != nil {
		s.Log.Error("ensure account", "err", err, "guid", id.GUID)
		gameFault(w, http.StatusInternalServerError,
			"The community server could not reach its database.")
		return nil, false
	}

	// Only the bypass gets here without a name: a certificate always carries
	// one, signed.
	if id.Name == "" {
		if q, err := s.Store.Quad(r.Context(), id.GUID); err == nil {
			id.Name = q.Name
		}
	}

	// First sighting: tell them the community screens work again. Logged and
	// not fatal -- a greeting that fails must not lock a new player out of the
	// service it is greeting them to.
	if created {
		if err := s.Store.WelcomeMail(r.Context(), id.GUID); err != nil {
			s.Log.Error("welcome mail", "err", err, "guid", id.GUID)
		}
	}

	return &dbproxy.Ctx{
		Ctx:   r.Context(),
		Store: s.Store,
		GUID:  id.GUID,
		Name:  id.Name,
		Log:   s.Log,
	}, true
}

// logRequests writes one line per request: what was asked, by whom, what came
// back, and how long it took.
//
// The uuid is deliberately never logged. It is a bearer credential: anyone
// holding one is that player here until it lapses, so a log file full of them
// would be as sensitive as a password store -- and logs get pasted into bug
// reports. The guid is logged: it identifies the player but grants nothing, and
// without it a log cannot answer "did that player's request arrive?".
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		// A page load fetches a dozen fingerprinted assets, and a log that
		// records each of them cannot answer the question it exists for: "did
		// that player's request arrive?". Successful asset requests are
		// therefore dropped. A failing one is still logged, because a 404 under
		// assets/ means a deploy shipped a broken index.
		if rec.status < 400 && strings.HasPrefix(r.URL.Path, "/assets/") {
			return
		}

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

// writeJSON emits an answer to the game client.
//
// It used to write a blank line before the body, because the backend this
// replaced did and a curl session against either was then byte-comparable.
// Nothing needs that now: the client trims the body before parsing it
// (api.cs:304) and the session layer skips blank lines outright
// (session.cs:399).
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	closeAfterResponse(w)
	w.WriteHeader(http.StatusOK)

	// HTML escaping off: Go would turn < and > into </>, and rows
	// legitimately carry markup the game renders -- every invitation mail has
	// an <a:acceptinvite...> link in its body, and a warrior profile can have
	// one too.
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// closeAfterResponse ends the connection once the body is written.
//
// Not an optimisation -- a correctness requirement, and the client's, not
// ours. Torque's HTTPObject reports a completed transfer as onDisconnect and
// has no other completion signal, so the mod's request queue advances only
// when the socket closes. Serve a keep-alive response and the queue stops dead
// with the answer already in its buffer: $TNB::Busy stays 1, and every
// subsequent request waits behind a transfer that finished minutes ago.
//
// Found by sweeping the ordinals: the client answered ten of them and then
// hung with the eleventh answer already sitting in its buffer, waiting for a
// socket close that a keep-alive response never sends.
func closeAfterResponse(w http.ResponseWriter) {
	w.Header().Set("Connection", "close")
}

// gameFault reports a transport failure to the game client -- no session, a
// route it should not have called, or a fault of ours.
//
// It answers in the same {status,result,rows} shape every ordinal uses, which
// is the one thing the client can always parse. The HTTP code goes in field 0
// of status, where onDatabaseQueryResult already looks, and a sentence in field
// 1, where several panes already read one to put in a MessageBoxOK. So a 500
// now says something a player can repeat to somebody, instead of arriving as
// "Unreadable response from the community server."
//
// This used to be an HTML page -- "<h1>Fatal Error</h1><h2>401 Authentication
// Required</h2>" -- copied from the backend this replaced so that the client's
// habit of grepping the raw body for "401" would keep working. The client reads
// the field now (api.cs), and nothing here imitates anybody.
func gameFault(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	closeAfterResponse(w)
	w.WriteHeader(code)

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(dbproxy.Answer{
		Status: strconv.Itoa(code) + "\t" + msg,
		Result: "0",
		Rows:   []string{},
	})
}
