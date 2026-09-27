import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: './',
  build: {
    outDir: '../internal/dashboard/dist',
    emptyOutDir: true,
  },
  server: {
    port: 1420,
    proxy: {
      '/api': 'http://127.0.0.1:8766',
    },
  },
})
