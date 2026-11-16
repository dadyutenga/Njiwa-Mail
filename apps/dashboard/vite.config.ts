import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// API target of the dev-server proxy; override with VITE_API_PROXY=http://127.0.0.1:8090
const api = process.env.VITE_API_PROXY ?? "http://127.0.0.1:8080";

export default defineConfig({
  server: {
    // Bind IPv4 explicitly. On this machine Vite otherwise listens only on
    // [::1], and a browser or preview that uses 127.0.0.1 gets a bad gateway.
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy: {
      "/admin": { target: api, changeOrigin: true },
      "/v1/unsubscribe": { target: api, changeOrigin: true },
    },
  },
  resolve: { tsconfigPaths: true },
  build: {
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            { name: "react-vendor", test: /node_modules\/(react|react-dom|scheduler)\// },
            { name: "radix-vendor", test: /node_modules\/@radix-ui\// },
          ],
        },
      },
    },
  },
  plugins: [react(), tailwindcss()],
});
