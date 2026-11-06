import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"
import { defineConfig } from "vite"
import { fileURLToPath, URL } from "node:url"

export default defineConfig({
  base: "/shop/",
  plugins: [react(), tailwindcss()],
  resolve: { dedupe: ["react", "react-dom", "@base-ui/react", "next-themes", "sonner"], alias: [
    { find: "@/components/ui", replacement: fileURLToPath(new URL("../frontend/src/components/ui", import.meta.url)) },
    { find: "@", replacement: fileURLToPath(new URL("./src", import.meta.url)) },
  ] },
  server: {
    host: "127.0.0.1", port: 1245, strictPort: true,
    hmr: { clientPort: 1245 },
    fs: { allow: [fileURLToPath(new URL("..", import.meta.url))] },
    proxy: {
      "^/(?:api|assets|images|css|js|skin)(?:[/?]|$)": {
        target: "http://127.0.0.1:18080", changeOrigin: false,
        configure(proxy) {
          proxy.on("proxyReq", (outgoing, request) => {
            const site = request.headers.cookie?.match(/(?:^|;\s*)gocms_dev_site_id=([1-9]\d*)(?:;|$)/)?.[1]
            if (site && !request.headers["x-site-id"]) outgoing.setHeader("X-Site-Id", site)
          })
        },
      },
    },
  },
})
