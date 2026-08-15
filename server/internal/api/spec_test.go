package api

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/henrik/tnbrowser-server/apidoc"
)

// The specification is hand-written, which is only defensible if something
// notices when it stops matching the server.
//
// This walks the paths declared in openapi.yaml against the routes Routes()
// registers and fails when either side has an endpoint the other does not. It
// is deliberately dumb about YAML -- a top-level key under `paths:` is a line
// with two spaces of indent ending in a colon -- because parsing YAML properly
// would mean a dependency, and this only has to read a file that lives in the
// same repository and is checked by this test.

// specPaths reads the keys under `paths:`.
func specPaths(t *testing.T) map[string]bool {
	t.Helper()

	out := map[string]bool{}
	key := regexp.MustCompile(`^  (/\S*):\s*$`)

	inPaths := false
	for _, line := range strings.Split(string(apidoc.Spec), "\n") {
		if strings.HasPrefix(line, "paths:") {
			inPaths = true
			continue
		}
		// A new top-level key ends the section.
		if inPaths && len(line) > 0 && line[0] != ' ' && line[0] != '#' {
			break
		}
		if m := key.FindStringSubmatch(line); inPaths && m != nil {
			out[m[1]] = true
		}
	}

	if len(out) == 0 {
		t.Fatal("no paths found in the specification; the reader below is broken")
	}
	return out
}

// routePaths reads the patterns Routes() registers, minus the ones a
// specification has no business describing.
func routePaths(t *testing.T) map[string]bool {
	t.Helper()

	srv := &Server{}

	out := map[string]bool{}
	for _, r := range srv.routes() {
		p := r.pattern
		// Strip the method and any wildcard braces: the spec spells a path
		// parameter {guid}, and so does the mux, but the spec has no method in
		// the key.
		if i := strings.IndexByte(p, ' '); i >= 0 {
			p = p[i+1:]
		}
		switch {
		case p == "/":
			continue // the app shell, not an API
		case p == "/api/":
			continue // the catch-all for mistyped endpoints
		case p == "/api/openapi.yaml":
			continue // the specification cannot usefully describe itself
		}
		out[p] = true
	}
	return out
}

func TestSpecificationMatchesTheRoutes(t *testing.T) {
	spec := specPaths(t)
	routes := routePaths(t)

	var undocumented, imaginary []string
	for p := range routes {
		if !spec[p] {
			undocumented = append(undocumented, p)
		}
	}
	for p := range spec {
		if !routes[p] {
			imaginary = append(imaginary, p)
		}
	}
	sort.Strings(undocumented)
	sort.Strings(imaginary)

	if len(undocumented) > 0 {
		t.Errorf("served but not in openapi.yaml: %v", undocumented)
	}
	if len(imaginary) > 0 {
		t.Errorf("in openapi.yaml but not served: %v", imaginary)
	}
}

// Every failure slug the server can emit has to be in the enum a caller
// branches on, or the enum is decoration.
func TestEveryErrorSlugIsDocumented(t *testing.T) {
	// The slugs fault() and siteError() are called with, gathered by hand
	// because Go cannot enumerate string literals. A new one that is not added
	// here and to the spec is caught by review rather than by this test -- what
	// this catches is the spec drifting away from the list.
	emitted := []string{
		"bad_request", "session_expired", "bad_certificate", "bad_response",
		"client_too_old", "not_configured", "not_found", "internal",
	}

	spec := string(apidoc.Spec)
	for _, slug := range emitted {
		if !strings.Contains(spec, slug) {
			t.Errorf("slug %q is emitted but not in the specification", slug)
		}
	}
}
