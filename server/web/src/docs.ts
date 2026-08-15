// Swagger UI over the server's own specification.
//
// Bundled rather than loaded from a CDN: the whole site is served out of the Go
// binary and works on a LAN with no internet, which a script tag pointing at
// unpkg would quietly undo.
//
// The spec is fetched from the running server rather than imported at build
// time, so the page always describes the server that served it -- an
// out-of-date binary cannot show an up-to-date spec.

import SwaggerUI from 'swagger-ui-dist/swagger-ui-es-bundle.js'
import 'swagger-ui-dist/swagger-ui.css'
import './docs.css'

SwaggerUI({
  url: '/api/openapi.yaml',
  dom_id: '#swagger',
  // The game routes want a session token, which nobody has in a browser, and
  // "Try it out" against a live community server would write real rows. The
  // page is for reading.
  supportedSubmitMethods: [],
  docExpansion: 'list',
  defaultModelsExpandDepth: 1,
  deepLinking: true,
})
