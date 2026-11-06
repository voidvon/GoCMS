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
    port: 1244,
    proxy: {
      // Keep the admin app and Vite's module/HMR endpoints on Vite. Forward
      // public pages, assets and APIs before Vite checks its /admin/ base.
      '^/(?!admin(?:[/?]|$)|@|__|src(?:[/?]|$)|node_modules(?:[/?]|$))': {
        target: 'http://127.0.0.1:18080',
        // Keep the browser-facing Host aligned with Origin for CSRF checks.
        changeOrigin: false,
        configure(proxy) {
          proxy.on('proxyReq', (proxyRequest, request) => {
            // Native links cannot send the admin's X-Site-Id header. In local
            // development, carry the selected site to pages and their APIs.
            const site = request.headers.cookie?.match(/(?:^|;\s*)gocms_dev_site_id=([1-9]\d*)(?:;|$)/)?.[1]
            if (site && !request.headers['x-site-id']) proxyRequest.setHeader('X-Site-Id', site)
          })
        },
      },
    },
  },
})
