import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'

export default defineConfig({
  plugins: [react()],
  base: '/',
  build: {
    outDir: fileURLToPath(new URL('../internal/webconsole/ui/', import.meta.url)),
    emptyOutDir: true,
    assetsDir: 'assets',
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
    css: true,
  },
})
