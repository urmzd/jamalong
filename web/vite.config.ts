import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// In development the Go server runs on :8080 and Vite proxies the API to it,
// WebSocket included.
export default defineConfig({
  plugins: [react()],
  // The content security policy allows fonts from this origin only, so no
  // asset may be inlined as a data: URL.
  build: { assetsInlineLimit: 0 },
  server: {
    proxy: {
      "/api": { target: "http://127.0.0.1:8080", ws: true },
    },
  },
});
