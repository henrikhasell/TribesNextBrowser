// Package api is the front door, and nothing here knows what an ordinal means.
//
// The game client:
//
//	POST /session   negotiate a session (internal/auth)
//	POST /db        one stored-procedure ordinal (internal/dbproxy)
//	GET  /cert      the identity WONGetAuthInfo() hands the scripts
//	GET  /clancert  a signed token a game server checks to render a tribe tag
//	GET  /healthz
//
// The website, which is read-only, unauthenticated and shares nothing with the
// above but the store underneath it -- see site.go:
//
//	GET  /api/stats           three numbers for the landing page
//	GET  /api/warriors        the warrior directory
//	GET  /api/tribes          the tribe directory
//	GET  /api/releases/latest where to get the newest .vl2 (internal/release)
//	GET  /api/openapi.yaml    the specification for all of the above
//	GET  /docs                Swagger UI over it
//	GET  /                    the built React app (server/web)
//
// Everything speaks JSON, in both directions, with two shapes and no third:
// an answer, or {"error": "<slug>", "message": "<sentence>"}. Identity travels
// in an Authorization header rather than in the body or the query string, so
// every authenticated route carries it the same way.
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
	"log/slog"
	"net/http"
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

// request is the body of /db.
//
// Args is an array rather than the tab-joined string the shipped scripts hand
// DatabaseQuery. The client splits it on the way out, which costs nothing and
// removes the one ambiguity a joined string has: "a\t\t" cannot say whether it
// is two arguments or three, and ["a","",""] can. Three ordinals genuinely vary
// their argument count, so that distinction is not academic.
type request struct {
	Form    string   `json:"form"`
	Ordinal string   `json:"ordinal"`
	Args    []string `json:"args"`
}

// route is one registration. The list is data rather than a sequence of calls
// so that a test can walk it against the specification -- a hand-written spec
// is only worth having if something notices when it stops matching.
type route struct {
	pattern string
	handler http.HandlerFunc
}

// routes is everything this server answers.
func (s *Server) routes() []route {
	return []route{
		// The game client. Reads are GET; the two that change something POST.
		{"POST /session", s.handleSession},
		{"POST /db", s.handleDB},
		{"GET /cert", s.handleCert},
		{"GET /clancert", s.handleClanCert},
		{"GET /healthz", func(w http.ResponseWriter, r *http.Request) {
			siteJSON(w, map[string]string{"status": "ok"})
		}},

		// The website. Read-only and unauthenticated; see site.go.
		{"GET /api/stats", s.handleStats},
		{"GET /api/warriors", s.handleWarriors},
		{"GET /api/warriors/{guid}", s.handleWarrior},
		{"GET /api/tribes", s.handleTribes},
		{"GET /api/tribes/{id}", s.handleTribe},
		{"GET /api/releases/latest", s.handleLatestRelease},

		// The specification, and something to read it with.
		{"GET /api/openapi.yaml", handleSpec},

		// Anything under /api/ that got this far is a mistyped endpoint. It
		// must answer as JSON rather than fall through to the app shell, or a
		// typo arrives at the caller looking like a parse failure.
		{"/api/", func(w http.ResponseWriter, r *http.Request) {
			siteError(w, http.StatusNotFound, "not_found", "No such endpoint.")
		}},
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	for _, r := range s.routes() {
		mux.HandleFunc(r.pattern, r.handler)
	}

	// The built app, and every path the browser resolves for itself. Registered
	// last and least specific, so it cannot shadow anything above.
	mux.Handle("/", newSite())

	return s.logRequests(mux)
}

//-----------------------------------------------------------------------------
// The session
//-----------------------------------------------------------------------------

// handleSession is the whole of authentication, in three shapes distinguished
// by what the body carries:
//
//	{guid, cert, nonce}  ->  {"state":"challenge","challenge":"<hex>"}
//	{guid, response}     ->  {"state":"granted","uuid":"<token>"}
//	{guid, uuid}         ->  {"state":"refreshed"} or {"state":"expired"}
//
// Every failure answers an error object, which the client shows and then
// retries with a quadratic backoff. No failure here distinguishes "no such
// account" from "wrong key" -- there is nothing to gain by helping someone find
// out which.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GUID     string `json:"guid"`
		Cert     string `json:"cert"`
		Nonce    string `json:"nonce"`
		Response string `json:"response"`
		UUID     string `json:"uuid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fault(w, http.StatusBadRequest, "bad_request", "That request could not be read.")
		return
	}
	if req.GUID == "" {
		fault(w, http.StatusBadRequest, "bad_request", "No GUID was sent.")
		return
	}

	switch {
	case req.Cert != "":
		blob, id, err := s.Sessions.Challenge(req.Cert, req.Nonce)
		if err != nil {
			s.Log.Warn("session challenge refused", "guid", req.GUID, "err", err)
			fault(w, http.StatusUnauthorized, "bad_certificate",
				"That account certificate was not accepted.")
			return
		}
		// The certificate names the account; a guid disagreeing with it is
		// either confusion or an attempt to have the challenge answered under
		// somebody else's name.
		if id.GUID != req.GUID {
			fault(w, http.StatusUnauthorized, "bad_certificate",
				"That certificate is for a different account.")
			return
		}
		writeJSON(w, sessionState{State: "challenge", Challenge: blob})

	case req.Response != "":
		token, id, err := s.Sessions.Answer(req.GUID, req.Response)
		if err != nil {
			fault(w, http.StatusUnauthorized, "bad_response",
				"That challenge response was not accepted.")
			return
		}
		s.Log.Info("session established", "guid", id.GUID, "name", id.Name)
		writeJSON(w, sessionState{State: "granted", UUID: token})

	case req.UUID != "":
		if _, ok := s.Sessions.Lookup(req.GUID, req.UUID); !ok {
			// Not an error: the client answers this by negotiating a fresh
			// session, which is exactly right after a restart dropped the table.
			writeJSON(w, sessionState{State: "expired"})
			return
		}
		writeJSON(w, sessionState{State: "refreshed"})

	case s.TrustGUID:
		// The bypass, and the only way a client with no account subsystem can
		// get a session at all: it has no certificate to send and no key to
		// answer a challenge with. Off in any deployment that matters.
		token, err := s.Sessions.Grant(auth.Identity{GUID: req.GUID})
		if err != nil {
			s.Log.Error("granting a bypass session", "err", err)
			fault(w, http.StatusInternalServerError, "internal",
				"That request could not be completed.")
			return
		}
		s.Log.Warn("session granted without proof (-dev-trust-guid)", "guid", req.GUID)
		writeJSON(w, sessionState{State: "granted", UUID: token})

	default:
		fault(w, http.StatusBadRequest, "bad_request", "Nothing to do.")
	}
}

// sessionState is the one shape /session answers with. Only the field the state
// needs is present, so a caller cannot read a challenge off a refusal.
type sessionState struct {
	State     string `json:"state"`
	Challenge string `json:"challenge,omitempty"`
	UUID      string `json:"uuid,omitempty"`
}

//-----------------------------------------------------------------------------
// The database proxy
//-----------------------------------------------------------------------------

func (s *Server) handleDB(w http.ResponseWriter, r *http.Request) {
	// A v1 mod posts its query as a urlencoded form field. Say so plainly:
	// without this the JSON decode fails and the player gets a parse error for
	// what is really an out-of-date install.
	if r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
		clientTooOld(w)
		return
	}

	c, ok := s.authenticate(w, r)
	if !ok {
		return
	}

	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, dbproxy.Refuse("The community server could not read that request."))
		return
	}

	if req.Form != dbproxy.Scalar && req.Form != dbproxy.Array {
		writeJSON(w, dbproxy.Refuse("Unknown query form "+req.Form+"."))
		return
	}

	answer, err := dbproxy.Dispatch(c, req.Form, req.Ordinal, req.Args)
	if err != nil {
		// A fault, not a refusal. Dispatch has already turned everything the
		// player could have caused into a well-formed refusal, so reaching here
		// means we are broken and should say so as a 500 rather than dress it
		// up as a rejected request.
		s.Log.Error("ordinal failed", "form", req.Form, "ordinal", req.Ordinal, "err", err)
		fault(w, http.StatusInternalServerError, "internal",
			"The community server failed on that request.")
		return
	}

	if answer.Rows == nil {
		answer.Rows = [][]any{}
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

	id, err := dbproxy.WarriorIdentity(c)
	if err != nil {
		s.Log.Error("identity failed", "guid", c.GUID, "err", err)
		fault(w, http.StatusInternalServerError, "internal",
			"The community server could not build your identity record.")
		return
	}
	writeJSON(w, id)
}

// handleClanCert issues the token a player carries into a game.
//
// The client fetches this and hands it to game servers that ask; a game server
// running TNBrowserServer checks the signature and renders the tag, with no
// HTTP request of its own on the connect path.
//
// The token inside is deliberately not JSON. Its signature covers the literal
// joined bytes, and the mod that verifies it does so inside
// GameConnection::onConnect with getField and sha1sum and no parser at all.
// This response is JSON around an opaque credential, exactly as an OAuth
// response is JSON around an access_token. See internal/clancert.
//
// Session-authenticated, unlike everything else about a tribe tag, because this
// one is issued *to* a player: it says who they are, so it may only be handed
// to them. The 404 for an unconfigured key comes before authentication, so a
// deployment without one does no database work to answer a request it cannot
// serve.
func (s *Server) handleClanCert(w http.ResponseWriter, r *http.Request) {
	if s.ClanCerts == nil {
		fault(w, http.StatusNotFound, "not_configured",
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
		fault(w, http.StatusInternalServerError, "internal",
			"The community server could not build your tribe record.")
		return
	}

	now := time.Now()
	token, err := s.ClanCerts.Sign(c.GUID, record, now)
	if err != nil {
		s.Log.Error("signing a clan certificate", "guid", c.GUID, "err", err)
		fault(w, http.StatusInternalServerError, "internal",
			"The community server could not sign your tribe record.")
		return
	}

	writeJSON(w, struct {
		Certificate string `json:"certificate"`
		Expires     int64  `json:"expires"`
	}{token, now.Add(s.ClanCerts.TTL()).Unix()})
}

//-----------------------------------------------------------------------------
// Shared front-door work
//-----------------------------------------------------------------------------

// authenticate reads the Authorization header and returns the context a handler
// runs in. It writes the failure itself and reports false, so a caller is a
// two-line guard.
//
//	Authorization: TNB <guid>:<uuid>
//
// A header rather than a body field or a query parameter, so every
// authenticated route carries identity the same way and the specification can
// describe it once as a security scheme. It also keeps the credential out of
// the request line, which is the part that ends up in access logs.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (*dbproxy.Ctx, bool) {
	guid, uuid, ok := credentials(r)
	if !ok || guid == "" {
		fault(w, http.StatusUnauthorized, "session_expired",
			"Session expired. Please try again.")
		return nil, false
	}

	id, found := s.Sessions.Lookup(guid, uuid)
	if !found {
		if !s.TrustGUID {
			fault(w, http.StatusUnauthorized, "session_expired",
				"Session expired. Please try again.")
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
		fault(w, http.StatusInternalServerError, "internal",
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

// credentials unpacks "TNB <guid>:<uuid>".
func credentials(r *http.Request) (guid, uuid string, ok bool) {
	const scheme = "TNB "

	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, scheme) {
		return "", "", false
	}
	guid, uuid, ok = strings.Cut(strings.TrimPrefix(h, scheme), ":")
	return guid, uuid, ok
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
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	closeAfterResponse(w)
	w.WriteHeader(http.StatusOK)

	// HTML escaping off: Go would turn < and > into \u003c, and rows
	// legitimately carry markup the game renders -- every invitation mail has
	// an <a:acceptinvite...> link in its body, and a warrior profile can have
	// one too.
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// closeAfterResponse ends the connection once the body is written.
//
// Not an optimisation -- a correctness requirement, and the client's, not ours.
// Torque's HTTPObject reports a completed transfer as onDisconnect and has no
// other completion signal, so the mod's request queue advances only when the
// socket closes. Serve a keep-alive response and the queue stops dead with the
// answer already in its buffer: $TNB::Busy stays 1, and every subsequent
// request waits behind a transfer that finished minutes ago.
//
// Found by sweeping the ordinals: the client answered ten of them and then hung
// with the eleventh answer already sitting in its buffer, waiting for a socket
// close that a keep-alive response never sends.
func closeAfterResponse(w http.ResponseWriter) {
	w.Header().Set("Connection", "close")
}

// fault is every failure, on every route the game client calls.
//
// One shape, and the same one site.go answers the website with: a stable slug a
// caller can branch on and a sentence a pane can show. The HTTP status carries
// the transport outcome, so nothing has to be smuggled through the body -- which
// is what the previous two designs did, first as an HTML page the client
// grepped for "401" and then as an ordinal status with the code in field 0.
//
// session_expired is the one slug the client acts on: it drops its token and
// the next request negotiates a fresh one.
func fault(w http.ResponseWriter, code int, slug, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	closeAfterResponse(w)
	w.WriteHeader(code)

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}{slug, msg})
}

// clientTooOld answers a v1 mod, which posts its query as a urlencoded form.
//
// Worth a sentence of its own rather than a parse failure: the player has an
// out-of-date install, which is a thing they can fix, and "could not read that
// request" would send them looking for a server fault instead.
func clientTooOld(w http.ResponseWriter) {
	fault(w, http.StatusBadRequest, "client_too_old",
		"This community server needs TNBrowser v2. Download the current "+
			"package and replace the one in GameData/base/.")
}
