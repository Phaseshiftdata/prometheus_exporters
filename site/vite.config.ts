import { defineConfig } from "vite";
import { readFileSync } from "fs";
import { resolve } from "path";

const version = readFileSync(resolve(__dirname, "../VERSION"), "utf-8").trim();

export default defineConfig({
  define: {
    __APP_VERSION__: JSON.stringify(version),
  },
  build: {
    outDir: "dist",
  },
});
