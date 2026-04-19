import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      // Use '/api/' so '/apis/...' (source folder) is not mistaken for API routes.
      '/api/': 'http://backend:8080',
    },
  }
})
