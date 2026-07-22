import { defineConfig } from "vite";
import { resolve } from "path";

// Builds the background service worker as a single ES module with no
// code-splitting, since Manifest V3 requires a plain JS file for
// background.service_worker (see docs/architecture.md §10).
export default defineConfig({
  build: {
    outDir: resolve(__dirname, "dist"),
    emptyOutDir: false,
    lib: {
      entry: resolve(__dirname, "src/background/service-worker.ts"),
      formats: ["es"],
      fileName: () => "background/service-worker.js",
    },
    rollupOptions: {
      output: {
        inlineDynamicImports: true,
      },
    },
  },
});
