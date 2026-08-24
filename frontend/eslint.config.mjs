import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
    // Monaco's own minified build, copied in by scripts/sync-monaco.mjs. It is
    // 24 MB of somebody else's output and linting it takes longer than
    // everything this repo actually wrote.
    "public/monaco/**",
  ]),
]);

export default eslintConfig;
