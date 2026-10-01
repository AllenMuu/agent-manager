import { defineConfig } from 'vitest/config'
import { fileURLToPath } from 'node:url'
import react from '@vitejs/plugin-react'

export default defineConfig({
  root: fileURLToPath(new URL('./install/', import.meta.url)),
  base: './',
  plugins: [react()],
  build: {
    outDir: fileURLToPath(new URL('../internal/webui/dist/', import.meta.url)),
    emptyOutDir: true,
  },
  test: {
    environment: 'node',
  },
})
