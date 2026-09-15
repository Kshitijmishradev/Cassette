import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { readFileSync } from 'node:fs'

export default defineConfig(({ mode }) => ({
  base: './',
  plugins: [react(), {
    name: 'build-entry',
    transformIndexHtml: {
      order: 'pre',
      handler(html) {
        return mode === 'site'
          ? html.replace('/src/main.jsx', '/src/site-main.jsx')
          : html.replace(/<title>.*?<\/title>/, '<title>Cassette — Local trace viewer</title>').replace(/<meta name="description"[^>]*>/, '<meta name="description" content="Inspect your local Cassette agent recordings and replay results." />')
      },
    },
  }, {
    name: 'security-headers',
    generateBundle() {
      this.emitFile({ type: 'asset', fileName: '_headers', source: readFileSync(new URL('./security-headers', import.meta.url), 'utf8') })
    },
  }],
  publicDir: 'fixtures',
  build: {
    copyPublicDir: false,
    outDir: mode === 'site' ? 'site-dist' : 'dist',
  },
}))
