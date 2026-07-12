import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const apiTarget = process.env.WINDFORCE_LITE_API_PROXY_TARGET || "http://127.0.0.1:18091";

export default defineConfig({
  base: "/ui/",
  plugins: [react()],
  server: {
    proxy: {
      "/api": { target: apiTarget, changeOrigin: true },
      "/healthz": { target: apiTarget, changeOrigin: true },
      "/readyz": { target: apiTarget, changeOrigin: true },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
