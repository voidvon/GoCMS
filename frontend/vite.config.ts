import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'
import { fileURLToPath, URL } from 'node:url'

// https://vite.dev/config/
export default defineConfig({
  base: "/admin/",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    proxy: {
      '/images': { target: 'http://127.0.0.1:18080', changeOrigin: true },
      '/css': { target: 'http://127.0.0.1:18080', changeOrigin: true },
      '/js': { target: 'http://127.0.0.1:18080', changeOrigin: true },
      '/skin': { target: 'http://127.0.0.1:18080', changeOrigin: true },
      '/api': {
        target: 'http://127.0.0.1:18080',
        changeOrigin: true,
      },
    },
  },
})
