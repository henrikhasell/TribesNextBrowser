// Package apidoc carries the API specification as a file compiled into the
// binary.
//
// Beside the YAML rather than under internal/ for the reason migrations/ and
// web/ give: go:embed cannot reach outside its own package directory, and a
// specification is worth being able to read with an editor rather than only
// through a server.
//
// Hand-written, not generated. The alternative is annotations above every
// handler and a code generator in the build, which buys accuracy this codebase
// gets another way: internal/api has a test that walks the paths here against
// the routes actually registered and fails when either side has an endpoint the
// other does not. A specification is only worth having if something notices
// when it starts lying.
package apidoc

import _ "embed"

//go:embed openapi.yaml
var Spec []byte
