// swagger-ui-dist ships no types for its ES bundle.
//
// The published @types package describes the React component API rather than
// this entry point, so a one-line declaration is both smaller and more honest
// than pulling in types for something else.
declare module 'swagger-ui-dist/swagger-ui-es-bundle.js' {
  interface SwaggerUIOptions {
    url?: string
    dom_id?: string
    supportedSubmitMethods?: string[]
    docExpansion?: 'list' | 'full' | 'none'
    defaultModelsExpandDepth?: number
    deepLinking?: boolean
  }
  export default function SwaggerUI(options: SwaggerUIOptions): unknown
}
