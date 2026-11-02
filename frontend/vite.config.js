import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In dev the browser calls /api/... and Vite forwards it to the Go API, so CORS is not needed.
// In production put the web app and the API behind one domain (Caddy) with the same /api prefix.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: process.env.API_URL || 'http://localhost:8080',
        changeOrigin: true,
        rewrite: (p) => p.replace(/^\/api/, ''),
      },
    },
  },
})
