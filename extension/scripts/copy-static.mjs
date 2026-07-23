// Copies the Manifest V3 assets (manifest.json + icons/) that Vite doesn't
// know about from chrome/ into dist/, so `dist/` is a complete, loadable
// unpacked extension on its own (see docs/architecture.md §10).
import { mkdirSync, copyFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const chromeDir = join(root, "chrome");
const distDir = join(root, "dist");

mkdirSync(distDir, { recursive: true });
copyFileSync(join(chromeDir, "manifest.json"), join(distDir, "manifest.json"));

const iconsSrc = join(chromeDir, "icons");
const iconsDest = join(distDir, "icons");
mkdirSync(iconsDest, { recursive: true });
for (const entry of readdirSync(iconsSrc)) {
  if (entry.startsWith(".")) continue;
  copyFileSync(join(iconsSrc, entry), join(iconsDest, entry));
}

console.log("Copied manifest.json and icons/ into dist/");
