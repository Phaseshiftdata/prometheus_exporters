import { defineConfig } from "vitest/config";
import { readFileSync } from "fs";
import { resolve, dirname } from "path";
import { fileURLToPath } from "url";

const __vitest_dirname = dirname(fileURLToPath(import.meta.url));
const version = readFileSync(resolve(__vitest_dirname, "../VERSION"), "utf-8").trim();

export default defineConfig({
  define: {
    __APP_VERSION__: JSON.stringify(version),
  },
  test: {
    environment: "jsdom",
    coverage: {
      provider: "istanbul",
      reporter: ["text", "lcov"],
      reportsDirectory: "coverage/vitest",
      include: ["src/**/*.ts"],
      exclude: ["src/**/*.d.ts"],
    },
    include: ["src/**/*.test.ts"],
  },
});
