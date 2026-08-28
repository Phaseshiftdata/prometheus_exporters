import { defineConfig } from "vite";
import { readFileSync } from "fs";
import { resolve, dirname } from "path";
import { fileURLToPath } from "url";
import istanbul from "vite-plugin-istanbul";

const __vite_dirname = dirname(fileURLToPath(import.meta.url));
const version = readFileSync(resolve(__vite_dirname, "../VERSION"), "utf-8").trim();

export default defineConfig({
  define: {
    __APP_VERSION__: JSON.stringify(version),
  },
  build: {
    outDir: "dist",
    sourcemap: !!process.env.INSTRUMENT_COVERAGE,
  },
  plugins: [
    ...(process.env.INSTRUMENT_COVERAGE
      ? [
          istanbul({
            include: "src/**/*.ts",
            exclude: ["node_modules"],
            extension: [".ts"],
            requireEnv: false,
            forceBuildInstrument: true,
          }),
        ]
      : []),
  ],
});
