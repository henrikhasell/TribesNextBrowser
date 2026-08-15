import { defineConfig } from 'vite'
import type { Plugin } from 'vite'
import react from '@vitejs/plugin-react'

/**
 * Emit dist/.gitkeep as part of every build.
 *
 * That file is what makes `//go:embed all:dist` compile on a checkout where
 * nothing has run npm -- see embed.go. Vite empties the output directory before
 * each build, which would delete it and leave git reporting a deleted file
 * after every single build. Emitting it as an asset costs nothing and means a
 * build never dirties the working tree.
 */
function keepEmbedPlaceholder(): Plugin {
  return {
    name: 'tnb-keep-embed-placeholder',
    generateBundle() {
      this.emitFile({ type: 'asset', fileName: '.gitkeep', source: '' })
    },
  }
}

// The build output is embedded into the Go binary (see embed.go), so two things
// are load-bearing here.
//
// outDir must stay `dist`: that is the directory the go:embed pattern names.
//
// Assets must stay under `assets/`, because internal/api/site.go serves that
// one prefix with a year-long immutable cache and everything else without one.
// Vite fingerprints these filenames, which is what makes that promise safe.
export default defineConfig({
  plugins: [react(), keepEmbedPlaceholder()],
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    // The whole site is a handful of components. Splitting it would cost more
    // round trips than it saves bytes.
    chunkSizeWarningLimit: 900,
  },
  server: {
    // `npm run dev` serves the UI and forwards the data to a server started
    // with `go run ./cmd/tnserver`, so the two can be worked on at once.
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
})
