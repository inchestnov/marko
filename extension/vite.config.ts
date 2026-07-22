import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { resolve } from "path";

// Multi-page build for the popup and options UIs. The background service
// worker is built separately via vite.config.background.ts because
// Manifest V3 service workers must be a single plain-JS entry with no
// code-splitting (see docs/architecture.md §10).
export default defineConfig({
  root: resolve(__dirname, "src"),
  plugins: [react()],
  build: {
    outDir: resolve(__dirname, "dist"),
    emptyOutDir: false,
    rollupOptions: {
      input: {
        popup: resolve(__dirname, "src/popup/index.html"),
        options: resolve(__dirname, "src/options/index.html"),
      },
    },
  },
});
