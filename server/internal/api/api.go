// Package api serves the wire protocol the Tribes 2 client already speaks.
//
// Paths, parameter names and response shapes are TribesNext's, so pointing the
// mod at this server is a one-line change on the client. The behaviour is the
// behaviour tools/mockserver.py encodes, which was itself written against the
// published json_browser.phps -- the two are kept comparable on purpose so the
// existing client test suites act as a conformance check for this server.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/henrik/tnbrowser-server/internal/auth"
	"github.com/henrik/tnbrowser-server/internal/model"
	"github.com/henrik/tnbrowser-server/internal/store"
)

type Server struct {
	Store    *store.Store
	Verifier *auth.Verifier
	Log      *slog.Logger
}

// payload is the decoded JSON `payload` parameter. Every field the protocol
// uses appears here; methods read the ones they need.
//
// Numeric fields are json.RawMessage-ish strings because the client sends them
// quoted ("id":"7") while some callers send them bare -- flexible on input,
// strict on output.
type payload struct {
	ID     string `json:"id"`
	To     string `json:"to"`
	Q      string `json:"q"`
	V      string `json:"v"`
	Tag    string `json:"tag"`
	Append string `json:"append"`
	Rank   string `json:"rank"`
	Title  string `json:"title"`
	Name   string `json:"name"`
	Info   string `json:"info"`
	Site   string `json:"site"`
	// Extensions beyond TribesNext parity.
	Folder  string `json:"folder"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (p payload) id64() int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(p.ID), 10, 64)
	return n
}

// truthy accepts every spelling the protocol uses for yes: the original PHP
// took "true", "yes" or 1 interchangeably, and the client sends "yes"/"no".
func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// handler is one API method. Returning a *store.UserError produces
// {"status":"error","msg":...}; any other error is a fault and becomes a 500.
type handler func(ctx context.Context, guid string, p payload) (any, error)

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/tn/json/json_browser.php", s.dispatch(s.browserMethods()))
	mux.HandleFunc("/tn/json/json_mail.php", s.dispatch(s.mailMethods()))
	// Not a client endpoint: the game-server mod's clan lookup.
	mux.HandleFunc("/tn/server/authinfo", s.handleAuthInfo)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	return s.logRequests(mux)
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
		// dispatch's own call from ever reporting a malformed one as 400.
		// r.Form is populated by then; the query string covers requests that
		// failed before it was.
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
		if m := field("method"); m != "" {
			attrs = append(attrs, "api", m)
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

// dispatch is the shared front door: parse, verify the session, run the method.
func (s *Server) dispatch(methods map[string]handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			fatal(w, http.StatusBadRequest, "400 Bad Request")
			return
		}

		guid := r.FormValue("guid")
		uuid := r.FormValue("uuid")
		method := r.FormValue("method")

		if guid == "" || uuid == "" {
			fatal(w, http.StatusUnauthorized, "401 Authentication Required")
			return
		}
		if method == "" {
			fatal(w, http.StatusNotImplemented, "501 Not Implemented")
			return
		}

		fn, ok := methods[method]
		if !ok {
			fatal(w, http.StatusNotImplemented, "501 Not Implemented")
			return
		}

		id, err := s.Verifier.Verify(r.Context(), guid, uuid)
		if err != nil {
			if errors.Is(err, auth.ErrUnauthorised) {
				fatal(w, http.StatusUnauthorized, "401 Authentication Required")
				return
			}
			// Upstream is broken, not the player. 503 keeps that distinction,
			// and the client reports it as a server problem rather than
			// silently dropping the session.
			s.Log.Error("upstream verification failed", "err", err)
			fatal(w, http.StatusServiceUnavailable, "503 Service Unavailable")
			return
		}

		if err := s.Store.EnsureAccount(r.Context(), id.GUID, id.Name); err != nil {
			s.Log.Error("ensure account", "err", err, "guid", id.GUID)
			fatal(w, http.StatusInternalServerError, "500 Internal Server Error")
			return
		}

		var p payload
		if raw := r.FormValue("payload"); raw != "" {
			if err := json.Unmarshal([]byte(raw), &p); err != nil {
				writeJSON(w, model.Status{Status: "error", Msg: "malformed payload"})
				return
			}
		}

		result, err := fn(r.Context(), id.GUID, p)
		if err != nil {
			var ue *store.UserError
			if errors.As(err, &ue) {
				writeJSON(w, model.Status{Status: "error", Msg: ue.Msg})
				return
			}
			s.Log.Error("method failed", "method", method, "err", err)
			fatal(w, http.StatusInternalServerError, "500 Internal Server Error")
			return
		}
		writeJSON(w, result)
	}
}

// writeJSON emits the body, preceded by the blank line the live server sends.
// The client trims it, but reproducing it keeps the two backends
// byte-comparable when debugging with curl.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("\n"))

	// HTML escaping off: Go would turn < and > into \u003c/\u003e, and profile
	// text legitimately contains markup like <a:www.example.com>link</a> that the
	// game renders. PHP did not escape it, so neither do we -- the client parser
	// would cope either way, but keeping the bytes identical means the two
	// backends can be compared directly.
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// fatal reproduces the HTML error body the PHP backend produced, so the
// client's existing error handling (which sniffs for "401"/"Authentication")
// behaves identically against either server.
func fatal(w http.ResponseWriter, code int, text string) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(code)
	_, _ = w.Write([]byte("<h1>Fatal Error</h1><h2>" + text + "</h2>"))
}

// notFoundIsEmpty turns a missing subject into an empty result, which is what
// the original did: viewing a clan that does not exist answers [] rather than
// an error.
func notFoundIsEmpty(v any, err error) (any, error) {
	if errors.Is(err, store.ErrNotFound) {
		return []any{}, nil
	}
	return v, err
}
